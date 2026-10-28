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
	var bar: XpBar = main.get_node("HUD/XpBar")
	var banner: LevelUpBanner = main.get_node("HUD/LevelUp")
	var summary: RunSummaryWindow = main.get_node("HUD/Windows/Right/Column/RunSummary")
	var abilities: AbilityBar = main.get_node("HUD/Abilities")
	main.get_node("Player").send_move = func(_direction: Vector2) -> int: return 0
	main.send_attack = func(_target: int, ability_id: String, _aim: Vector2) -> int:
		_sent.append(ability_id)
		return 1

	connection._handle(_joined(_character("ann", 2, 50, 150, 0)))
	await _frames(1)
	_check(connection.character.get_name() == "ann", "the connection keeps the character from JoinAccepted")
	_check(bar.visible and bar.text() == "Lv 2 · 50/150 XP", "XP bar shows banked progress to the next level")
	_check(is_equal_approx(bar.banked_fraction(), 1.0 / 3.0) and is_equal_approx(bar.pending_fraction(), 1.0 / 3.0), "no run XP means no pending fill")

	connection._handle(_xp_gained(10, 10))
	connection._handle(_xp_gained(5, 15))
	await _frames(1)
	_check(bar.text() == "Lv 2 · 50/150 XP (+15)", "run XP shows as pending on the bar")
	_check(is_equal_approx(bar.banked_fraction(), 1.0 / 3.0) and is_equal_approx(bar.pending_fraction(), 65.0 / 150.0), "run XP is a second fill past the banked one")
	var popups: Array[Node] = main.xp_popups()
	_check(popups.size() == 2 and (popups[0] as Label).text == "+10 XP" and (popups[1] as Label).text == "+5 XP", "each XpGained shows a +N XP popup")
	_check((popups[0] as Control).position != (popups[1] as Control).position, "popups stack instead of overlapping")
	await _seconds(XpPopup.SECONDS + 0.3)
	_check(main.xp_popups().is_empty(), "popups go away on their own")

	connection._handle(_xp_gained(500, 515))
	_check(is_equal_approx(bar.pending_fraction(), 1.0), "pending fill stops at the end of the bar")

	# Death: the level comes first, then the summary.
	connection.player_id = 1
	connection._handle(_level_up(3))
	await _frames(1)
	_check(banner.visible and "level 3" in banner.text, "LevelUp shows a level up message")
	_check(abilities.level == 3, "LevelUp moves the ability bar's level")
	_check(bar.modulate != Color.WHITE, "LevelUp flashes the XP bar")
	connection._handle(_death(_character("ann", 3, 3, 200, 0)))
	await _frames(1)
	_check(summary.visible, "a death shows the run summary")
	_check(_has_text(summary, "You died") and _has_text(summary, "Run XP 515") and _has_text(summary, "Banked 128 XP") and _has_text(summary, "Levels gained 1 · level 3"), "summary shows run XP, banked XP and levels")
	_check(_has_text(summary, "nothing") and summary.find_children("*", "RichTextLabel", true, false).is_empty(), "a death keeps no loot")
	_check(bar.text() == "Lv 3 · 3/200 XP", "the bar moves to the banked XP after the run")
	_check(summary.is_in_group("windows"), "the summary is a window")

	_sent.clear()
	await _click(summary.get_global_rect().position + Vector2(4, 4))
	_check(_sent.is_empty(), "a click on the summary does not attack")
	await _click(Vector2(100, 600))
	_check(_sent == ["slash"], "a click outside the summary still attacks")

	await _key(KEY_ESCAPE)
	await _frames(2)
	_check(not summary.visible, "escape closes the summary")
	await _seconds(LevelUpBanner.POP_SECONDS + LevelUpBanner.HOLD_SECONDS + LevelUpBanner.FADE_SECONDS + 0.3)
	_check(not banner.visible, "the level up message fades out")

	# Extraction: the summary rides on Extracted and lists the bag.
	connection._handle(_extracted(1, _character("ann", 3, 40, 200, 0), ["Iron Helm", "Health Potion"]))
	await _frames(1)
	_check(summary.visible and _has_text(summary, "Extracted") and _has_text(summary, "Iron Helm") and _has_text(summary, "Health Potion"), "an extraction summary lists the loot kept")
	_check(not _has_text(summary, "nothing"), "kept loot replaces the old rows")
	_button(summary, "Close").pressed.emit()
	_check(not summary.visible, "the close button closes the summary")

	connection._handle(_extracted(2, null, []))
	_check(not summary.visible, "another player's extraction shows nothing")

	summary.visible = true
	connection._handle(_joined(_character("ann", 3, 40, 200, 0)))
	_check(not summary.visible, "joining again closes the last summary")

	print("progression_test: %s" % ("PASS" if _failures == 0 else "FAIL (%d)" % _failures))
	quit(1 if _failures > 0 else 0)


