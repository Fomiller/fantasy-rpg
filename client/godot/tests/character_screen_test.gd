extends SceneTree

var _failures := 0
var _sent: Array[String] = []


func _initialize() -> void:
	_run.call_deferred()


func _run() -> void:
	var main: Node = load("res://scenes/main.tscn").instantiate()
	root.add_child(main)
	await _frames(2)
	var connection: ServerConnection = main.get_node("ServerConnection")
	var screen: CharacterScreen = main.get_node("HUD/Character")
	connection.player_id = 1
	main.get_node("Player").send_move = func(_direction: Vector2) -> int: return 0
	main.get_node("HUD/Abilities").setup(_warrior())
	main.send_attack = func(_target: int, ability_id: String, _aim: Vector2) -> int:
		_sent.append(ability_id)
		return 1
	screen.send_unequip = func(slot: String) -> void: _sent.append("unequip %s" % slot)
	connection.player_name = "local-name"
	screen.setup(_character("ann", 3, 50, 200), _warrior())

	await _key(KEY_C)
	await _frames(1)
	_check(screen.visible, "C opens the character screen")
	_check(_has_text(screen, "ann · Warrior · level 3"), "screen shows the server's name, class and level")
	_check(_has_text(screen, "XP 50/200 to level 4"), "screen shows XP into the level")

	connection._handle(_xp_gained(10, 15))
	_check(_has_text(screen, "Run XP 15"), "run XP shows after an XpGained")
	connection._handle(_run_ended(_character("ann", 4, 20, 250)))
	_check(_has_text(screen, "ann · Warrior · level 4") and _has_text(screen, "XP 20/250 to level 5") and not _has_text(screen, "Run XP"), "a run summary moves the screen to the new level")
	main.get_node("HUD/RunSummary").visible = false
	screen.show_character(_character("ann", 10, 30, 0))
	_check(_has_text(screen, "level 10") and _has_text(screen, "max level"), "the cap shows max level")
	_check(screen.find_children("*", "CharacterSprite", true, false).size() == 1, "screen shows the character sprite")

	connection._handle(_equipment(["Iron Sword"], {"damage": 17, "armor": 13, "max_health": 140, "move_speed": 5}))
	_check(screen.slot_item("weapon") != null and screen.slot_item("weapon").get_name() == "Iron Sword", "weapon slot shows the equipped item")
	_check(_rows_with(screen, "empty") == 5, "the other five slots show empty")
	_check(_has_text(screen, "Damage 17") and _has_text(screen, "Armor 13") and _has_text(screen, "Max Health 140") and _has_text(screen, "Move Speed 5%"), "stats come from the server message")

	main._on_snapshot(_snapshot(1, 80, 140, 20))
	_check(_has_text(screen, "Health 80/140") and _has_text(screen, "Rage 20/100"), "health and resource come from the snapshot")

	_button(screen, "Unequip").pressed.emit()
	_check(_sent == ["unequip weapon"], "unequip sends the slot")

	_sent.clear()
	await _frames(2)
	var unequip := _button(screen, "Unequip")
	await _click(unequip.get_global_rect().get_center())
	_check(_sent == ["unequip weapon"], "a click on unequip presses it and does not attack")
	_sent.clear()
	await _click(screen.get_global_rect().position + Vector2(4, 4))
	_check(_sent.is_empty(), "a click on the screen does not attack")

	connection._handle(_equipment([], {"damage": 10, "armor": 5, "max_health": 120}))
	_check(screen.slot_item("weapon") == null and _rows_with(screen, "empty") == 6, "an unequipped weapon leaves its slot empty")
	_check(_has_text(screen, "Damage 10") and not _has_text(screen, "Damage 17"), "new stats replace the old ones")

	await _press_escape()
	await _frames(2)
	_check(not screen.visible, "escape closes the character screen")

	await _key(KEY_C)
	await _frames(1)
	await _key(KEY_C)
	await _frames(1)
	_check(not screen.visible, "C toggles the screen closed")

	print("character_screen_test: %s" % ("PASS" if _failures == 0 else "FAIL (%d)" % _failures))
	quit(1 if _failures > 0 else 0)


