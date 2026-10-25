extends SceneTree

# Needs a world-server on 127.0.0.1:8080 with instance servers running the
# loop cave. `just net-test` starts them.
const TIMEOUT_MSEC := 5000

var _failures := 0


func _initialize() -> void:
	_run.call_deferred()


func _run() -> void:
	var main: Node = load("res://scenes/main.tscn").instantiate()
	root.add_child(main)
	var connection: ServerConnection = main.get_node("ServerConnection")

	var lobby: LobbyWindow = main.get_node("HUD/Lobby")
	var in_lobby := await _until(func() -> bool: return connection.in_lobby and lobby.visible)
	_check(in_lobby and not connection.is_joined(), "joining puts the player in the lobby")
	if not in_lobby:
		_finish()
		return
	await _test_lobby(lobby)

	var joined := await _until(func() -> bool: return connection.is_joined())
	_check(joined and "/instances/" in connection._url, "Enter cave puts the player on an instance server")
	if not joined:
		_finish()
		return

	var character := connection.character
	_check(character != null and character.get_name() == connection.player_name and character.get_level() == 1, "server sends back the character under the joined name")
	var bar: XpBar = main.get_node("HUD/XpBar")
	_check(bar.visible and bar.text() == "Lv 1 · 0/100 XP", "XP bar shows a new character at level 1")

	var hud: RichTextLabel = main.get_node("HUD/Equipment")
	_check(await _until(func() -> bool: return "Rusty Sword" in hud.text), "HUD shows the starter kit")

	var latest := {"snapshot": null}
	connection.snapshot_received.connect(func(s: GameV1.WorldSnapshot) -> void: latest.snapshot = s)
	_check(await _until(func() -> bool: return latest.snapshot != null), "receives world snapshots")
	await _test_abilities(main, connection)

	var cave: Cave = main.get_node("Cave")
	var player: Player = main.get_node("Player")
	_check(cave.get_child_count() > 10 and connection.layout.get_walls().size() > 10, "built the cave the server sent")
	_check(player.position.distance_to(connection.spawn) < 1.0 and connection.spawn != Vector2.ZERO, "player starts at the server spawn")
	_check(cave.extraction.has_area(), "cave has an extraction zone")
	await _test_chest(main, connection, latest)

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

	await _test_loot_then_equip(main, hud)
	_finish()


# Stash the starter sword and equip it again from the stash, then enter.
func _test_lobby(lobby: LobbyWindow) -> void:
	_check(await _until(func() -> bool: return _lobby_button(lobby, "Rusty Sword", "To stash") != null), "the lobby lists the starter sword as gear")
	var stash := _lobby_button(lobby, "Rusty Sword", "To stash")
	if stash:
		stash.pressed.emit()
	_check(await _until(func() -> bool: return _lobby_button(lobby, "Rusty Sword", "Equip") != null), "the server moves the sword to the stash")
	var equip := _lobby_button(lobby, "Rusty Sword", "Equip")
	if equip:
		equip.pressed.emit()
	_check(await _until(func() -> bool: return _lobby_button(lobby, "Rusty Sword", "To stash") != null), "the sword is equipped again from the stash")
	_lobby_button(lobby, "", "Enter cave").pressed.emit()


# An empty item_name finds a button that is not on an item row.
func _lobby_button(lobby: LobbyWindow, item_name: String, text: String) -> Button:
	for node in lobby.find_children("*", "Button", true, false):
		var button := node as Button
		if button.text != text or button.is_queued_for_deletion():
			continue
		var label := button.get_parent().get_child(0)
		if item_name == "" or (label is RichTextLabel and item_name in (label as RichTextLabel).get_parsed_text()):
			return button
	return null


# A new player is level 1 and cleave unlocks at 2, so the server says no. The
# basic attack is open from level 1, so the server starts it and tells everyone.
func _test_abilities(main: Node, connection: ServerConnection) -> void:
	var bar: AbilityBar = main.get_node("HUD/Abilities")
	_check(bar.visible and bar.slot(0) != null and bar.slot(0).get_id() == "slash", "ability bar is built from the class the server sent")
	var seen := {"used": null, "rejected": null}
	connection.ability_used.connect(func(e: GameV1.AbilityUsed) -> void:
		if e.get_source_id() == connection.player_id:
			seen.used = e)
	connection.ability_rejected.connect(func(e: GameV1.AbilityRejected) -> void: seen.rejected = e)

	_check(bar.is_locked("cleave") and "Cleave (30)  Lv 2" in bar._list.get_parsed_text(), "the bar shows cleave locked until level 2")

	await _press("ability_1")
	_check(await _until(func() -> bool: return seen.rejected != null), "server rejects a locked cleave")
	if seen.rejected:
		_check(seen.rejected.get_ability_id() == "cleave" and seen.rejected.get_reason() == "locked", "rejected as locked")
		_check("unlocks at Lv 2" in bar.rejection(), "HUD says when cleave unlocks")
	var player: Player = main.get_node("Player")
	_check(player._rooted == 0.0 and player._sprite.animation() != "windup", "a rejected cleave releases the root")

	var motion := InputEventMouseMotion.new()
	motion.position = player.get_global_transform_with_canvas().origin + Vector2(-60, 80)
	motion.relative = Vector2(10, 0)
	root.push_input(motion, true)
	await _press("attack")
	_check(await _until(func() -> bool: return seen.used != null), "server starts the basic attack")
	if seen.used:
		_check(seen.used.get_ability_id() == "slash" and seen.used.get_windup_ms() == 100, "basic attack is slash with its windup")
		var aim := Vector2(seen.used.get_aim().get_x(), seen.used.get_aim().get_y())
		_check(aim.is_equal_approx(Vector2(-60, 80).normalized()) and aim.is_equal_approx(player.facing), "basic attack aims where the player faces")
		_check(bar.cooldown_left("slash") > 0.0, "slash shows its cooldown")