func _message() -> GameV1.ServerMessage:
	var msg := GameV1.ServerMessage.new()
	msg.set_protocol_version(1)
	return msg


func _character(character_name: String, level: int, level_xp: int, to_next: int, run_xp: int) -> Dictionary:
	return {"name": character_name, "level": level, "level_xp": level_xp, "to_next": to_next, "run_xp": run_xp}


func _fill(info: GameV1.CharacterInfo, c: Dictionary) -> void:
	info.set_name(c.name)
	info.set_class_id("warrior")
	info.set_level(c.level)
	info.set_level_xp(c.level_xp)
	info.set_xp_to_next(c.to_next)
	info.set_run_xp(c.run_xp)


func _joined(c: Dictionary) -> PackedByteArray:
	var msg := _message()
	var joined := msg.new_joined()
	joined.set_player_id(1)
	var cave := joined.new_layout()
	cave.set_name("test")
	for rect: GameV1.Rect in [cave.new_bounds(), cave.new_extraction()]:
		rect.new_min()
		rect.new_max().set_x(10)
	joined.new_spawn()
	var info := joined.new_class_info()
	info.set_name("Warrior")
	info.set_basic("slash")
	var slash := info.add_abilities()
	slash.set_id("slash")
	slash.set_name("Slash")
	slash.set_unlock_level(1)
	_fill(joined.new_character(), c)
	return msg.to_bytes()


func _xp_gained(amount: int, run_xp: int) -> PackedByteArray:
	var msg := _message()
	var xp := msg.new_xp_gained()
	xp.set_amount(amount)
	xp.set_source("kill")
	xp.set_run_xp(run_xp)
	return msg.to_bytes()


func _level_up(level: int) -> PackedByteArray:
	var msg := _message()
	msg.new_level_up().set_level(level)
	return msg.to_bytes()


func _death(c: Dictionary) -> PackedByteArray:
	var msg := _message()
	var summary := msg.new_run_ended().new_summary()
	summary.set_run_xp(515)
	summary.set_banked_xp(128)
	summary.set_levels_gained(1)
	_fill(summary.new_character(), c)
	return msg.to_bytes()


func _extracted(id: int, c: Variant, kept: Array) -> PackedByteArray:
	var msg := _message()
	var extracted := msg.new_extracted()
	extracted.set_player_id(id)
	if c != null:
		var summary := extracted.new_summary()
		summary.set_extracted(true)
		summary.set_run_xp(40)
		summary.set_banked_xp(75)
		_fill(summary.new_character(), c)
		for item_name: String in kept:
			var item := summary.add_kept()
			item.set_id(item_name.to_snake_case())
			item.set_name(item_name)
			item.set_rarity(GameV1.Rarity.COMMON)
			item.set_quantity(1)
	return msg.to_bytes()


func _has_text(window: Control, text: String) -> bool:
	for node in window.find_children("*", "", true, false):
		if node.is_queued_for_deletion():
			continue
		if node is Label and text in (node as Label).text:
			return true
		if node is RichTextLabel and text in (node as RichTextLabel).get_parsed_text():
			return true
	return false


func _button(window: Control, text: String) -> Button:
	for node in window.find_children("*", "Button", true, false):
		if (node as Button).text == text and not node.is_queued_for_deletion():
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


func _seconds(seconds: float) -> void:
	await create_timer(seconds).timeout


func _frames(count: int) -> void:
	for i in count:
		await physics_frame


func _check(ok: bool, name: String) -> void:
	print("%s %s" % ["ok  " if ok else "FAIL", name])
	if not ok:
		_failures += 1
