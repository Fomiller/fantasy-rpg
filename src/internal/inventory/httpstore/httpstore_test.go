package httpstore_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory/httpstore"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory/storetest"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
)

func serve(t *testing.T, store inventory.Store) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	httpstore.Register(mux, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestClientContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) inventory.Store {
		srv := serve(t, inventory.NewMemory())
		return httpstore.NewClient(srv.URL, srv.Client())
	})
}

func TestNamesAreEscaped(t *testing.T) {
	srv := serve(t, inventory.NewMemory())
	c := httpstore.NewClient(srv.URL+"/", nil)
	ctx := t.Context()
	for _, name := range []string{"ann/bob", "a b?c", "%2F"} {
		if err := c.Create(ctx, inventory.Character{Name: name, Class: "warrior", XP: 7}); err != nil {
			t.Fatal(err)
		}
		if got, ok, err := c.Load(ctx, name, "warrior"); err != nil || !ok || got.Name != name || got.XP != 7 {
			t.Fatalf("load %q = %+v, %v, %v", name, got, ok, err)
		}
	}
}

// broken fails every call with an error that is not one of the store's.
type broken struct{ inventory.Store }

func (broken) Load(context.Context, string, string) (inventory.Character, bool, error) {
	return inventory.Character{}, false, errors.New("database is down")
}

func TestOtherErrorsKeepTheirMessage(t *testing.T) {
	srv := serve(t, broken{inventory.NewMemory()})
	_, _, err := httpstore.NewClient(srv.URL, nil).Load(t.Context(), "ann", "warrior")
	if err == nil || !strings.Contains(err.Error(), "database is down") || errors.Is(err, inventory.ErrNoCharacter) {
		t.Fatalf("load = %v", err)
	}
}

func TestBadBody(t *testing.T) {
	srv := serve(t, inventory.NewMemory())
	resp, err := http.Post(srv.URL+"/v1/runs/start", "application/json", strings.NewReader("{"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestServiceDown(t *testing.T) {
	srv := serve(t, inventory.NewMemory())
	srv.Close()
	c := httpstore.NewClient(srv.URL, nil)
	if _, err := c.StartRun(t.Context(), "ann", "warrior", "run", "instance-0@3f9a1c"); err == nil {
		t.Fatal("start run with the service down did not fail")
	}
}

func TestOpenPicksByURL(t *testing.T) {
	srv := serve(t, inventory.NewMemory())
	for _, tt := range []struct{ inventoryURL, want string }{{"", "memory"}, {srv.URL, "inventory"}} {
		s, name, closeStore, err := httpstore.Open(t.Context(), tt.inventoryURL, "", prometheus.NewRegistry())
		if err != nil || name != tt.want {
			t.Fatalf("open %q = %s, %v, want %s", tt.inventoryURL, name, err, tt.want)
		}
		ch := inventory.Character{Name: "ann", Class: "warrior", Stash: []items.Instance{{ID: "x", DefID: "iron_sword", Slot: items.Weapon, Quantity: 1, Stack: 1}}}
		if err := s.Create(t.Context(), ch); err != nil {
			t.Fatalf("create on %s: %v", name, err)
		}
		closeStore()
	}
}
