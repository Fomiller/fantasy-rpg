class_name Cave
extends Node2D

const WALL_SCENE := preload("res://scenes/wall.tscn")
const FLOOR_TEXTURE := preload("res://assets/kenney_tiny_dungeon/tile_0000.png")
const CHEST_TEXTURE := preload("res://assets/kenney_tiny_dungeon/tile_0089.png")
# Darkened so the art does not compete with telegraphs.
const FLOOR_TINT := Color(0.45, 0.45, 0.5)
const EXIT_COLOR := Color(0.2, 0.75, 0.4, 0.35)
const ENTRANCE_COLOR := Color(0.85, 0.6, 0.25, 0.35)

# Empty when the layout has none: a cave has no entrance, the town no exit.
var extraction := Rect2()
var entrance := Rect2()
var stash := Vector2.ZERO
var has_stash := false
var walls: Array[Rect2] = []


# Rebuilds the cave or town from the layout the server sent on join.
func build(layout: GameV1.InstanceLayout) -> void:
	clear()

	var ground := _box(to_rect(layout.get_bounds()), FLOOR_TINT)
	ground.texture = FLOOR_TEXTURE
	ground.texture_scale = Vector2(0.5, 0.5)
	ground.texture_repeat = CanvasItem.TEXTURE_REPEAT_ENABLED
	add_child(ground)

	if layout.has_extraction():
		extraction = to_rect(layout.get_extraction())
		_zone(extraction, EXIT_COLOR, "EXIT")
	if layout.has_entrance():
		entrance = to_rect(layout.get_entrance())
		_zone(entrance, ENTRANCE_COLOR, "CAVE")
	if layout.has_stash():
		stash = Vector2(layout.get_stash().get_x(), layout.get_stash().get_y())
		has_stash = true
		var chest := Sprite2D.new()
		chest.texture = CHEST_TEXTURE
		chest.scale = Vector2(2, 2)
		chest.position = stash
		add_child(chest)

	for r: GameV1.Rect in layout.get_walls():
		var rect := to_rect(r)
		walls.append(rect)
		var wall := WALL_SCENE.instantiate()
		wall.position = rect.get_center()
		wall.size = rect.size
		add_child(wall)


func clear() -> void:
	for child in get_children():
		remove_child(child)
		child.queue_free()
	extraction = Rect2()
	entrance = Rect2()
	stash = Vector2.ZERO
	has_stash = false
	walls.clear()


static func to_rect(r: GameV1.Rect) -> Rect2:
	var a := Vector2(r.get_min().get_x(), r.get_min().get_y())
	var b := Vector2(r.get_max().get_x(), r.get_max().get_y())
	return Rect2(a, b - a)


func _zone(rect: Rect2, color: Color, text: String) -> void:
	add_child(_box(rect, color))
	var label := Label.new()
	label.text = text
	label.position = rect.get_center() - Vector2(16, 12)
	add_child(label)


func _box(rect: Rect2, color: Color) -> Polygon2D:
	var box := Polygon2D.new()
	box.color = color
	box.polygon = PackedVector2Array([rect.position, Vector2(rect.end.x, rect.position.y), rect.end, Vector2(rect.position.x, rect.end.y)])
	return box
