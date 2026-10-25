extends SceneTree

# The whole loop: the world's town and its stash chest, a walk into the
# entrance and a ticket into an instance, a run that loots and extracts, and
# back to the town with the loot in the stash.
# Needs a world-server on the URL in --server whose one instance runs the
# yard cave. `just net-test` starts both and passes the URL.
const TIMEOUT_MSEC := 5000

var _failures := 0


func _initialize() -> void:
	_run.call_deferred()


func _run() -> void:
	var main: Node = load("res://scenes/main.tscn").instantiate()
	root.add_child(main)
	var connection: ServerConnection = main.get_node("ServerConnection")
	var stash: StashWindow = main.get_node("HUD/Stash")
	var cave: Cave = main.get_node("Cave")
	var latest := {"snapshot": null}
	connection.snapshot_received.connect(func(s: GameV1.WorldSnapshot) -> void: latest.snapshot = s)
	connection.joined.connect(func(_id: int) -> void: latest.snapshot = null)
	var server_pos := func() -> Vector2:
		var me: GameV1.EntityState = _me(latest.snapshot, connection) if latest.snapshot else null
		return Vector2(me.get_position().get_x(), me.get_position().get_y()) if me else Vector2.INF

	var in_town := await _until(func() -> bool: return connection.in_town and latest.snapshot != null)
	_check(in_town and connection.is_joined(), "the world server puts the player in the town")
	if not in_town:
		_finish()
		return
	_check(":8090/" in connection._url, "the client is pointed at the world server")
	_check(not connection.saving and not stash.saving, "a new character is not saving")

	await _walk(main, "move_left", func() -> bool: return server_pos.call().x < cave.stash.x + 40.0)
	await _press("interact")
	_check(await _until(func() -> bool: return stash.visible), "interact at the chest opens the stash")
	_check(await _until(func() -> bool: return _window_button(stash, "Rusty Sword", "To stash") != null), "the stash lists the starter sword as gear")
	_check("Stash 0/100" in _texts(stash), "the stash starts empty")
	_window_button(stash, "Rusty Sword", "To stash").pressed.emit()
	_check(await _until(func() -> bool: return _window_button(stash, "Rusty Sword", "Equip") != null), "the world server moves the sword to the stash")
	_check("Stash 1/100" in _texts(stash), "the stash counts the sword")
	_window_button(stash, "Rusty Sword", "Equip").pressed.emit()
	_check(await _until(func() -> bool: return _window_button(stash, "Rusty Sword", "To stash") != null), "the sword is equipped again from the stash")

	await _walk(main, "move_right", func() -> bool: return server_pos.call().x > cave.entrance.get_center().x - 16.0)
	Input.action_press("move_up")
	var joined := await _until(func() -> bool: return connection.is_joined() and not connection.in_town, 8000)
	Input.action_release("move_up")
	_check(joined and "/instances/" in connection._url, "walking into the entrance sends the client to an instance server (at %s)" % connection._url)
	if not joined:
		_finish()
		return
	var ticket := {"url": connection._url, "token": connection._token}
	_check(await _until(func() -> bool: return latest.snapshot != null), "the instance sends snapshots")

	# The yard's chest and exit are straight right of the spawn.
	var chest := _first(latest.snapshot, GameV1.EntityKind.CHEST)
	Input.action_press("move_right")
	await _until(func() -> bool:
		var me := _me(latest.snapshot, connection)
		return me != null and chest != null and me.get_position().get_x() > chest.get_position().get_x() - 20.0)
	Input.action_release("move_right")
	await _until(func() -> bool: return false, 300)
	var loot: LootWindow = main.get_node("HUD/Loot")
	await _press("interact")
	_check(await _until(func() -> bool: return loot.visible and not loot._items.is_empty()), "interact opens the chest")
	if not loot.visible or loot._items.is_empty():
		_finish()
		return
	var item_name: String = loot._items[0].get_name()
	var bag := {"names": []}
	connection.inventory_received.connect(func(items: Array[GameV1.Item], _capacity: int) -> void:
		bag.names = items.map(func(item: GameV1.Item) -> String: return item.get_name()))
	for row in loot.get_node("List").get_children():
		if row is HBoxContainer and row.get_child(1) is Button:
			(row.get_child(1) as Button).pressed.emit()
			break
	_check(await _until(func() -> bool: return item_name in bag.names), "takes %s from the chest" % item_name)

	var summary: RunSummaryWindow = main.get_node("HUD/RunSummary")
	Input.action_press("move_right")
	var extracted := await _until(func() -> bool: return summary.visible, 10000)
	Input.action_release("move_right")
	_check(extracted and _has_label(summary, "Extracted"), "walking into the exit extracts and shows the summary")

	var back := await _until(func() -> bool: return connection.in_town and latest.snapshot != null and not connection.saving, 10000)
	_check(back and ":8090/" in connection._url, "the client is back in the world server's town")
	_check(cave.entrance.has_area() and connection.spawn.distance_to(cave.entrance.get_center()) < 160.0 and not cave.entrance.has_point(connection.spawn), "a returning player lands just outside the entrance")
	_check(summary.visible, "the summary stays up over the town")

	await _walk(main, "move_down", func() -> bool: return server_pos.call().y > cave.stash.y - 8.0)
	await _walk(main, "move_left", func() -> bool: return server_pos.call().x < cave.stash.x + 40.0)
	await _press("interact")
	_check(await _until(func() -> bool: return stash.visible and _window_button(stash, item_name, "To bag") != null), "the loot is in the stash (stash shows %s)" % _texts(stash).replace("\n", " | "))

	await _test_reused_token(ticket.url, ticket.token)
	_finish()