# Abilities are read from input events, which Input.action_press does not make.
func _press(action: String) -> void:
	for pressed in [true, false]:
		var event := InputEventAction.new()
		event.action = action
		event.pressed = pressed
		Input.parse_input_event(event)
		await physics_frame
		await physics_frame


# Walk from the spawn up the left corridor to the chest in the top left room,
# open it, and take one item.
func _test_chest(main: Node, connection: ServerConnection, latest: Dictionary) -> void:
	var server_pos := func() -> Vector2: return _server_pos(latest.snapshot, connection.player_id)
	Input.action_press("move_up")
	await _until(func() -> bool: return server_pos.call().y < 100.0, 8000)
	Input.action_release("move_up")
	Input.action_press("move_left")
	await _until(func() -> bool: return server_pos.call().x < 100.0, 3000)
	Input.action_release("move_left")

	var loot: LootWindow = main.get_node("HUD/Loot")
	await _press("interact")
	_check(await _until(func() -> bool: return loot.visible), "interact opens the chest's loot window")
	var bar: XpBar = main.get_node("HUD/XpBar")
	_check(await _until(func() -> bool: return bar.run_xp == 5 and not main.xp_popups().is_empty()), "opening the chest shows its run XP and a popup")
	if not loot.visible:
		return
	var rows := _loot_rows(loot)
	_check(rows.size() >= 2, "loot window lists the chest's items")
	var bag := {"ids": []}
	connection.inventory_received.connect(func(items: Array[GameV1.Item], _capacity: int) -> void:
		bag.ids = items.map(func(item: GameV1.Item) -> String: return item.get_id()))
	var want: String = loot._items[0].get_id()
	(rows[0].get_child(1) as Button).pressed.emit()
	_check(await _until(func() -> bool: return want in bag.ids), "take puts that one item in the bag")
	_check(await _until(func() -> bool: return loot.visible and _loot_rows(loot).size() == rows.size() - 1), "the item leaves the loot window, the rest stay")

	Input.parse_input_event(_key(KEY_ESCAPE, true))
	await physics_frame
	await physics_frame
	Input.parse_input_event(_key(KEY_ESCAPE, false))
	_check(await _until(func() -> bool: return not loot.visible), "escape closes the loot window")


func _loot_rows(loot: LootWindow) -> Array[HBoxContainer]:
	var rows: Array[HBoxContainer] = []
	for row in loot.get_node("List").get_children():
		if row is HBoxContainer and row.get_child(0) is RichTextLabel and not row.is_queued_for_deletion():
			rows.append(row)
	return rows


func _key(keycode: Key, pressed: bool) -> InputEventKey:
	var event := InputEventKey.new()
	event.physical_keycode = keycode
	event.keycode = keycode
	event.pressed = pressed
	return event


# Unequip the sword, drop it, loot it back with interact, and equip it again,
# all through the inventory panel and the server.
func _test_loot_then_equip(main: Node, hud: RichTextLabel) -> void:
	var panel: InventoryPanel = main.get_node("HUD/Inventory")
	Input.action_press("inventory")
	await physics_frame
	await physics_frame
	Input.action_release("inventory")
	_check(panel.visible, "inventory key opens the panel")

	var unequip := _button(panel, "Rusty Sword", "Unequip")
	_check(unequip != null, "panel lists the equipped sword")
	if unequip == null:
		return
	unequip.pressed.emit()
	_check(await _until(func() -> bool: return _button(panel, "Rusty Sword", "Drop") != null), "unequipped sword is in the bag")
	_check(await _until(func() -> bool: return "Rusty Sword" not in hud.text), "HUD drops the sword")

	_button(panel, "Rusty Sword", "Drop").pressed.emit()
	_check(await _until(func() -> bool: return main.nearest_loot(main.get_node("ServerGhost").position) != null), "dropped sword lies on the ground")
	_check(_button(panel, "Rusty Sword", "Drop") == null, "bag is empty after the drop")

	var loot: LootWindow = main.get_node("HUD/Loot")
	await _press("interact")
	_check(await _until(func() -> bool: return loot.visible and _loot_rows(loot).size() == 1), "interact opens the dropped sword's loot window")
	if _loot_rows(loot).is_empty():
		return
	_check("Rusty Sword" in _loot_rows(loot)[0].get_child(0).text, "the window shows the dropped sword")
	_loot_rows(loot)[0].get_child(1).pressed.emit()
	_check(await _until(func() -> bool: return _button(panel, "Rusty Sword", "Equip") != null), "take loots the sword into the bag")
	_check(await _until(func() -> bool: return not loot.visible), "loot window closes once the pile is empty")

	_button(panel, "Rusty Sword", "Equip").pressed.emit()
	_check(await _until(func() -> bool: return "Rusty Sword" in hud.text), "equipped the looted sword")
	_check(_button(panel, "Rusty Sword", "Unequip") != null and _button(panel, "Rusty Sword", "Equip") == null, "panel shows the sword equipped, not in the bag")


func _button(panel: InventoryPanel, item_name: String, text: String) -> Button:
	for row in panel.get_node("List").get_children():
		if not row is HBoxContainer or item_name not in (row.get_child(0) as RichTextLabel).text:
			continue
		for child in row.get_children():
			if child is Button and child.text == text:
				return child
	return null


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
