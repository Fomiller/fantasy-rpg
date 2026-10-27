package inventory

import (
	"bytes"
	"io"
	"log/slog"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/Fomiller/fantasy-rpg/src/internal/events"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
)

func rolled() items.Instance {
	return items.Instance{
		ID: "r", DefID: "iron_sword", Name: "Keen Iron Sword", Slot: items.Weapon, WeaponType: "sword", Rarity: items.Rare, Level: 3,
		Base: map[items.Stat]int{"damage": 7}, Affixes: []items.Affix{{ID: "keen", Name: "Keen", Stat: "crit", Tier: 2, Value: 4}}, Quantity: 1, Stack: 1,
	}
}

func sameItems(t *testing.T, what string, got, want []items.Instance) {
	t.Helper()
	g, err := items.EncodeJSON(got)
	if err != nil {
		t.Fatal(err)
	}
	w, err := items.EncodeJSON(want)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(g, w) {
		t.Fatalf("%s = %s, want %s", what, g, w)
	}
}

func TestRunEndRoundTrips(t *testing.T) {
	want := RunEnd{
		RunID: "run-1", Instance: "instance-0@3f9a1c", Outcome: Died, Time: time.Unix(1700000000, 0).UTC(),
		Character: Character{Name: "ann", Class: "warrior", XP: 120, Loadout: Loadout{Equipment: []items.Instance{rolled()}, Items: []items.Instance{potions("p", 3)}}, Stash: []items.Instance{sword("s")}},
	}
	msg := want.Event()
	if msg.GetDeaths() != 1 {
		t.Fatalf("deaths = %d, want 1 for a death", msg.GetDeaths())
	}
	got, err := RunEndOf(msg)
	if err != nil {
		t.Fatal(err)
	}
	if got.RunID != want.RunID || got.Instance != want.Instance || got.Outcome != want.Outcome || !got.Time.Equal(want.Time) ||
		got.Character.Name != "ann" || got.Character.Class != "warrior" || got.Character.XP != 120 {
		t.Fatalf("run end = %+v, want %+v", got, want)
	}
	sameItems(t, "equipment", got.Character.Loadout.Equipment, want.Character.Loadout.Equipment)
	sameItems(t, "bag", got.Character.Loadout.Items, want.Character.Loadout.Items)
	sameItems(t, "stash", got.Character.Stash, want.Character.Stash)
}

func payload(t *testing.T, e RunEnd) []byte {
	t.Helper()
	b, err := proto.Marshal(e.Event())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The same run.ended twice writes once. The second is the retry no-op even
// after a lobby move changed the stash.
func TestRunEndedHandlerIsIdempotent(t *testing.T) {
	ctx := t.Context()
	m := NewMemory()
	if err := m.Create(ctx, Character{Name: "ann", Class: "warrior"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.StartRun(ctx, "ann", "warrior", "run-1", "instance-0@3f9a1c"); err != nil {
		t.Fatal(err)
	}
	handle := RunEndedHandler(m, slog.New(slog.NewTextHandler(io.Discard, nil)))
	b := payload(t, RunEnd{RunID: "run-1", Outcome: Extracted, Character: Character{Name: "ann", Class: "warrior", XP: 50, Stash: []items.Instance{sword("loot")}}})
	if err := handle(ctx, "run-1", b); err != nil {
		t.Fatal(err)
	}
	if err := m.Claim(ctx, Session{Name: "ann", ID: "s1", Shard: "world-0", Boot: "b1"}); err != nil {
		t.Fatal(err)
	}
	if err := m.SaveLobby(ctx, "ann", "warrior", "s1", Loadout{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := handle(ctx, "run-1", b); err != nil {
		t.Fatalf("second delivery = %v, want the no-op", err)
	}
	c, _, err := m.Load(ctx, "ann", "warrior")
	if err != nil {
		t.Fatal(err)
	}
	if c.XP != 50 || len(c.Stash) != 0 || c.Run.Held() {
		t.Fatalf("after two deliveries = %+v, want XP 50, the lobby's empty stash and no lease", c)
	}
}

// A result that can never apply is dropped, not retried.
func TestRunEndedHandlerDrops(t *testing.T) {
	ctx := t.Context()
	m := NewMemory()
	if err := m.Create(ctx, Character{Name: "ann", Class: "warrior"}); err != nil {
		t.Fatal(err)
	}
	handle := RunEndedHandler(m, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for name, b := range map[string][]byte{
		"garbage":    []byte("not proto"),
		"no lease":   payload(t, RunEnd{RunID: "run-1", Character: Character{Name: "ann", Class: "warrior"}}),
		"no account": payload(t, RunEnd{RunID: "run-1", Character: Character{Name: "bob", Class: "warrior"}}),
	} {
		err := handle(ctx, "run-1", b)
		if !events.Dropped(err) {
			t.Fatalf("%s: %v, want dropped", name, err)
		}
	}
}
