package inventory

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/Fomiller/fantasy-rpg/src/internal/items"
)

// StashSize is how many entries a stash holds. Stacks count as one, as in the bag.
const StashSize = 100

var (
	ErrNoCharacter = errors.New("no such character")
	ErrNoRunID     = errors.New("run id is empty")
	ErrInRun       = errors.New("account is in a run")
	ErrRunUsed     = errors.New("run id already ended")
	ErrLeaseLost   = errors.New("run does not hold the lease")
	ErrStashFull   = errors.New("stash is over its limit")
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
	// Run is the account's run lease. Only Load and StartRun fill it.
	Run Lease
}

// Lease is the run that holds an account. While it is held only that run
// may write the account, so a lobby move cannot land under a run end.
type Lease struct {
	RunID string
	// Instance is the instance id and boot id, like instance-0@3f9a1c.
	Instance string
	Since    time.Time
}

func (l Lease) Held() bool { return l.RunID != "" }

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
	// SaveLobby writes the loadout and stash after a lobby move. ErrInRun while
	// a lease is held.
	SaveLobby(ctx context.Context, name, class string, l Loadout, stash []items.Instance) error
	// StartRun takes the run lease and returns the character and stash.
	// ErrInRun if a lease is held. ErrRunUsed if runID is the character's last run.
	StartRun(ctx context.Context, name, class, runID, instance string) (Character, error)
	// EndRun writes XP, loadout and stash after a run and clears the lease.
	// Writing the same run id twice is a no-op, so a retry after a lost reply
	// cannot apply it twice. ErrLeaseLost if the lease is not runID's.
	EndRun(ctx context.Context, runID string, c Character) error
	// ClearRun drops a dead lease. A no-op unless the lease is still runID's.
	ClearRun(ctx context.Context, name, runID string) error
}

// Memory is a Store that lasts as long as the process. db.Store is the
// Postgres one.
type Memory struct {
	mu         sync.Mutex
	stashes    map[string][]items.Instance
	leases     map[string]Lease
	characters map[charKey]*saved
}

type charKey struct{ name, class string }

type saved struct {
	xp      int
	loadout Loadout
	lastRun string
}

func NewMemory() *Memory {
	return &Memory{stashes: map[string][]items.Instance{}, leases: map[string]Lease{}, characters: map[charKey]*saved{}}
}

func (m *Memory) Load(_ context.Context, name, class string) (Character, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := Character{Name: name, Class: class, Stash: slices.Clone(m.stashes[name]), Run: m.leases[name]}
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
	if m.leases[name].Held() {
		return ErrInRun
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
	if m.leases[c.Name].RunID != runID {
		return ErrLeaseLost
	}
	s.xp, s.loadout, s.lastRun = c.XP, c.Loadout.clone(), runID
	m.stashes[c.Name] = slices.Clone(c.Stash)
	delete(m.leases, c.Name)
	return nil
}

func (m *Memory) StartRun(_ context.Context, name, class, runID, instance string) (Character, error) {
	if runID == "" {
		return Character{}, ErrNoRunID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.characters[charKey{name, class}]
	switch {
	case !ok:
		return Character{}, fmt.Errorf("%w: %s %s", ErrNoCharacter, name, class)
	case m.leases[name].Held():
		return Character{}, ErrInRun
	case s.lastRun == runID:
		return Character{}, ErrRunUsed
	}
	lease := Lease{RunID: runID, Instance: instance, Since: time.Now()}
	m.leases[name] = lease
	return Character{Name: name, Class: class, XP: s.xp, Loadout: s.loadout.clone(), Stash: slices.Clone(m.stashes[name]), Run: lease}, nil
}

func (m *Memory) ClearRun(_ context.Context, name, runID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if runID != "" && m.leases[name].RunID == runID {
		delete(m.leases, name)
	}
	return nil
}

func checkStash(stash []items.Instance) error {
	if len(stash) > StashSize {
		return fmt.Errorf("%w: %d entries, the limit is %d", ErrStashFull, len(stash), StashSize)
	}
	return nil
}

// clone stops a caller from changing a saved loadout through a shared slice.
func (l Loadout) clone() Loadout {
	return Loadout{Equipment: slices.Clone(l.Equipment), Items: slices.Clone(l.Items)}
}
