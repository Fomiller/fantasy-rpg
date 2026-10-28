extends SceneTree

var _failures := 0


func _initialize() -> void:
	_run.call_deferred()


func _run() -> void:
	var main: Node = load("res://scenes/main.tscn").instantiate()
	root.add_child(main)
	var inventory: InventoryPanel = main.get_node("HUD/Windows/Right/Column/Inventory")
	var player: Player = main.get_node("Player")
	await _frames(2)

	# The built-in ui_ actions are left out. The game never reads them.
	var escape_actions: Array[StringName] = []
	for action in InputMap.get_actions():
		if not action.begins_with("ui_") and InputMap.action_has_event(action, _escape(true)):
			escape_actions.append(action)
	_check(escape_actions == [&"close_window"], "escape is only bound to close_window")

	await _tap("inventory")
	await _frames(2)
	_check(inventory.visible, "inventory opens")

	# A focused control in the window must not keep Escape from closing it.
	var button := Button.new()
	inventory.get_node("List").add_child(button)
	button.grab_focus()
	await _frames(1)
	await _press_escape()
	await _frames(2)
	_check(not inventory.visible, "escape closes the inventory")

	var status: String = main.get_node("HUD/Status").text
	var position := player.position
	await _press_escape()
	await _frames(2)
	_check(not inventory.visible and player.alive and player.position == position and main.get_node("HUD/Status").text == status, "escape with nothing open changes nothing")

	var area: Control = main.get_node("HUD/Windows")
	var xp_bar: Control = main.get_node("HUD/XpBar")
	var character: CharacterScreen = main.get_node("HUD/Windows/Left/Character")
	var stash: StashWindow = main.get_node("HUD/Windows/Right/Column/Stash")
	var summary: RunSummaryWindow = main.get_node("HUD/Windows/Right/Column/RunSummary")

	var bag := _items(30)
	var gear := _items(6)
	var stats: Array[GameV1.ItemStat] = []
	for i in 12:
		var stat := GameV1.ItemStat.new()
		stat.set_stat("stat_%d" % i)
		stat.set_value(i)
		stats.append(stat)
	inventory.show_equipment(gear)
	inventory.show_inventory(bag, 30)
	character.show_equipment(gear)
	character.show_stats(stats)
	stash.show_equipment(gear)
	stash.show_inventory(bag, 30)
	stash.show_stash(bag, 30)
	var run := GameV1.RunSummary.new()
	for item in bag:
		run.add_kept().set_name(item.get_name())
	summary.show_summary(run)
	inventory.visible = true
	character.visible = true
	stash.visible = true
	await _frames(3)

	var shown: Array[Control] = [character, inventory, stash, summary]
	var overlap := false
	for i in shown.size():
		_check(shown[i].visible, "%s is open" % shown[i].name)
		for j in range(i + 1, shown.size()):
			overlap = overlap or shown[i].get_global_rect().intersects(shown[j].get_global_rect())
	_check(not overlap, "windows open together do not overlap")
	for column: Control in [area.get_node("Left"), area.get_node("Right")]:
		var rect := column.get_global_rect()
		_check(area.get_global_rect().encloses(rect), "%s column stays inside the window area" % column.name)
		_check(rect.end.y <= xp_bar.get_global_rect().position.y, "%s column stays above the XP bar" % column.name)
	for window in shown:
		var column: Control = window.get_parent().get_parent() if window != character else window.get_parent()
		_check(window.get_global_rect().position.x >= column.get_global_rect().position.x and window.get_global_rect().end.x <= column.get_global_rect().end.x + 1, "%s fits its column's width" % window.name)

	print("windows_test: %s" % ("PASS" if _failures == 0 else "FAIL (%d)" % _failures))
	quit(1 if _failures > 0 else 0)


func _items(count: int) -> Array[GameV1.Item]:
	var items: Array[GameV1.Item] = []
	for i in count:
		var item := GameV1.Item.new()
		item.set_id("item_%d" % i)
		item.set_name("Rusted Greatsword of the Long Night %d" % i)
		item.set_slot("weapon")
		item.set_rarity(GameV1.Rarity.RARE)
		item.set_quantity(1)
		for stat in ["attack", "attack_speed", "max_health"]:
			var base := item.add_base()
			base.set_stat(stat)
			base.set_value(i + 1)
		items.append(item)
	return items


func _escape(pressed: bool) -> InputEventKey:
	var event := InputEventKey.new()
	event.physical_keycode = KEY_ESCAPE
	event.keycode = KEY_ESCAPE
	event.pressed = pressed
	return event


func _press_escape() -> void:
	Input.parse_input_event(_escape(true))
	await _frames(1)
	Input.parse_input_event(_escape(false))


func _tap(action: String) -> void:
	Input.action_press(action)
	await _frames(1)
	Input.action_release(action)


func _frames(count: int) -> void:
	for i in count:
		await physics_frame


func _check(ok: bool, name: String) -> void:
	print("%s %s" % ["ok  " if ok else "FAIL", name])
	if not ok:
		_failures += 1
