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
}

// Memory is a Store that lasts as long as the process. Postgres replaces it
// in FOM-114.
type Memory struct {
	mu       sync.Mutex
	loadouts map[string]Loadout
}

func NewMemory() *Memory {
	return &Memory{loadouts: map[string]Loadout{}}
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

// clone stops a caller from changing a saved loadout through a shared slice.
func (l Loadout) clone() Loadout {
	return Loadout{Equipment: slices.Clone(l.Equipment), Items: slices.Clone(l.Items)}
}
