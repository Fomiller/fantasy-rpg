@tool
extends StaticBody2D

# Art is 16px, drawn at 2x like the characters.
const ART_SCALE := 2.0
const EDGE_COLOR := Color(0, 0, 0, 0.5)

@export var size := Vector2(64, 64):
	set(value):
		size = value
		_update()

@export var texture: Texture2D


func _ready() -> void:
	_update()


func _update() -> void:
	# The setter also runs while the scene loads, before children exist.
	if not is_node_ready():
		return
	var shape := RectangleShape2D.new()
	shape.size = size
	$CollisionShape2D.shape = shape
	queue_redraw()


func _draw() -> void:
	if texture:
		draw_set_transform(Vector2.ZERO, 0.0, Vector2.ONE * ART_SCALE)
		draw_texture_rect(texture, Rect2(-size / 2 / ART_SCALE, size / ART_SCALE), true)
		draw_set_transform(Vector2.ZERO)
	draw_rect(Rect2(-size / 2, size), EDGE_COLOR, false, 2.0)
