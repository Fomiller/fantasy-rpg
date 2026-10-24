package inventory

import (
	"context"
	"slices"
	"sync"
)

// Store keeps each player's loadout by name, so a player who leaves and
// comes back gets their items again.
type Store interface {
	Load(ctx context.Context, name string) (Loadout, bool, error)
	Save(ctx context.Context, name string, l Loadout) error
	Delete(ctx context.Context, name string) error
	// Banked XP is kept apart from the loadout, which is deleted when a
	// player loses everything. It is 0 for a new player.
	LoadXP(ctx context.Context, name string) (int, error)
	SaveXP(ctx context.Context, name string, xp int) error
}

// Memory is a Store that lasts as long as the process. Postgres replaces it
// in FOM-114.
type Memory struct {
	mu       sync.Mutex
	loadouts map[string]Loadout
	xp       map[string]int
}

func NewMemory() *Memory {
	return &Memory{loadouts: map[string]Loadout{}, xp: map[string]int{}}
}

func (m *Memory) Load(_ context.Context, name string) (Loadout, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.loadouts[name]
	return l.clone(), ok, nil
}

func (m *Memory) Save(_ context.Context, name string, l Loadout) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loadouts[name] = l.clone()
	return nil
}

func (m *Memory) Delete(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.loadouts, name)
	return nil
}

func (m *Memory) LoadXP(_ context.Context, name string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.xp[name], nil
}

func (m *Memory) SaveXP(_ context.Context, name string, xp int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.xp[name] = xp
	return nil
}

// clone stops a caller from changing a saved loadout through a shared slice.
func (l Loadout) clone() Loadout {
	return Loadout{Equipment: slices.Clone(l.Equipment), Items: slices.Clone(l.Items)}
}
