extends SceneTree

var _failures := 0


func _initialize() -> void:
	_run.call_deferred()


func _run() -> void:
	var main: Node = load("res://scenes/main.tscn").instantiate()
	root.add_child(main)
	var player: Player = main.get_node("Player")
	var chest: Node = main.get_node("TestRoom/Chest")
	await _frames(2)

	_check(InputMap.has_action("move_right") and InputMap.has_action("interact"), "input actions exist")

	Input.action_press("move_right")
	await _frames(4)
	_check(player.velocity.x > 0.0 and player.velocity.x < player.max_speed, "player accelerates instead of jumping to full speed")
	await _frames(116)
	Input.action_release("move_right")

	# The pillar's left edge is x=168 and the player is 32 wide, so it stops near x=152.
	_check(player.position.x > 140.0 and player.position.x < 153.0, "pillar stops the player (x=%.1f)" % player.position.x)

	await _frames(30)
	_check(player.velocity.is_zero_approx(), "player stops when input is released")

	player.position = Vector2(-160, 0)
	await _frames(2)
	Input.action_press("interact")
	await _frames(1)
	Input.action_release("interact")
	await _frames(1)
	_check(chest.is_open, "interact opens the nearby chest")

	print("player_test: %s" % ("PASS" if _failures == 0 else "FAIL (%d)" % _failures))
	quit(1 if _failures > 0 else 0)


func _frames(count: int) -> void:
	for i in count:
		await physics_frame


func _check(ok: bool, name: String) -> void:
	print("%s %s" % ["ok  " if ok else "FAIL", name])
	if not ok:
		_failures += 1
