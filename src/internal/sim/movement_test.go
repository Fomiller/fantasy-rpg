package sim

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the movement fixture")

// movementFixture is replayed by client/godot/tests/movement_test.gd. If the
// client's prediction moves the same way, both sides end in the same spot.
type movementFixture struct {
	DT        float64      `json:"dt"`
	MaxSpeed  float64      `json:"max_speed"`
	Walls     [][4]float64 `json:"walls"`
	Start     [2]float64   `json:"start"`
	Inputs    [][2]float64 `json:"inputs"`
	Positions [][2]float64 `json:"positions"`
}

func repeat(n int, x, y float64) [][2]float64 {
	return slices.Repeat([][2]float64{{x, y}}, n)
}

func TestMovementFixture(t *testing.T) {
	cfg := DefaultConfig()
	f := movementFixture{
		DT:       dt.Seconds(),
		MaxSpeed: cfg.MaxSpeed,
		Inputs: slices.Concat(
			repeat(30, 1, 0),
			repeat(10, 0, -1),
			repeat(30, 1, -0.5),
			repeat(15, 0, 0),
			repeat(40, -0.25, -1),
			repeat(20, 0.5, 0.75),
			repeat(15, 0, 0),
		),
	}
	walls := testRoomWalls()
	for _, r := range walls {
		f.Walls = append(f.Walls, [4]float64{r.Min.X, r.Min.Y, r.Max.X, r.Max.Y})
	}

	w := NewWorld(cfg, walls)
	id := w.AddPlayer(Vec2{})
	for i, in := range f.Inputs {
		w.Submit(MoveCommand{Player: id, Seq: uint32(i + 1), Direction: Vec2{in[0], in[1]}})
		w.Step(dt)
		p, _ := w.Player(id)
		f.Positions = append(f.Positions, [2]float64{p.Pos.X, p.Pos.Y})
	}

	path := filepath.Join("..", "..", "..", "proto", "testdata", "movement.json")
	if *update {
		b, err := json.MarshalIndent(f, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture (run go test -update to create it): %v", err)
	}
	var want movementFixture
	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f, want) {
		t.Fatal("movement.json is stale, run go test -update")
	}
}

func TestOneInputPerTick(t *testing.T) {
	w := NewWorld(DefaultConfig(), nil)
	id := w.AddPlayer(Vec2{})
	for seq := range uint32(3) {
		w.Submit(MoveCommand{Player: id, Seq: seq + 1, Direction: Vec2{1, 0}})
	}
	for want := range uint32(3) {
		steps(w, 1)
		if p, _ := w.Player(id); p.LastInputSeq != want+1 {
			t.Fatalf("after tick %d last input = %d, want %d", want+1, p.LastInputSeq, want+1)
		}
	}
	steps(w, 1)
	if p, _ := w.Player(id); p.LastInputSeq != 3 || p.Vel.X != 220 {
		t.Fatalf("with no new input: seq = %d vel.x = %v, want last input kept", p.LastInputSeq, p.Vel.X)
	}
}

func TestInputQueueDropsOldest(t *testing.T) {
	w := NewWorld(DefaultConfig(), nil)
	id := w.AddPlayer(Vec2{})
	for seq := range uint32(maxQueuedInputs + 2) {
		w.Submit(MoveCommand{Player: id, Seq: seq + 1, Direction: Vec2{1, 0}})
	}
	steps(w, 1)
	if p, _ := w.Player(id); p.LastInputSeq != 3 {
		t.Fatalf("first input used = %d, want 3 after the two oldest were dropped", p.LastInputSeq)
	}
}
