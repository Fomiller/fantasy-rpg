// Package gameserver is the instance server's side of Agones: its state,
// health, and the runs list that is its seat count. Local stands in for the
// sidecar when AGONES_SDK_GRPC_PORT is unset, so local dev needs no
// Kubernetes.
package gameserver

import (
	"errors"
	"slices"
	"sync"
)

// BootKey is the annotation the boot id goes in. The SDK prefixes it, so it
// lands on the GameServer as agones.dev/sdk-boot.
const BootKey = "boot"

type GameServer interface {
	Ready() error
	Health() error
	// Shutdown ends this instance. On Agones the pod goes away. Locally it
	// resets the world, as an empty instance always has.
	Shutdown() error
	SetAnnotation(key, value string) error
	// AddRun is a no-op for a run already listed, as the allocation lists it.
	AddRun(runID string) error
	RemoveRun(runID string) error
	Runs() ([]string, error)
	SetRunCapacity(n int64) error
}

// ShutdownIfEmpty shuts gs down if its runs list is empty, and reports if it
// did. Capacity drops to 0 before the read, so no allocation can take a seat
// between the read and the Shutdown. A run that landed first puts capacity
// back.
func ShutdownIfEmpty(gs GameServer, capacity int64) (bool, error) {
	if err := gs.SetRunCapacity(0); err != nil {
		return false, err
	}
	runs, err := gs.Runs()
	if err != nil || len(runs) > 0 {
		return false, errors.Join(err, gs.SetRunCapacity(capacity))
	}
	return true, gs.Shutdown()
}

var (
	ErrNoRun = errors.New("run not listed")
	ErrFull  = errors.New("runs list full")
)

// Local keeps the runs list in memory. Shutdown calls OnShutdown and opens
// the list again at the last capacity above 0.
type Local struct {
	OnShutdown func()
	mu         sync.Mutex
	runs       []string
	capacity   int64
	open       int64
	annotation map[string]string
}

func NewLocal(capacity int64) *Local {
	return &Local{capacity: capacity, open: capacity, annotation: map[string]string{}}
}

func (l *Local) Ready() error  { return nil }
func (l *Local) Health() error { return nil }

func (l *Local) Shutdown() error {
	l.mu.Lock()
	l.capacity = l.open
	l.mu.Unlock()
	if l.OnShutdown != nil {
		l.OnShutdown()
	}
	return nil
}

func (l *Local) SetAnnotation(key, value string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.annotation[key] = value
	return nil
}

func (l *Local) Annotation(key string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.annotation[key]
}

func (l *Local) AddRun(runID string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	switch {
	case slices.Contains(l.runs, runID):
		return nil
	case int64(len(l.runs)) >= l.capacity:
		return ErrFull
	}
	l.runs = append(l.runs, runID)
	return nil
}

func (l *Local) RemoveRun(runID string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	i := slices.Index(l.runs, runID)
	if i < 0 {
		return ErrNoRun
	}
	l.runs = slices.Delete(l.runs, i, i+1)
	return nil
}

func (l *Local) Runs() ([]string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.runs), nil
}

func (l *Local) SetRunCapacity(n int64) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.capacity = n
	if n > 0 {
		l.open = n
	}
	return nil
}

func (l *Local) RunCapacity() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.capacity
}
