package db_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/Fomiller/fantasy-rpg/src/internal/db"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory/httpstore"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory/storetest"
)

// The inventory service as world and instance servers see it.
func TestStoreContractOverHTTP(t *testing.T) {
	t.Parallel()
	storetest.Run(t, func(t *testing.T) inventory.Store {
		s, err := db.NewStore(t.Context(), db.NewDBURL(t, true))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(s.Close)
		mux := http.NewServeMux()
		httpstore.Register(mux, s, slog.New(slog.NewTextHandler(io.Discard, nil)))
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)
		return httpstore.NewClient(srv.URL, srv.Client())
	})
}

// With INVENTORY_URL empty, world and instance servers open Postgres
// themselves.
func TestOpenFallsToDatabaseURL(t *testing.T) {
	t.Parallel()
	s, name, closeStore, err := httpstore.Open(t.Context(), "", db.NewDBURL(t, true), "", prometheus.NewRegistry())
	if err != nil || name != "postgres" {
		t.Fatalf("open = %s, %v, want postgres", name, err)
	}
	defer closeStore()
	if _, _, err := s.Load(t.Context(), "ann", "warrior"); err != nil {
		t.Fatal(err)
	}
}
