extends SceneTree

const STASH := Vector2(300, 300)
const ENTRANCE := Rect2(600, 0, 128, 64)
const RETURN_SPAWN := Vector2(640, 100)

var _failures := 0
var _sent: Array[String] = []


func _initialize() -> void:
	_run.call_deferred()


func _run() -> void:
	var main: Node = load("res://scenes/main.tscn").instantiate()
	root.add_child(main)
	await _frames(2)
	var connection: ServerConnection = main.get_node("ServerConnection")
	var stash: StashWindow = main.get_node("HUD/Windows/Right/Column/Stash")
	var summary: RunSummaryWindow = main.get_node("HUD/Windows/Right/Column/RunSummary")
	var player: Player = main.get_node("Player")
	var ghost: Node2D = main.get_node("ServerGhost")
	var cave: Cave = main.get_node("Cave")
	var status: Label = main.get_node("HUD/Status")
	var toast: Label = main.get_node("HUD/Toast")
	var panel: InventoryPanel = main.get_node("HUD/Windows/Right/Column/Inventory")
	player.send_move = func(_direction: Vector2) -> int: return 0
	main.send_attack = func(_target: int, ability_id: String, _aim: Vector2) -> int:
		_sent.append("attack %s" % ability_id)
		return 1
	stash.send_move = func(item_id: String, to: int) -> void: _sent.append("move %s %s" % [item_id, GameV1.ItemPlace.find_key(to)])

	_check(not stash.visible, "no stash window before the town")
	connection._handle(_joined(true, Vector2(300, 400)))
	connection._handle(_lobby())
	connection._handle(_equipment([_item("sword", "Rusty Sword", "weapon")]))
	connection._handle(_inventory([_item("potion", "Health Potion", "consumable")]))
	connection._handle(_stash([_item("helm", "Iron Helm", "head")]))
	await _frames(1)
	_check(connection.in_town and connection.is_joined(), "the town join gives the player a body")
	_check(player.visible and player.alive and player.position == Vector2(300, 400), "the player stands at the town spawn")
	_check(cave.has_stash and cave.stash == STASH and cave.entrance == ENTRANCE and not cave.extraction.has_area(), "the town draws the chest and the entrance, and no exit")
	_check(_has_text(cave, "CAVE") and not _has_text(cave, "EXIT"), "the entrance is labeled CAVE")
	_check(not stash.visible, "the stash window waits for the chest")
	_check(main.get_node("HUD/Equipment").text != "", "the HUD shows gear in the town")

	await _tap("inventory")
	_check(panel.visible and _has_row(panel, "Health Potion") and not _has_row(panel, "Rusty Sword"), "the inventory panel lists the bag in the town")
	_check(_button(panel, "Health Potion", "To stash") == null, "no To stash button while the stash is shut")
	_check(_button(panel, "Health Potion", "Use") == null and _button(panel, "Rusty Sword", "Drop") == null, "the town hides Use and Drop")
	await _tap("inventory")

	await _tap("attack")
	await _tap("ability_1")
	main.use_ability(0)
	await _click(Vector2(100, 600))
	_check(_sent.is_empty() and not player._sprite.animation() == "windup", "the town never attacks (sent %s)" % [_sent])

	ghost.position = Vector2(300, 400)
	await _tap("interact")
	_check(not stash.visible, "interact far from the chest does nothing")
	ghost.position = STASH + Vector2(0, 60)
	await _tap("interact")
	_check(stash.visible, "interact by the chest opens the stash")
	_check(panel.visible, "opening the stash opens the inventory")
	_check(_has_text(stash, "Stash 1/100") and not _has_text(stash, "Bag"), "the stash window shows only the stash")
	_check(_has_text(panel, "Bag 1/20") and not _has_text(panel, "Equipped"), "the inventory shows only the bag")
	_check(_has_row(stash, "Iron Helm") and not _has_row(stash, "Health Potion") and not _has_row(stash, "Rusty Sword"), "the stash window lists only stash items")
	_check(not _has_text(stash, "level 2") and _button(stash, "", "Enter cave") == null, "the stash window has no title and no enter button")

	_check(_button(panel, "Health Potion", "To stash") != null, "the bag gets a To stash button while the stash is open")
	_button(stash, "Iron Helm", "Equip").pressed.emit()
	_button(stash, "Iron Helm", "To bag").pressed.emit()
	_check(_sent == ["move helm GEAR", "move helm BAG"], "each button asks the server to move that item (sent %s)" % [_sent])
	_check(_button(stash, "Health Potion", "Equip") == null, "a potion has no equip button")
	_check(_has_row(stash, "Iron Helm") and _has_row(panel, "Health Potion") and not _has_row(stash, "Health Potion"), "nothing moves before the server answers")

	connection._handle(_rejected("helm", "far"))
	await _frames(1)
	_check(_has_text(stash, "too far from the chest"), "a move from too far says why")
	connection._handle(_equipment([]))
	connection._handle(_stash([_item("helm", "Iron Helm", "head"), _item("sword", "Rusty Sword", "weapon")]))
	await _frames(1)
	_check(not _has_text(stash, "too far") and _button(stash, "Rusty Sword", "Equip") != null, "the server's answer moves the sword to the stash")

	await _key(KEY_ESCAPE)
	await _frames(2)
	_check(not stash.visible, "escape closes the stash window")
	_check(panel.visible and _button(panel, "Health Potion", "To stash") == null, "closing the stash takes the To stash button away and leaves the inventory open")
	await _tap("interact")
	ghost.position = STASH + Vector2(0, 100)
	await _frames(2)
	_check(not stash.visible, "walking out of range closes the stash window")

	connection._handle(_lobby(true))
	ghost.position = STASH
	await _tap("interact")
	_check(connection.saving and _has_text(stash, "Saving..."), "a run still saving shows Saving...")
	connection._handle(_rejected("helm", "saving"))
	await _frames(1)
	_check(_has_text(stash, "Couldn't save, try again"), "a move the server could not save says so")
	connection._handle(_lobby())
	await _frames(1)
	_check(not connection.saving and not _has_text(stash, "Saving..."), "the save landing clears Saving...")
	_check(player.visible and connection.is_joined(), "a Lobby keeps the body")

	# The predicted player walks into the entrance.
	var inside := ENTRANCE.get_center()
	player.place(inside)
	await _frames(2)
	connection._handle(_snapshot(inside))
	await _frames(1)
	_check(status.text == "Entering the cave...", "stepping into the entrance says so (got %s)" % status.text)
	connection._handle(_enter_rejected("saving"))
	connection._handle(_snapshot(inside))
	await _frames(1)
	_check(toast.visible and toast.text == "Still saving your last run" and status.text == "town", "a rejected entrance says why and stops waiting")
	player.place(RETURN_SPAWN)
	await _frames(2)
	player.place(inside)
	await _frames(2)
	connection._handle(_snapshot(inside))
	await _frames(1)
	_check(status.text == "Entering the cave...", "stepping out and back in tries again")

	var world_url := connection._url
	connection._handle(_ticket("ws://127.0.0.1:8081/instances/instance-0/ws", "tok"))
	connection._closed(1000, "entering instance")
	_check(connection._url == "ws://127.0.0.1:8081/instances/instance-0/ws" and connection._token == "tok", "the world's close sends the client to the ticket's instance")
	await _frames(1)
	_check(not player.visible and main._views.is_empty() and not cave.has_stash, "the town clears when its socket closes")
	_check(not status.text.begins_with("offline"), "the way in is not offline")
	connection._handle(_joined(false, Vector2(10, 10)))
	await _frames(1)
	_check(connection.is_joined() and not connection.in_town and player.visible and player.alive, "the player is in the cave")
	_check(cave.extraction.has_area() and not cave.entrance.has_area() and _has_text(cave, "EXIT"), "the cave draws its exit and no entrance")
	main._views[9] = main._spawn_view(GameV1.EntityKind.ENEMY)

	# Extraction: the summary comes first and stays on top of the town.
	connection._handle(_extracted())
	connection._closed(1000, "run over")
	_check(connection._url == world_url and connection._token == "" and connection._returning, "the run's close sends the client back to the world, returning")
	connection._handle(_joined(true, RETURN_SPAWN))
	connection._handle(_lobby())
	await _frames(1)
	_check(not connection._returning, "the town join clears returning")
	_check(summary.visible and player.visible and player.position == RETURN_SPAWN, "after a run the player is back in town under the summary")
	_check(main._views.is_empty(), "the cave is cleared")
	_check(_has_text(summary, "Stash full, left in the bag") and _has_text(summary, "Iron Axe"), "the summary lists what the stash had no room for")
	await _key(KEY_ESCAPE)
	await _frames(2)
	_check(not summary.visible, "escape closes the summary")

	# An instance that turns the token away sends the player back with why.
	connection._handle(_ticket("ws://127.0.0.1:8081/instances/instance-0/ws", "old"))
	connection._closed(1000, "entering instance")
	connection._closed(1008, "token expired")
	_check(connection._url == world_url and connection._returning, "a rejected token goes back to the world")
	connection._handle(_joined(true, RETURN_SPAWN))
	connection._handle(_lobby())
	await _frames(1)
	_check(toast.visible and toast.text == "Took too long to join. Try again.", "the town says the token expired")

	connection._closed(1013, "characters unavailable")
	await _frames(1)
	_check(status.text == "offline · Can't load your character. Try again in a minute.", "the join screen says why the server closed (got %s)" % status.text)
	connection._closed(1013, "town full")
	await _frames(1)
	_check(status.text == "offline · The town is full. Trying again...", "a full town says it will try again (got %s)" % status.text)
	connection._closed(1008, "name already playing")
	await _frames(1)
	_check(status.text == "offline · name already playing", "a reason with no text of its own is shown as sent (got %s)" % status.text)

	# The front door sends the player to a shard's own path, with the same join.
	var shard_url := "ws://127.0.0.1:8080/towns/world-1/ws"
	var offline := [0]
	connection.disconnected.connect(func() -> void: offline[0] += 1)
	connection._returning = true
	connection._handle(_town_ticket(shard_url))
	connection._closed(1000, "changing town")
	_check(connection._url == shard_url and connection._retry_in == 0.0 and connection._returning, "a TownTicket sends the client to the shard at once, still returning")
	connection._closed(1013, "town full")
	_check(connection._url == world_url and connection._retry_in == 0.0, "a full shard sends the client back to the front door at once")
	_check(offline[0] == 0, "going back to the front door once is not offline")
	connection._handle(_town_ticket(shard_url))
	connection._closed(1000, "changing town")
	connection._closed(1013, "town full")
	await _frames(1)
	_check(connection._url == world_url and status.text == "offline · The town is full. Trying again...", "a second full shard goes offline and retries the front door (got %s)" % status.text)
	connection._handle(_town_ticket(shard_url))
	connection._closed(1000, "changing town")
	connection._closed(1006, "")
	_check(connection._url == world_url, "a drop on a shard reconnects through the front door")
	connection._handle(_joined(true, RETURN_SPAWN))
	_check(not connection._fell_back, "a town join lets the next full shard send the client back once more")

	print("town_test: %s" % ("PASS" if _failures == 0 else "FAIL (%d)" % _failures))
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


