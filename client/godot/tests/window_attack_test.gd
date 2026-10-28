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
	var inventory: InventoryPanel = main.get_node("HUD/Windows/Right/Column/Inventory")
	var loot: LootWindow = main.get_node("HUD/Windows/Right/Column/Loot")
	connection.player_id = 1
	main.get_node("Player").send_move = func(_direction: Vector2) -> int: return 0
	main.get_node("HUD/Abilities").setup(_warrior())
	main.send_attack = func(_target: int, ability_id: String, _aim: Vector2) -> int:
		_sent.append(ability_id)
		return 1
	loot.send_open = func(_id: int) -> void: pass
	loot.send_close = func(_id: int) -> void: pass
	loot.send_pickup = func(id: int, item_id: String) -> void: _sent.append("take %d %s" % [id, item_id])

	# The window closes itself when its pile is out of range.
	var chest: LootView = main._spawn_view(GameV1.EntityKind.CHEST)
	chest.apply(_pile(5))
	main._views[5] = chest
	main.get_node("ServerGhost").position = Vector2.ZERO

	await _tap("inventory")
	loot.open(5)
	loot.show_contents(_contents(5))
	await _frames(2)
	_check(inventory.visible and loot.visible, "inventory and loot windows are open")

	await _key(KEY_SPACE)
	await _key(KEY_Q)
	await _key(KEY_R)
	_check(_sent == ["slash", "cleave", "brace"], "space, Q and R use abilities with windows open")

	_sent.clear()
	var take := _button(loot, "Take all")
	await _click(take.get_global_rect().get_center())
	_check(_sent == ["take 5 "], "a click on a window button presses it and does not attack")

	_sent.clear()
	await _click(loot.get_global_rect().end - Vector2(4, 4))
	await _click(inventory.get_global_rect().position + Vector2(4, 4))
	_check(_sent.is_empty(), "a click on a window panel does not attack")

	await _click(Vector2(100, 600))
	_check(_sent == ["slash"], "a click outside the windows attacks")

	print("window_attack_test: %s" % ("PASS" if _failures == 0 else "FAIL (%d)" % _failures))
	quit(1 if _failures > 0 else 0)


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


func _tap(action: String) -> void:
	Input.action_press(action)
	await _frames(1)
	Input.action_release(action)


func _button(window: Control, text: String) -> Button:
	for node in window.find_children("*", "Button", true, false):
		if (node as Button).text == text:
			return node
	return null


func _pile(id: int) -> GameV1.EntityState:
	var state := GameV1.EntityState.new()
	state.set_id(id)
	state.set_kind(GameV1.EntityKind.CHEST)
	state.new_position()
	state.set_item_count(1)
	return state


func _contents(id: int) -> GameV1.LootContents:
	var contents := GameV1.LootContents.new()
	contents.set_target_id(id)
	var item := contents.add_items()
	item.set_id("helm-id")
	item.set_name("Iron Helm")
	item.set_rarity(GameV1.Rarity.COMMON)
	item.set_quantity(1)
	return contents


func _warrior() -> GameV1.ClassInfo:
	var info := GameV1.ClassInfo.new()
	info.set_basic("slash")
	for id in ["slash", "cleave", "brace"]:
		var ability := info.add_abilities()
		ability.set_id(id)
		ability.set_name(id.capitalize())
	return info


func _frames(count: int) -> void:
	for i in count:
		await physics_frame


func _check(ok: bool, name: String) -> void:
	print("%s %s" % ["ok  " if ok else "FAIL", name])
	if not ok:
		_failures += 1
