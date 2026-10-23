extends SceneTree

# Needs a cave-server on 127.0.0.1:8080. `just net-test` starts one.
const TIMEOUT_MSEC := 5000

var _failures := 0


func _initialize() -> void:
	_run.call_deferred()


func _run() -> void:
	var main: Node = load("res://scenes/main.tscn").instantiate()
	root.add_child(main)
	var connection: ServerConnection = main.get_node("ServerConnection")

	var joined := await _until(func() -> bool: return connection.is_joined())
	_check(joined, "joined the server")
	if not joined:
		_finish()
		return

	var hud: RichTextLabel = main.get_node("HUD/Equipment")
	_check(await _until(func() -> bool: return "Rusty Sword" in hud.text), "HUD shows the starter kit")

	var latest := {"snapshot": null}
	connection.snapshot_received.connect(func(s: GameV1.WorldSnapshot) -> void: latest.snapshot = s)
	_check(await _until(func() -> bool: return latest.snapshot != null), "receives world snapshots")

	var cave: Cave = main.get_node("Cave")
	var player: Player = main.get_node("Player")
	_check(cave.get_child_count() > 10 and connection.cave.get_walls().size() > 10, "built the cave the server sent")
	_check(player.position.distance_to(connection.spawn) < 1.0 and connection.spawn != Vector2.ZERO, "player starts at the server spawn")
	_check(cave.extraction.has_area(), "cave has an extraction zone")

	var start_tick: int = latest.snapshot.get_tick()
	var start_x := _server_x(latest.snapshot, connection.player_id)
	Input.action_press("move_right")
	var moved := await _until(func() -> bool: return _server_x(latest.snapshot, connection.player_id) > start_x + 40.0)
	Input.action_release("move_right")
	_check(moved, "server moved the player after MoveInput")

	# Walk up into the cave wall and slide along it, where drift used to show.
	Input.action_press("move_up")
	Input.action_press("move_right")
	await _until(func() -> bool: return false, 1500)
	Input.action_release("move_up")
	Input.action_release("move_right")
	var server_pos := func() -> Vector2: return _server_pos(latest.snapshot, connection.player_id)
	var settled := await _until(func() -> bool: return player.velocity.is_zero_approx() and player.position.distance_to(server_pos.call()) < 1.0)
	_check(settled, "predicted player ends where the server has it (off by %.1f)" % player.position.distance_to(server_pos.call()))
	_check(latest.snapshot.get_tick() > start_tick, "server tick advances")

	var enemies := 0
	for e: GameV1.EntityState in latest.snapshot.get_entities():
		if e.get_kind() == GameV1.EntityKind.ENEMY:
			enemies += 1
	_check(enemies == 5, "snapshot includes all five enemies of the loop cave")

	_finish()


func _server_pos(snapshot: GameV1.WorldSnapshot, player_id: int) -> Vector2:
	for e: GameV1.EntityState in snapshot.get_entities():
		if e.get_id() == player_id:
			return Vector2(e.get_position().get_x(), e.get_position().get_y())
	return Vector2.INF


func _server_x(snapshot: GameV1.WorldSnapshot, player_id: int) -> float:
	for e: GameV1.EntityState in snapshot.get_entities():
		if e.get_id() == player_id:
			return e.get_position().get_x()
	return -INF


func _until(condition: Callable, timeout_msec := TIMEOUT_MSEC) -> bool:
	var deadline := Time.get_ticks_msec() + timeout_msec
	while Time.get_ticks_msec() < deadline:
		if condition.call():
			return true
		await process_frame
	return condition.call()


func _finish() -> void:
	print("net_test: %s" % ("PASS" if _failures == 0 else "FAIL (%d)" % _failures))
	quit(1 if _failures > 0 else 0)


func _check(ok: bool, name: String) -> void:
	print("%s %s" % ["ok  " if ok else "FAIL", name])
	if not ok:
		_failures += 1
