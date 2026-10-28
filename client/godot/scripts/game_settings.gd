class_name GameSettings
extends RefCounted

signal changed

const SECTION := "display"

var path := "user://settings.cfg"
var show_names := true
var show_bars := true


func load_file() -> void:
	var file := ConfigFile.new()
	if file.load(path) != OK:
		return
	show_names = bool(file.get_value(SECTION, "show_names", true))
	show_bars = bool(file.get_value(SECTION, "show_bars", true))
	changed.emit()


func save() -> void:
	var file := ConfigFile.new()
	file.set_value(SECTION, "show_names", show_names)
	file.set_value(SECTION, "show_bars", show_bars)
	file.save(path)


func set_show_names(on: bool) -> void:
	show_names = on
	save()
	changed.emit()


func set_show_bars(on: bool) -> void:
	show_bars = on
	save()
	changed.emit()
