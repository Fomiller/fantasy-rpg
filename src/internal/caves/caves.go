// Package caves holds cave layouts and the hub town's. The server sends the
// layout to each client on join, so this is the only copy of the map.
package caves

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Fomiller/fantasy-rpg/src/internal/items"
	"github.com/Fomiller/fantasy-rpg/src/internal/sim"
)

const TileSize = 64

type EnemySpawn struct {
	Pos       sim.Vec2
	Archetype sim.ArchetypeID
}

var enemyTiles = map[rune]sim.ArchetypeID{'m': sim.Melee, 'r': sim.Ranged, 'E': sim.Elite}

type Cave struct {
	Name         string
	Bounds       sim.Rect
	Walls        []sim.Rect
	PlayerSpawns []sim.Vec2
	Enemies      []EnemySpawn
	Chests       []sim.Vec2
	Extraction   sim.Rect
	// Town is the world server's hub. It has an entrance into a run, the
	// stash chest and return spawns, and no enemies, chests or exit.
	Town         bool
	Entrance     sim.Rect
	Stash        sim.Vec2
	ReturnSpawns []sim.Vec2
}

// NewWorld builds a sim world with this cave's walls, enemies, chests and
// exit. loot rolls drops and chest contents, and may be nil.
func (c Cave) NewWorld(cfg sim.Config, loot *items.Roller) *sim.World {
	w := sim.NewWorld(cfg, c.Walls)
	w.SetLoot(loot)
	for _, e := range c.Enemies {
		w.AddEnemy(e.Pos, e.Archetype)
	}
	for _, pos := range c.Chests {
		w.AddChest(pos)
	}
	if !c.Town {
		w.SetExtraction(c.Extraction)
	}
	return w
}

// Loop is the first gray box cave: two rooms on each side, joined by
// corridors around a central chamber, with the exit in the top right. An
// elite guards the exit, and ranged enemies hold the long corridors.
func Loop() Cave {
	return mustParse("loop", loopArt)
}

const loopArt = `
###############################
#c......#############...XXX...#
#.......#############...XXX...#
#..........r..................#
#.......#############......E..#
###.#####...........######.####
###.####......c.......####.####
###.###.....######.....###.####
###.......########...m........#
###.###.....######.....###.####
###.####..............####.####
###.#####...........######.####
#.......#############........c#
#..SS..........m...........r..#
#.......#############.........#
###############################
`

// Yard is a small room with no enemies: a chest on the way from the spawn
// to the exit. It is for trying the run loop without a fight.
func Yard() Cave {
	return mustParse("yard", yardArt)
}

const yardArt = `
############
#..........#
#S...c...XX#
#........XX#
############
`

// Town is the hub on the world server. The spawn is in the middle, the
// stash chest left of it, and the cave entrance at the end of the corridor
// at the top. The return spawn sits just outside the entrance.
func Town() Cave {
	return mustParse("town", townArt)
}

const townArt = `
########################
##########DD############
##########..############
#.........R............#
#..##.............##...#
#..##.............##...#
#......................#
#....T....SSS..........#
#......................#
#..##.............##...#
#..##.............##...#
#......................#
########################
`

// ByName is the cave an instance server builds, from CAVE.
func ByName(name string) (Cave, bool) {
	switch name {
	case "loop":
		return Loop(), true
	case "yard":
		return Yard(), true
	}
	return Cave{}, false
}

func mustParse(name, art string) Cave {
	c, err := Parse(name, art)
	if err != nil {
		panic(err)
	}
	return c
}

