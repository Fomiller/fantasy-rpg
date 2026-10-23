class_name Cave
extends Node2D

const WALL_SCENE := preload("res://scenes/wall.tscn")
const FLOOR_COLOR := Color(0.13, 0.13, 0.16)
const EXIT_COLOR := Color(0.2, 0.75, 0.4, 0.35)

var extraction := Rect2()
var walls: Array[Rect2] = []


# Rebuilds the cave from the layout the server sent on join.
func build(layout: GameV1.CaveLayout) -> void:
	for child in get_children():
		remove_child(child)
		child.queue_free()

	add_child(_box(to_rect(layout.get_bounds()), FLOOR_COLOR))

	extraction = to_rect(layout.get_extraction())
	add_child(_box(extraction, EXIT_COLOR))
	var label := Label.new()
	label.text = "EXIT"
	label.position = extraction.get_center() - Vector2(16, 12)
	add_child(label)

	walls.clear()
	for r: GameV1.Rect in layout.get_walls():
		var rect := to_rect(r)
		walls.append(rect)
		var wall := WALL_SCENE.instantiate()
		wall.position = rect.get_center()
		wall.size = rect.size
		add_child(wall)


static func to_rect(r: GameV1.Rect) -> Rect2:
	var a := Vector2(r.get_min().get_x(), r.get_min().get_y())
	var b := Vector2(r.get_max().get_x(), r.get_max().get_y())
	return Rect2(a, b - a)


func _box(rect: Rect2, color: Color) -> Polygon2D:
	var box := Polygon2D.new()
	box.color = color
	box.polygon = PackedVector2Array([rect.position, Vector2(rect.end.x, rect.position.y), rect.end, Vector2(rect.position.x, rect.end.y)])
	return box
