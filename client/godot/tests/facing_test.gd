extends SceneTree

var _failures := 0
var _player: Player


func _initialize() -> void:
	_run.call_deferred()


func _run() -> void:
	_player = load("res://scenes/player.tscn").instantiate()
	root.add_child(_player)
	await _frames(2)
	var sprite: CharacterSprite = _player._sprite

	await _mouse(Vector2(-100, 0))
	_check(_player.facing.is_equal_approx(Vector2.LEFT), "mouse motion faces the cursor")
	_check(sprite._rig.scale.x < 0, "sprite faces the mouse")
	_check(_player._marker.rotation == Vector2.LEFT.angle() and _player._marker.visible, "marker points where the player faces")

	Input.action_press("move_right")
	await _frames(2)
	_check(_player.facing.is_equal_approx(Vector2.RIGHT), "a later move key overrides the mouse")
	_check(sprite._rig.scale.x > 0, "sprite faces the move direction")

	await _mouse(Vector2(-100, -100))
	_check(_player.facing.is_equal_approx(Vector2(-1, -1).normalized()), "a later mouse motion overrides the held key")
	_check(sprite._rig.scale.x < 0, "sprite walks backwards while facing the mouse")
	Input.action_release("move_right")
	await _frames(1)

	var before := _player.facing
	await _mouse(Vector2(100, 0), Vector2(1, 1))
	_check(_player.facing == before, "mouse jitter does not turn the player")

	_player.cast(Vector2.DOWN, 0.25, true)
	_check(_player.facing == Vector2.DOWN, "a cast faces its aim")
	await _mouse(Vector2(-100, 0))
	Input.action_press("move_left")
	await _frames(2)
	_check(_player.facing == Vector2.DOWN, "facing stays on the aim during a rooting windup")
	Input.action_release("move_left")
	await create_timer(0.3).timeout
	await _mouse(Vector2(-100, 0))
	_check(_player.facing.is_equal_approx(Vector2.LEFT), "the mouse turns the player again once the windup ends")

	print("facing_test: %s" % ("PASS" if _failures == 0 else "FAIL (%d)" % _failures))
	quit(1 if _failures > 0 else 0)


# Moves the mouse to offset from the player on screen.
func _mouse(offset: Vector2, relative := Vector2(10, 0)) -> void:
	var motion := InputEventMouseMotion.new()
	motion.position = _player.get_global_transform_with_canvas().origin + offset
	motion.relative = relative
	root.push_input(motion, true)
	await _frames(1)


func _frames(count: int) -> void:
	for i in count:
		await physics_frame


func _check(ok: bool, name: String) -> void:
	print("%s %s" % ["ok  " if ok else "FAIL", name])
	if not ok:
		_failures += 1
