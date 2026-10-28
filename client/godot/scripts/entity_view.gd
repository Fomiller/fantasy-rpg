class_name EntityView
extends Node2D

@export var dead_color := Color(0.35, 0.35, 0.35)

# Snapshots come 20 times a second. Easing toward the latest one hides the steps.
const SMOOTHING := 20.0

var entity_id := 0
var health := 0
var max_health := 0
# Main sets it on every view, including ones spawned after the toggle.
var debug := false:
	set(value):
		debug = value
		_show_debug()

var _target := Vector2.ZERO
var _placed := false

@onready var _body: CanvasItem = $Body
@onready var overhead: Overhead = get_node_or_null("Overhead")
@onready var _health: Label = $Health


func apply(state: GameV1.EntityState) -> void:
	entity_id = state.get_id()
	health = state.get_health()
	max_health = state.get_max_health()
	var pos := state.get_position()
	_target = Vector2(pos.get_x(), pos.get_y())
	if not _placed:
		position = _target
		_placed = true
	_health.text = "" if is_alive() else "dead"
	if overhead:
		overhead.set_vitals(health, max_health)
		overhead.set_player_name(state.get_name())
	_body.modulate = Color.WHITE if is_alive() else dead_color


func _process(delta: float) -> void:
	position = position.lerp(_target, minf(1.0, SMOOTHING * delta))


func _show_debug() -> void:
	pass


func is_alive() -> bool:
	return health > 0


func flash() -> void:
	modulate = Color(2, 2, 2)
	create_tween().tween_property(self, "modulate", Color.WHITE, 0.15)
