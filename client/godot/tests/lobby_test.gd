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
	var lobby: LobbyWindow = main.get_node("HUD/Lobby")
	var summary: RunSummaryWindow = main.get_node("HUD/RunSummary")
	var player: Player = main.get_node("Player")
	main.get_node("Player").send_move = func(_direction: Vector2) -> int: return 0
	main.send_attack = func(_target: int, ability_id: String, _aim: Vector2) -> int:
		_sent.append("attack %s" % ability_id)
		return 1
	lobby.send_move = func(item_id: String, to: int) -> void: _sent.append("move %s %s" % [item_id, GameV1.ItemPlace.find_key(to)])
	lobby.send_enter = func() -> void: _sent.append("enter")

	_check(not lobby.visible, "no lobby before the server sends one")
	connection._handle(_lobby())
	connection._handle(_equipment([_item("sword", "Rusty Sword", "weapon")]))
	connection._handle(_inventory([_item("potion", "Health Potion", "consumable")]))
	connection._handle(_stash([_item("helm", "Iron Helm", "head")]))
	await _frames(1)
	_check(connection.in_lobby and not connection.is_joined(), "the connection is in the lobby, not the cave")
	_check(lobby.visible and _has_text(lobby, "ann · Warrior · level 2"), "the lobby shows the character")
	_check(_has_text(lobby, "Bag 1/20") and _has_text(lobby, "Stash 1/100"), "the lobby shows bag and stash space")
	_check(main.get_node("HUD/Equipment").text == "", "the cave HUD does not show gear in the lobby")

	_button(lobby, "Rusty Sword", "To stash").pressed.emit()
	_button(lobby, "Health Potion", "To stash").pressed.emit()
	_button(lobby, "Iron Helm", "Equip").pressed.emit()
	_button(lobby, "Iron Helm", "To bag").pressed.emit()
	_check(_sent == ["move sword STASH", "move potion STASH", "move helm GEAR", "move helm BAG"], "each button asks the server to move that item (sent %s)" % [_sent])
	_check(_button(lobby, "Health Potion", "Equip") == null, "a potion has no equip button")
	_check(_has_row(lobby, "Iron Helm") and _has_row(lobby, "Rusty Sword"), "nothing moves before the server answers")

	connection._handle(_rejected("helm", "full"))
	await _frames(1)
	_check(_has_text(lobby, "no room"), "a rejected move says why")
	connection._handle(_equipment([]))
	connection._handle(_stash([_item("helm", "Iron Helm", "head"), _item("sword", "Rusty Sword", "weapon")]))
	await _frames(1)
	_check(not _has_text(lobby, "no room") and _button(lobby, "Rusty Sword", "Equip") != null, "the server's answer moves the sword to the stash")

	_sent.clear()
	await _click(lobby.get_global_rect().position + Vector2(4, 4))
	await _click(Vector2(100, 600))
	_check(_sent.is_empty(), "clicks in the lobby never attack")
	await _key(KEY_ESCAPE)
	await _frames(2)
	_check(lobby.visible, "escape does not close the lobby")

	connection._handle(_lobby(true))
	await _frames(1)
	_check(connection.saving and _has_text(lobby, "Saving..."), "a run still saving shows Saving...")
	_check(_button(lobby, "", "Enter cave").disabled, "enter cave is off while saving")
	connection._handle(_enter_rejected("saving"))
	await _frames(1)
	_check(_has_text(lobby, "Still saving your last run"), "a rejected enter cave says why")
	connection._handle(_rejected("helm", "saving"))
	await _frames(1)
	_check(_has_text(lobby, "Couldn't save, try again"), "a move the server could not save says so")
	connection._handle(_lobby())
	await _frames(1)
	_check(not connection.saving and not _has_text(lobby, "Saving...") and not _button(lobby, "", "Enter cave").disabled, "the save landing turns enter cave back on")

	_button(lobby, "", "Enter cave").pressed.emit()
	_check(_sent == ["enter"], "enter cave asks the server")

	connection._handle(_joined())
	await _frames(1)
	_check(connection.is_joined() and not connection.in_lobby and not lobby.visible, "entering the cave hides the lobby")
	_check(player.visible and player.alive, "the player is in the cave")
	main._views[9] = main._spawn_view(GameV1.EntityKind.ENEMY)

	# Extraction: the summary comes first and stays on top of the lobby.
	connection._handle(_extracted())
	connection._handle(_lobby())
	await _frames(1)
	_check(lobby.visible and summary.visible, "after a run the lobby is back under the summary")
	_check(not player.visible and main._views.is_empty(), "the cave is cleared")
	_check(_has_text(summary, "Stash full, left in the bag") and _has_text(summary, "Iron Axe"), "the summary lists what the stash had no room for")
	await _key(KEY_ESCAPE)
	await _frames(2)
	_check(not summary.visible and lobby.visible, "escape closes the summary and leaves the lobby")

	var status: Label = main.get_node("HUD/Status")
	connection._closed(1013, "characters unavailable")
	await _frames(1)
	_check(status.text == "offline · Can't load your character. Try again in a minute.", "the join screen says why the server closed (got %s)" % status.text)
	connection._closed(1008, "name already playing")
	await _frames(1)
	_check(status.text == "offline · name already playing", "a reason with no text of its own is shown as sent (got %s)" % status.text)

	print("lobby_test: %s" % ("PASS" if _failures == 0 else "FAIL (%d)" % _failures))
	quit(1 if _failures > 0 else 0)


