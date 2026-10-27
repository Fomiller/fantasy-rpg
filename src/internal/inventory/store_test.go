package inventory_test

import (
	"context"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory/storetest"
)

func TestMemoryStore(t *testing.T) {
	storetest.Run(t, func(*testing.T) inventory.Store { return inventory.NewMemory() })
}

func TestMeteredStore(t *testing.T) {
	storetest.Run(t, func(*testing.T) inventory.Store {
		return inventory.Metered(inventory.NewMemory(), prometheus.NewRegistry())
	})
}

func TestMeteredCountsErrors(t *testing.T) {
	reg := prometheus.NewRegistry()
	s := inventory.Metered(inventory.NewMemory(), reg)
	ctx := context.Background()
	s.Load(ctx, "ann", "warrior")
	if err := s.SaveLobby(ctx, "ann", "warrior", "s1", inventory.Loadout{}, nil); err == nil {
		t.Fatal("saved a lobby for a character that does not exist")
	}
	if n := testutil.CollectAndCount(reg, "store_duration_seconds"); n != 2 {
		t.Fatalf("%d duration series, want load and save_lobby", n)
	}
	want := `
# HELP store_errors_total Store calls that returned an error.
# TYPE store_errors_total counter
store_errors_total{call="save_lobby"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "store_errors_total"); err != nil {
		t.Fatal(err)
	}
}
