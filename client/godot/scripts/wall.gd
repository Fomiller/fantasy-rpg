@tool
extends StaticBody2D

@export var size := Vector2(64, 64):
	set(value):
		size = value
		_update()

@export var color := Color(0.32, 0.32, 0.38)


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
	draw_rect(Rect2(-size / 2, size), color)
