package chat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/proto"

	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
)

const (
	dialPatience = 15 * time.Second
	whisperWait  = 2 * time.Second
)

var (
	ack  = []byte("ok")
	nack = []byte("no")
)

type NATS struct {
	nc *nats.Conn
}

func Dial(ctx context.Context, url string) (*NATS, error) {
	ctx, cancel := context.WithTimeout(ctx, dialPatience)
	defer cancel()
	opts := []nats.Option{
		nats.Name("fantasy-rpg"),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(time.Second),
		// No buffer, so a line sent while down fails now and is not sent late.
		nats.ReconnectBufSize(-1),
		nats.Timeout(2 * time.Second),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			if err != nil {
				slog.Warn("chat disconnected", "err", err)
			}
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			slog.Info("chat reconnected", "url", nc.ConnectedUrl())
		}),
	}
	for {
		nc, err := nats.Connect(url, opts...)
		if err == nil {
			return &NATS{nc: nc}, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("connect to NATS: %w", err)
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (n *NATS) Publish(channel string, m *gamev1.ChatMessage) error {
	b, err := proto.Marshal(m)
	if err != nil {
		return err
	}
	if err := n.nc.Publish(ChannelSubject(channel), b); err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return nil
}

func (n *NATS) Subscribe(channel string, fn func(*gamev1.ChatMessage)) (func(), error) {
	sub, err := n.nc.Subscribe(ChannelSubject(channel), func(msg *nats.Msg) {
		var m gamev1.ChatMessage
		if proto.Unmarshal(msg.Data, &m) == nil {
			fn(&m)
		}
	})
	if err == nil {
		err = n.nc.FlushTimeout(2 * time.Second)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return func() { sub.Unsubscribe() }, nil
}

func (n *NATS) Whisper(ctx context.Context, name string, m *gamev1.ChatMessage) error {
	b, err := proto.Marshal(m)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, whisperWait)
	defer cancel()
	reply, err := n.nc.RequestWithContext(ctx, PlayerSubject(name), b)
	switch {
	case errors.Is(err, nats.ErrNoResponders):
		return ErrNotOnline
	case err != nil:
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	case string(reply.Data) != string(ack):
		return ErrNotOnline
	}
	return nil
}

func (n *NATS) Listen(name string, fn func(*gamev1.ChatMessage) bool) (func(), error) {
	sub, err := n.nc.Subscribe(PlayerSubject(name), func(msg *nats.Msg) {
		var m gamev1.ChatMessage
		if proto.Unmarshal(msg.Data, &m) != nil {
			return
		}
		if fn(&m) {
			msg.Respond(ack)
		} else {
			msg.Respond(nack)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return func() { sub.Unsubscribe() }, nil
}

func (n *NATS) Close() {
	n.nc.Drain()
}
