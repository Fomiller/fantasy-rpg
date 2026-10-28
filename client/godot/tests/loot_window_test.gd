extends SceneTree

var _failures := 0
var _sent: Array[String] = []


func _initialize() -> void:
	_run.call_deferred()


func _run() -> void:
	var main: Node = load("res://scenes/main.tscn").instantiate()
	root.add_child(main)
	await _frames(2)
	var loot: LootWindow = main.get_node("HUD/Windows/Right/Column/Loot")
	loot.send_open = func(id: int) -> void: _sent.append("open %d" % id)
	loot.send_close = func(id: int) -> void: _sent.append("close %d" % id)
	loot.send_pickup = func(id: int, item_id: String) -> void: _sent.append("take %d %s" % [id, item_id])

	var chest: LootView = main._spawn_view(GameV1.EntityKind.CHEST)
	chest.apply(_pile(5, Vector2(20, 0), 2))
	main._views[5] = chest
	main.get_node("ServerGhost").position = Vector2.ZERO

	await _tap("interact")
	_check(_sent == ["open 5"] and not loot.visible, "interact asks the server for the nearest pile")
	loot.show_contents(_contents(5, ["Iron Helm", "Health Potion"]))
	await _frames(1)
	_check(loot.visible and _row_count(loot) == 2, "window opens with what the server sent")

	_take(loot, "Health Potion").pressed.emit()
	_check(_sent.back() == "take 5 potion-id", "take sends the item's id")
	_button(loot, "Take all").pressed.emit()
	_check(_sent.back() == "take 5 ", "take all sends no item id")

	var full := GameV1.PickupRejected.new()
	full.set_target_id(5)
	full.set_reason("bag_full")
	loot.show_rejected(full)
	_check(_has_text(loot, "bag full"), "window says the bag is full")

	loot.show_contents(_contents(5, ["Iron Helm"]))
	_check(loot.visible and _row_count(loot) == 1, "window updates when the contents change")

	_sent.clear()
	await _press_escape()
	await _frames(2)
	_check(not loot.visible and _sent == ["close 5"], "escape closes the window and tells the server")
	_check(_row_count(loot) == 0, "a closed window keeps no rows to show next time")

	await _tap("interact")
	loot.show_contents(_contents(5, ["Iron Helm"]))
	main.get_node("ServerGhost").position = Vector2(200, 0)
	_sent.clear()
	await _frames(2)
	_check(not loot.visible and _sent == ["close 5"], "walking out of range closes the window")

	main.get_node("ServerGhost").position = Vector2.ZERO
	await _tap("interact")
	loot.show_contents(_contents(5, ["Iron Helm"]))
	_sent.clear()
	loot.show_contents(_contents(5, []))
	_check(not loot.visible and loot.target_id == 0 and _sent.is_empty(), "an empty pile closes the window")

	await _tap("interact")
	var closed := _contents(5, [])
	closed.set_closed(true)
	loot.show_contents(closed)
	_check(not loot.visible and loot.target_id == 0, "the server can close the window")

	loot.target_id = 0
	loot.show_contents(_contents(6, ["Iron Helm"]))
	_check(not loot.visible, "contents for a pile that is not open are ignored")

	print("loot_window_test: %s" % ("PASS" if _failures == 0 else "FAIL (%d)" % _failures))
	quit(1 if _failures > 0 else 0)


func _pile(id: int, pos: Vector2, count: int) -> GameV1.EntityState:
	var state := GameV1.EntityState.new()
	state.set_id(id)
	state.set_kind(GameV1.EntityKind.CHEST)
	var p := state.new_position()
	p.set_x(pos.x)
	p.set_y(pos.y)
	state.set_item_count(count)
	return state


func _contents(id: int, names: Array) -> GameV1.LootContents:
	var contents := GameV1.LootContents.new()
	contents.set_target_id(id)
	for item_name: String in names:
		var item := contents.add_items()
		item.set_id("potion-id" if "Potion" in item_name else "helm-id")
		item.set_name(item_name)
		item.set_rarity(GameV1.Rarity.COMMON)
		item.set_quantity(1)
	return contents


func _rows(loot: LootWindow) -> Array[HBoxContainer]:
	var rows: Array[HBoxContainer] = []
	for row in loot.get_node("List").get_children():
		if row is HBoxContainer and row.get_child(0) is RichTextLabel and not row.is_queued_for_deletion():
			rows.append(row)
	return rows


func _row_count(loot: LootWindow) -> int:
	return _rows(loot).size()


func _take(loot: LootWindow, item_name: String) -> Button:
	for row in _rows(loot):
		if item_name in (row.get_child(0) as RichTextLabel).text:
			return row.get_child(1)
	return null


func _button(loot: LootWindow, text: String) -> Button:
	for row in loot.get_node("List").get_children():
		for child in row.get_children():
			if child is Button and child.text == text and not row.is_queued_for_deletion():
				return child
	return null


func _has_text(loot: LootWindow, text: String) -> bool:
	for row in loot.get_node("List").get_children():
		for child in row.get_children():
			if child is Label and text in child.text:
				return true
	return false


func _escape(pressed: bool) -> InputEventKey:
	var event := InputEventKey.new()
	event.physical_keycode = KEY_ESCAPE
	event.keycode = KEY_ESCAPE
	event.pressed = pressed
	return event


func _press_escape() -> void:
	Input.parse_input_event(_escape(true))
	await _frames(2)
	Input.parse_input_event(_escape(false))


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
