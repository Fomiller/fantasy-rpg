package director

import (
	"slices"
	"testing"
	"time"
)

func shares(a *Assigner, pods ...string) map[string][]int {
	out := map[string][]int{}
	for _, p := range pods {
		out[p] = a.Slots(p)
	}
	return out
}

// poll is a few rounds in which every pod says it runs what it was given.
// A slot moves over two of them: one to let go, one to take it.
func poll(a *Assigner, desired int, now time.Time, pods ...string) {
	for range 3 {
		for _, p := range pods {
			a.Poll(p, a.Slots(p), now)
			a.Balance(desired, now)
		}
	}
}

func total(s map[string][]int) []int {
	var all []int
	for _, v := range s {
		all = append(all, v...)
	}
	slices.Sort(all)
	return all
}

func TestAssignSplitsEvenly(t *testing.T) {
	a := NewAssigner(25, 15*time.Second)
	now := time.Unix(0, 0)
	poll(a, 7, now, "w-a", "w-b", "w-c")
	s := shares(a, "w-a", "w-b", "w-c")
	if !slices.Equal(total(s), []int{0, 1, 2, 3, 4, 5, 6}) {
		t.Fatalf("slots = %v", s)
	}
	for p, v := range s {
		if len(v) < 2 || len(v) > 3 {
			t.Fatalf("%s has %v", p, v)
		}
	}
}

func TestAssignCapsPerPod(t *testing.T) {
	a := NewAssigner(3, 15*time.Second)
	now := time.Unix(0, 0)
	poll(a, 10, now, "w-a", "w-b")
	s := shares(a, "w-a", "w-b")
	if len(s["w-a"]) != 3 || len(s["w-b"]) != 3 {
		t.Fatalf("slots = %v, want 3 each until a third pod", s)
	}
}

// Shrinking takes the highest slots, and they come back only once the pod
// says their bots are gone.
func TestAssignShrinkFreesSlotsOnRelease(t *testing.T) {
	a := NewAssigner(25, 15*time.Second)
	now := time.Unix(0, 0)
	poll(a, 4, now, "w-a")
	a.Balance(2, now)
	if got := a.Slots("w-a"); !slices.Equal(got, []int{0, 1}) {
		t.Fatalf("after shrink = %v", got)
	}
	// Still stopping 2 and 3, so growing again does not hand them to w-b.
	a.Poll("w-b", nil, now)
	a.Balance(4, now)
	if got := a.Slots("w-b"); len(got) != 0 {
		t.Fatalf("w-b = %v, want nothing while w-a stops 2 and 3", got)
	}
	a.Poll("w-a", []int{0, 1}, now)
	a.Balance(4, now)
	if got := a.Slots("w-b"); !slices.Equal(got, []int{2, 3}) {
		t.Fatalf("w-b = %v, want 2 and 3 once free", got)
	}
}

// A new pod takes slots from the busiest one, and the rest stay put.
func TestAssignMovesFewSlots(t *testing.T) {
	a := NewAssigner(25, 15*time.Second)
	now := time.Unix(0, 0)
	poll(a, 6, now, "w-a", "w-b")
	before := shares(a, "w-a", "w-b")
	a.Poll("w-c", nil, now)
	a.Balance(6, now)
	for _, p := range []string{"w-a", "w-b"} {
		for _, slot := range a.Slots(p) {
			if !slices.Contains(before[p], slot) {
				t.Fatalf("%s gained %d", p, slot)
			}
		}
		if len(a.Slots(p)) != 2 {
			t.Fatalf("%s = %v, want 2", p, a.Slots(p))
		}
	}
	// w-c gets the freed slots once the others let go of them.
	poll(a, 6, now, "w-a", "w-b", "w-c")
	if s := shares(a, "w-a", "w-b", "w-c"); !slices.Equal(total(s), []int{0, 1, 2, 3, 4, 5}) || len(s["w-c"]) != 2 {
		t.Fatalf("after = %v", s)
	}
}

func TestAssignDropsAPodThatStopsPolling(t *testing.T) {
	a := NewAssigner(25, 15*time.Second)
	now := time.Unix(0, 0)
	poll(a, 4, now, "w-a", "w-b")
	later := now.Add(20 * time.Second)
	poll(a, 4, later, "w-a")
	if got := a.Slots("w-a"); len(got) != 4 || a.Pods() != 1 {
		t.Fatalf("w-a = %v, pods %d", got, a.Pods())
	}
}

// After a director restart, each worker keeps the slots it already runs.
func TestAssignAdoptsWhatWorkersHave(t *testing.T) {
	a := NewAssigner(25, 15*time.Second)
	now := time.Unix(0, 0)
	a.Poll("w-b", []int{0, 1, 2}, now)
	a.Poll("w-a", []int{3, 4, 5}, now)
	a.Balance(6, now)
	if got := a.Slots("w-b"); !slices.Equal(got, []int{0, 1, 2}) {
		t.Fatalf("w-b = %v", got)
	}
	if got := a.Slots("w-a"); !slices.Equal(got, []int{3, 4, 5}) {
		t.Fatalf("w-a = %v", got)
	}
}
