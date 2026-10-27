package director

import (
	"testing"
	"time"
)

func rule() *FillRule {
	return &FillRule{Max: 10, Slack: 2, Step: 2, UpDelay: 10 * time.Second, DownDelay: time.Minute}
}

// steps runs Next once a second for d and returns the last answer.
func steps(f *FillRule, start time.Time, d time.Duration, target, humans int) (int, time.Time) {
	var n int
	for t := start; !t.After(start.Add(d)); t = t.Add(time.Second) {
		n = f.Next(target, humans, t)
	}
	return n, start.Add(d)
}

func TestFillWaitsThenRampsUp(t *testing.T) {
	f := rule()
	now := time.Unix(0, 0)
	if n := f.Next(6, 0, now); n != 0 {
		t.Fatalf("at once = %d, want 0 until the up delay", n)
	}
	if n, _ := steps(f, now, 9*time.Second, 6, 0); n != 0 {
		t.Fatalf("after 9s = %d, want 0", n)
	}
	if n := f.Next(6, 0, now.Add(10*time.Second)); n != 2 {
		t.Fatalf("after 10s = %d, want one step of 2", n)
	}
	if n := f.Next(6, 0, now.Add(11*time.Second)); n != 4 {
		t.Fatalf("next = %d, want 4", n)
	}
	if n, _ := steps(f, now.Add(12*time.Second), 5*time.Second, 6, 0); n != 6 {
		t.Fatalf("settled = %d, want 6", n)
	}
}

// A human who drops out of both counts for a few seconds, as on the way
// into a cave, changes nothing.
func TestFillIgnoresABlip(t *testing.T) {
	f := rule()
	f.Reset(4)
	now := time.Unix(0, 0)
	n, now := steps(f, now, 5*time.Second, 6, 2)
	if n != 4 {
		t.Fatalf("steady = %d", n)
	}
	n, now = steps(f, now.Add(time.Second), 5*time.Second, 6, 1)
	if n != 4 {
		t.Fatalf("blip = %d, want 4", n)
	}
	if n, _ = steps(f, now.Add(time.Second), 5*time.Second, 6, 2); n != 4 {
		t.Fatalf("after = %d, want 4", n)
	}
}

func TestFillRemovesAfterTheDownDelay(t *testing.T) {
	f := rule()
	f.Reset(6)
	now := time.Unix(0, 0)
	// One human joins: one bot over, inside the slack, so it waits.
	n, _ := steps(f, now, 59*time.Second, 6, 1)
	if n != 6 {
		t.Fatalf("within the delay = %d, want 6", n)
	}
	if n := f.Next(6, 1, now.Add(time.Minute)); n != 5 {
		t.Fatalf("after the delay = %d, want 5", n)
	}
}

// Past the slack, bots leave at once, so a crowd is never kept out by bots
// waiting out the delay.
func TestFillCutsPastTheSlack(t *testing.T) {
	f := rule()
	f.Reset(6)
	now := time.Unix(0, 0)
	if n := f.Next(6, 5, now); n != 3 {
		t.Fatalf("five humans = %d, want 1 wanted plus 2 slack", n)
	}
	if n := f.Next(6, 9, now.Add(time.Second)); n != 2 {
		t.Fatalf("over target = %d, want the slack only", n)
	}
	if n, _ := steps(f, now.Add(2*time.Second), time.Minute, 6, 9); n != 0 {
		t.Fatalf("after the delay = %d, want 0", n)
	}
}

func TestFillCapsAtMax(t *testing.T) {
	f := rule()
	f.Reset(40)
	if n := f.Next(50, 0, time.Unix(0, 0)); n != 10 {
		t.Fatalf("over max = %d, want 10 at once", n)
	}
	if n, _ := steps(f, time.Unix(1, 0), 2*time.Minute, 50, 0); n != 10 {
		t.Fatalf("held = %d, want 10", n)
	}
}

// Humans going back and forth across the wanted count reset the wait, so
// bots never flap.
func TestFillDoesNotFlap(t *testing.T) {
	f := rule()
	f.Reset(4)
	now := time.Unix(0, 0)
	for i := range 20 {
		humans := 2
		if i%2 == 0 {
			humans = 3
		}
		var n int
		n, now = steps(f, now, 8*time.Second, 6, humans)
		if n != 4 {
			t.Fatalf("round %d = %d, want 4", i, n)
		}
		now = now.Add(time.Second)
	}
}
