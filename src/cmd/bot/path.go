package main

import (
	"math"

	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
)

// tileSize matches caves.TileSize. Layouts come from the server, and every
// wall is a whole number of tiles.
const tileSize = 64

type point struct{ x, y float64 }

func (p point) dist(q point) float64 { return math.Hypot(q.x-p.x, q.y-p.y) }

type tile struct{ x, y int }

// grid is the layout as tiles, for BFS.
type grid struct {
	w, h int
	wall []bool
}

func newGrid(l *gamev1.InstanceLayout) *grid {
	b := l.GetBounds()
	g := &grid{
		w: int(math.Round(float64(b.GetMax().GetX()-b.GetMin().GetX()) / tileSize)),
		h: int(math.Round(float64(b.GetMax().GetY()-b.GetMin().GetY()) / tileSize)),
	}
	g.wall = make([]bool, g.w*g.h)
	for y := range g.h {
		for x := range g.w {
			c := g.center(tile{x, y})
			for _, r := range l.GetWalls() {
				if inRect(r, c) {
					g.wall[y*g.w+x] = true
					break
				}
			}
		}
	}
	return g
}

func inRect(r *gamev1.Rect, p point) bool {
	return p.x >= float64(r.GetMin().GetX()) && p.x <= float64(r.GetMax().GetX()) &&
		p.y >= float64(r.GetMin().GetY()) && p.y <= float64(r.GetMax().GetY())
}

func (g *grid) center(t tile) point {
	return point{(float64(t.x) + 0.5) * tileSize, (float64(t.y) + 0.5) * tileSize}
}

func (g *grid) tileAt(p point) tile {
	return tile{int(math.Floor(p.x / tileSize)), int(math.Floor(p.y / tileSize))}
}

func (g *grid) floor(t tile) bool {
	return t.x >= 0 && t.y >= 0 && t.x < g.w && t.y < g.h && !g.wall[t.y*g.w+t.x]
}

// floors is every tile a player can stand on, optionally only those in r.
func (g *grid) floors(r *gamev1.Rect) []tile {
	var out []tile
	for y := range g.h {
		for x := range g.w {
			t := tile{x, y}
			if g.floor(t) && (r == nil || inRect(r, g.center(t))) {
				out = append(out, t)
			}
		}
	}
	return out
}

// path is the tile centers from start to the nearest goal, with straight
// runs merged so the bot only slows down at turns. Nil when no goal is
// reachable.
func (g *grid) path(start point, goals []tile) []point {
	from := g.tileAt(start)
	if !g.floor(from) {
		return nil
	}
	goal := map[tile]bool{}
	for _, t := range goals {
		goal[t] = true
	}
	prev := map[tile]tile{from: from}
	queue := []tile{from}
	for len(queue) > 0 {
		t := queue[0]
		queue = queue[1:]
		if goal[t] {
			return g.walk(prev, from, t)
		}
		for _, d := range []tile{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			n := tile{t.x + d.x, t.y + d.y}
			if _, seen := prev[n]; seen || !g.floor(n) {
				continue
			}
			prev[n] = t
			queue = append(queue, n)
		}
	}
	return nil
}

func (g *grid) walk(prev map[tile]tile, from, to tile) []point {
	var tiles []tile
	for t := to; t != from; t = prev[t] {
		tiles = append(tiles, t)
	}
	tiles = append(tiles, from)
	var out []point
	for i := len(tiles) - 1; i >= 0; i-- {
		if i > 0 && i < len(tiles)-1 {
			a, b := tiles[i+1], tiles[i-1]
			if a.x == b.x || a.y == b.y {
				continue
			}
		}
		out = append(out, g.center(tiles[i]))
	}
	return out
}
