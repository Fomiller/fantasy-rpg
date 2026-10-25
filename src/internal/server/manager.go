package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
)

const managerTimeout = 5 * time.Second

// InstanceManager is the pool when INSTANCE_MANAGER_URL is set. Agones keeps
// the seat counts, so there is nothing to poll.
type InstanceManager struct {
	base   string
	client *http.Client
}

func NewInstanceManager(base string) *InstanceManager {
	return &InstanceManager{base: strings.TrimSuffix(base, "/"), client: &http.Client{Timeout: managerTimeout}}
}

// NewPool asks the instance manager when managerURL is set. Otherwise it is
// the static pool from INSTANCES, so local dev needs no Kubernetes.
func NewPool(managerURL string, list []Instance) Pool {
	if managerURL != "" {
		return NewInstanceManager(managerURL)
	}
	return NewDirectory(list)
}

type managedInstance struct {
	ID   string   `json:"instance"`
	Boot string   `json:"boot"`
	URL  string   `json:"url"`
	Runs []string `json:"runs"`
}

func (m *InstanceManager) Reserve(ctx context.Context, runs []string, _ time.Time) (Placement, error) {
	body, err := json.Marshal(map[string][]string{"runs": runs})
	if err != nil {
		return Placement{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.base+"/v1/allocate", bytes.NewReader(body))
	if err != nil {
		return Placement{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	var inst managedInstance
	status, err := m.do(req, &inst)
	switch {
	case err != nil:
		return Placement{}, err
	case status == http.StatusConflict:
		return Placement{}, ErrFull
	case status != http.StatusOK:
		return Placement{}, fmt.Errorf("allocate: status %d", status)
	case inst.ID == "" || inst.Boot == "" || inst.URL == "":
		return Placement{}, fmt.Errorf("allocate answered %+v", inst)
	}
	return Placement{ID: inst.ID, Boot: inst.Boot, URL: inst.URL}, nil
}

// DeadLease is true when the GameServer is gone or is a later boot, or has
// not listed the run long after the token expired. A manager that does not
// answer is waited on.
func (m *InstanceManager) DeadLease(ctx context.Context, l inventory.Lease, now time.Time) bool {
	if !l.Held() {
		return false
	}
	id, boot, _ := strings.Cut(l.Instance, "@")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.base+"/v1/instances/"+url.PathEscape(id), nil)
	if err != nil {
		return false
	}
	var inst managedInstance
	status, err := m.do(req, &inst)
	switch {
	case err != nil:
		return false
	case status == http.StatusNotFound:
		return true
	case status != http.StatusOK:
		return false
	case inst.Boot != "" && inst.Boot != boot:
		return true
	}
	return !slices.Contains(inst.Runs, l.RunID) && now.Sub(l.Since) > deadLeaseAge
}

// do decodes a 200 body into out and reports the status.
func (m *InstanceManager) do(req *http.Request, out any) (int, error) {
	resp, err := m.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil
	}
	return resp.StatusCode, json.NewDecoder(resp.Body).Decode(out)
}
