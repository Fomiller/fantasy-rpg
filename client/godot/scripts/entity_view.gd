class_name EntityView
extends Node2D

@export var dead_color := Color(0.35, 0.35, 0.35)

var entity_id := 0
var health := 0

@onready var _body: Polygon2D = $Body
@onready var _health: Label = $Health


func apply(state: GameV1.EntityState) -> void:
	entity_id = state.get_id()
	health = state.get_health()
	var pos := state.get_position()
	position = Vector2(pos.get_x(), pos.get_y())
	_health.text = str(health) if is_alive() else "dead"
	if not is_alive():
		_body.color = dead_color


func is_alive() -> bool:
	return health > 0


func flash() -> void:
	modulate = Color(2, 2, 2)
	create_tween().tween_property(self, "modulate", Color.WHITE, 0.15)
