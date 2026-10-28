class_name SettingsWindow
extends PanelContainer

var _settings: GameSettings
var _names: CheckButton
var _bars: CheckButton

@onready var _list: VBoxContainer = $List


func _ready() -> void:
	add_to_group("windows")
	var title := Label.new()
	title.text = "Settings"
	_list.add_child(title)
	_names = _toggle("Show player names")
	_bars = _toggle("Show health bars")
	_names.toggled.connect(func(on: bool) -> void: _settings.set_show_names(on))
	_bars.toggled.connect(func(on: bool) -> void: _settings.set_show_bars(on))


func setup(settings: GameSettings) -> void:
	_settings = settings
	settings.changed.connect(_sync)
	_sync()


func _sync() -> void:
	_names.set_pressed_no_signal(_settings.show_names)
	_bars.set_pressed_no_signal(_settings.show_bars)


func _toggle(text: String) -> CheckButton:
	var toggle := CheckButton.new()
	toggle.text = text
	# Space is attack. A focused toggle would take it too.
	toggle.focus_mode = Control.FOCUS_NONE
	_list.add_child(toggle)
	return toggle
