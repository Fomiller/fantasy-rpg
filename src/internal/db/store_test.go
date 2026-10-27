package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory/storetest"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
)

func newStore(t *testing.T, url string) *Store {
	t.Helper()
	s, err := NewStore(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestStoreContract(t *testing.T) {
	t.Parallel()
	storetest.Run(t, func(t *testing.T) inventory.Store { return newStore(t, newDBURL(t, true)) })
}

// A migration that breaks the store shows up here, not in production.
func TestStoreOnFreshlyMigratedDatabase(t *testing.T) {
	t.Parallel()
	storetest.Run(t, func(t *testing.T) inventory.Store {
		url := newDBURL(t, false)
		conn, err := Open(url)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if _, err := Migrate(t.Context(), conn); err != nil {
			t.Fatal(err)
		}
		return newStore(t, url)
	})
}

func sword() items.Instance {
	return items.Instance{ID: "5f0c", DefID: "iron_sword", Name: "Iron Sword", Slot: items.Weapon, WeaponType: "sword", Rarity: items.Uncommon, Level: 2,
		Base: map[items.Stat]int{"damage": 15}, Affixes: []items.Affix{{ID: "sharp", Name: "Sharp", Stat: "damage", Tier: 1, Value: 2}}, Quantity: 1, Stack: 1}
}

// The saved shape is the contract with every row already in the database.
// Changing it needs a migration that rewrites the JSON.
func TestItemJSONShape(t *testing.T) {
	b, err := items.EncodeJSON([]items.Instance{sword()})
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"id":"5f0c","def":"iron_sword","name":"Iron Sword","slot":"weapon","weapon":"sword","rarity":"uncommon","level":2,` +
		`"base":{"damage":15},"affixes":[{"id":"sharp","name":"Sharp","stat":"damage","tier":1,"value":2}],"qty":1,"stack":1}]`
	if string(b) != want {
		t.Fatalf("encoded\n%s\nwant\n%s", b, want)
	}
	got, err := items.DecodeJSON(b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []items.Instance{sword()}) {
		t.Fatalf("decoded %+v", got)
	}
}

func TestEmptyListsAreArrays(t *testing.T) {
	b, err := items.EncodeJSON(nil)
	if err != nil || string(b) != "[]" {
		t.Fatalf("encoded nil as %s, %v", b, err)
	}
}

func TestUnknownRarityFailsLoad(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := newStore(t, newDBURL(t, true))
	if err := s.Create(ctx, inventory.Character{Name: "ann", Class: "warrior"}); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal([]map[string]any{{"id": "x", "def": "iron_sword", "slot": "weapon", "rarity": "legendary", "qty": 1, "stack": 1}})
	if _, err := s.pool.Exec(ctx, "UPDATE characters SET bag = $1", b); err != nil {
		t.Fatal(err)
	}
	_, _, err := s.Load(ctx, "ann", "warrior")
	if err == nil || !strings.Contains(err.Error(), "legendary") {
		t.Fatalf("load = %v, want an unknown rarity error", err)
	}
}

func TestRunIDMustBeAUUID(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := newStore(t, newDBURL(t, true))
	c := inventory.Character{Name: "ann", Class: "warrior"}
	if err := s.Create(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartRun(ctx, "ann", "warrior", "not-a-uuid", "instance-0@3f9a1c"); err == nil {
		t.Fatal("started a run with a run id that is not a uuid")
	}
	if err := s.EndRun(ctx, "not-a-uuid", c); err == nil {
		t.Fatal("ended a run with a run id that is not a uuid")
	}
}

func TestOpenStorePicksByConfig(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ url, store, want string }{{"", Memory, "memory"}, {newDBURL(t, true), "", "postgres"}, {newDBURL(t, true), Memory, "postgres"}} {
		s, name, closeStore, err := OpenStore(t.Context(), tt.url, tt.store, prometheus.NewRegistry())
		if err != nil || name != tt.want {
			t.Fatalf("open %q STORE=%q = %s, %v, want %s", tt.url, tt.store, name, err, tt.want)
		}
		if _, _, err := s.Load(t.Context(), "ann", "warrior"); err != nil {
			t.Fatalf("load on %s: %v", name, err)
		}
		closeStore()
	}
}

func TestOpenStoreFailsClosed(t *testing.T) {
	t.Parallel()
	if _, _, _, err := OpenStore(t.Context(), "", "", prometheus.NewRegistry()); !errors.Is(err, ErrNoStore) {
		t.Fatalf("open with no config = %v, want ErrNoStore", err)
	}
	if _, _, _, err := OpenStore(t.Context(), "", "disk", prometheus.NewRegistry()); err == nil {
		t.Fatal("open with STORE=disk did not fail")
	}
}

// Two inventory replicas are two pools on one database. Only one claim for a
// name may win across them.
func TestClaimAcrossReplicas(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	url := newDBURL(t, true)
	replicas := []*Store{newStore(t, url), newStore(t, url)}
	for i := range 2 {
		if err := replicas[0].Heartbeat(ctx, fmt.Sprint("world-", i), "b0", time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	for round := range 20 {
		name := fmt.Sprint("p", round)
		var won atomic.Int32
		var wg sync.WaitGroup
		for i, s := range replicas {
			wg.Go(func() {
				err := s.Claim(ctx, inventory.Session{Name: name, ID: fmt.Sprint("s", i), Shard: fmt.Sprint("world-", i), Boot: "b0"})
				switch {
				case err == nil:
					won.Add(1)
				case !errors.Is(err, inventory.ErrPlaying):
					t.Errorf("claim %s on replica %d = %v", name, i, err)
				}
			})
		}
		wg.Wait()
		if won.Load() != 1 {
			t.Fatalf("%s: %d claims won, want 1", name, won.Load())
		}
	}
}
