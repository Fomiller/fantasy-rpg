package caves

import (
	"slices"
	"testing"
	"time"

	"github.com/Fomiller/fantasy-rpg/src/internal/classes"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
	"github.com/Fomiller/fantasy-rpg/src/internal/sim"
)

func TestLoopParses(t *testing.T) {
	c := Loop()
	if len(c.PlayerSpawns) != 2 || len(c.Enemies) != 5 || len(c.Chests) != 3 {
		t.Fatalf("spawns = %d, enemies = %d, chests = %d", len(c.PlayerSpawns), len(c.Enemies), len(c.Chests))
	}
	kinds := map[sim.ArchetypeID]int{}
	for _, e := range c.Enemies {
		kinds[e.Archetype]++
	}
	if kinds[sim.Melee] != 2 || kinds[sim.Ranged] != 2 || kinds[sim.Elite] != 1 {
		t.Fatalf("archetypes = %v, want 2 melee, 2 ranged and an elite", kinds)
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

// The yard's chest and exit lie straight right of the spawn, so a walk
// along that row loots the chest and extracts.
func TestYardIsOneStraightWalk(t *testing.T) {
	c, ok := ByName("yard")
	if !ok || len(c.Enemies) != 0 || len(c.PlayerSpawns) != 1 || len(c.Chests) != 1 {
		t.Fatalf("yard = %+v", c)
	}
	spawn, chest := c.PlayerSpawns[0], c.Chests[0]
	if chest.Y != spawn.Y || chest.X <= spawn.X || !c.Extraction.Contains(sim.Vec2{X: c.Extraction.Min.X + 1, Y: spawn.Y}) || c.Extraction.Min.X <= chest.X {
		t.Fatalf("spawn %v, chest %v, exit %v", spawn, chest, c.Extraction)
	}
	if _, ok := ByName("nope"); ok {
		t.Fatal("unknown cave found")
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
	cfg := sim.DefaultConfig()
	w := c.NewWorld(cfg, items.NewRoller(items.Default(), 1))
	enemies := w.Enemies()
	if len(enemies) != len(c.Enemies) {
		t.Fatalf("world has %d enemies, want %d", len(enemies), len(c.Enemies))
	}
	for i, e := range enemies {
		if e.Archetype != c.Enemies[i].Archetype || e.Health != cfg.Archetypes[e.Archetype].Health {
			t.Fatalf("enemy %+v, want a %s at full health", e, c.Enemies[i].Archetype)
		}
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
			for id, a := range cfg.Archetypes {
				a.DetectRadius = 0
				cfg.Archetypes[id] = a
			}
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

// A player runs a lap past every enemy and leaves. Each enemy must make it
// back home through the real walls instead of snagging on a corner.
func TestEnemiesFindTheirWayHome(t *testing.T) {
	tile := func(x, y float64) sim.Vec2 { return sim.Vec2{X: (x + 0.5) * TileSize, Y: (y + 0.5) * TileSize} }
	route := []sim.Vec2{tile(3, 13), tile(26, 13), tile(26, 8), tile(21, 8), tile(26, 8), tile(26, 3), tile(3, 3), tile(3, 13)}
	c := Loop()
	cfg := sim.DefaultConfig()
	// Keep the runner alive. This test is about the enemies.
	cfg.Classes.Classes[classes.Starter].Base[items.MaxHealth] = 1_000_000
	w := c.NewWorld(cfg, nil)
	id := w.AddPlayer(c.PlayerSpawns[0])
	chased := map[sim.EntityID]bool{}

	next, seq := 0, uint32(0)
	for range 4000 {
		p, _ := w.Player(id)
		if p.Pos.DistanceTo(route[next]) < 8 {
			if next++; next == len(route) {
				break
			}
		}
		d := route[next].Sub(p.Pos)
		seq++
		w.Submit(sim.MoveCommand{Player: id, Seq: seq, Direction: d.Scale(1 / max(d.Len(), 1))})
		w.Step(50 * time.Millisecond)
		for _, e := range w.Enemies() {
			if e.State == sim.EnemyChase {
				chased[e.ID] = true
			}
		}
	}
	if next != len(route) {
		t.Fatalf("player stuck on waypoint %d", next)
	}
	if len(chased) < 3 {
		t.Fatalf("only %d enemies chased the player, want most of them", len(chased))
	}

	w.RemovePlayer(id)
	for range 1200 {
		w.Step(50 * time.Millisecond)
	}
	for i, e := range w.Enemies() {
		home := c.Enemies[i].Pos
		if (e.State != sim.EnemyPatrol && e.State != sim.EnemyIdle) || e.Pos.DistanceTo(home) > 100 {
			t.Errorf("%s enemy %d is %v at %v, %v from home", e.Archetype, e.ID, e.State, e.Pos, e.Pos.DistanceTo(home))
		}
	}
}
