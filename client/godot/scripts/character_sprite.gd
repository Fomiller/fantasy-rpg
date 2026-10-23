class_name CharacterSprite
extends Node2D

@export var texture: Texture2D:
	set(value):
		texture = value
		if is_node_ready():
			_body.texture = value

# Null hides the weapon.
@export var weapon: Texture2D:
	set(value):
		weapon = value
		if is_node_ready():
			_show_weapon()

var dead := false

# Counts casts, so a windup cut short by a newer one does not swing.
var _casts := 0

@onready var _rig: Node2D = $Rig
@onready var _body: Sprite2D = $Rig/Body
@onready var _weapon: Sprite2D = $Rig/Weapon
@onready var _anim: AnimationPlayer = $AnimationPlayer


func _ready() -> void:
	_body.texture = texture
	_show_weapon()
	_anim.animation_finished.connect(_on_finished)
	_play("idle")


# A facing, when given, wins over velocity, so a player can back away while
# facing an enemy.
func move(velocity: Vector2, facing := Vector2.ZERO) -> void:
	if dead or _anim.current_animation in ["swing", "windup"]:
		return
	if facing.x != 0.0:
		_rig.scale.x = signf(facing.x)
	else:
		face(velocity)
	_play("walk" if velocity.length() > 1.0 else "idle")


func face(direction: Vector2) -> void:
	if absf(direction.x) > 1.0:
		_rig.scale.x = signf(direction.x)


func swing() -> void:
	if not dead:
		_anim.play("swing")


# Plays the windup for windup_seconds, then the swing.
func cast(direction: Vector2, windup_seconds: float) -> void:
	if dead:
		return
	if direction.x != 0.0:
		_rig.scale.x = signf(direction.x)
	_casts += 1
	if windup_seconds <= 0.0:
		swing()
		return
	var this_cast := _casts
	_play("windup")
	await get_tree().create_timer(windup_seconds).timeout
	if this_cast == _casts and _anim.current_animation == "windup":
		swing()


func windup(on: bool) -> void:
	if dead:
		return
	if on:
		_play("windup")
	elif _anim.current_animation == "windup":
		_play("idle")


func set_dead(value: bool) -> void:
	if value == dead:
		return
	dead = value
	_rig.rotation = PI / 2 if dead else 0.0
	_play("RESET" if dead else "idle")


func animation() -> String:
	return _anim.current_animation


func _on_finished(_anim_name: StringName) -> void:
	if not dead:
		_play("idle")


func _play(anim: String) -> void:
	if _anim.current_animation != anim:
		_anim.play(anim)


func _show_weapon() -> void:
	_weapon.texture = weapon
	_weapon.visible = weapon != null
