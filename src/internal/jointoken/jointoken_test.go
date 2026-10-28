package jointoken

import (
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

var (
	key  = []byte("test-key-that-is-long-enough-for-hmac")
	now  = time.Unix(1_800_000_000, 0)
	inst = "instance-0@3f9a1c"
)

func sign(t *testing.T, c Claims, at time.Time) string {
	t.Helper()
	tok, err := Sign(key, c, at)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func claims() Claims {
	return Claims{Name: "ann", Class: "warrior", Inst: inst, Run: "4f6c2a3e-0000-4000-8000-000000000001"}
}

func TestSignThenCheck(t *testing.T) {
	tok := sign(t, claims(), now)
	got, err := NewVerifier(key).Check(tok, inst, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	want := claims()
	want.V, want.Iat, want.Exp = Version, now.Unix(), now.Add(Lifetime).Unix()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("claims = %+v, want %+v", got, want)
	}
}

func TestCheckRejects(t *testing.T) {
	good := sign(t, claims(), now)
	body, sig, _ := strings.Cut(good, ".")
	enc := base64.RawURLEncoding
	forged := func(payload string) string { return enc.EncodeToString([]byte(payload)) + "." + sig }
	resign := func(payload string) string {
		return enc.EncodeToString([]byte(payload)) + "." + enc.EncodeToString(mac(key, []byte(payload)))
	}

	otherKey, err := Sign([]byte("another key"), claims(), now)
	if err != nil {
		t.Fatal(err)
	}
	other := claims()
	other.Inst = "instance-1@3f9a1c"
	reboot := claims()
	reboot.Inst = "instance-0@000000"

	for _, tc := range []struct {
		name  string
		token string
		at    time.Time
		want  error
	}{
		{"empty", "", now, ErrBad},
		{"no signature", body, now, ErrBad},
		{"bad base64", body + ".!!", now, ErrBad},
		{"other key", otherKey, now, ErrBad},
		{"changed payload", forged(`{"v":1,"name":"bob","class":"warrior","inst":"instance-0@3f9a1c","run":"r","iat":1800000000,"exp":1800000030}`), now, ErrBad},
		{"unknown version", resign(`{"v":2,"name":"ann","class":"warrior","inst":"instance-0@3f9a1c","run":"r","iat":1800000000,"exp":1800000030}`), now, ErrBad},
		{"no run", resign(`{"v":1,"name":"ann","class":"warrior","inst":"instance-0@3f9a1c","iat":1800000000,"exp":1800000030}`), now, ErrBad},
		{"not json", resign(`nope`), now, ErrBad},
		{"expired", good, now.Add(Lifetime), ErrExpired},
		{"from the future", sign(t, claims(), now.Add(10*time.Second)), now, ErrBad},
		{"other instance", sign(t, other, now), now, ErrWrongInstance},
		{"old boot", sign(t, reboot, now), now, ErrWrongInstance},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewVerifier(key).Check(tc.token, inst, tc.at); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// A few seconds of clock skew is fine.
func TestCheckAllowsSkew(t *testing.T) {
	if _, err := NewVerifier(key).Check(sign(t, claims(), now.Add(4*time.Second)), inst, now); err != nil {
		t.Fatal(err)
	}
}

func TestTokenWorksOnce(t *testing.T) {
	v := NewVerifier(key)
	tok := sign(t, claims(), now)
	if _, err := v.Check(tok, inst, now); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Check(tok, inst, now.Add(time.Second)); !errors.Is(err, ErrUsed) {
		t.Fatalf("second use: %v, want %v", err, ErrUsed)
	}
	// A new token for the same run is still a reuse.
	if _, err := v.Check(sign(t, claims(), now.Add(time.Second)), inst, now.Add(2*time.Second)); !errors.Is(err, ErrUsed) {
		t.Fatalf("same run, new token: %v, want %v", err, ErrUsed)
	}
}

// A rejected token is not remembered, so it does not burn the run id.
func TestRejectedTokenIsNotSeen(t *testing.T) {
	v := NewVerifier(key)
	other := claims()
	other.Inst = "instance-1@3f9a1c"
	if _, err := v.Check(sign(t, other, now), inst, now); !errors.Is(err, ErrWrongInstance) {
		t.Fatal(err)
	}
	if _, err := v.Check(sign(t, claims(), now), inst, now); err != nil {
		t.Fatalf("after a rejected token: %v", err)
	}
}

func TestSeenRunsAreForgottenAfterExpiry(t *testing.T) {
	v := NewVerifier(key)
	if _, err := v.Check(sign(t, claims(), now), inst, now); err != nil {
		t.Fatal(err)
	}
	other := claims()
	other.Run = "another"
	if _, err := v.Check(sign(t, other, now.Add(Lifetime)), inst, now.Add(Lifetime)); err != nil {
		t.Fatal(err)
	}
	if len(v.seen) != 1 {
		t.Fatalf("seen = %v, want only the live run", v.seen)
	}
}

func TestKeyIsRequired(t *testing.T) {
	if _, err := Key(""); err == nil {
		t.Fatal("empty key accepted")
	}
}

func TestChatLeftRidesInTheToken(t *testing.T) {
	c := claims()
	c.ChatLeft = []string{"general", "trade"}
	got, err := NewVerifier(key).Check(sign(t, c, now), inst, now)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.ChatLeft, c.ChatLeft) {
		t.Fatalf("ChatLeft = %v, want %v", got.ChatLeft, c.ChatLeft)
	}
}