func _equipment(names: Array, stats: Dictionary) -> PackedByteArray:
	var msg := GameV1.ServerMessage.new()
	msg.set_protocol_version(1)
	var equipment := msg.new_equipment()
	for item_name: String in names:
		var item := equipment.add_items()
		item.set_id(item_name.to_snake_case())
		item.set_item_def_id(item_name.to_snake_case())
		item.set_name(item_name)
		item.set_slot("weapon")
		item.set_rarity(GameV1.Rarity.COMMON)
		item.set_quantity(1)
	for stat: String in stats:
		var s := equipment.add_stats()
		s.set_stat(stat)
		s.set_value(stats[stat])
	return msg.to_bytes()


func _character(character_name: String, level: int, level_xp: int, to_next: int) -> GameV1.CharacterInfo:
	var info := GameV1.CharacterInfo.new()
	info.set_name(character_name)
	info.set_class_id("warrior")
	info.set_level(level)
	info.set_level_xp(level_xp)
	info.set_xp_to_next(to_next)
	return info


func _xp_gained(amount: int, run_xp: int) -> PackedByteArray:
	var msg := GameV1.ServerMessage.new()
	msg.set_protocol_version(1)
	var xp := msg.new_xp_gained()
	xp.set_amount(amount)
	xp.set_source("kill")
	xp.set_run_xp(run_xp)
	return msg.to_bytes()


func _run_ended(character: GameV1.CharacterInfo) -> PackedByteArray:
	var msg := GameV1.ServerMessage.new()
	msg.set_protocol_version(1)
	var c := msg.new_run_ended().new_summary().new_character()
	c.set_name(character.get_name())
	c.set_level(character.get_level())
	c.set_level_xp(character.get_level_xp())
	c.set_xp_to_next(character.get_xp_to_next())
	return msg.to_bytes()


func _snapshot(id: int, health: int, max_health: int, resource: int) -> GameV1.WorldSnapshot:
	var snapshot := GameV1.WorldSnapshot.new()
	var state := snapshot.add_entities()
	state.set_id(id)
	state.set_kind(GameV1.EntityKind.PLAYER)
	state.new_position()
	state.new_velocity()
	state.set_health(health)
	state.set_max_health(max_health)
	state.set_resource(resource)
	state.set_max_resource(100)
	return snapshot


func _warrior() -> GameV1.ClassInfo:
	var info := GameV1.ClassInfo.new()
	info.set_name("Warrior")
	info.set_resource("rage")
	info.set_max_resource(100)
	info.set_basic("slash")
	var ability := info.add_abilities()
	ability.set_id("slash")
	ability.set_name("Slash")
	return info


func _has_text(window: Control, text: String) -> bool:
	for node in window.find_children("*", "", true, false):
		if (node is Label or node is RichTextLabel) and text in node.text:
			return true
	return false


func _rows_with(window: Control, text: String) -> int:
	var count := 0
	for node in window.find_children("*", "Label", true, false):
		if (node as Label).text == text:
			count += 1
	return count


func _button(window: Control, text: String) -> Button:
	for node in window.find_children("*", "Button", true, false):
		if (node as Button).text == text:
			return node
	return null


func _key(keycode: Key) -> void:
	for pressed in [true, false]:
		var event := InputEventKey.new()
		event.physical_keycode = keycode
		event.keycode = keycode
		event.pressed = pressed
		Input.parse_input_event(event)
		Input.flush_buffered_events()
		await _frames(1)


func _press_escape() -> void:
	await _key(KEY_ESCAPE)


func _click(at: Vector2) -> void:
	for pressed in [true, false]:
		var event := InputEventMouseButton.new()
		event.button_index = MOUSE_BUTTON_LEFT
		event.button_mask = MOUSE_BUTTON_MASK_LEFT if pressed else 0
		event.position = at
		event.global_position = at
		event.pressed = pressed
		root.push_input(event, true)
		await _frames(1)


func _frames(count: int) -> void:
	for i in count:
		await physics_frame


func _check(ok: bool, name: String) -> void:
	print("%s %s" % ["ok  " if ok else "FAIL", name])
	if not ok:
		_failures += 1
