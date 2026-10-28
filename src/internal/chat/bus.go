package chat

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
)

// Local is the CHAT value that allows chat with no NATS. Nothing falls back
// to it, so a pod missing NATS_URL crash-loops instead of running with a
// chat that stops at its own shard.
const Local = "local"

var (
	ErrNoChat      = errors.New("no chat bus")
	ErrUnavailable = errors.New("chat unavailable")
	ErrNotOnline   = errors.New("not online")
)

type Bus interface {
	Publish(channel string, m *gamev1.ChatMessage) error
	// Subscribe and Listen call fn on a goroutine of the bus, and the
	// returned func stops them.
	Subscribe(channel string, fn func(*gamev1.ChatMessage)) (func(), error)
	Whisper(ctx context.Context, name string, m *gamev1.ChatMessage) error
	// Listen's fn reports whether it delivered, and may block.
	Listen(name string, fn func(*gamev1.ChatMessage) bool) (func(), error)
	Close()
}

type Off struct{}

func (Off) Publish(string, *gamev1.ChatMessage) error { return ErrUnavailable }
func (Off) Subscribe(string, func(*gamev1.ChatMessage)) (func(), error) {
	return func() {}, nil
}
func (Off) Whisper(context.Context, string, *gamev1.ChatMessage) error { return ErrUnavailable }
func (Off) Listen(string, func(*gamev1.ChatMessage) bool) (func(), error) {
	return func() {}, nil
}
func (Off) Close() {}

const prefix = "frpg."

func ChannelSubject(channel string) string {
	return prefix + "chat.channel." + channel
}

// PlayerSubject lowercases name and escapes anything that is not a letter,
// digit, dash or underscore. NATS reads dots, spaces and wildcards in a
// subject, so a name like ">" must not become one.
func PlayerSubject(name string) string {
	var b strings.Builder
	b.WriteString(prefix + "player.")
	for _, c := range []byte(strings.ToLower(name)) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02x", c)
		}
	}
	return b.String()
}

func Open(ctx context.Context, natsURL, mode string, reg prometheus.Registerer) (Bus, string, error) {
	if mode != "" && mode != Local {
		return nil, "", fmt.Errorf("CHAT %q is not %q", mode, Local)
	}
	if natsURL == "" {
		if mode != Local {
			return nil, "", fmt.Errorf("%w: set NATS_URL, or CHAT=local for local dev", ErrNoChat)
		}
		return Off{}, "local", nil
	}
	if _, err := url.Parse(natsURL); err != nil {
		return nil, "", fmt.Errorf("NATS_URL: %w", err)
	}
	n, err := Dial(ctx, natsURL)
	if err != nil {
		return nil, "", err
	}
	return n, "nats", nil
}
