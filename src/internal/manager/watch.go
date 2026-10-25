package manager

import (
	"context"
	"time"

	agonesv1 "agones.dev/agones/pkg/apis/agones/v1"
	"k8s.io/client-go/tools/cache"
)

const routeTimeout = 10 * time.Second

// Watch routes every GameServer that is Ready or Allocated, so a warm one is
// routed before anyone needs it. Add it to a GameServer informer.
func (m *Manager) Watch(informer cache.SharedIndexInformer) error {
	_, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			if gs, ok := obj.(*agonesv1.GameServer); ok && routable(gs) {
				m.route(gs)
			}
		},
		UpdateFunc: func(old, cur any) {
			before, ok1 := old.(*agonesv1.GameServer)
			gs, ok2 := cur.(*agonesv1.GameServer)
			if !ok1 || !ok2 || before.Status.State == gs.Status.State {
				return
			}
			// Only a change seen live counts. A list after a restart would
			// count servers that went Ready long ago.
			if gs.Status.State == agonesv1.GameServerStateReady {
				m.metrics.startup.Observe(time.Since(gs.CreationTimestamp.Time).Seconds())
			}
			if routable(gs) {
				m.route(gs)
			}
		},
	})
	return err
}

func (m *Manager) route(gs *agonesv1.GameServer) {
	ctx, cancel := context.WithTimeout(context.Background(), routeTimeout)
	defer cancel()
	if err := m.EnsureRoutes(ctx, gs); err != nil {
		m.log.Error("route gameserver", "gameserver", gs.Name, "err", err)
	}
}

func routable(gs *agonesv1.GameServer) bool {
	return gs.DeletionTimestamp == nil &&
		(gs.Status.State == agonesv1.GameServerStateReady || gs.Status.State == agonesv1.GameServerStateAllocated)
}
