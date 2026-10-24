class_name XpBar
extends Control

const BACK_COLOR := Color(0, 0, 0, 0.5)
const BANKED_COLOR := Color(0.95, 0.78, 0.3)
# Run XP is not banked until the run ends, so it is a lighter fill past the banked one.
const PENDING_COLOR := Color(0.95, 0.78, 0.3, 0.35)
const FLASH_COLOR := Color(1.8, 1.8, 1.2)
const FLASH_SECONDS := 0.8

var level := 1
var level_xp := 0
var xp_to_next := 0
var run_xp := 0

var _label := Label.new()


func _ready() -> void:
	mouse_filter = Control.MOUSE_FILTER_IGNORE
	_label.set_anchors_preset(Control.PRESET_FULL_RECT)
	_label.horizontal_alignment = HORIZONTAL_ALIGNMENT_CENTER
	_label.vertical_alignment = VERTICAL_ALIGNMENT_CENTER
	_label.add_theme_font_size_override("font_size", 12)
	add_child(_label)
	clear()


func show_character(info: GameV1.CharacterInfo) -> void:
	level = info.get_level()
	level_xp = info.get_level_xp()
	xp_to_next = info.get_xp_to_next()
	run_xp = info.get_run_xp()
	visible = true
	_refresh()


func set_run_xp(value: int) -> void:
	run_xp = value
	_refresh()


func clear() -> void:
	level = 1
	level_xp = 0
	xp_to_next = 0
	run_xp = 0
	visible = false
	_refresh()


func flash() -> void:
	modulate = FLASH_COLOR
	create_tween().tween_property(self, "modulate", Color.WHITE, FLASH_SECONDS)


# At the level cap the bar is full.
func banked_fraction() -> float:
	return 1.0 if xp_to_next == 0 else clampf(float(level_xp) / xp_to_next, 0.0, 1.0)


func pending_fraction() -> float:
	return 1.0 if xp_to_next == 0 else clampf(float(level_xp + run_xp) / xp_to_next, 0.0, 1.0)


func text() -> String:
	return _label.text


func _refresh() -> void:
	var progress := "max" if xp_to_next == 0 else "%d/%d XP" % [level_xp, xp_to_next]
	_label.text = "Lv %d · %s" % [level, progress]
	if run_xp > 0:
		_label.text += " (+%d)" % run_xp
	queue_redraw()


func _draw() -> void:
	draw_rect(Rect2(Vector2.ZERO, size), BACK_COLOR)
	draw_rect(Rect2(Vector2.ZERO, Vector2(size.x * pending_fraction(), size.y)), PENDING_COLOR)
	draw_rect(Rect2(Vector2.ZERO, Vector2(size.x * banked_fraction(), size.y)), BANKED_COLOR)
