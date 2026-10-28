class_name ChatBox
extends PanelContainer

signal typing_changed(on: bool)

const COLORS := {
	"say": Color(1, 1, 1),
	"yell": Color(1, 0.4, 0.35),
	"general": Color(1, 0.8, 0.6),
	"trade": Color(0.95, 0.75, 0.3),
	"lfg": Color(0.55, 0.85, 0.5),
	"whisper_from": Color(0.9, 0.55, 1),
	"whisper_to": Color(0.9, 0.55, 1),
	"system": Color(1, 0.9, 0.4),
}
const CHANNEL_NAMES := {
	"general": "General",
	"trade": "Trade",
	"lfg": "LookingForGroup",
}
const MODE_WORDS := {
	"s": "say", "say": "say",
	"y": "yell", "yell": "yell",
	"1": "general", "general": "general",
	"2": "trade", "trade": "trade",
	"3": "lfg", "lfg": "lfg", "lookingforgroup": "lfg",
}
const MODE_PREFIXES := {
	"say": "",
	"yell": "/y ",
	"general": "/1 ",
	"trade": "/2 ",
	"lfg": "/3 ",
}
const WHISPER_WORDS := ["w", "whisper", "t", "tell"]
const MAX_LINES := 200
const MAX_LINE := 300

# Set by main. Tests swap it for a fake.
var send_chat: Callable

var typing: bool:
	get:
		return _line.visible

var _mode := "say"
var _target := ""
var _last_whisperer := ""

@onready var _log: RichTextLabel = $List/Log
@onready var _row: PanelContainer = $List/Row
@onready var _mode_label: Label = $List/Row/Fields/Mode
@onready var _line: LineEdit = $List/Row/Fields/Input


func _ready() -> void:
	_line.max_length = MAX_LINE
	_row.add_theme_stylebox_override("panel", _line.get_theme_stylebox("normal").duplicate())
	for style in ["normal", "focus", "read_only"]:
		_line.add_theme_stylebox_override(style, StyleBoxEmpty.new())
	_line.visible = false
	_mode_label.visible = false
	_refresh_mode()
	_line.text_submitted.connect(_submit)
	_line.text_changed.connect(_on_text_changed)
	_line.focus_exited.connect(close)
	_log.meta_clicked.connect(_on_name_clicked)


# Before the GUI sees it, so a focused button does not take Enter.
func _input(event: InputEvent) -> void:
	if not typing and event.is_action_pressed("chat"):
		open()
		get_viewport().set_input_as_handled()


func open(prefill := "") -> void:
	var was_typing := typing
	_row.visible = true
	_line.visible = true
	_mode_label.visible = true
	_line.text = prefill
	_line.grab_focus()
	_line.caret_column = prefill.length()
	if not was_typing:
		typing_changed.emit(true)


func close() -> void:
	if not typing:
		return
	_line.text = ""
	_row.visible = false
	_line.visible = false
	_mode_label.visible = false
	_line.release_focus()
	typing_changed.emit(false)


func apply_left_channels(left: Array[String]) -> void:
	if left.has(_mode):
		_switch_mode("say")


func add_message(message: GameV1.ChatMessage) -> void:
	var channel := message.get_channel()
	var sender := message.get_sender()
	if channel == "whisper_from":
		_last_whisperer = sender
	_log.push_color(COLORS.get(channel, Color.WHITE))
	match channel:
		"system":
			_log.add_text(message.get_text())
		"whisper_to":
			_log.add_text("To ")
			_add_name(sender)
			_log.add_text(": %s" % message.get_text())
		"whisper_from":
			_add_name(sender)
			_log.add_text(" whispers: %s" % message.get_text())
		"say", "yell":
			_add_name(sender)
			_log.add_text(" %s: %s" % ["says" if channel == "say" else "yells", message.get_text()])
		_:
			_log.add_text("[%s] " % CHANNEL_NAMES.get(channel, channel))
			_add_name(sender)
			_log.add_text(": %s" % message.get_text())
	_log.pop()
	_log.newline()
	while _log.get_paragraph_count() > MAX_LINES:
		_log.remove_paragraph(0)


func _add_name(player: String) -> void:
	_log.push_meta(player)
	_log.add_text(player)
	_log.pop()


func _on_name_clicked(meta: Variant) -> void:
	_switch_mode("whisper", str(meta))
	open()


func _on_text_changed(text: String) -> void:
	if not text.begins_with("/") or not text.ends_with(" "):
		return
	var parts := text.strip_edges().substr(1).split(" ", false)
	var word := parts[0].to_lower() if parts.size() > 0 else ""
	if parts.size() == 1 and MODE_WORDS.has(word):
		_switch_mode(MODE_WORDS[word])
	elif parts.size() == 1 and word in ["r", "reply"] and _last_whisperer != "":
		_switch_mode("whisper", _last_whisperer)
	elif parts.size() == 2 and word in WHISPER_WORDS:
		_switch_mode("whisper", parts[1])
	else:
		return
	_line.text = ""


func _switch_mode(mode: String, target := "") -> void:
	_mode = mode
	_target = target
	_refresh_mode()


func _refresh_mode() -> void:
	var color_key := "whisper_to" if _mode == "whisper" else _mode
	_mode_label.add_theme_color_override("font_color", COLORS[color_key])
	if _mode == "whisper":
		_mode_label.text = "To %s:" % _target
	elif CHANNEL_NAMES.has(_mode):
		_mode_label.text = "[%s]" % CHANNEL_NAMES[_mode]
	else:
		_mode_label.text = "%s:" % _mode.capitalize()


func _follow_command(line: String) -> void:
	var word := line.substr(1).get_slice(" ", 0).to_lower()
	var rest := line.substr(1 + word.length()).strip_edges()
	if MODE_WORDS.has(word):
		_switch_mode(MODE_WORDS[word])
	elif word in WHISPER_WORDS and rest != "":
		_switch_mode("whisper", rest.get_slice(" ", 0))
	elif word in ["r", "reply"] and _last_whisperer != "":
		_switch_mode("whisper", _last_whisperer)


func _prefix() -> String:
	if _mode == "whisper":
		return "/w %s " % _target
	return MODE_PREFIXES[_mode]


func _submit(text: String) -> void:
	var line := text.strip_edges()
	close()
	if line == "":
		return
	if line.begins_with("/"):
		_follow_command(line)
	else:
		line = _prefix() + line
	if send_chat.is_valid():
		send_chat.call(line)
