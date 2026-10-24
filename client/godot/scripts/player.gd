class_name Player
extends CharacterBody2D

# Errors up to this size are eased out. Anything bigger means the server
# disagrees for real, like a knockback, so the player jumps there.
const SNAP_DISTANCE := 64.0
const CORRECTION_SMOOTHING := 10.0
const MAX_PENDING := 64
const DEAD_COLOR := Color(0.35, 0.35, 0.35)
const WEAPONS := {
	"rusty_sword": preload("res://assets/kenney_tiny_dungeon/tile_0107.png"),
	"iron_sword": preload("res://assets/kenney_tiny_dungeon/tile_0104.png"),
	"war_axe": preload("res://assets/kenney_tiny_dungeon/tile_0119.png"),
}
# For weapons without their own art yet.
const DEFAULT_WEAPON := preload("res://assets/kenney_tiny_dungeon/tile_0104.png")
# Mouse motion smaller than this, in pixels, is a resting hand, not aiming.
const MOUSE_JITTER := 3.0
const MARKER_DISTANCE := 22.0

# The server sends both. These are its defaults, used until it does.
var max_speed := 220.0
var tick_seconds := 1.0 / 20.0
var alive := true
# Where attacks and abilities go. The last mouse motion or move key sets it.
var facing := Vector2.RIGHT
var cave: Cave
# Sends one input to the server and returns its seq, or 0 when offline.
var send_move: Callable

var _body := Movement.new()
var _previous := Vector2.ZERO
var _accumulator := 0.0
var _correction := Vector2.ZERO
# Inputs sent that the server has not applied yet, as [seq, direction].
var _pending: Array[Array] = []
var _no_walls: Array[Rect2] = []
# Seconds left of a rooting windup. The server ignores movement then.
var _rooted := 0.0
var _last_move := Vector2.ZERO

@onready var _interact_area: Area2D = $InteractArea
@onready var _sprite: CharacterSprite = $Body
@onready var _marker: Node2D = $FacingMarker


func _input(event: InputEvent) -> void:
	var motion := event as InputEventMouseMotion
	if motion and motion.relative.length() >= MOUSE_JITTER:
		face((make_input_local(motion) as InputEventMouseMotion).position)


# Movement runs in fixed steps of one server tick, the same steps the server
# takes. The node is drawn between the last two steps so it still moves smoothly.
func _physics_process(delta: float) -> void:
	_rooted = maxf(_rooted - delta, 0.0)
	# Only a change counts, so a held key does not undo a later mouse motion.
	var move := get_move_input()
	if move != _last_move:
		_last_move = move
		face(move)
	_accumulator += delta
	while _accumulator >= tick_seconds:
		_accumulator -= tick_seconds
		_tick(get_move_input())
	_correction = _correction.lerp(Vector2.ZERO, minf(1.0, CORRECTION_SMOOTHING * delta))
	position = _previous.lerp(_body.position, _accumulator / tick_seconds) + _correction
	velocity = _body.velocity
	_sprite.move(velocity, facing)
	_marker.position = facing * MARKER_DISTANCE
	_marker.rotation = facing.angle()
	_marker.visible = alive

	if Input.is_action_just_pressed("interact"):
		interact()


func get_move_input() -> Vector2:
	if not alive or _rooted > 0.0:
		return Vector2.ZERO
	return Input.get_vector("move_left", "move_right", "move_up", "move_down")


# A rooting windup holds facing on its aim until it lands.
func face(direction: Vector2) -> void:
	if _rooted > 0.0 or direction.length() < 1e-3:
		return
	facing = direction.normalized()


func place(at: Vector2) -> void:
	_body.position = at
	_body.velocity = Vector2.ZERO
	_previous = at
	_correction = Vector2.ZERO
	_pending.clear()
	position = at
	velocity = Vector2.ZERO


# Starts again from where the server says the player was after last_input_seq,
# then replays the inputs it has not applied yet.
func reconcile(last_input_seq: int, server_position: Vector2, server_velocity: Vector2) -> void:
	while not _pending.is_empty() and _pending[0][0] <= last_input_seq:
		_pending.pop_front()
	var predicted := _body.position
	_body.position = server_position
	_body.velocity = server_velocity
	for input in _pending:
		_step(input[1])

	var error := predicted - _body.position
	if error.length() > SNAP_DISTANCE:
		_previous = _body.position
		_correction = Vector2.ZERO
	else:
		_previous -= error
		_correction += error


func _tick(direction: Vector2) -> void:
	var seq: int = send_move.call(direction) if send_move.is_valid() else 0
	if seq > 0:
		_pending.append([seq, direction])
		if _pending.size() > MAX_PENDING:
			_pending.pop_front()
	_previous = _body.position
	_step(direction)


func _step(direction: Vector2) -> void:
	if not alive:
		_body.velocity = Vector2.ZERO
		return
	_body.step(direction, max_speed, cave.walls if cave else _no_walls, tick_seconds)


func die() -> void:
	alive = false
	_sprite.set_dead(true)
	_sprite.modulate = DEAD_COLOR


func revive() -> void:
	alive = true
	_sprite.set_dead(false)
	_sprite.modulate = Color.WHITE


# Plays the ability right away, before the server answers, to hide latency.
func cast(aim: Vector2, windup_seconds: float, root: bool) -> void:
	if not alive:
		return
	face(aim)
	_sprite.cast(facing, windup_seconds)
	if root:
		_rooted = windup_seconds


# The server said no, so the windup played early never happened.
func cancel_cast() -> void:
	_rooted = 0.0
	_sprite.windup(false)


# An empty id means no weapon is equipped.
func equip_weapon(item_def_id: String) -> void:
	_sprite.weapon = weapon_texture(item_def_id)


# Null for an empty id.
static func weapon_texture(item_def_id: String) -> Texture2D:
	return WEAPONS.get(item_def_id, DEFAULT_WEAPON) if item_def_id != "" else null


func flash() -> void:
	modulate = Color(2, 2, 2)
	create_tween().tween_property(self, "modulate", Color.WHITE, 0.15)


func interact() -> void:
	var target := _nearest_interactable()
	if target:
		target.interact(self)


func _nearest_interactable() -> Node2D:
	var nearest: Node2D = null
	var nearest_distance := INF
	for body in _interact_area.get_overlapping_bodies():
		if not body.is_in_group("interactable"):
			continue
		var distance := global_position.distance_to(body.global_position)
		if distance < nearest_distance:
			nearest = body
			nearest_distance = distance
	return nearest
