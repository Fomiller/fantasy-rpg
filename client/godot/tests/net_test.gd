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

	var latest := {"snapshot": null}
	connection.snapshot_received.connect(func(s: GameV1.WorldSnapshot) -> void: latest.snapshot = s)
	_check(await _until(func() -> bool: return latest.snapshot != null), "receives world snapshots")

	var start_tick: int = latest.snapshot.get_tick()
	Input.action_press("move_right")
	var moved := await _until(func() -> bool: return _server_x(latest.snapshot, connection.player_id) > 60.0)
	Input.action_release("move_right")
	_check(moved, "server moved the player after MoveInput")
	_check(latest.snapshot.get_tick() > start_tick, "server tick advances")

	var enemies := 0
	for e: GameV1.EntityState in latest.snapshot.get_entities():
		if e.get_kind() == GameV1.EntityKind.ENEMY:
			enemies += 1
	_check(enemies == 1, "snapshot includes the enemy")

	_finish()


func _server_x(snapshot: GameV1.WorldSnapshot, player_id: int) -> float:
	for e: GameV1.EntityState in snapshot.get_entities():
		if e.get_id() == player_id:
			return e.get_position().get_x()
	return -INF


func _until(condition: Callable) -> bool:
	var deadline := Time.get_ticks_msec() + TIMEOUT_MSEC
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
