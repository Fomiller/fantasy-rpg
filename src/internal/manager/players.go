package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	agonesv1 "agones.dev/agones/pkg/apis/agones/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const playersTimeout = 2 * time.Second

// Players is every cave's head count, as each instance server's
// /v1/players says it.
type Players struct {
	Humans int `json:"humans"`
	Bots   int `json:"bots"`
	// Unreachable is the Allocated GameServers that did not answer. Their
	// players are not in the count.
	Unreachable int `json:"unreachable"`
}

// Players asks every Allocated GameServer in the Fleet at once. A Ready one
// has nobody in it.
func (m *Manager) Players(ctx context.Context) (Players, error) {
	list, err := m.agones.AgonesV1().GameServers(m.cfg.Namespace).List(ctx, metav1.ListOptions{LabelSelector: agonesv1.FleetNameLabel + "=" + m.cfg.Fleet})
	if err != nil {
		return Players{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, playersTimeout)
	defer cancel()
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		out Players
	)
	for i := range list.Items {
		gs := &list.Items[i]
		if gs.DeletionTimestamp != nil || gs.Status.State != agonesv1.GameServerStateAllocated {
			continue
		}
		wg.Go(func() {
			p, err := m.instancePlayers(ctx, gs)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				m.log.Warn("instance players", "gameserver", gs.Name, "err", err)
				out.Unreachable++
				return
			}
			out.Humans += p.Humans
			out.Bots += p.Bots
		})
	}
	wg.Wait()
	return out, nil
}

func (m *Manager) instancePlayers(ctx context.Context, gs *agonesv1.GameServer) (Players, error) {
	url := m.instanceURL(gs) + "/v1/players"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Players{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Players{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Players{}, fmt.Errorf("%s: %s", url, resp.Status)
	}
	var p Players
	return p, json.NewDecoder(resp.Body).Decode(&p)
}

// internalURL is the GameServer's own Service, which EnsureRoutes made.
func (m *Manager) internalURL(gs *agonesv1.GameServer) string {
	return fmt.Sprintf("http://%s.%s.svc:%d", gs.Name, m.cfg.Namespace, containerPort(gs))
}
