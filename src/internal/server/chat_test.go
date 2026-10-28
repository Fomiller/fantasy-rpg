package server

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	natsserver "github.com/nats-io/nats-server/v2/test"
	"github.com/prometheus/client_golang/prometheus/testutil"

	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
	"github.com/Fomiller/fantasy-rpg/src/internal/caves"
	"github.com/Fomiller/fantasy-rpg/src/internal/chat"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/protocol"
	"github.com/Fomiller/fantasy-rpg/src/internal/sim"
)

// A town wide enough that the two spawns are out of earshot.
func wideTown() caves.Cave {
	row := func(s string) string { return s + strings.Repeat(".", 40-len(s)-1) + "#" }
	art := "\n" + strings.Repeat("#", 40) + "\n#D" + strings.Repeat("#", 38) + "\n" +
		"#S.T" + strings.Repeat(".", 34) + "S#\n" +
		row("#R") + "\n" +
		strings.Repeat("#", 40) + "\n"
	c, err := caves.Parse("wide town", art)
	if err != nil {
		panic(err)
	}
	return c
}

// serveChat runs the world server and one instance server on separate
// connections to one NATS, as the cluster does.
func serveChat(t *testing.T, town caves.Cave) harness {
	t.Helper()
	nats := natsserver.RunRandClientPortServer()
	t.Cleanup(nats.Shutdown)
	dial := func() chat.Bus {
		bus, err := chat.Dial(context.Background(), nats.ClientURL())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(bus.Close)
		return bus
	}
	worldBus, caveBus := dial(), dial()
	return serveBoth(t, testCave(), town, calm(sim.DefaultConfig()), inventory.NewMemory(), func(l *Lobby) {
		l.Chat = worldBus
		if err := l.StartChat(); err != nil {
			t.Fatal(err)
		}
	}, func(s *Server) {
		s.Chat = caveBus
		if err := s.StartChat(); err != nil {
			t.Fatal(err)
		}
	})
}

// joinTownChat joins the town like a client, saying which channels it left,
// and returns the channels the server says it is in.
func joinTownChat(t *testing.T, url, name string, left ...string) (*websocket.Conn, []string) {
	t.Helper()
	conn := dial(t, url)
	send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_Join{
		Join: &gamev1.JoinRequest{Name: name, ChatLeft: left},
	}})
	joined := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetJoined() != nil }).GetJoined()
	townIDs.Store(conn, joined.GetPlayerId())
	channels := waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetChatChannels() != nil }).GetChatChannels().GetJoined()
	return conn, channels
}

func line(t *testing.T, conn *websocket.Conn, text string) {
	t.Helper()
	send(t, conn, &gamev1.ClientMessage{ProtocolVersion: protocol.Version, Payload: &gamev1.ClientMessage_Chat{
		Chat: &gamev1.ChatSend{Text: text},
	}})
}

// hear collects the chat lines that arrive up to and including the one with
// text, which a test sends last so it knows nothing is still coming.
func hear(t *testing.T, conn *websocket.Conn, last string) []*gamev1.ChatMessage {
	t.Helper()
	var got []*gamev1.ChatMessage
	waitFor(t, conn, func(m *gamev1.ServerMessage) bool {
		if c := m.GetChat(); c != nil {
			got = append(got, c)
			return c.GetText() == last
		}
		return false
	})
	return got
}

func hearOne(t *testing.T, conn *websocket.Conn) *gamev1.ChatMessage {
	t.Helper()
	return waitFor(t, conn, func(m *gamev1.ServerMessage) bool { return m.GetChat() != nil }).GetChat()
}

func want(t *testing.T, got *gamev1.ChatMessage, channel, sender, text string) {
	t.Helper()
	if got.GetChannel() != channel || got.GetSender() != sender || got.GetText() != text {
		t.Fatalf("chat = %v, want %s from %q: %q", got, channel, sender, text)
	}
}

func texts(msgs []*gamev1.ChatMessage) []string {
	var out []string
	for _, m := range msgs {
		out = append(out, m.GetChannel()+":"+m.GetText())
	}
	return out
}

func TestSayCarriesNearYellCarriesFar(t *testing.T) {
	h := serveBoth(t, testCave(), wideTown(), calm(sim.DefaultConfig()), inventory.NewMemory(), nil)
	ann, _ := joinTownChat(t, h.url, "ann")
	bob, _ := joinTownChat(t, h.url, "bob")
	cat, _ := joinTownChat(t, h.url, "cat")

	line(t, ann, "hello")
	line(t, ann, "/y loud")
	if got := hear(t, ann, "loud"); !slices.Equal(texts(got), []string{"say:hello", "yell:loud"}) {
		t.Fatalf("ann heard %v, want her own say and yell", texts(got))
	}
	// Cat spawned on ann's side of the town, and bob across it.
	if got := hear(t, cat, "loud"); !slices.Equal(texts(got), []string{"say:hello", "yell:loud"}) {
		t.Fatalf("cat heard %v, want the say and the yell", texts(got))
	}
	got := hear(t, bob, "loud")
	if !slices.Equal(texts(got), []string{"yell:loud"}) {
		t.Fatalf("bob heard %v, want only the yell", texts(got))
	}
	want(t, got[0], "yell", "ann", "loud")
}

