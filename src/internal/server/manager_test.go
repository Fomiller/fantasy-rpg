package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/jointoken"
)

// fakeManager answers like the instance manager. allocate and instance set
// the status and body of each route.
func fakeManager(t *testing.T, allocate func(runs []string) (int, any), instance func(id string) (int, any)) *InstanceManager {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/allocate", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Runs []string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("allocate body: %v", err)
		}
		status, body := allocate(req.Runs)
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(body)
	})
	mux.HandleFunc("GET /v1/instances/{id}", func(w http.ResponseWriter, r *http.Request) {
		status, body := instance(r.PathValue("id"))
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return NewInstanceManager(srv.URL + "/")
}

func TestInstanceManagerReserve(t *testing.T) {
	var got []string
	status := http.StatusOK
	m := fakeManager(t, func(runs []string) (int, any) {
		got = runs
		return status, managedInstance{ID: "gs-abc", Boot: "b1", URL: "wss://host/instances/gs-abc/ws", Runs: runs}
	}, nil)

	p, err := m.Reserve(t.Context(), []string{"r1", "r2"}, time.Now())
	if err != nil || p != (Placement{ID: "gs-abc", Boot: "b1", URL: "wss://host/instances/gs-abc/ws"}) {
		t.Fatalf("reserve = %+v, %v", p, err)
	}
	if !slices.Equal(got, []string{"r1", "r2"}) {
		t.Fatalf("manager got runs %v", got)
	}

	status = http.StatusConflict
	if _, err := m.Reserve(t.Context(), []string{"r3"}, time.Now()); !errors.Is(err, ErrFull) {
		t.Fatalf("409 = %v, want ErrFull", err)
	}
	status = http.StatusServiceUnavailable
	if _, err := m.Reserve(t.Context(), []string{"r3"}, time.Now()); err == nil || errors.Is(err, ErrFull) {
		t.Fatalf("503 = %v, want an error that is not full", err)
	}
	down := NewInstanceManager("http://127.0.0.1:1")
	if _, err := down.Reserve(t.Context(), []string{"r3"}, time.Now()); err == nil {
		t.Fatal("reserve with no manager answering worked")
	}
}

func TestInstanceManagerDeadLease(t *testing.T) {
	now := time.Now()
	old, young := now.Add(-2*deadLeaseAge), now.Add(-time.Second)
	m := fakeManager(t, nil, func(id string) (int, any) {
		switch id {
		case "live":
			return http.StatusOK, managedInstance{ID: id, Boot: "1", Runs: []string{"run"}}
		case "empty":
			return http.StatusOK, managedInstance{ID: id, Boot: "1", Runs: []string{}}
		case "broken":
			return http.StatusServiceUnavailable, nil
		}
		return http.StatusNotFound, nil
	})
	for _, tc := range []struct {
		name  string
		lease inventory.Lease
		dead  bool
	}{
		{"no lease", inventory.Lease{}, false},
		{"live run", inventory.Lease{RunID: "run", Instance: "live@1", Since: old}, false},
		{"later boot", inventory.Lease{RunID: "run", Instance: "live@0", Since: young}, true},
		{"run gone, token may still land", inventory.Lease{RunID: "run", Instance: "empty@1", Since: young}, false},
		{"run gone long ago", inventory.Lease{RunID: "run", Instance: "empty@1", Since: old}, true},
		{"gameserver gone", inventory.Lease{RunID: "run", Instance: "gone@1", Since: young}, true},
		{"manager failing", inventory.Lease{RunID: "run", Instance: "broken@1", Since: old}, false},
	} {
		if got := m.DeadLease(t.Context(), tc.lease, now); got != tc.dead {
			t.Errorf("%s: dead = %v, want %v", tc.name, got, tc.dead)
		}
	}
	down := NewInstanceManager("http://127.0.0.1:1")
	if down.DeadLease(t.Context(), inventory.Lease{RunID: "run", Instance: "gone@1", Since: old}, now) {
		t.Error("cleared a lease on a manager that does not answer")
	}
}

func TestNewPool(t *testing.T) {
	list := []Instance{{ID: "instance-0", Internal: "http://a", Public: "ws://a"}}
	if d, ok := NewPool("", list).(*Directory); !ok || len(d.list) != 1 {
		t.Fatalf("no manager url = %T, want the static directory", NewPool("", list))
	}
	if _, ok := NewPool("http://manager", list).(*InstanceManager); !ok {
		t.Fatalf("manager url = %T, want the manager", NewPool("http://manager", list))
	}
}

// The entrance sends a ticket for what the manager allocated, with the
// token bound to its id and boot.
func TestWorldTicketFromTheManager(t *testing.T) {
	m := fakeManager(t, func(runs []string) (int, any) {
		return http.StatusOK, managedInstance{ID: "gs-abc", Boot: "b1", URL: "wss://host/instances/gs-abc/ws", Runs: runs}
	}, nil)
	h := serveWorld(t, func(l *Lobby) {
		l.Instances = m
		l.JoinKey = testKey
		l.closeGrace = 10 * time.Millisecond
	})
	conn := dial(t, h.url)
	lobbyAs(t, conn, "ann")
	ticket := enterTicket(t, conn)
	if ticket.GetUrl() != "wss://host/instances/gs-abc/ws" || ticket.GetInstance() != "gs-abc" {
		t.Fatalf("ticket = %v", ticket)
	}
	if _, err := jointoken.NewVerifier(testKey).Check(ticket.GetToken(), "gs-abc@b1", time.Now()); err != nil {
		t.Fatalf("token: %v", err)
	}
}

// A manager that is down rejects the entrance as full.
func TestWorldEnterFullWhenManagerFails(t *testing.T) {
	m := fakeManager(t, func([]string) (int, any) { return http.StatusServiceUnavailable, nil }, nil)
	h := serveWorld(t, func(l *Lobby) { l.Instances = m })
	conn := dial(t, h.url)
	lobbyAs(t, conn, "ann")
	if r := enterRejected(t, conn); r != EnterFull {
		t.Fatalf("enter rejected = %q, want full", r)
	}
}
