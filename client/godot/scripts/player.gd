class_name Player
extends CharacterBody2D

@export var max_speed := 220.0
@export var acceleration := 1600.0
@export var friction := 2000.0

var alive := true

@onready var _interact_area: Area2D = $InteractArea
@onready var _body: Polygon2D = $Body
@onready var _alive_color := _body.color


func _physics_process(delta: float) -> void:
	var direction := get_move_input()
	if direction != Vector2.ZERO:
		velocity = velocity.move_toward(direction * max_speed, acceleration * delta)
	else:
		velocity = velocity.move_toward(Vector2.ZERO, friction * delta)
	move_and_slide()

	if Input.is_action_just_pressed("interact"):
		interact()


# Kept apart from movement so it can become the MoveInput sent to the server.
func get_move_input() -> Vector2:
	if not alive:
		return Vector2.ZERO
	return Input.get_vector("move_left", "move_right", "move_up", "move_down")


func die() -> void:
	alive = false
	_body.color = Color(0.35, 0.35, 0.35)


func revive() -> void:
	alive = true
	_body.color = _alive_color


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
