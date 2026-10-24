extends SceneTree

# Needs a world-server on the URL in --server. `just net-test` starts one on
# 127.0.0.1:8090 and passes it.
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
	_check(in_lobby and not connection.is_joined(), "the world server puts the player in the lobby")
	if not in_lobby:
		_finish()
		return
	_check(":8090/" in connection._url, "the client is pointed at the world server")
	_check(not connection.saving and not lobby.saving, "a new character is not saving")
	_check(await _until(func() -> bool: return _lobby_button(lobby, "Rusty Sword", "To stash") != null), "the lobby lists the starter sword as gear")
	_check("Stash 0/100" in _texts(lobby), "the lobby shows an empty stash")

	_lobby_button(lobby, "Rusty Sword", "To stash").pressed.emit()
	_check(await _until(func() -> bool: return _lobby_button(lobby, "Rusty Sword", "Equip") != null), "the world server moves the sword to the stash")
	_check("Stash 1/100" in _texts(lobby), "the stash counts the sword")
	_lobby_button(lobby, "Rusty Sword", "Equip").pressed.emit()
	_check(await _until(func() -> bool: return _lobby_button(lobby, "Rusty Sword", "To stash") != null), "the sword is equipped again from the stash")

	# No instance to go to until FOM-228.
	_lobby_button(lobby, "", "Enter cave").pressed.emit()
	_check(await _until(func() -> bool: return lobby.message == "All instances are full."), "Enter cave says every instance is full")
	_check(not connection.is_joined() and connection.in_lobby, "the player stays in the lobby")
	_finish()


func _texts(lobby: LobbyWindow) -> String:
	var out := ""
	for node in lobby.find_children("*", "Label", true, false):
		if not node.is_queued_for_deletion():
			out += (node as Label).text + "\n"
	return out


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
