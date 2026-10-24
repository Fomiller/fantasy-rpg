extends SceneTree

# F3 shows the server ghost, the overlay and labels with server state. Views
# spawned while it is on show them too.
var _failures := 0


func _initialize() -> void:
	_run.call_deferred()


func _run() -> void:
	var main: Node = load("res://scenes/main.tscn").instantiate()
	root.add_child(main)
	await _frames(2)
	var connection: ServerConnection = main.get_node("ServerConnection")
	var overlay: Label = main.get_node("HUD/DebugOverlay")
	var ghost: Node2D = main.get_node("ServerGhost")
	var status: Label = main.get_node("HUD/Status")
	connection.player_id = 7
	main.get_node("Player").send_move = func(_direction: Vector2) -> int: return 0

	_check(InputMap.action_has_event("debug_overlay", _f3()), "F3 is bound to debug_overlay")

	main._on_snapshot(_snapshot(42, [_pile(5), _enemy(9)]))
	await _frames(1)
	var chest: LootView = main._views[5]
	var enemy: EnemyView = main._views[9]
	_check(not main.debug and not overlay.visible and not ghost.visible, "overlay and ghost are off by default")
	_check(not chest._health.visible and enemy._health.text == "!", "no item count or raw enemy state when off")
	_check("7" not in status.text and "tick" not in status.text, "status shows no player id or tick")

	await _tap("debug_overlay")
	await _frames(1)
	_check(main.debug and overlay.visible and ghost.visible, "F3 shows the overlay and the ghost")
	_check(overlay.mouse_filter == Control.MOUSE_FILTER_IGNORE, "overlay does not take clicks")
	_check("player 7" in overlay.text and "tick 42" in overlay.text and "fps" in overlay.text and "Hz" in overlay.text and "snapshot age" in overlay.text, "overlay shows player, tick, rate, fps and snapshot age")
	_check(chest._health.visible and chest._health.text == "2", "piles show their item count")
	_check(enemy._health.text == "#9 detect", "enemies show their id and state")

	main._on_snapshot(_snapshot(43, [_pile(5), _enemy(9), _pile(11), _enemy(12)]))
	var late_pile: LootView = main._views[11]
	var late_enemy: EnemyView = main._views[12]
	_check(late_pile._health.visible and late_enemy._health.text == "#12 detect", "views spawned while on show debug info")

	await _tap("debug_overlay")
	await _frames(1)
	_check(not main.debug and not overlay.visible and not ghost.visible, "F3 again hides the overlay and the ghost")
	_check(not chest._health.visible and not late_pile._health.visible, "item counts hide again")
	_check(enemy._health.text == "!" and late_enemy._health.text == "!", "enemies go back to the alert")
	main._on_snapshot(_snapshot(44, [_pile(5), _enemy(9)]))
	_check(not ghost.visible, "a new snapshot keeps the ghost hidden")

	print("debug_overlay_test: %s" % ("PASS" if _failures == 0 else "FAIL (%d)" % _failures))
	quit(1 if _failures > 0 else 0)


func _snapshot(tick: int, others: Array) -> GameV1.WorldSnapshot:
	var snapshot := GameV1.WorldSnapshot.new()
	snapshot.set_tick(tick)
	var me := snapshot.add_entities()
	me.set_id(7)
	me.set_kind(GameV1.EntityKind.PLAYER)
	me.set_health(100)
	me.set_max_health(100)
	_vec(me.new_position(), Vector2(30, 40))
	_vec(me.new_velocity(), Vector2.ZERO)
	for other: Callable in others:
		other.call(snapshot.add_entities())
	return snapshot


func _pile(id: int) -> Callable:
	return func(state: GameV1.EntityState) -> void:
		state.set_id(id)
		state.set_kind(GameV1.EntityKind.CHEST)
		state.set_item_count(2)
		_vec(state.new_position(), Vector2(id * 10, 0))


func _enemy(id: int) -> Callable:
	return func(state: GameV1.EntityState) -> void:
		state.set_id(id)
		state.set_kind(GameV1.EntityKind.ENEMY)
		state.set_archetype("melee")
		state.set_enemy_state(GameV1.EnemyState.DETECT)
		state.set_health(20)
		state.set_max_health(30)
		_vec(state.new_position(), Vector2(0, id * 10))
		_vec(state.new_velocity(), Vector2.ZERO)


func _vec(v: GameV1.Vec2, value: Vector2) -> void:
	v.set_x(value.x)
	v.set_y(value.y)


func _f3() -> InputEventKey:
	var event := InputEventKey.new()
	event.physical_keycode = KEY_F3
	return event


func _tap(action: String) -> void:
	Input.action_press(action)
	await _frames(2)
	Input.action_release(action)


func _frames(count: int) -> void:
	for i in count:
		await physics_frame


func _check(ok: bool, name: String) -> void:
	print("%s %s" % ["ok  " if ok else "FAIL", name])
	if not ok:
		_failures += 1
