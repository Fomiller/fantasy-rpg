package gameserver

import (
	"errors"
	"slices"
	"testing"
)

func TestLocalRunsList(t *testing.T) {
	l := NewLocal(2)
	for _, run := range []string{"a", "b", "a"} {
		if err := l.AddRun(run); err != nil {
			t.Fatalf("add %s: %v", run, err)
		}
	}
	if err := l.AddRun("c"); !errors.Is(err, ErrFull) {
		t.Fatalf("add past capacity = %v, want full", err)
	}
	if runs, _ := l.Runs(); !slices.Equal(runs, []string{"a", "b"}) {
		t.Fatalf("runs = %v", runs)
	}
	if err := l.RemoveRun("a"); err != nil {
		t.Fatal(err)
	}
	if err := l.RemoveRun("a"); !errors.Is(err, ErrNoRun) {
		t.Fatalf("remove twice = %v, want not listed", err)
	}
	l.SetRunCapacity(0)
	if err := l.AddRun("c"); !errors.Is(err, ErrFull) {
		t.Fatalf("add at capacity 0 = %v, want full", err)
	}
}

// Locally Shutdown resets the world and opens the list again.
func TestLocalShutdownReopens(t *testing.T) {
	l := NewLocal(8)
	resets := 0
	l.OnShutdown = func() { resets++ }
	l.SetAnnotation(BootKey, "3f9a1c")
	if stopped, err := ShutdownIfEmpty(l, 8); !stopped || err != nil {
		t.Fatalf("shutdown on empty = %v, %v", stopped, err)
	}
	if resets != 1 || l.RunCapacity() != 8 {
		t.Fatalf("resets = %d, capacity = %d, want one reset and capacity back", resets, l.RunCapacity())
	}
	if l.Annotation(BootKey) != "3f9a1c" {
		t.Fatalf("boot = %q, want it kept", l.Annotation(BootKey))
	}
}

// recorder logs every call and lets a run land between two of them.
type recorder struct {
	*Local
	calls []string
	// afterClose runs once capacity drops to 0.
	afterClose func()
}

func (r *recorder) SetRunCapacity(n int64) error {
	if n == 0 {
		r.calls = append(r.calls, "capacity 0")
	} else {
		r.calls = append(r.calls, "capacity open")
	}
	err := r.Local.SetRunCapacity(n)
	if n == 0 && r.afterClose != nil {
		r.afterClose()
	}
	return err
}

func (r *recorder) Runs() ([]string, error) {
	r.calls = append(r.calls, "runs")
	return r.Local.Runs()
}

func (r *recorder) Shutdown() error {
	r.calls = append(r.calls, "shutdown")
	return r.Local.Shutdown()
}

func TestShutdownIfEmptyClosesBeforeReading(t *testing.T) {
	r := &recorder{Local: NewLocal(8)}
	if stopped, err := ShutdownIfEmpty(r, 8); !stopped || err != nil {
		t.Fatalf("shutdown on empty = %v, %v", stopped, err)
	}
	if want := []string{"capacity 0", "runs", "shutdown"}; !slices.Equal(r.calls, want) {
		t.Fatalf("calls = %v, want %v", r.calls, want)
	}
}

// An allocation that lands while capacity drops keeps the server up.
func TestShutdownIfEmptyStaysForALateRun(t *testing.T) {
	r := &recorder{Local: NewLocal(8)}
	r.afterClose = func() { r.Local.runs = append(r.Local.runs, "late") }
	stopped, err := ShutdownIfEmpty(r, 8)
	if stopped || err != nil {
		t.Fatalf("shutdown with a run = %v, %v", stopped, err)
	}
	if want := []string{"capacity 0", "runs", "capacity open"}; !slices.Equal(r.calls, want) {
		t.Fatalf("calls = %v, want %v", r.calls, want)
	}
	if r.RunCapacity() != 8 {
		t.Fatalf("capacity = %d, want 8 again", r.RunCapacity())
	}
}
