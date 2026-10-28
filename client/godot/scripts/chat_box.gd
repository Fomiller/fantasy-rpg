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
const MAX_LINES := 200
const MAX_LINE := 300

# Set by main. Tests swap it for a fake.
var send_chat: Callable

var typing: bool:
	get:
		return _line.visible

@onready var _log: RichTextLabel = $List/Log
@onready var _line: LineEdit = $List/Input


func _ready() -> void:
	_line.max_length = MAX_LINE
	_line.visible = false
	_line.text_submitted.connect(_submit)
	_line.focus_exited.connect(close)
	_log.meta_clicked.connect(_on_name_clicked)


# Before the GUI sees it, so a focused button does not take Enter.
func _input(event: InputEvent) -> void:
	if not typing and event.is_action_pressed("chat"):
		open()
		get_viewport().set_input_as_handled()


func open(prefill := "") -> void:
	var was_typing := typing
	_line.visible = true
	_line.text = prefill
	_line.grab_focus()
	_line.caret_column = prefill.length()
	if not was_typing:
		typing_changed.emit(true)


func close() -> void:
	if not typing:
		return
	_line.text = ""
	_line.visible = false
	_line.release_focus()
	typing_changed.emit(false)


func add_message(message: GameV1.ChatMessage) -> void:
	var channel := message.get_channel()
	var sender := message.get_sender()
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
	open("/w %s " % str(meta))


func _submit(text: String) -> void:
	var line := text.strip_edges()
	close()
	if line != "" and send_chat.is_valid():
		send_chat.call(line)
