package chat

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/test"
	"github.com/prometheus/client_golang/prometheus"

	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
)

func TestOpenFailsClosed(t *testing.T) {
	ctx := context.Background()
	reg := prometheus.NewRegistry()
	if _, _, err := Open(ctx, "", "", reg); !errors.Is(err, ErrNoChat) {
		t.Fatalf("no NATS_URL and no CHAT: %v, want ErrNoChat", err)
	}
	if _, _, err := Open(ctx, "", "nope", reg); err == nil {
		t.Fatal("CHAT=nope started")
	}
	bus, name, err := Open(ctx, "", Local, reg)
	if err != nil || name != "local" {
		t.Fatalf("CHAT=local = %v, %q", err, name)
	}
	if err := bus.Publish(General, &gamev1.ChatMessage{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("local publish = %v, want unavailable", err)
	}
	if err := bus.Whisper(ctx, "bob", &gamev1.ChatMessage{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("local whisper = %v, want unavailable", err)
	}
}

func natsURL(t *testing.T) string {
	t.Helper()
	s := natsserver.RunRandClientPortServer()
	t.Cleanup(s.Shutdown)
	return s.ClientURL()
}

func dialNATS(t *testing.T, url string) *NATS {
	t.Helper()
	n, err := Dial(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(n.Close)
	return n
}

type inbox struct {
	mu   sync.Mutex
	msgs []*gamev1.ChatMessage
}

func (i *inbox) add(m *gamev1.ChatMessage) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.msgs = append(i.msgs, m)
}

func (i *inbox) wait(t *testing.T, n int) []*gamev1.ChatMessage {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		i.mu.Lock()
		got := append([]*gamev1.ChatMessage(nil), i.msgs...)
		i.mu.Unlock()
		if len(got) >= n {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("got %d messages, want %d", len(i.msgs), n)
	return nil
}

func TestChannelReachesEveryServer(t *testing.T) {
	url := natsURL(t)
	a, b, c := dialNATS(t, url), dialNATS(t, url), dialNATS(t, url)
	var onA, onB, tradeOnB inbox
	for bus, in := range map[*NATS]*inbox{a: &onA, b: &onB} {
		if _, err := bus.Subscribe(General, in.add); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.Subscribe(Trade, tradeOnB.add); err != nil {
		t.Fatal(err)
	}
	if err := c.Publish(General, &gamev1.ChatMessage{Channel: General, Sender: "cat", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	for name, in := range map[string]*inbox{"a": &onA, "b": &onB} {
		got := in.wait(t, 1)
		if got[0].GetSender() != "cat" || got[0].GetText() != "hello" {
			t.Errorf("server %s got %v", name, got[0])
		}
	}
	time.Sleep(50 * time.Millisecond)
	if len(tradeOnB.msgs) != 0 {
		t.Fatalf("trade got a general message: %v", tradeOnB.msgs)
	}
}

func TestStoppedSubscriptionHearsNothing(t *testing.T) {
	url := natsURL(t)
	a, b := dialNATS(t, url), dialNATS(t, url)
	var in inbox
	stop, err := a.Subscribe(LFG, in.add)
	if err != nil {
		t.Fatal(err)
	}
	stop()
	a.nc.Flush()
	b.Publish(LFG, &gamev1.ChatMessage{Text: "late"})
	b.nc.Flush()
	time.Sleep(50 * time.Millisecond)
	if len(in.msgs) != 0 {
		t.Fatalf("stopped subscription got %v", in.msgs)
	}
}

func TestWhisper(t *testing.T) {
	url := natsURL(t)
	home, away := dialNATS(t, url), dialNATS(t, url)
	var got inbox
	stop, err := home.Listen("Bob", func(m *gamev1.ChatMessage) bool {
		got.add(m)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	home.nc.Flush()
	ctx := context.Background()

	// The name matches without regard to case.
	if err := away.Whisper(ctx, "bOB", &gamev1.ChatMessage{Sender: "ann", Text: "psst"}); err != nil {
		t.Fatalf("whisper to an online player: %v", err)
	}
	if m := got.wait(t, 1)[0]; m.GetSender() != "ann" || m.GetText() != "psst" {
		t.Fatalf("whisper = %v", m)
	}

	if err := away.Whisper(ctx, "nobody", &gamev1.ChatMessage{Text: "hi"}); !errors.Is(err, ErrNotOnline) {
		t.Fatalf("whisper to nobody = %v, want not online", err)
	}

	stop()
	home.nc.Flush()
	if err := away.Whisper(ctx, "bob", &gamev1.ChatMessage{Text: "hi"}); !errors.Is(err, ErrNotOnline) {
		t.Fatalf("whisper after the player left = %v, want not online", err)
	}
}

func TestWhisperTheListenerRefuses(t *testing.T) {
	url := natsURL(t)
	home, away := dialNATS(t, url), dialNATS(t, url)
	if _, err := home.Listen("bob", func(*gamev1.ChatMessage) bool { return false }); err != nil {
		t.Fatal(err)
	}
	home.nc.Flush()
	if err := away.Whisper(context.Background(), "bob", &gamev1.ChatMessage{Text: "hi"}); !errors.Is(err, ErrNotOnline) {
		t.Fatalf("whisper the listener refused = %v, want not online", err)
	}
}

func TestOddNamesStayOnOneToken(t *testing.T) {
	url := natsURL(t)
	home, away := dialNATS(t, url), dialNATS(t, url)
	var got inbox
	for _, name := range []string{"a.b", "*", ">"} {
		if _, err := home.Listen(name, func(m *gamev1.ChatMessage) bool { got.add(m); return true }); err != nil {
			t.Fatal(err)
		}
	}
	home.nc.Flush()
	if err := away.Whisper(context.Background(), "a.b", &gamev1.ChatMessage{Text: "x"}); err != nil {
		t.Fatal(err)
	}
	// A player named * must not hear whispers meant for others.
	if err := away.Whisper(context.Background(), "other", &gamev1.ChatMessage{Text: "y"}); !errors.Is(err, ErrNotOnline) {
		t.Fatalf("whisper to an unrelated name = %v, want not online", err)
	}
	if n := len(got.wait(t, 1)); n != 1 {
		t.Fatalf("listeners heard %d whispers, want 1", n)
	}
}

func TestPublishFailsWhileDisconnected(t *testing.T) {
	s := natsserver.RunRandClientPortServer()
	bus := dialNATS(t, s.ClientURL())
	s.Shutdown()
	deadline := time.Now().Add(2 * time.Second)
	for bus.nc.IsConnected() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if err := bus.Publish(General, &gamev1.ChatMessage{Text: "lost"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("publish with NATS down = %v, want unavailable", err)
	}
	err := bus.Whisper(context.Background(), "bob", &gamev1.ChatMessage{Text: "lost"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("whisper with NATS down = %v, want unavailable", err)
	}
}
