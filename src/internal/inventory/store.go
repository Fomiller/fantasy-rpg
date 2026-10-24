package inventory

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/Fomiller/fantasy-rpg/src/internal/items"
)

// StashSize is how many entries a stash holds. Stacks count as one, as in the bag.
const StashSize = 100

var (
	ErrNoCharacter = errors.New("no such character")
	ErrNoRunID     = errors.New("run id is empty")
)

// Character is what the store keeps for one class of one account. Level is
// not kept, since it comes from XP. Stash belongs to the account, so every
// class of that account shares it.
type Character struct {
	Name    string
	Class   string
	XP      int
	Loadout Loadout
	Stash   []items.Instance
}

// Store keeps characters between connections. Each write is one call that
// replaces whole values, so a crash never leaves the stash and the
// character disagreeing.
type Store interface {
	// Load returns false for a name and class that have never joined. The
	// stash is filled even then if the account has one.
	Load(ctx context.Context, name, class string) (Character, bool, error)
	// Create saves a new character with its starter kit. It is a no-op if the
	// character already exists, so two racing joins cannot both hand out a kit.
	// A new account starts with c.Stash; an existing one keeps its own.
	Create(ctx context.Context, c Character) error
	// SaveLobby writes the loadout and stash after a lobby move.
	SaveLobby(ctx context.Context, name, class string, l Loadout, stash []items.Instance) error
	// EndRun writes XP, loadout and stash after a run. Writing the same run id
	// twice is a no-op, so a retry after a lost reply cannot apply it twice.
	EndRun(ctx context.Context, runID string, c Character) error
}

// Memory is a Store that lasts as long as the process. Postgres replaces it
// in FOM-216.
type Memory struct {
	mu         sync.Mutex
	stashes    map[string][]items.Instance
	characters map[charKey]*saved
}

type charKey struct{ name, class string }

type saved struct {
	xp      int
	loadout Loadout
	lastRun string
}

func NewMemory() *Memory {
	return &Memory{stashes: map[string][]items.Instance{}, characters: map[charKey]*saved{}}
}

func (m *Memory) Load(_ context.Context, name, class string) (Character, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := Character{Name: name, Class: class, Stash: slices.Clone(m.stashes[name])}
	s, ok := m.characters[charKey{name, class}]
	if !ok {
		return c, false, nil
	}
	c.XP, c.Loadout = s.xp, s.loadout.clone()
	return c, true, nil
}

func (m *Memory) Create(_ context.Context, c Character) error {
	if err := checkStash(c.Stash); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.stashes[c.Name]; !ok {
		m.stashes[c.Name] = slices.Clone(c.Stash)
	}
	key := charKey{c.Name, c.Class}
	if _, ok := m.characters[key]; !ok {
		m.characters[key] = &saved{xp: c.XP, loadout: c.Loadout.clone()}
	}
	return nil
}

func (m *Memory) SaveLobby(_ context.Context, name, class string, l Loadout, stash []items.Instance) error {
	if err := checkStash(stash); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.characters[charKey{name, class}]
	if !ok {
		return fmt.Errorf("%w: %s %s", ErrNoCharacter, name, class)
	}
	s.loadout = l.clone()
	m.stashes[name] = slices.Clone(stash)
	return nil
}

func (m *Memory) EndRun(_ context.Context, runID string, c Character) error {
	if runID == "" {
		return ErrNoRunID
	}
	if err := checkStash(c.Stash); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.characters[charKey{c.Name, c.Class}]
	if !ok {
		return fmt.Errorf("%w: %s %s", ErrNoCharacter, c.Name, c.Class)
	}
	if s.lastRun == runID {
		return nil
	}
	s.xp, s.loadout, s.lastRun = c.XP, c.Loadout.clone(), runID
	m.stashes[c.Name] = slices.Clone(c.Stash)
	return nil
}

func checkStash(stash []items.Instance) error {
	if len(stash) > StashSize {
		return fmt.Errorf("stash has %d entries, the limit is %d", len(stash), StashSize)
	}
	return nil
}

// clone stops a caller from changing a saved loadout through a shared slice.
func (l Loadout) clone() Loadout {
	return Loadout{Equipment: slices.Clone(l.Equipment), Items: slices.Clone(l.Items)}
}
