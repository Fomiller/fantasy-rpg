// Package manager is the instance manager: it allocates Agones GameServers
// for parties walking into a cave, and gives each GameServer a Service and an
// IngressRoute owned by it.
package manager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	agonesv1 "agones.dev/agones/pkg/apis/agones/v1"
	allocationv1 "agones.dev/agones/pkg/apis/allocation/v1"
	"agones.dev/agones/pkg/client/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// RunsList is the GameServer list that holds one value per run id.
const RunsList = "runs"

// BootAnnotation is where the instance server's boot id lands. The SDK
// prefixes the key it sets.
const BootAnnotation = "agones.dev/sdk-boot"

var ErrFull = errors.New("no instance has room")

type Config struct {
	Namespace string
	// Fleet limits allocation to the GameServers of one Fleet.
	Fleet string
	// PublicURL is the WebSocket base a client dials, like
	// wss://fantasy-rpg.fomiller.com.
	PublicURL string
	// Host and EntryPoint go on each IngressRoute. No host matches any.
	Host       string
	EntryPoint string
}

type Manager struct {
	cfg     Config
	agones  versioned.Interface
	kube    kubernetes.Interface
	dynamic dynamic.Interface
	log     *slog.Logger
	metrics *Metrics
	// instanceURL is where a GameServer's internal port is. Tests point it
	// at a local server.
	instanceURL func(*agonesv1.GameServer) string
}

func New(cfg Config, agones versioned.Interface, kube kubernetes.Interface, dyn dynamic.Interface, metrics *Metrics, log *slog.Logger) *Manager {
	m := &Manager{cfg: cfg, agones: agones, kube: kube, dynamic: dyn, metrics: metrics, log: log}
	m.instanceURL = m.internalURL
	return m
}

// Instance is a GameServer as the world server sees it.
type Instance struct {
	ID    string   `json:"instance"`
	Boot  string   `json:"boot"`
	URL   string   `json:"url"`
	State string   `json:"state,omitempty"`
	Runs  []string `json:"runs"`
}

// Allocate takes a seat per run on one GameServer, in one atomic update.
func (m *Manager) Allocate(ctx context.Context, runs []string) (Instance, error) {
	start := time.Now()
	inst, result, err := m.allocate(ctx, runs)
	m.metrics.allocation.WithLabelValues(result).Observe(time.Since(start).Seconds())
	return inst, err
}

func (m *Manager) allocate(ctx context.Context, runs []string) (Instance, string, error) {
	gsa, err := m.agones.AllocationV1().GameServerAllocations(m.cfg.Namespace).Create(ctx, m.allocation(runs), metav1.CreateOptions{})
	if err != nil {
		m.metrics.routingErrors.WithLabelValues("allocate").Inc()
		return Instance{}, "error", fmt.Errorf("allocate: %w", err)
	}
	switch gsa.Status.State {
	case allocationv1.GameServerAllocationAllocated:
	case allocationv1.GameServerAllocationUnAllocated:
		m.metrics.routingErrors.WithLabelValues("full").Inc()
		return Instance{}, "full", ErrFull
	default:
		m.metrics.routingErrors.WithLabelValues("allocate").Inc()
		return Instance{}, "error", fmt.Errorf("allocate: state %s", gsa.Status.State)
	}

	name := gsa.Status.GameServerName
	var boot string
	if gsa.Status.Metadata != nil {
		boot = gsa.Status.Metadata.Annotations[BootAnnotation]
	}
	if boot == "" {
		m.metrics.routingErrors.WithLabelValues("allocate").Inc()
		return Instance{}, "error", fmt.Errorf("allocated %s has no %s annotation", name, BootAnnotation)
	}
	// A Ready server has no runs, so more values than this party's means the
	// party joined a cave already running.
	values := gsa.Status.Lists[RunsList].Values
	result := "ready"
	if len(values) > len(runs) {
		result = "reused"
	}

	gs, err := m.agones.AgonesV1().GameServers(m.cfg.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		m.metrics.routingErrors.WithLabelValues("route").Inc()
		return Instance{}, "error", fmt.Errorf("get %s: %w", name, err)
	}
	if err := m.EnsureRoutes(ctx, gs); err != nil {
		return Instance{}, "error", err
	}
	return Instance{ID: name, Boot: boot, URL: m.url(name), State: string(agonesv1.GameServerStateAllocated), Runs: values}, result, nil
}

// allocation asks first for a running cave with room for the whole party,
// fullest first, so one cave fills before the next. Then for a warm one.
func (m *Manager) allocation(runs []string) *allocationv1.GameServerAllocation {
	allocated, ready := agonesv1.GameServerStateAllocated, agonesv1.GameServerStateReady
	fleet := metav1.LabelSelector{MatchLabels: map[string]string{agonesv1.FleetNameLabel: m.cfg.Fleet}}
	return &allocationv1.GameServerAllocation{
		ObjectMeta: metav1.ObjectMeta{Namespace: m.cfg.Namespace},
		Spec: allocationv1.GameServerAllocationSpec{
			Selectors: []allocationv1.GameServerSelector{
				{
					LabelSelector:   fleet,
					GameServerState: &allocated,
					Lists:           map[string]allocationv1.ListSelector{RunsList: {MinAvailable: int64(len(runs))}},
				},
				{LabelSelector: fleet, GameServerState: &ready},
			},
			// Ascending sorts by available capacity, so the fullest comes first.
			Priorities: []agonesv1.Priority{{Type: agonesv1.GameServerPriorityList, Key: RunsList, Order: agonesv1.GameServerPriorityAscending}},
			Lists:      map[string]allocationv1.ListAction{RunsList: {AddValues: slices.Clone(runs)}},
		},
	}
}

// Instance reads one GameServer. ok is false once it is gone.
func (m *Manager) Instance(ctx context.Context, name string) (Instance, bool, error) {
	gs, err := m.agones.AgonesV1().GameServers(m.cfg.Namespace).Get(ctx, name, metav1.GetOptions{})
	switch {
	case notFound(err):
		return Instance{}, false, nil
	case err != nil:
		return Instance{}, false, err
	}
	runs := gs.Status.Lists[RunsList].Values
	if runs == nil {
		runs = []string{}
	}
	return Instance{
		ID:    gs.Name,
		Boot:  gs.Annotations[BootAnnotation],
		URL:   m.url(gs.Name),
		State: string(gs.Status.State),
		Runs:  runs,
	}, true, nil
}

func (m *Manager) url(name string) string {
	return m.cfg.PublicURL + "/instances/" + name + "/ws"
}
