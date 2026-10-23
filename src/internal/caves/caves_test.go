package caves

import (
	"slices"
	"testing"
	"time"

	"github.com/Fomiller/fantasy-rpg/src/internal/items"
	"github.com/Fomiller/fantasy-rpg/src/internal/sim"
)

func TestLoopParses(t *testing.T) {
	c := Loop()
	if len(c.PlayerSpawns) != 2 || len(c.Enemies) != 5 || len(c.Chests) != 3 {
		t.Fatalf("spawns = %d, enemies = %d, chests = %d", len(c.PlayerSpawns), len(c.Enemies), len(c.Chests))
	}
	if c.Bounds.Max != (sim.Vec2{X: 31 * TileSize, Y: 16 * TileSize}) {
		t.Fatalf("bounds = %v", c.Bounds)
	}
	if c.Extraction != (sim.Rect{Min: sim.Vec2{X: 24 * TileSize, Y: TileSize}, Max: sim.Vec2{X: 27 * TileSize, Y: 3 * TileSize}}) {
		t.Fatalf("extraction = %v", c.Extraction)
	}
	if len(c.Walls) > 40 {
		t.Fatalf("%d wall rects, merge is not working", len(c.Walls))
	}
}

func TestWallsCoverEveryWallTileOnce(t *testing.T) {
	c := Loop()
	rows := splitArt(loopArt)
	for y, row := range rows {
		for x, ch := range row {
			center := sim.Vec2{X: (float64(x) + 0.5) * TileSize, Y: (float64(y) + 0.5) * TileSize}
			n := 0
			for _, w := range c.Walls {
				if w.Contains(center) {
					n++
				}
			}
			if want := map[bool]int{true: 1, false: 0}[ch == '#']; n != want {
				t.Fatalf("tile %d,%d (%q) is in %d walls, want %d", x, y, ch, n, want)
			}
		}
	}
}

// A player must be able to walk from spawn to the exit, using the same
// collision as the game.
func TestSpawnCanReachExtraction(t *testing.T) {
	c := Loop()
	rows := splitArt(loopArt)
	type cell struct{ x, y int }
	start := cell{int(c.PlayerSpawns[0].X / TileSize), int(c.PlayerSpawns[0].Y / TileSize)}
	seen := map[cell]bool{start: true}
	queue := []cell{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if rows[cur.y][cur.x] == 'X' {
			return
		}
		for _, d := range []cell{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			n := cell{cur.x + d.x, cur.y + d.y}
			if !seen[n] && rows[n.y][n.x] != '#' {
				seen[n] = true
				queue = append(queue, n)
			}
		}
	}
	t.Fatal("extraction is not reachable from spawn")
}

func TestParseErrors(t *testing.T) {
	tests := map[string]string{
		"ragged":     "###\n#S\n###",
		"unknown":    "####\n#S?#\n####",
		"no spawn":   "####\n#.X#\n####",
		"no exit":    "####\n#S.#\n####",
		"split exit": "#####\n#XSX#\n#####",
	}
	for name, art := range tests {
		if _, err := Parse(name, art); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestNewWorldPlacesEnemiesAndChests(t *testing.T) {
	c := Loop()
	w := c.NewWorld(sim.DefaultConfig(), items.NewRoller(items.Default(), 1))
	if got := len(w.Enemies()); got != len(c.Enemies) {
		t.Fatalf("world has %d enemies, want %d", got, len(c.Enemies))
	}
	piles := w.Piles()
	if len(piles) != len(c.Chests) {
		t.Fatalf("world has %d piles, want %d chests", len(piles), len(c.Chests))
	}
	for _, p := range piles {
		if !p.Chest || len(p.Items) < 2 {
			t.Fatalf("chest %+v, want at least two items", p)
		}
	}
}

// Walks a player through the real collision both ways around the loop. Each
// route must reach the exit, so the cave is a loop and not a dead end.
func TestBothRoutesReachExtraction(t *testing.T) {
	tile := func(x, y float64) sim.Vec2 { return sim.Vec2{X: (x + 0.5) * TileSize, Y: (y + 0.5) * TileSize} }
	routes := map[string][]sim.Vec2{
		"bottom and right": {tile(3, 13), tile(26, 13), tile(26, 3), tile(25, 2)},
		"left and top":     {tile(3, 13), tile(3, 3), tile(25, 3), tile(25, 2)},
	}
	for name, route := range routes {
		t.Run(name, func(t *testing.T) {
			cfg := sim.DefaultConfig()
			cfg.DetectRadius = 0
			w := Loop().NewWorld(cfg, nil)
			id := w.AddPlayer(Loop().PlayerSpawns[0])

			next, seq := 0, uint32(0)
			for range 2000 {
				p, ok := w.Player(id)
				if !ok {
					t.Fatal("player left the world without extracting")
				}
				if p.Pos.DistanceTo(route[next]) < 8 && next < len(route)-1 {
					next++
				}
				d := route[next].Sub(p.Pos)
				seq++
				w.Submit(sim.MoveCommand{Player: id, Seq: seq, Direction: d.Scale(1 / max(d.Len(), 1))})
				if ev := w.Step(50 * time.Millisecond); slices.Contains(ev.Extracted, id) {
					return
				}
			}
			p, _ := w.Player(id)
			t.Fatalf("stuck at %v heading for waypoint %d %v", p.Pos, next, route[next])
		})
	}
}