// Parse reads a map drawn one character per tile:
//
//	#  wall
//	.  floor
//	S  player spawn
//	m  melee enemy
//	r  ranged enemy
//	E  elite enemy
//	c  chest
//	X  extraction zone, which must be one solid rectangle
//	D  town entrance into a run, which must be one solid rectangle
//	T  town stash chest, exactly one
//	R  town return spawn, where a player lands after a run
//
// A map with D, T or R is a town. It needs all three and can have no
// enemies, chests or exit, so nothing in a town can fight or drop loot.
func Parse(name, art string) (Cave, error) {
	rows := splitArt(art)
	width := len(rows[0])
	wall := make([][]bool, len(rows))
	c := Cave{Name: name}
	var stashes, caveOnly int

	for y, row := range rows {
		if len(row) != width {
			return Cave{}, fmt.Errorf("%s: row %d is %d wide, want %d", name, y, len(row), width)
		}
		wall[y] = make([]bool, width)
		for x, ch := range row {
			center := sim.Vec2{X: (float64(x) + 0.5) * TileSize, Y: (float64(y) + 0.5) * TileSize}
			switch ch {
			case '#':
				wall[y][x] = true
			case '.':
			case 'S':
				c.PlayerSpawns = append(c.PlayerSpawns, center)
			case 'm', 'r', 'E':
				c.Enemies = append(c.Enemies, EnemySpawn{Pos: center, Archetype: enemyTiles[ch]})
				caveOnly++
			case 'c':
				c.Chests = append(c.Chests, center)
				caveOnly++
			case 'X':
				caveOnly++
			case 'D':
				c.Town = true
			case 'T':
				c.Stash = center
				c.Town = true
				stashes++
			case 'R':
				c.ReturnSpawns = append(c.ReturnSpawns, center)
				c.Town = true
			default:
				return Cave{}, fmt.Errorf("%s: unknown tile %q at %d,%d", name, ch, x, y)
			}
		}
	}

	if len(c.PlayerSpawns) == 0 {
		return Cave{}, errors.New(name + ": no player spawn")
	}
	var err error
	if c.Town {
		switch {
		case caveOnly > 0:
			return Cave{}, errors.New(name + ": a town cannot have enemies, chests or an exit")
		case stashes != 1:
			return Cave{}, fmt.Errorf("%s: a town needs one stash chest, has %d", name, stashes)
		case len(c.ReturnSpawns) == 0:
			return Cave{}, errors.New(name + ": a town needs a return spawn")
		}
		c.Entrance, err = solidRect(name, rows, 'D', "entrance")
	} else {
		c.Extraction, err = solidRect(name, rows, 'X', "extraction zone")
	}
	if err != nil {
		return Cave{}, err
	}

	c.Bounds = tiles(0, 0, width, len(rows))
	c.Walls = mergeWalls(wall)
	return c, nil
}

// solidRect is the box around every ch tile, which must fill it.
func solidRect(name string, rows []string, ch rune, what string) (sim.Rect, error) {
	minX, minY, maxX, maxY := len(rows[0]), len(rows), -1, -1
	for y, row := range rows {
		for x, c := range row {
			if c == ch {
				minX, minY, maxX, maxY = min(minX, x), min(minY, y), max(maxX, x), max(maxY, y)
			}
		}
	}
	if maxX < 0 {
		return sim.Rect{}, fmt.Errorf("%s: no %s", name, what)
	}
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			if rune(rows[y][x]) != ch {
				return sim.Rect{}, fmt.Errorf("%s: %s is not a rectangle", name, what)
			}
		}
	}
	return tiles(minX, minY, maxX+1, maxY+1), nil
}

// mergeWalls joins wall tiles into large rectangles, so collision and sight
// checks loop over a few dozen boxes instead of hundreds of tiles.
func mergeWalls(wall [][]bool) []sim.Rect {
	used := make([][]bool, len(wall))
	for y := range wall {
		used[y] = make([]bool, len(wall[y]))
	}
	free := func(x, y int) bool { return wall[y][x] && !used[y][x] }

	var out []sim.Rect
	for y := range wall {
		for x := range wall[y] {
			if !free(x, y) {
				continue
			}
			x1 := x
			for x1 < len(wall[y]) && free(x1, y) {
				x1++
			}
			y1 := y + 1
		grow:
			for y1 < len(wall) {
				for i := x; i < x1; i++ {
					if !free(i, y1) {
						break grow
					}
				}
				y1++
			}
			for j := y; j < y1; j++ {
				for i := x; i < x1; i++ {
					used[j][i] = true
				}
			}
			out = append(out, tiles(x, y, x1, y1))
		}
	}
	return out
}

func tiles(x0, y0, x1, y1 int) sim.Rect {
	return sim.Rect{
		Min: sim.Vec2{X: float64(x0) * TileSize, Y: float64(y0) * TileSize},
		Max: sim.Vec2{X: float64(x1) * TileSize, Y: float64(y1) * TileSize},
	}
}

func splitArt(art string) []string {
	return strings.Split(strings.Trim(art, "\n"), "\n")
}
