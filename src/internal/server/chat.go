package server

import (
	"errors"
	"fmt"
	"strings"
	"time"

	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
	"github.com/Fomiller/fantasy-rpg/src/internal/chat"
	"github.com/Fomiller/fantasy-rpg/src/internal/sim"
)

const whisperHandoff = time.Second

var channelNames = map[string]string{
	chat.General: "General",
	chat.Trade:   "Trade",
	chat.LFG:     "LookingForGroup",
}

func (s *Lobby) members() map[sim.EntityID]*client {
	if s.roster != nil {
		return s.roster()
	}
	return s.inTown
}

func (s *Lobby) StartChat() error {
	for _, ch := range chat.Shared {
		stop, err := s.Chat.Subscribe(ch, func(m *gamev1.ChatMessage) {
			s.enqueue(func() { s.channelMessage(ch, m) })
		})
		if err != nil {
			return fmt.Errorf("subscribe to %s: %w", ch, err)
		}
		s.chatStops = append(s.chatStops, stop)
	}
	return nil
}

func (s *Lobby) chatEnter(c *client) {
	stop, err := s.Chat.Listen(c.name, func(m *gamev1.ChatMessage) bool {
		got := make(chan bool, 1)
		if !s.enqueue(func() { got <- s.deliverWhisper(c, m) }) {
			return false
		}
		select {
		case ok := <-got:
			return ok
		case <-time.After(whisperHandoff):
			return false
		case <-s.done:
			return false
		}
	})
	if err != nil {
		s.log.Warn("listen for whispers", "name", c.name, "err", err)
	} else {
		c.unlisten = stop
	}
	s.sendChannels(c)
}

func (s *Lobby) chatLeave(c *client) {
	if c.unlisten != nil {
		c.unlisten()
		c.unlisten = nil
	}
}

func (s *Lobby) sendChannels(c *client) {
	s.sendToClient(c, &gamev1.ServerMessage{Payload: &gamev1.ServerMessage_ChatChannels{
		ChatChannels: &gamev1.ChatChannels{Joined: chat.Joined(c.chatOff)},
	}})
}

func (s *Lobby) sendChat(c *client, channel, sender, text string) {
	s.sendToClient(c, &gamev1.ServerMessage{Payload: &gamev1.ServerMessage_Chat{
		Chat: &gamev1.ChatMessage{Channel: channel, Sender: sender, Text: text},
	}})
}

func (s *Lobby) chatSystem(c *client, text string) {
	s.sendChat(c, chat.System, "", text)
}

func (s *Lobby) chatDrop(c *client, reason, text string) {
	s.ChatMetrics.Drops.WithLabelValues(reason).Inc()
	if text != "" {
		s.chatSystem(c, text)
	}
}

func (s *Lobby) chatSend(c *client, line string) {
	cmd := chat.Parse(line)
	switch cmd.Kind {
	case chat.Empty:
		s.chatDrop(c, chat.DropEmpty, "")
		return
	case chat.Usage:
		s.chatDrop(c, chat.DropBadCommand, cmd.Text)
		return
	case chat.Unknown:
		s.chatDrop(c, chat.DropBadCommand, "Unknown command: "+cmd.Text)
		return
	case chat.Join, chat.Leave:
		s.setMembership(c, cmd)
		return
	}
	if !c.chatLimit.Allow(time.Now()) {
		s.chatDrop(c, chat.DropRateLimited, "You are chatting too fast.")
		return
	}
	switch cmd.Kind {
	case chat.Speak:
		s.speak(c, cmd.Channel, cmd.Text)
	case chat.Whisper:
		s.whisper(c, cmd.Target, cmd.Text)
	case chat.Reply:
		if c.replyTo == "" {
			s.chatDrop(c, chat.DropNoReply, "Nobody has whispered you.")
			return
		}
		s.whisper(c, c.replyTo, cmd.Text)
	}
}

func (s *Lobby) setMembership(c *client, cmd chat.Command) {
	name := channelNames[cmd.Channel]
	if cmd.Kind == chat.Join {
		delete(c.chatOff, cmd.Channel)
		s.chatSystem(c, "You joined "+name+".")
	} else {
		if c.chatOff == nil {
			c.chatOff = map[string]bool{}
		}
		c.chatOff[cmd.Channel] = true
		s.chatSystem(c, "You left "+name+".")
	}
	s.sendChannels(c)
}

func (s *Lobby) speak(c *client, channel, text string) {
	switch channel {
	case chat.Say, chat.Yell:
		s.local(c, channel, text)
	default:
		s.shared(c, channel, text)
	}
}

func (s *Lobby) local(c *client, channel, text string) {
	from, ok := s.rules.Player(c.id)
	if !ok {
		return
	}
	s.ChatMetrics.Messages.WithLabelValues(channel).Inc()
	for id, to := range s.members() {
		if channel == chat.Say {
			p, ok := s.rules.Player(id)
			if !ok || p.Pos.DistanceTo(from.Pos) > chat.SayRange {
				continue
			}
		}
		s.sendChat(to, channel, c.name, text)
	}
}

func (s *Lobby) shared(c *client, channel, text string) {
	if c.chatOff[channel] {
		s.chatDrop(c, chat.DropNotJoined, fmt.Sprintf("You are not in %s. Type /join %s.", channelNames[channel], channel))
		return
	}
	err := s.Chat.Publish(channel, &gamev1.ChatMessage{Channel: channel, Sender: c.name, Text: text})
	if err != nil {
		s.log.Warn("publish chat", "channel", channel, "err", err)
		s.chatDrop(c, chat.DropUnavailable, "Chat is unavailable right now.")
		return
	}
	s.ChatMetrics.Messages.WithLabelValues(channel).Inc()
}

// The channel comes from the subject, not from the payload.
func (s *Lobby) channelMessage(channel string, m *gamev1.ChatMessage) {
	for _, c := range s.members() {
		if !c.chatOff[channel] {
			s.sendChat(c, channel, m.GetSender(), m.GetText())
		}
	}
}

func (s *Lobby) whisper(c *client, to, text string) {
	if strings.EqualFold(to, c.name) {
		s.chatDrop(c, chat.DropBadCommand, "You cannot whisper yourself.")
		return
	}
	msg := &gamev1.ChatMessage{Channel: chat.WhisperFrom, Sender: c.name, Text: text}
	if local := s.findMember(to); local != nil {
		if s.deliverWhisper(local, msg) {
			s.whispered(c, to, text)
			return
		}
	}
	go func() {
		err := s.Chat.Whisper(c.ctx, to, msg)
		s.enqueue(func() {
			switch {
			case err == nil:
				s.whispered(c, to, text)
			case errors.Is(err, chat.ErrNotOnline):
				s.chatDrop(c, chat.DropNotOnline, to+" is not online.")
			default:
				s.log.Warn("whisper", "to", to, "err", err)
				s.chatDrop(c, chat.DropUnavailable, "Chat is unavailable right now.")
			}
		})
	}()
}

func (s *Lobby) whispered(c *client, to, text string) {
	s.ChatMetrics.Messages.WithLabelValues("whisper").Inc()
	s.sendChat(c, chat.WhisperTo, to, text)
}

func (s *Lobby) findMember(name string) *client {
	for _, c := range s.members() {
		if strings.EqualFold(c.name, name) {
			return c
		}
	}
	return nil
}

func (s *Lobby) deliverWhisper(c *client, m *gamev1.ChatMessage) bool {
	if c.gone || s.members()[c.id] != c {
		return false
	}
	c.replyTo = m.GetSender()
	s.sendChat(c, chat.WhisperFrom, m.GetSender(), m.GetText())
	return true
}