func _message() -> GameV1.ServerMessage:
	var msg := GameV1.ServerMessage.new()
	msg.set_protocol_version(1)
	return msg


func _lobby(saving := false) -> PackedByteArray:
	var msg := _message()
	var lobby := msg.new_lobby()
	lobby.set_saving(saving)
	var info := lobby.new_class_info()
	info.set_name("Warrior")
	info.set_basic("slash")
	var character := lobby.new_character()
	character.set_name("ann")
	character.set_level(2)
	return msg.to_bytes()


func _item(id: String, item_name: String, slot: String) -> GameV1.Item:
	var item := GameV1.Item.new()
	item.set_id(id)
	item.set_name(item_name)
	item.set_slot(slot)
	item.set_rarity(GameV1.Rarity.COMMON)
	item.set_quantity(1)
	return item


func _fill(add: Callable, items: Array) -> void:
	for item: GameV1.Item in items:
		var copy: GameV1.Item = add.call()
		copy.set_id(item.get_id())
		copy.set_name(item.get_name())
		copy.set_slot(item.get_slot())
		copy.set_rarity(item.get_rarity())
		copy.set_quantity(item.get_quantity())


func _equipment(items: Array) -> PackedByteArray:
	var msg := _message()
	_fill(msg.new_equipment().add_items, items)
	return msg.to_bytes()


func _inventory(items: Array) -> PackedByteArray:
	var msg := _message()
	var inventory := msg.new_inventory()
	inventory.set_capacity(20)
	_fill(inventory.add_items, items)
	return msg.to_bytes()


func _stash(items: Array) -> PackedByteArray:
	var msg := _message()
	var stash := msg.new_stash()
	stash.set_capacity(100)
	_fill(stash.add_items, items)
	return msg.to_bytes()


func _rejected(item_id: String, reason: String) -> PackedByteArray:
	var msg := _message()
	var r := msg.new_stash_move_rejected()
	r.set_item_id(item_id)
	r.set_reason(reason)
	return msg.to_bytes()


func _enter_rejected(reason: String) -> PackedByteArray:
	var msg := _message()
	msg.new_enter_instance_rejected().set_reason(reason)
	return msg.to_bytes()


func _joined() -> PackedByteArray:
	var msg := _message()
	var joined := msg.new_joined()
	joined.set_player_id(1)
	var cave := joined.new_layout()
	cave.set_name("test")
	for rect: GameV1.Rect in [cave.new_bounds(), cave.new_extraction()]:
		rect.new_min()
		rect.new_max().set_x(10)
	joined.new_spawn()
	joined.new_class_info().set_name("Warrior")
	joined.new_character().set_name("ann")
	return msg.to_bytes()


func _extracted() -> PackedByteArray:
	var msg := _message()
	var extracted := msg.new_extracted()
	extracted.set_player_id(1)
	var summary := extracted.new_summary()
	summary.set_extracted(true)
	summary.new_character().set_name("ann")
	_fill(summary.add_kept, [_item("axe", "Iron Axe", "weapon")])
	_fill(summary.add_overflow, [_item("axe", "Iron Axe", "weapon")])
	return msg.to_bytes()


func _rows(window: Control) -> Array[HBoxContainer]:
	var rows: Array[HBoxContainer] = []
	for node in window.find_children("*", "HBoxContainer", true, false):
		if node.get_child_count() > 0 and node.get_child(0) is RichTextLabel and not node.is_queued_for_deletion():
			rows.append(node)
	return rows


func _has_row(window: Control, item_name: String) -> bool:
	for row in _rows(window):
		if item_name in (row.get_child(0) as RichTextLabel).get_parsed_text():
			return true
	return false


# An empty item_name finds a button that is not on an item row.
func _button(window: Control, item_name: String, text: String) -> Button:
	if item_name == "":
		for node in window.find_children("*", "Button", true, false):
			if (node as Button).text == text and not node.is_queued_for_deletion():
				return node
		return null
	for row in _rows(window):
		if item_name not in (row.get_child(0) as RichTextLabel).get_parsed_text():
			continue
		for child in row.get_children():
			if child is Button and child.text == text:
				return child
	return null


func _has_text(window: Control, text: String) -> bool:
	for node in window.find_children("*", "", true, false):
		if node.is_queued_for_deletion():
			continue
		if node is Label and text in (node as Label).text:
			return true
		if node is RichTextLabel and text in (node as RichTextLabel).get_parsed_text():
			return true
	return false


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


func _frames(count: int) -> void:
	for i in count:
		await physics_frame


func _check(ok: bool, name: String) -> void:
	print("%s %s" % ["ok  " if ok else "FAIL", name])
	if not ok:
		_failures += 1
