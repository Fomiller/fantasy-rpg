package director

import (
	"cmp"
	"slices"
	"time"
)

// Assigner splits slots 0 to desired-1 across the worker pods that poll.
// It moves as few slots as it can, and a slot leaves one pod before another
// gets it, so a bot's name is never on two sockets at once.
type Assigner struct {
	// PerPod caps the slots one pod runs.
	PerPod int
	// TTL drops a pod that has not polled for this long, and frees its slots.
	TTL time.Duration

	seen  map[string]time.Time
	owner map[int]*owned
}

type owned struct {
	pod string
	// leaving is a slot taken from pod, held until pod says its bot is gone.
	leaving bool
}

func NewAssigner(perPod int, ttl time.Duration) *Assigner {
	return &Assigner{PerPod: perPod, TTL: ttl, seen: map[string]time.Time{}, owner: map[int]*owned{}}
}

// Poll records that pod is alive with bots running in have. A slot pod gave
// up and no longer has is free. A slot pod has that nobody owns is its own,
// as after a director restart.
func (a *Assigner) Poll(pod string, have []int, now time.Time) {
	a.seen[pod] = now
	for slot, o := range a.owner {
		if o.pod == pod && o.leaving && !slices.Contains(have, slot) {
			delete(a.owner, slot)
		}
	}
	for _, slot := range have {
		if _, ok := a.owner[slot]; !ok && slot >= 0 {
			a.owner[slot] = &owned{pod: pod}
		}
	}
}

// Slots is what pod should run.
func (a *Assigner) Slots(pod string) []int {
	out := []int{}
	for slot, o := range a.owner {
		if o.pod == pod && !o.leaving {
			out = append(out, slot)
		}
	}
	slices.Sort(out)
	return out
}

// Pods is the pods polling now.
func (a *Assigner) Pods() int {
	return len(a.seen)
}

// Balance gives each live pod an even share of desired, at most PerPod.
func (a *Assigner) Balance(desired int, now time.Time) {
	for pod, at := range a.seen {
		if now.Sub(at) > a.TTL {
			delete(a.seen, pod)
		}
	}
	for slot, o := range a.owner {
		if _, ok := a.seen[o.pod]; !ok {
			delete(a.owner, slot)
		} else if slot >= desired {
			o.leaving = true
		}
	}
	if len(a.seen) == 0 {
		return
	}

	held := map[string][]int{}
	for slot, o := range a.owner {
		if !o.leaving {
			held[o.pod] = append(held[o.pod], slot)
		}
	}
	pods := make([]string, 0, len(a.seen))
	for pod := range a.seen {
		pods = append(pods, pod)
	}
	// The pods holding most keep the odd slots, so fewer move.
	slices.SortFunc(pods, func(x, y string) int {
		return cmp.Or(cmp.Compare(len(held[y]), len(held[x])), cmp.Compare(x, y))
	})
	quota := map[string]int{}
	for i, pod := range pods {
		q := desired / len(pods)
		if i < desired%len(pods) {
			q++
		}
		quota[pod] = min(q, a.PerPod)
	}

	for _, pod := range pods {
		slots := held[pod]
		slices.Sort(slots)
		for _, slot := range slots[min(quota[pod], len(slots)):] {
			a.owner[slot].leaving = true
		}
		held[pod] = slots[:min(quota[pod], len(slots))]
	}
	slices.Sort(pods)
	for slot := range desired {
		if _, ok := a.owner[slot]; ok {
			continue
		}
		for _, pod := range pods {
			if len(held[pod]) < quota[pod] {
				a.owner[slot] = &owned{pod: pod}
				held[pod] = append(held[pod], slot)
				break
			}
		}
	}
}