func TestSenderComesFromTheConnection(t *testing.T) {
	h := serveBoth(t, testCave(), testTown(), calm(sim.DefaultConfig()), inventory.NewMemory(), nil)
	ann, _ := joinTownChat(t, h.url, "ann")
	bob, _ := joinTownChat(t, h.url, "bob")
	line(t, ann, "/y I am bob")
	want(t, hearOne(t, bob), "yell", "ann", "I am bob")
}

func TestLinesAreCleaned(t *testing.T) {
	h := serveBoth(t, testCave(), testTown(), calm(sim.DefaultConfig()), inventory.NewMemory(), nil)
	ann, _ := joinTownChat(t, h.url, "ann")
	line(t, ann, "")
	line(t, ann, "/y \n\t\x00")
	line(t, ann, "/y a\x00b\nc")
	line(t, ann, "/y "+strings.Repeat("z", 400))
	line(t, ann, "/y end")
	got := hear(t, ann, "end")
	if len(got) != 3 || got[0].GetText() != "abc" || got[1].GetText() != strings.Repeat("z", chat.MaxLen) {
		t.Fatalf("ann heard %v, want the clean line, the cut line and the end", texts(got))
	}
	if n := testutil.ToFloat64(h.world.ChatMetrics.Drops.WithLabelValues(chat.DropEmpty)); n != 2 {
		t.Fatalf("empty drops = %v, want 2", n)
	}
}

func TestChatRateLimitIsASystemMessage(t *testing.T) {
	h := serveBoth(t, testCave(), testTown(), calm(sim.DefaultConfig()), inventory.NewMemory(), nil)
	ann, _ := joinTownChat(t, h.url, "ann")
	for i := 0; i < chat.Burst+3; i++ {
		line(t, ann, "/y spam")
	}
	var yells, limited int
	waitFor(t, ann, func(m *gamev1.ServerMessage) bool {
		switch c := m.GetChat(); {
		case c.GetChannel() == chat.Yell:
			yells++
		case c.GetChannel() == chat.System && c.GetText() == "You are chatting too fast.":
			limited++
		}
		return yells+limited == chat.Burst+3
	})
	if yells != chat.Burst || limited != 3 {
		t.Fatalf("%d yells and %d warnings, want %d and 3", yells, limited, chat.Burst)
	}
	if n := testutil.ToFloat64(h.world.ChatMetrics.Drops.WithLabelValues(chat.DropRateLimited)); n != 3 {
		t.Fatalf("rate limit drops = %v, want 3", n)
	}
	if n := testutil.ToFloat64(h.world.ChatMetrics.Messages.WithLabelValues(chat.Yell)); n != chat.Burst {
		t.Fatalf("yell count = %v, want %d", n, chat.Burst)
	}
	// Still connected, and joins and leaves are not chat.
	line(t, ann, "/leave trade")
	want(t, hearOne(t, ann), chat.System, "", "You left Trade.")
}

func TestSharedChannelsReachTownAndCave(t *testing.T) {
	h := serveChat(t, testTown())
	ann, _ := joinTownChat(t, h.url, "ann")
	bobWorld := dial(t, h.url)
	lobbyAs(t, bobWorld, "bob")
	bob, _ := enterInstance(t, bobWorld)
	waitFor(t, bob, func(m *gamev1.ServerMessage) bool { return m.GetChatChannels() != nil })

	line(t, ann, "/1 anyone there")
	want(t, hearOne(t, ann), chat.General, "ann", "anyone there")
	want(t, hearOne(t, bob), chat.General, "ann", "anyone there")

	line(t, bob, "/3 need a tank")
	want(t, hearOne(t, ann), chat.LFG, "bob", "need a tank")
	want(t, hearOne(t, bob), chat.LFG, "bob", "need a tank")

	line(t, ann, "/leave 2")
	waitFor(t, ann, func(m *gamev1.ServerMessage) bool { return m.GetChatChannels() != nil })
	line(t, ann, "/2 wts sword")
	want(t, hearOne(t, ann), chat.System, "", "You are not in Trade. Type /join trade.")
	line(t, bob, "/2 wtb sword")
	want(t, hearOne(t, bob), chat.Trade, "bob", "wtb sword")
	line(t, ann, "/join trade")
	line(t, bob, "/2 wtb helm")
	if got := hear(t, ann, "wtb helm"); got[len(got)-1].GetChannel() != chat.Trade || slices.ContainsFunc(got, func(m *gamev1.ChatMessage) bool { return m.GetText() == "wtb sword" }) {
		t.Fatalf("ann heard %v, want the helm and not the sword she left before", texts(got))
	}
	if n := testutil.ToFloat64(h.world.ChatMetrics.Messages.WithLabelValues(chat.General)); n != 1 {
		t.Fatalf("world general count = %v, want 1", n)
	}
	if n := testutil.ToFloat64(h.srv.ChatMetrics.Messages.WithLabelValues(chat.LFG)); n != 1 {
		t.Fatalf("cave lfg count = %v, want 1", n)
	}
}

