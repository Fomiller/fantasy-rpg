package chat

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	long := strings.Repeat("a", 300)
	tests := []struct {
		line string
		want Command
	}{
		{"hello", Command{Kind: Speak, Channel: Say, Text: "hello"}},
		{"/s hello there", Command{Kind: Speak, Channel: Say, Text: "hello there"}},
		{"/SAY hi", Command{Kind: Speak, Channel: Say, Text: "hi"}},
		{"/y over here", Command{Kind: Speak, Channel: Yell, Text: "over here"}},
		{"/1 anyone", Command{Kind: Speak, Channel: General, Text: "anyone"}},
		{"/2 wts sword", Command{Kind: Speak, Channel: Trade, Text: "wts sword"}},
		{"/3 need tank", Command{Kind: Speak, Channel: LFG, Text: "need tank"}},
		{"/w bob  psst  ", Command{Kind: Whisper, Target: "bob", Text: "psst"}},
		{"/tell bob psst", Command{Kind: Whisper, Target: "bob", Text: "psst"}},
		{"/r ok", Command{Kind: Reply, Text: "ok"}},
		{"/join trade", Command{Kind: Join, Channel: Trade}},
		{"/leave 1", Command{Kind: Leave, Channel: General}},
		{"/leave LFG", Command{Kind: Leave, Channel: LFG}},
		{"/w bob", Command{Kind: Usage, Text: "Usage: /w <name> <message>"}},
		{"/w", Command{Kind: Usage, Text: "Usage: /w <name> <message>"}},
		{"/r", Command{Kind: Usage, Text: "Usage: /r <message>"}},
		{"/join", Command{Kind: Usage, Text: "Usage: /join general, trade or lfg"}},
		{"/join say", Command{Kind: Usage, Text: "Usage: /join general, trade or lfg"}},
		{"/leave nowhere", Command{Kind: Usage, Text: "Usage: /leave general, trade or lfg"}},
		{"/dance", Command{Kind: Unknown, Text: "/dance"}},
		{"/", Command{Kind: Unknown, Text: "/"}},
		{"", Command{}},
		{"   ", Command{}},
		{"/s", Command{}},
		{"/1   ", Command{}},
		{"/s \x00\x07\n\t", Command{}},
		{"/y a\nb\x1b[0mc", Command{Kind: Speak, Channel: Yell, Text: "ab[0mc"}},
		{"/s a‮b", Command{Kind: Speak, Channel: Say, Text: "ab"}},
		{"/s " + long, Command{Kind: Speak, Channel: Say, Text: strings.Repeat("a", MaxLen)}},
		{"/w bob " + long, Command{Kind: Whisper, Target: "bob", Text: strings.Repeat("a", MaxLen)}},
	}
	for _, tt := range tests {
		if got := Parse(tt.line); got != tt.want {
			t.Errorf("Parse(%q) = %+v, want %+v", tt.line, got, tt.want)
		}
	}
}

func TestTruncateCountsCharacters(t *testing.T) {
	s := strings.Repeat("é", MaxLen+10)
	if got := Truncate(s); got != strings.Repeat("é", MaxLen) {
		t.Fatalf("Truncate cut to %d characters, want %d", len([]rune(got)), MaxLen)
	}
	if got := Truncate("short"); got != "short" {
		t.Fatalf("Truncate(short) = %q", got)
	}
}

func TestParseBoundsHugeLines(t *testing.T) {
	cmd := Parse("/s " + strings.Repeat("x", 1<<20))
	if cmd.Kind != Speak || len(cmd.Text) != MaxLen {
		t.Fatalf("huge line = %d bytes of text, kind %v", len(cmd.Text), cmd.Kind)
	}
}

func TestLimiter(t *testing.T) {
	var l Limiter
	now := time.Unix(1000, 0)
	for i := 0; i < Burst; i++ {
		if !l.Allow(now) {
			t.Fatalf("line %d of the burst refused", i+1)
		}
	}
	if l.Allow(now) {
		t.Fatal("line past the burst allowed")
	}
	if l.Allow(now.Add(Refill / 2)) {
		t.Fatal("line allowed before a token came back")
	}
	if !l.Allow(now.Add(Refill)) {
		t.Fatal("line refused after a token came back")
	}
	if l.Allow(now.Add(Refill)) {
		t.Fatal("one token paid for two lines")
	}
	// A long quiet spell refills to the burst and no further.
	later := now.Add(time.Hour)
	allowed := 0
	for i := 0; i < Burst*2; i++ {
		if l.Allow(later) {
			allowed++
		}
	}
	if allowed != Burst {
		t.Fatalf("after a quiet hour %d lines allowed, want %d", allowed, Burst)
	}
}

func TestChannelMembership(t *testing.T) {
	if got := Joined(nil); !slices.Equal(got, Shared) {
		t.Fatalf("new player has %v, want all of %v", got, Shared)
	}
	left := Left([]string{Trade, "say", "bogus", Trade})
	if !slices.Equal(Joined(left), []string{General, LFG}) {
		t.Fatalf("joined after leaving trade = %v", Joined(left))
	}
	if !slices.Equal(LeftList(left), []string{Trade}) {
		t.Fatalf("left list = %v, want only trade", LeftList(left))
	}
	if Left(nil) != nil || Left([]string{"say"}) != nil {
		t.Fatal("no valid names should leave nothing")
	}
	if !slices.Equal(Joined(Left(LeftList(left))), Joined(left)) {
		t.Fatal("a left list does not survive the trip back")
	}
}

func TestPlayerSubject(t *testing.T) {
	tests := map[string]string{
		"Bob":         "frpg.player.bob",
		"bot-fill_3":  "frpg.player.bot-fill_3",
		"a.b":         "frpg.player.a%2eb",
		"a b":         "frpg.player.a%20b",
		"*":           "frpg.player.%2a",
		">":           "frpg.player.%3e",
		"café":        "frpg.player.caf%c3%a9",
		"frpg.player": "frpg.player.frpg%2eplayer",
	}
	for name, want := range tests {
		if got := PlayerSubject(name); got != want {
			t.Errorf("PlayerSubject(%q) = %q, want %q", name, got, want)
		}
	}
	if ChannelSubject(General) != "frpg.chat.channel.general" || ChannelSubject(LFG) != "frpg.chat.channel.lfg" {
		t.Fatal("channel subjects changed")
	}
}
