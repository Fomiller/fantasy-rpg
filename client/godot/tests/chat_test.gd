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
	var chat: ChatBox = main.get_node("HUD/Chat")
	var input: LineEdit = main.get_node("HUD/Chat/List/Input")
	var chat_log: RichTextLabel = main.get_node("HUD/Chat/List/Log")
	var inventory: InventoryPanel = main.get_node("HUD/Inventory")
	var player: Player = main.get_node("Player")
	chat.send_chat = func(line: String) -> void: _sent.append(line)

	_check(not chat.typing and not input.visible, "the input is closed at the start")
	await _press(KEY_ENTER)
	await _frames(1)
	_check(chat.typing and input.visible and input.has_focus(), "enter opens the input")
	_check(player.input_blocked, "the player is blocked while typing")

	await _tap("inventory")
	await _frames(1)
	_check(not inventory.visible, "typing an I does not open the inventory")
	Input.action_press("move_right")
	await _frames(2)
	_check(player.get_move_input() == Vector2.ZERO, "typing does not move the player")
	Input.action_release("move_right")

	await _press(KEY_ESCAPE)
	await _frames(2)
	_check(not chat.typing and not input.visible, "escape closes the input")
	Input.action_press("move_right")
	await _frames(1)
	_check(player.get_move_input() != Vector2.ZERO, "the player moves again once it is closed")
	Input.action_release("move_right")

	chat.open()
	input.text = "  /w bob hello  "
	input.text_submitted.emit(input.text)
	await _frames(1)
	_check(_sent == ["/w bob hello"] and not chat.typing, "enter sends the trimmed line and closes")
	_sent.clear()
	chat.open()
	input.text_submitted.emit("   ")
	_check(_sent.is_empty() and not chat.typing, "an empty line is not sent")

	connection._handle(_chat("say", "ann", "hello [b]there[/b]"))
	connection._handle(_chat("yell", "bob", "over here"))
	connection._handle(_chat("general", "cat", "anyone"))
	connection._handle(_chat("lfg", "cat", "need a tank"))
	connection._handle(_chat("whisper_from", "dan", "psst"))
	connection._handle(_chat("whisper_to", "eve", "hi"))
	connection._handle(_chat("system", "", "You are chatting too fast."))
	var text := chat_log.get_parsed_text()
	for expected in [
		"ann says: hello [b]there[/b]",
		"bob yells: over here",
		"[General] cat: anyone",
		"[LookingForGroup] cat: need a tank",
		"dan whispers: psst",
		"To eve: hi",
		"You are chatting too fast.",
	]:
		_check(text.contains(expected), "the log shows '%s'" % expected)

	chat._on_name_clicked("dan")
	await _frames(1)
	_check(chat.typing and input.text == "/w dan ", "clicking a name starts a whisper")
	chat.close()

	for i in 250:
		connection._handle(_chat("system", "", "line %d" % i))
	_check(chat_log.get_paragraph_count() <= ChatBox.MAX_LINES + 1, "the log keeps a bounded scrollback")

	connection._handle(_channels(["lfg"]))
	_check(connection.chat_left == ["general", "trade"], "the connection remembers the channels the player left")
	connection._handle(_channels(["general", "trade", "lfg"]))
	_check(connection.chat_left.is_empty(), "joining everything leaves nothing")

	print("chat_test: %s" % ("PASS" if _failures == 0 else "FAIL (%d)" % _failures))
	quit(1 if _failures > 0 else 0)


func _chat(channel: String, sender: String, text: String) -> PackedByteArray:
	var msg := GameV1.ServerMessage.new()
	var chat := msg.new_chat()
	chat.set_channel(channel)
	chat.set_sender(sender)
	chat.set_text(text)
	return msg.to_bytes()


func _channels(joined: Array[String]) -> PackedByteArray:
	var msg := GameV1.ServerMessage.new()
	var channels := msg.new_chat_channels()
	for channel in joined:
		channels.add_joined(channel)
	return msg.to_bytes()


func _key(code: Key, pressed: bool) -> InputEventKey:
	var event := InputEventKey.new()
	event.physical_keycode = code
	event.keycode = code
	event.pressed = pressed
	return event


func _press(code: Key) -> void:
	Input.parse_input_event(_key(code, true))
	await _frames(1)
	Input.parse_input_event(_key(code, false))


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
