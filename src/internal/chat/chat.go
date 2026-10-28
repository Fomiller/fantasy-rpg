// Package chat is the rules of the chat box: the channels, how a typed line
// is read, and how fast a player may talk. The server package delivers, and
// Bus carries what has to leave the process.
package chat

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Fomiller/fantasy-rpg/src/internal/caves"
)

const (
	Say         = "say"
	Yell        = "yell"
	WhisperFrom = "whisper_from"
	WhisperTo   = "whisper_to"
	System      = "system"
	General     = "general"
	Trade       = "trade"
	LFG         = "lfg"
)

// Shared are the channels a player can join, in the order /1, /2 and /3
// number them.
var Shared = []string{General, Trade, LFG}

const (
	MaxLen   = 255
	maxLine  = 1024
	SayRange = 6 * caves.TileSize
)

type Kind int

const (
	Empty Kind = iota
	Speak
	Whisper
	Reply
	Join
	Leave
	Usage
	Unknown
)

type Command struct {
	Kind    Kind
	Channel string
	Target  string
	Text    string
}

// Clean takes out control and format characters, so a line cannot carry a
// newline or a right-to-left override.
func Clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == utf8.RuneError {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

func Truncate(s string) string {
	if utf8.RuneCountInString(s) <= MaxLen {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:MaxLen]))
}

var speakers = map[string]string{
	"s": Say, "say": Say,
	"y": Yell, "yell": Yell,
	"1": General, "general": General,
	"2": Trade, "trade": Trade,
	"3": LFG, "lfg": LFG, "lookingforgroup": LFG,
}

func Parse(line string) Command {
	line = Clean(truncateRunes(line, maxLine))
	if line == "" {
		return Command{}
	}
	if line[0] != '/' {
		return speak(Say, line)
	}
	word, rest, _ := strings.Cut(line[1:], " ")
	word, rest = strings.ToLower(word), strings.TrimSpace(rest)
	if ch, ok := speakers[word]; ok {
		return speak(ch, rest)
	}
	switch word {
	case "w", "whisper", "t", "tell":
		target, text, _ := strings.Cut(rest, " ")
		text = strings.TrimSpace(text)
		if target == "" || text == "" {
			return Command{Kind: Usage, Text: "Usage: /w <name> <message>"}
		}
		return Command{Kind: Whisper, Target: target, Text: Truncate(text)}
	case "r", "reply":
		if rest == "" {
			return Command{Kind: Usage, Text: "Usage: /r <message>"}
		}
		return Command{Kind: Reply, Text: Truncate(rest)}
	case "join", "leave":
		kind := Join
		if word == "leave" {
			kind = Leave
		}
		ch, ok := speakers[strings.ToLower(rest)]
		if !ok || !isShared(ch) {
			return Command{Kind: Usage, Text: "Usage: /" + word + " general, trade or lfg"}
		}
		return Command{Kind: kind, Channel: ch}
	}
	return Command{Kind: Unknown, Text: "/" + word}
}

func speak(channel, text string) Command {
	if text == "" {
		return Command{}
	}
	return Command{Kind: Speak, Channel: channel, Text: Truncate(text)}
}

func isShared(ch string) bool {
	for _, s := range Shared {
		if s == ch {
			return true
		}
	}
	return false
}

func truncateRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func Left(names []string) map[string]bool {
	var left map[string]bool
	for _, n := range names {
		if isShared(n) {
			if left == nil {
				left = map[string]bool{}
			}
			left[n] = true
		}
	}
	return left
}

func LeftList(left map[string]bool) []string {
	var out []string
	for _, ch := range Shared {
		if left[ch] {
			out = append(out, ch)
		}
	}
	return out
}

func Joined(left map[string]bool) []string {
	var out []string
	for _, ch := range Shared {
		if !left[ch] {
			out = append(out, ch)
		}
	}
	return out
}

// Limiter is a token bucket for one player. The zero value starts full.
type Limiter struct {
	tokens float64
	last   time.Time
}

const (
	Burst  = 5
	Refill = time.Second
)

func (l *Limiter) Allow(now time.Time) bool {
	if l.last.IsZero() {
		l.tokens = Burst
	} else {
		l.tokens = min(Burst, l.tokens+now.Sub(l.last).Seconds()/Refill.Seconds())
	}
	l.last = now
	if l.tokens < 1 {
		return false
	}
	l.tokens--
	return true
}