# The token from the run that just ended never starts another.
func _test_reused_token(url: String, token: String) -> void:
	var socket := WebSocketPeer.new()
	socket.connect_to_url(url)
	var open := await _until(func() -> bool:
		socket.poll()
		return socket.get_ready_state() == WebSocketPeer.STATE_OPEN)
	_check(open, "the instance takes a new connection")
	if not open:
		return
	var msg := GameV1.ClientMessage.new()
	msg.set_protocol_version(ServerConnection.PROTOCOL_VERSION)
	msg.new_instance_join().set_token(token)
	socket.send(msg.to_bytes(), WebSocketPeer.WRITE_MODE_BINARY)
	var closed := await _until(func() -> bool:
		socket.poll()
		return socket.get_ready_state() == WebSocketPeer.STATE_CLOSED)
	_check(closed and socket.get_close_code() == 1008 and socket.get_close_reason() == "token used", "a used token is turned away (%d %s)" % [socket.get_close_code(), socket.get_close_reason()])


func _first(snapshot: GameV1.WorldSnapshot, kind: int) -> GameV1.EntityState:
	for e: GameV1.EntityState in snapshot.get_entities():
		if e.get_kind() == kind:
			return e
	return null


func _me(snapshot: GameV1.WorldSnapshot, connection: ServerConnection) -> GameV1.EntityState:
	for e: GameV1.EntityState in snapshot.get_entities():
		if e.get_id() == connection.player_id:
			return e
	return null


func _has_label(node: Node, text: String) -> bool:
	for label in node.find_children("*", "Label", true, false):
		if (label as Label).text == text and not label.is_queued_for_deletion():
			return true
	return false


# Abilities and interact are read from input events, which Input.action_press does not make.
func _press(action: String) -> void:
	for pressed in [true, false]:
		var event := InputEventAction.new()
		event.action = action
		event.pressed = pressed
		Input.parse_input_event(event)
		await physics_frame
		await physics_frame


# Holds one key until done, then waits for the player to stop.
func _walk(main: Node, action: String, done: Callable) -> void:
	Input.action_press(action)
	await _until(done, 8000)
	Input.action_release(action)
	var player: Player = main.get_node("Player")
	await _until(func() -> bool: return player.velocity.is_zero_approx(), 1000)
	await _until(func() -> bool: return false, 200)


func _texts(window: Control) -> String:
	var out := ""
	for node in window.find_children("*", "Label", true, false):
		if not node.is_queued_for_deletion():
			out += (node as Label).text + "\n"
	return out


func _window_button(window: Control, item_name: String, text: String) -> Button:
	for node in window.find_children("*", "Button", true, false):
		var button := node as Button
		if button.text != text or button.is_queued_for_deletion():
			continue
		var label := button.get_parent().get_child(0)
		if label is RichTextLabel and item_name in (label as RichTextLabel).get_parsed_text():
			return button
	return null


func _until(condition: Callable, timeout_msec := TIMEOUT_MSEC) -> bool:
	var deadline := Time.get_ticks_msec() + timeout_msec
	while Time.get_ticks_msec() < deadline:
		if condition.call():
			return true
		await process_frame
	return condition.call()


func _finish() -> void:
	print("world_test: %s" % ("PASS" if _failures == 0 else "FAIL (%d)" % _failures))
	quit(1 if _failures > 0 else 0)


func _check(ok: bool, name: String) -> void:
	print("%s %s" % ["ok  " if ok else "FAIL", name])
	if not ok:
		_failures += 1
