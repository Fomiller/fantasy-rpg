// Package towns is the town directory: each town shard's URL and head count,
// and the shard each player was last in. It lives in Valkey and is a routing
// hint only. Losing it costs a worse pick, never an item.
package towns

import (
	"context"
	"errors"
	"strconv"
	"time"
)

const (
	// Beat is how often a shard publishes itself. TownTTL drops a dead shard
	// after a few missed beats.
	Beat    = 2 * time.Second
	TownTTL = 10 * time.Second
	// PlayerTTL keeps a player's last shard long enough to cover a cave run.
	PlayerTTL = 10 * time.Minute
)

// ErrFull is no shard with room for the player.
var ErrFull = errors.New("town full")

// Town is one shard as the directory has it.
type Town struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	Players  int    `json:"players"`
	Revision string `json:"revision"`
	Draining bool   `json:"draining"`
	// Started is when the shard's process started. Revisions are hashes, so
	// the newest revision is the one the latest started shard runs.
	Started   time.Time `json:"started"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Directory is where shards publish themselves and front doors pick one.
type Directory interface {
	Publish(ctx context.Context, t Town) error
	// Towns is every live shard.
	Towns(ctx context.Context) ([]Town, error)
	// Remember sets the shard name was last in.
	Remember(ctx context.Context, name, shard string) error
	// Pick is the shard name should join. self is the asking shard, which
	// counts even before its first publish.
	Pick(ctx context.Context, name string, self Town) (Town, error)
	Close()
}

// Caps are a shard's player limits. Hard is TOWN_MAX_PLAYERS. Soft is where
// fill-first stops, leaving room for parties and returning players.
type Caps struct {
	Hard, Soft int
}

// NewCaps sets Soft to percent of hard, and at least 1.
func NewCaps(hard, percent int) Caps {
	return Caps{Hard: hard, Soft: max(1, hard*percent/100)}
}

// Choose is the pick order from the scaling design: the last shard, then the
// fullest under the soft cap on the newest revision, then the emptiest under
// the hard cap. A draining shard is never picked. self replaces any entry
// with its id, so a shard always sees its own count.
func Choose(ts []Town, self Town, last string, c Caps) (Town, error) {
	all := make([]Town, 0, len(ts)+1)
	for _, t := range ts {
		if t.ID != self.ID {
			all = append(all, t)
		}
	}
	if self.ID != "" {
		all = append(all, self)
	}
	open := func(t Town) bool { return !t.Draining && t.Players < c.Hard }

	if last != "" {
		for _, t := range all {
			if t.ID == last && open(t) {
				return t, nil
			}
		}
	}

	rev := newest(all)
	var best *Town
	for i, t := range all {
		if !open(t) || t.Revision != rev || t.Players >= c.Soft {
			continue
		}
		if best == nil || t.Players > best.Players || t.Players == best.Players && before(t.ID, best.ID) {
			best = &all[i]
		}
	}
	if best != nil {
		return *best, nil
	}

	for i, t := range all {
		if !open(t) {
			continue
		}
		if best == nil || t.Players < best.Players || t.Players == best.Players && before(t.ID, best.ID) {
			best = &all[i]
		}
	}
	if best != nil {
		return *best, nil
	}
	return Town{}, ErrFull
}

// newest is the revision of the latest started shard that is not draining.
func newest(ts []Town) string {
	var rev string
	var at time.Time
	found := false
	for _, t := range ts {
		if t.Draining {
			continue
		}
		if !found || t.Started.After(at) {
			rev, at, found = t.Revision, t.Started, true
		}
	}
	return rev
}

// before orders shard ids by ordinal, so world-2 comes before world-10. Ids
// without one fall back to string order.
func before(a, b string) bool {
	na, oka := ordinal(a)
	nb, okb := ordinal(b)
	if oka && okb && na != nb {
		return na < nb
	}
	return a < b
}

// ordinal is the number at the end of id, as in world-3.
func ordinal(id string) (int, bool) {
	i := len(id)
	for i > 0 && id[i-1] >= '0' && id[i-1] <= '9' {
		i--
	}
	if i == len(id) {
		return 0, false
	}
	n, err := strconv.Atoi(id[i:])
	return n, err == nil
}

// failOpen is Pick without the directory. The Postgres claim still stops a
// second session, so taking the player here is safe.
func failOpen(self Town, c Caps, err error) (Town, error) {
	if !self.Draining && self.Players < c.Hard {
		return self, nil
	}
	return Town{}, errors.Join(ErrFull, err)
}

// Off is no directory, as in `just dev`. Every pick is the asking shard.
type Off struct {
	Caps Caps
}

func (Off) Publish(context.Context, Town) error            { return nil }
func (Off) Towns(context.Context) ([]Town, error)          { return nil, nil }
func (Off) Remember(context.Context, string, string) error { return nil }
func (o Off) Pick(_ context.Context, _ string, self Town) (Town, error) {
	return Choose(nil, self, "", o.Caps)
}
func (Off) Close() {}
