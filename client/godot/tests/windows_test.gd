extends SceneTree

var _failures := 0


func _initialize() -> void:
	_run.call_deferred()


func _run() -> void:
	var main: Node = load("res://scenes/main.tscn").instantiate()
	root.add_child(main)
	var inventory: InventoryPanel = main.get_node("HUD/Inventory")
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

	print("windows_test: %s" % ("PASS" if _failures == 0 else "FAIL (%d)" % _failures))
	quit(1 if _failures > 0 else 0)


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