func _ticket(url: String, token: String) -> PackedByteArray:
	var msg := _message()
	var ticket := msg.new_ticket()
	ticket.set_url(url)
	ticket.set_token(token)
	return msg.to_bytes()


func _town_ticket(url: String) -> PackedByteArray:
	var msg := _message()
	msg.new_town_ticket().set_url(url)
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


func _rect(rect: GameV1.Rect, r: Rect2) -> void:
	var a := rect.new_min()
	a.set_x(r.position.x)
	a.set_y(r.position.y)
	var b := rect.new_max()
	b.set_x(r.end.x)
	b.set_y(r.end.y)


func _joined(town: bool, spawn: Vector2) -> PackedByteArray:
	var msg := _message()
	var joined := msg.new_joined()
	joined.set_player_id(1)
	joined.set_town(town)
	var layout := joined.new_layout()
	layout.set_name("town" if town else "test")
	_rect(layout.new_bounds(), Rect2(0, 0, 1536, 832))
	if town:
		_rect(layout.new_entrance(), ENTRANCE)
		var chest := layout.new_stash()
		chest.set_x(STASH.x)
		chest.set_y(STASH.y)
	else:
		_rect(layout.new_extraction(), Rect2(0, 0, 10, 10))
	var at := joined.new_spawn()
	at.set_x(spawn.x)
	at.set_y(spawn.y)
	joined.new_class_info().set_name("Warrior")
	joined.new_character().set_name("ann")
	return msg.to_bytes()


func _snapshot(at: Vector2) -> PackedByteArray:
	var msg := _message()
	var me := msg.new_snapshot().add_entities()
	me.set_id(1)
	me.set_kind(GameV1.EntityKind.PLAYER)
	var pos := me.new_position()
	pos.set_x(at.x)
	pos.set_y(at.y)
	me.new_velocity()
	me.set_health(120)
	me.set_max_health(120)
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
			if child is Button and child.text == text and not child.is_queued_for_deletion():
				return child
	return null


func _has_text(node: Node, text: String) -> bool:
	for child in node.find_children("*", "", true, false):
		if child.is_queued_for_deletion():
			continue
		if child is Label and text in (child as Label).text:
			return true
		if child is RichTextLabel and text in (child as RichTextLabel).get_parsed_text():
			return true
	return false


# Abilities and interact are read from input events, which Input.action_press does not make.
func _tap(action: String) -> void:
	for pressed in [true, false]:
		var event := InputEventAction.new()
		event.action = action
		event.pressed = pressed
		Input.parse_input_event(event)
		Input.flush_buffered_events()
		await _frames(2)


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
