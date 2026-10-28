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
	var input: LineEdit = main.get_node("HUD/Chat/List/Row/Fields/Input")
	var mode_label: Label = main.get_node("HUD/Chat/List/Row/Fields/Mode")
	var chat_log: RichTextLabel = main.get_node("HUD/Chat/List/Log")
	var inventory: InventoryPanel = main.get_node("HUD/Windows/Right/Column/Inventory")
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
		"[dan] whispers: psst",
		"To [eve]: hi",
		"You are chatting too fast.",
	]:
		_check(text.contains(expected), "the log shows '%s'" % expected)

	chat._on_name_clicked("dan")
	await _frames(1)
	_check(chat.typing and input.text == "", "clicking a name opens an empty input")
	_check(mode_label.text == "To [dan]:", "clicking a name sets the whisper mode")
	chat.close()

	await _sticky_checks(chat, input, mode_label, connection)

	for i in 250:
		connection._handle(_chat("system", "", "line %d" % i))
	_check(chat_log.get_paragraph_count() <= ChatBox.MAX_LINES + 1, "the log keeps a bounded scrollback")

	connection._handle(_channels(["lfg"]))
	_check(connection.chat_left == ["general", "trade"], "the connection remembers the channels the player left")
	connection._handle(_channels(["general", "trade", "lfg"]))
	_check(connection.chat_left.is_empty(), "joining everything leaves nothing")

	print("chat_test: %s" % ("PASS" if _failures == 0 else "FAIL (%d)" % _failures))
	quit(1 if _failures > 0 else 0)


func _sticky_checks(chat: ChatBox, input: LineEdit, mode_label: Label, connection: ServerConnection) -> void:
	_sent.clear()
	_send(chat, input, "/s hi")
	_check(mode_label.text == "Say:" and _sent == ["/s hi"], "/s sets the Say mode and sends")
	_sent.clear()

	_send(chat, input, "/1 hello")
	_check(mode_label.text == "[General]" and _sent == ["/1 hello"], "/1 sets General and sends")
	_check(mode_label.get_theme_color("font_color") == ChatBox.COLORS["general"], "the label uses the channel color")
	_sent.clear()
	_send(chat, input, "plain")
	_check(_sent == ["/1 plain"], "a plain line goes to the current mode")
	_sent.clear()

	for row in [["/2 a", "[Trade]", "/2 b"], ["/3 a", "[LookingForGroup]", "/3 b"], ["/y a", "Yell:", "/y b"]]:
		_send(chat, input, row[0])
		_check(mode_label.text == row[1], "%s sets the mode" % row[0].substr(0, 2))
		_sent.clear()
		_send(chat, input, "b")
		_check(_sent == [row[2]], "a plain line uses %s" % row[1])
		_sent.clear()

	_send(chat, input, "/w bob hey")
	_check(mode_label.text == "To [bob]:" and _sent == ["/w bob hey"], "/w sets the whisper mode and sends")
	_sent.clear()
	_send(chat, input, "again")
	_check(_sent == ["/w bob again"], "a plain line whispers the target")
	_sent.clear()

	_send(chat, input, "/s")
	_sent.clear()
	_send(chat, input, "plain")
	_check(_sent == ["plain"], "Say sends the line as typed")
	_sent.clear()

	_send(chat, input, "/r hi dan")
	_check(mode_label.text == "To [dan]:" and _sent == ["/r hi dan"], "/r whispers whoever whispered last")
	_sent.clear()
	chat.add_message(_chat_message("whisper_from", "zed", "psst"))
	_send(chat, input, "/r back")
	_check(mode_label.text == "To [zed]:" and _sent == ["/r back"], "/r sets the mode to the last whisperer")
	_sent.clear()

	_send(chat, input, "/join 2")
	_send(chat, input, "/leave 3")
	_send(chat, input, "/bogus")
	_check(mode_label.text == "To [zed]:", "join, leave and unknown commands keep the mode")
	_sent.clear()

	chat.open()
	input.text = "/1 "
	input.text_changed.emit(input.text)
	_check(mode_label.text == "[General]" and input.text == "" and _sent.is_empty(), "typing /1 and a space switches at once")
	input.text = "/y "
	input.text_changed.emit(input.text)
	_check(mode_label.text == "Yell:" and input.text == "", "typing /y and a space switches at once")
	input.text = "/w "
	input.text_changed.emit(input.text)
	_check(mode_label.text == "Yell:" and input.text == "/w ", "a bare /w waits for the name")
	input.text = "/w amy "
	input.text_changed.emit(input.text)
	_check(mode_label.text == "To [amy]:" and input.text == "", "typing /w name and a space switches at once")
	input.text = "/r "
	input.text_changed.emit(input.text)
	_check(mode_label.text == "To [zed]:" and input.text == "", "typing /r and a space switches to the last whisperer")
	input.text = "/1 hi"
	input.text_changed.emit(input.text)
	_check(mode_label.text == "To [zed]:" and input.text == "/1 hi", "a command with text is left alone")
	chat.close()

	await _press(KEY_ENTER)
	await _frames(1)
	_check(chat.typing and mode_label.visible and mode_label.text == "To [zed]:", "enter opens in the last mode")
	await _press(KEY_ESCAPE)
	await _frames(2)
	_check(not chat.typing and not mode_label.visible and mode_label.text == "To [zed]:", "escape closes without changing the mode")

	chat._on_name_clicked("kim")
	connection._handle(_channels(["general", "trade", "lfg"]))
	_check(mode_label.text == "To [kim]:", "a whisper mode survives channel updates")
	_send(chat, input, "/2 x")
	connection._handle(_channels(["general", "lfg"]))
	_check(mode_label.text == "Say:", "leaving the current channel falls back to Say")
	_send(chat, input, "/1 x")
	connection._handle(_channels(["general", "trade", "lfg"]))
	_check(mode_label.text == "[General]", "joining channels keeps the mode")
	_sent.clear()


func _send(chat: ChatBox, input: LineEdit, line: String) -> void:
	chat.open()
	input.text = line
	input.text_submitted.emit(input.text)


func _chat_message(channel: String, sender: String, text: String) -> GameV1.ChatMessage:
	var message := GameV1.ChatMessage.new()
	message.set_channel(channel)
	message.set_sender(sender)
	message.set_text(text)
	return message


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
		await process_frame


func _check(ok: bool, name: String) -> void:
	print("%s %s" % ["ok  " if ok else "FAIL", name])
	if not ok:
		_failures += 1
