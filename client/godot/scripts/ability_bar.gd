class_name AbilityBar
extends VBoxContainer

# Key hints in slot order. Slot 0 is the basic attack.
const KEYS := ["LMB", "1/Q", "2/R", "3/F"]
const REJECT_SECONDS := 1.5
const LOCKED_COLOR := "#ffffff80"
const REASONS := {
	"cooldown": "not ready",
	"resource": "not enough %s",
	"locked": "unlocks at Lv %d",
	"busy": "busy",
	"aim": "nothing to aim at",
	"unknown": "unknown ability",
}

var resource := 0
var max_resource := 0
# From the player's snapshot. Abilities above it are locked.
var level := 1

var _info: GameV1.ClassInfo
var _slots: Array[GameV1.AbilityInfo] = []
# Seconds left, counted from the AbilityUsed the server sent.
var _cooldowns: Dictionary[String, float] = {}
var _reject_left := 0.0

var _resource_bar := ProgressBar.new()
var _list := RichTextLabel.new()
var _rejected := Label.new()


func _ready() -> void:
	mouse_filter = Control.MOUSE_FILTER_IGNORE
	_resource_bar.custom_minimum_size = Vector2(200, 12)
	_resource_bar.show_percentage = false
	_resource_bar.mouse_filter = Control.MOUSE_FILTER_IGNORE
	_rejected.modulate = Color(1, 0.5, 0.4)
	_list.bbcode_enabled = true
	_list.fit_content = true
	_list.mouse_filter = Control.MOUSE_FILTER_IGNORE
	for child: Control in [_resource_bar, _list, _rejected]:
		add_child(child)
	clear()


func setup(info: GameV1.ClassInfo) -> void:
	_info = info
	_slots.clear()
	_cooldowns.clear()
	for a: GameV1.AbilityInfo in info.get_abilities():
		if a.get_id() == info.get_basic():
			_slots.push_front(a)
		else:
			_slots.append(a)
	max_resource = info.get_max_resource()
	visible = true
	_refresh()


func clear() -> void:
	_info = null
	_slots.clear()
	_cooldowns.clear()
	level = 1
	visible = false


# Null when the slot has no ability.
func slot(index: int) -> GameV1.AbilityInfo:
	return _slots[index] if index < _slots.size() else null


func cooldown_left(ability_id: String) -> float:
	return _cooldowns.get(ability_id, 0.0)


func set_resource(value: int, max_value: int) -> void:
	resource = value
	max_resource = max_value


func set_level(value: int) -> void:
	level = value


func is_locked(ability_id: String) -> bool:
	var a := _ability(ability_id)
	return a != null and a.get_unlock_level() > level


func started(ability_id: String, cooldown_ms: int) -> void:
	_cooldowns[ability_id] = cooldown_ms / 1000.0


func rejected(ability_id: String, reason: String) -> void:
	var text: String = REASONS.get(reason, reason)
	if reason == "resource" and _info:
		text = text % _info.get_resource()
	elif reason == "locked":
		var a := _ability(ability_id)
		text = text % a.get_unlock_level() if a else reason
	_rejected.text = "%s: %s" % [_name(ability_id), text]
	_reject_left = REJECT_SECONDS


func rejection() -> String:
	return _rejected.text


func _process(delta: float) -> void:
	for id: String in _cooldowns.keys():
		_cooldowns[id] = maxf(_cooldowns[id] - delta, 0.0)
	_reject_left = maxf(_reject_left - delta, 0.0)
	if _reject_left == 0.0:
		_rejected.text = ""
	_refresh()


func _refresh() -> void:
	if _info == null:
		return
	_resource_bar.max_value = max_resource
	_resource_bar.value = resource
	_resource_bar.tooltip_text = _info.get_resource()
	var lines: Array[String] = ["%s %d/%d" % [_info.get_resource(), resource, max_resource]]
	for i in _slots.size():
		var a := _slots[i]
		var line := "[lb]%s] %s" % [KEYS[i] if i < KEYS.size() else "-", a.get_name()]
		if a.get_cost() > 0:
			line += " (%d)" % a.get_cost()
		var left := cooldown_left(a.get_id())
		if is_locked(a.get_id()):
			line = "[color=%s]%s  Lv %d[/color]" % [LOCKED_COLOR, line, a.get_unlock_level()]
		elif left > 0.0:
			line += "  %.1fs" % left
		elif a.get_cost() > resource:
			line += "  -"
		lines.append(line)
	_list.text = "\n".join(lines)


func _name(ability_id: String) -> String:
	var a := _ability(ability_id)
	return a.get_name() if a else ability_id


func _ability(ability_id: String) -> GameV1.AbilityInfo:
	for a in _slots:
		if a.get_id() == ability_id:
			return a
	return null
