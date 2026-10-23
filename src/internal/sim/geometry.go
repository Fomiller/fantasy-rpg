package sim

import "math"

type Vec2 struct {
	X, Y float64
}

func (v Vec2) Add(o Vec2) Vec2           { return Vec2{v.X + o.X, v.Y + o.Y} }
func (v Vec2) Sub(o Vec2) Vec2           { return Vec2{v.X - o.X, v.Y - o.Y} }
func (v Vec2) Scale(s float64) Vec2      { return Vec2{v.X * s, v.Y * s} }
func (v Vec2) Len() float64              { return math.Hypot(v.X, v.Y) }
func (v Vec2) DistanceTo(o Vec2) float64 { return v.Sub(o).Len() }

// MoveToward matches Godot's Vector2.move_toward so client and server agree.
func (v Vec2) MoveToward(target Vec2, delta float64) Vec2 {
	diff := target.Sub(v)
	dist := diff.Len()
	if dist <= delta || dist < 1e-9 {
		return target
	}
	return v.Add(diff.Scale(delta / dist))
}

// Rect is an axis-aligned box from Min to Max.
type Rect struct {
	Min, Max Vec2
}

func box(center Vec2, half float64) Rect {
	return Rect{Vec2{center.X - half, center.Y - half}, Vec2{center.X + half, center.Y + half}}
}

func (r Rect) Overlaps(o Rect) bool {
	return r.Min.X < o.Max.X && r.Max.X > o.Min.X && r.Min.Y < o.Max.Y && r.Max.Y > o.Min.Y
}

// TestRoomWalls mirrors client/godot/scenes/test_room.tscn.
func TestRoomWalls() []Rect {
	return []Rect{
		{Vec2{-616, -352}, Vec2{616, -320}}, // top
		{Vec2{-616, 320}, Vec2{616, 352}},   // bottom
		{Vec2{-632, -320}, Vec2{-600, 320}}, // left
		{Vec2{600, -320}, Vec2{632, 320}},   // right
		{Vec2{168, -32}, Vec2{232, 32}},     // pillar
		{Vec2{-220, -14}, Vec2{-180, 14}},   // chest
	}
}
