// Package caves holds cave layouts. The server sends the layout to each
// client on join, so this is the only copy of the map.
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
	w.SetExtraction(c.Extraction)
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
func Parse(name, art string) (Cave, error) {
	rows := splitArt(art)
	width := len(rows[0])
	wall := make([][]bool, len(rows))
	c := Cave{Name: name}
	minX, minY, maxX, maxY := width, len(rows), -1, -1

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
			case 'c':
				c.Chests = append(c.Chests, center)
			case 'X':
				minX, minY, maxX, maxY = min(minX, x), min(minY, y), max(maxX, x), max(maxY, y)
			default:
				return Cave{}, fmt.Errorf("%s: unknown tile %q at %d,%d", name, ch, x, y)
			}
		}
	}

	if len(c.PlayerSpawns) == 0 {
		return Cave{}, errors.New(name + ": no player spawn")
	}
	if maxX < 0 {
		return Cave{}, errors.New(name + ": no extraction zone")
	}
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			if rows[y][x] != 'X' {
				return Cave{}, fmt.Errorf("%s: extraction zone is not a rectangle", name)
			}
		}
	}

	c.Bounds = tiles(0, 0, width, len(rows))
	c.Extraction = tiles(minX, minY, maxX+1, maxY+1)
	c.Walls = mergeWalls(wall)
	return c, nil
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
