package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/jointoken"
)

const (
	pollTimeout = time.Second
	// deadLeaseAge is past the token lifetime, so a join still on its way is
	// never cut off.
	deadLeaseAge = 60 * time.Second
)

// InstanceStatus is what an instance server's /status says.
type InstanceStatus struct {
	ID      string `json:"id"`
	Boot    string `json:"boot"`
	Players int    `json:"players"`
	// Bots is how many of Players are bots.
	Bots     int `json:"bots"`
	Capacity int `json:"capacity"`
	// Draining is set once the instance got SIGTERM. It takes no one new.
	Draining bool `json:"draining"`
	// Runs is every run the process still has, including run ends that have
	// not landed.
	Runs []string `json:"runs"`
}

// Pool is where the town's entrance sends players. Directory is the static
// pool from INSTANCES, and InstanceManager asks the instance manager.
type Pool interface {
	// Reserve takes a seat for every run in runs on one instance, so a party
	// lands together.
	Reserve(ctx context.Context, runs []string, now time.Time) (Placement, error)
	// DeadLease reports a lease whose run is gone for sure. When in doubt it
	// is false, since the run end may still land.
	DeadLease(ctx context.Context, l inventory.Lease, now time.Time) bool
}

// Placement is where a reserved party goes. URL is the WebSocket URL a
// client gets in its ticket.
type Placement struct {
	ID, Boot, URL string
}

// ErrFull is a Reserve that found no instance with room.
var ErrFull = errors.New("every instance is full")

// Instance is one instance server the world server can send players to.
type Instance struct {
	ID string
	// Internal is the base URL the world server polls /status on. Public is
	// the WebSocket URL a client gets in its ticket.
	Internal, Public string
}

// ParseInstances reads INSTANCES: space separated id=internal_url=public_url.
func ParseInstances(v string) ([]Instance, error) {
	var out []Instance
	for _, entry := range strings.Fields(v) {
		parts := strings.SplitN(entry, "=", 3)
		if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
			return nil, fmt.Errorf("INSTANCES entry %q is not id=internal_url=public_url", entry)
		}
		if slices.ContainsFunc(out, func(i Instance) bool { return i.ID == parts[0] }) {
			return nil, fmt.Errorf("INSTANCES lists %s twice", parts[0])
		}
		out = append(out, Instance{ID: parts[0], Internal: strings.TrimSuffix(parts[1], "/"), Public: parts[2]})
	}
	return out, nil
}

// Directory is the world server's view of its instance servers, from
// polling each one's /status. It is safe to use from any goroutine.
type Directory struct {
	list   []Instance
	client *http.Client
	mu     sync.Mutex
	// status only has instances that answered their last poll.
	status map[string]InstanceStatus
	// issued is the tickets sent per instance, until their tokens expire.
	issued map[string][]issued
}

type issued struct {
	run string
	at  time.Time
}

func NewDirectory(list []Instance) *Directory {
	return &Directory{
		list:   list,
		client: &http.Client{Timeout: pollTimeout},
		status: map[string]InstanceStatus{},
		issued: map[string][]issued{},
	}
}

// Run polls every instance now and then once per interval until ctx ends.
func (d *Directory) Run(ctx context.Context, every time.Duration) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		d.Poll(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// Poll asks every instance for its status at once.
func (d *Directory) Poll(ctx context.Context) {
	var wg sync.WaitGroup
	for _, inst := range d.list {
		wg.Go(func() {
			st, err := d.fetch(ctx, inst)
			d.mu.Lock()
			defer d.mu.Unlock()
			if err != nil {
				delete(d.status, inst.ID)
				return
			}
			d.status[inst.ID] = st
		})
	}
	wg.Wait()
}

func (d *Directory) fetch(ctx context.Context, inst Instance) (InstanceStatus, error) {
	var st InstanceStatus
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, inst.Internal+"/status", nil)
	if err != nil {
		return st, err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return st, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return st, fmt.Errorf("status %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return st, err
	}
	if st.ID != inst.ID || st.Boot == "" {
		return st, fmt.Errorf("%s answered as %q", inst.ID, st.ID)
	}
	return st, nil
}

// Reserve picks one instance with room for every run in runs and counts
// them against it until their tokens expire. Of those with room it takes
// the one with the most players, so one cave fills before the next. A
// draining instance is never picked.
func (d *Directory) Reserve(_ context.Context, runs []string, now time.Time) (Placement, error) {
	if d == nil {
		return Placement{}, ErrFull
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	best, bestPlayers := -1, -1
	for i, inst := range d.list {
		st, ok := d.status[inst.ID]
		if !ok || st.Draining || st.Players+d.inFlight(inst.ID, st, now)+len(runs) > st.Capacity {
			continue
		}
		if st.Players > bestPlayers {
			best, bestPlayers = i, st.Players
		}
	}
	if best < 0 {
		return Placement{}, ErrFull
	}
	inst := d.list[best]
	for _, run := range runs {
		d.issued[inst.ID] = append(d.issued[inst.ID], issued{run: run, at: now})
	}
	return Placement{ID: inst.ID, Boot: d.status[inst.ID].Boot, URL: inst.Public}, nil
}

// inFlight counts tickets that are still good and not yet a player the
// instance reports, so a join on its way is not counted twice.
func (d *Directory) inFlight(id string, st InstanceStatus, now time.Time) int {
	live := slices.DeleteFunc(d.issued[id], func(t issued) bool { return now.Sub(t.at) >= jointoken.Lifetime })
	d.issued[id] = live
	n := 0
	for _, t := range live {
		if !slices.Contains(st.Runs, t.run) {
			n++
		}
	}
	return n
}

// DeadLease reports a lease whose run is gone for sure: its instance left
// the list, restarted, or answers without the run long after the token
// expired. An instance that does not answer is waited on, since it may
// still land the run end.
func (d *Directory) DeadLease(_ context.Context, l inventory.Lease, now time.Time) bool {
	if d == nil || !l.Held() {
		return false
	}
	id, boot, _ := strings.Cut(l.Instance, "@")
	if !slices.ContainsFunc(d.list, func(i Instance) bool { return i.ID == id }) {
		return true
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	st, ok := d.status[id]
	switch {
	case !ok:
		return false
	case st.Boot != boot:
		return true
	}
	return !slices.Contains(st.Runs, l.RunID) && now.Sub(l.Since) > deadLeaseAge
}

func (s *Lobby) deadLease(ctx context.Context, l inventory.Lease) bool {
	return s.Instances != nil && s.Instances.DeadLease(ctx, l, time.Now())
}