func TestCaveSayAndYellStayInTheCave(t *testing.T) {
	h := serveChat(t, testTown())
	ann, _ := joinTownChat(t, h.url, "ann")
	bobWorld := dial(t, h.url)
	lobbyAs(t, bobWorld, "bob")
	bob, _ := enterInstance(t, bobWorld)
	catWorld := dial(t, h.url)
	lobbyAs(t, catWorld, "cat")
	cat, _ := enterInstance(t, catWorld)

	line(t, bob, "psst")
	line(t, bob, "/y hey")
	line(t, ann, "/y town only")
	if got := hear(t, cat, "hey"); !slices.Equal(texts(got), []string{"say:psst", "yell:hey"}) {
		t.Fatalf("cat heard %v in the cave", texts(got))
	}
	if got := hear(t, ann, "town only"); !slices.Equal(texts(got), []string{"yell:town only"}) {
		t.Fatalf("ann heard %v in the town, want only her own yell", texts(got))
	}
}

func TestWhisperAcrossTownAndCave(t *testing.T) {
	h := serveChat(t, testTown())
	ann, _ := joinTownChat(t, h.url, "ann")
	bobWorld := dial(t, h.url)
	lobbyAs(t, bobWorld, "bob")
	bob, _ := enterInstance(t, bobWorld)
	waitFor(t, bob, func(m *gamev1.ServerMessage) bool { return m.GetChatChannels() != nil })

	line(t, ann, "/r anyone")
	want(t, hearOne(t, ann), chat.System, "", "Nobody has whispered you.")

	line(t, ann, "/w BOB the secret is out")
	want(t, hearOne(t, bob), chat.WhisperFrom, "ann", "the secret is out")
	want(t, hearOne(t, ann), chat.WhisperTo, "BOB", "the secret is out")

	line(t, bob, "/r thanks")
	want(t, hearOne(t, ann), chat.WhisperFrom, "bob", "thanks")
	want(t, hearOne(t, bob), chat.WhisperTo, "ann", "thanks")

	line(t, ann, "/w nobody hello")
	want(t, hearOne(t, ann), chat.System, "", "nobody is not online.")
	line(t, ann, "/w ann hello")
	want(t, hearOne(t, ann), chat.System, "", "You cannot whisper yourself.")
	line(t, ann, "/w bob")
	want(t, hearOne(t, ann), chat.System, "", "Usage: /w <name> <message>")
	line(t, ann, "/dance")
	want(t, hearOne(t, ann), chat.System, "", "Unknown command: /dance")
	if n := testutil.ToFloat64(h.world.ChatMetrics.Drops.WithLabelValues(chat.DropNotOnline)); n != 1 {
		t.Fatalf("not online drops = %v, want 1", n)
	}

	// Leaving the cave takes the name off NATS.
	bob.Close(websocket.StatusNormalClosure, "")
	deadline := time.Now().Add(2 * time.Second)
	for {
		line(t, ann, "/w bob are you there")
		got := hearOne(t, ann)
		if got.GetText() == "bob is not online." {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("whispers still reach a player who left the cave")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestWhisperBetweenTwoTownShards(t *testing.T) {
	nats := natsserver.RunRandClientPortServer()
	t.Cleanup(nats.Shutdown)
	shard := func(id string) harness {
		bus, err := chat.Dial(context.Background(), nats.ClientURL())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(bus.Close)
		return serveBoth(t, testCave(), testTown(), calm(sim.DefaultConfig()), inventory.NewMemory(), func(l *Lobby) {
			l.WorldID = id
			l.Chat = bus
			if err := l.StartChat(); err != nil {
				t.Fatal(err)
			}
		})
	}
	one, two := shard("world-0"), shard("world-1")
	ann, _ := joinTownChat(t, one.url, "ann")
	bob, _ := joinTownChat(t, two.url, "bob")

	line(t, ann, "/w bob across the shards")
	want(t, hearOne(t, bob), chat.WhisperFrom, "ann", "across the shards")
	line(t, ann, "/y only mine")
	line(t, bob, "/1 everyone")
	// The whisper's echo waits for the other shard's reply, so it can come after
	// both of the others. Read until all three are in.
	var got []string
	for len(got) < 3 {
		got = append(got, texts([]*gamev1.ChatMessage{hearOne(t, ann)})...)
	}
	slices.Sort(got)
	if !slices.Equal(got, []string{"general:everyone", "whisper_to:across the shards", "yell:only mine"}) {
		t.Fatalf("ann heard %v", got)
	}
	if got := hear(t, bob, "everyone"); !slices.Equal(texts(got), []string{"general:everyone"}) {
		t.Fatalf("bob heard %v, want ann's yell to stay on her shard", texts(got))
	}
}

// Joined channels ride in JoinRequest.chat_left, which the client sends
// again on every join, and in the join token for the cave.
func TestLeftChannelsSurviveMoves(t *testing.T) {
	h := serveChat(t, testTown())
	world, joined := joinTownChat(t, h.url, "ann", "trade")
	if !slices.Equal(joined, []string{"general", "lfg"}) {
		t.Fatalf("joined with trade left = %v", joined)
	}
	if _, fresh := joinTownChat(t, h.url, "bob"); !slices.Equal(fresh, chat.Shared) {
		t.Fatalf("a new player is in %v, want all of %v", fresh, chat.Shared)
	}

	line(t, world, "/leave general")
	got := waitFor(t, world, func(m *gamev1.ServerMessage) bool { return m.GetChatChannels() != nil }).GetChatChannels().GetJoined()
	if !slices.Equal(got, []string{"lfg"}) {
		t.Fatalf("after /leave general = %v", got)
	}

	cave, _ := enterInstance(t, world)
	got = waitFor(t, cave, func(m *gamev1.ServerMessage) bool { return m.GetChatChannels() != nil }).GetChatChannels().GetJoined()
	if !slices.Equal(got, []string{"lfg"}) {
		t.Fatalf("channels in the cave = %v, want only lfg", got)
	}
	line(t, cave, "/1 hello")
	want(t, hearOne(t, cave), chat.System, "", "You are not in General. Type /join general.")

	line(t, cave, "/join trade")
	got = waitFor(t, cave, func(m *gamev1.ServerMessage) bool { return m.GetChatChannels() != nil }).GetChatChannels().GetJoined()
	if !slices.Equal(got, []string{"trade", "lfg"}) {
		t.Fatalf("after /join trade in the cave = %v", got)
	}

	// The client works out chat_left from the last ChatChannels it saw.
	_, back := joinTownChat(t, h.url, "ann", "general")
	if !slices.Equal(back, []string{"trade", "lfg"}) {
		t.Fatalf("back in town = %v, want trade and lfg", back)
	}
}

func TestChatWithoutABus(t *testing.T) {
	h := serveWorld(t)
	ann, _ := joinTownChat(t, h.url, "ann")
	bob, _ := joinTownChat(t, h.url, "bob")

	line(t, ann, "/1 anyone")
	want(t, hearOne(t, ann), chat.System, "", "Chat is unavailable right now.")
	line(t, ann, "/y still works")
	want(t, hearOne(t, bob), chat.Yell, "ann", "still works")
	line(t, ann, "/w bob same server")
	if got := hear(t, bob, "same server"); got[len(got)-1].GetChannel() != chat.WhisperFrom {
		t.Fatalf("bob heard %v, want the whisper", texts(got))
	}
	line(t, ann, "/w far away")
	if got := hear(t, ann, "Chat is unavailable right now."); got[len(got)-1].GetChannel() != chat.System {
		t.Fatalf("ann heard %v", texts(got))
	}
	if n := testutil.ToFloat64(h.lobby.ChatMetrics.Drops.WithLabelValues(chat.DropUnavailable)); n != 2 {
		t.Fatalf("unavailable drops = %v, want 2", n)
	}
}

func TestBotsAreNotBurdenedByChat(t *testing.T) {
	h := serveChat(t, testTown())
	bot, joined := joinTownChat(t, h.url, "bot-fill-1", chat.Shared...)
	if len(joined) != 0 {
		t.Fatalf("a bot that left every channel is in %v", joined)
	}
	ann, _ := joinTownChat(t, h.url, "ann")
	line(t, ann, "/1 hello")
	line(t, ann, "/y done")
	if got := hear(t, bot, "done"); !slices.Equal(texts(got), []string{"yell:done"}) {
		t.Fatalf("bot heard %v, want no general chat", texts(got))
	}
}
