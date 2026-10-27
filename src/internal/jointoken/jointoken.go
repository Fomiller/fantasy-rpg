// Package jointoken makes and checks the token the world server hands a
// player to join one instance server. It is base64url(payload) "."
// base64url(HMAC-SHA256(key, payload)), and the payload is compact JSON. The
// client never reads it.
package jointoken

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
)

const (
	Version = 1
	// Lifetime covers the world server's close, a new TLS connection and a
	// slow phone, and is short enough that a leaked token is useless at once.
	Lifetime = 30 * time.Second
	// skew is how far in the future iat may be, for clocks that disagree.
	skew = 5 * time.Second
)

// The errors are the close reasons the instance server sends.
var (
	ErrBad           = errors.New("bad token")
	ErrExpired       = errors.New("token expired")
	ErrWrongInstance = errors.New("wrong instance")
	ErrUsed          = errors.New("token used")
)

type Claims struct {
	V     int    `json:"v"`
	Name  string `json:"name"`
	Class string `json:"class"`
	// Inst is the instance id and boot id, like instance-0@3f9a1c, so a token
	// only works on the process it was made for.
	Inst string `json:"inst"`
	// Run is the run id, which is also the nonce.
	Run string `json:"run"`
	// Bot is the player's JoinRequest.bot, so the instance counts it the
	// same way the world did.
	Bot bool  `json:"bot,omitempty"`
	Iat int64 `json:"iat"`
	Exp int64 `json:"exp"`
}

// Key reads JOIN_TOKEN_KEY's value. Both servers refuse to start without one.
func Key(v string) ([]byte, error) {
	if v == "" {
		return nil, errors.New("JOIN_TOKEN_KEY is not set")
	}
	return []byte(v), nil
}

// Sign fills in the version and times and makes the token.
func Sign(key []byte, c Claims, now time.Time) (string, error) {
	c.V = Version
	c.Iat = now.Unix()
	c.Exp = now.Add(Lifetime).Unix()
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding
	return enc.EncodeToString(payload) + "." + enc.EncodeToString(mac(key, payload)), nil
}

func mac(key, payload []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(payload)
	return h.Sum(nil)
}

// Verifier checks tokens for one instance server process. It remembers the
// run ids it let in until they expire, so a token works once.
type Verifier struct {
	key  []byte
	mu   sync.Mutex
	seen map[string]time.Time
}

func NewVerifier(key []byte) *Verifier {
	return &Verifier{key: key, seen: map[string]time.Time{}}
}

// Check runs every check that needs no database, in order. instance is this
// process's id and boot id.
func (v *Verifier) Check(token, instance string, now time.Time) (Claims, error) {
	var c Claims
	enc := base64.RawURLEncoding
	body, sig, ok := strings.Cut(token, ".")
	if !ok {
		return c, ErrBad
	}
	payload, err := enc.DecodeString(body)
	if err != nil {
		return c, ErrBad
	}
	got, err := enc.DecodeString(sig)
	if err != nil || !hmac.Equal(got, mac(v.key, payload)) {
		return c, ErrBad
	}
	if err := json.Unmarshal(payload, &c); err != nil || c.V != Version || c.Name == "" || c.Run == "" {
		return Claims{}, ErrBad
	}
	if !now.Before(time.Unix(c.Exp, 0)) {
		return Claims{}, ErrExpired
	}
	if time.Unix(c.Iat, 0).After(now.Add(skew)) {
		return Claims{}, ErrBad
	}
	if c.Inst != instance {
		return Claims{}, ErrWrongInstance
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	for run, exp := range v.seen {
		if !now.Before(exp) {
			delete(v.seen, run)
		}
	}
	if _, used := v.seen[c.Run]; used {
		return Claims{}, ErrUsed
	}
	v.seen[c.Run] = time.Unix(c.Exp, 0)
	return c, nil
}
