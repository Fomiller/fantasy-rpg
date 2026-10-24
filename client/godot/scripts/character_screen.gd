class_name CharacterScreen
extends PanelContainer

# Mirrors items.GearSlots, so an empty slot still gets a row.
const SLOTS := ["weapon", "head", "chest", "hands", "feet", "trinket"]
# The server has no levels yet.
const LEVEL := 1
const SPRITE_SCENE := preload("res://scenes/character_sprite.tscn")
const BODY := preload("res://assets/kenney_tiny_dungeon/tile_0097.png")
const SPRITE_SCALE := 4.0

# Set by main. Tests swap it for a fake. The screen only changes when the
# server answers with new equipment.
var send_unequip: Callable

var _name := ""
var _info: GameV1.ClassInfo
var _equipment: Dictionary[String, GameV1.Item] = {}

var _title := Label.new()
var _portrait := Control.new()
var _sprite: CharacterSprite = SPRITE_SCENE.instantiate()
var _slots := VBoxContainer.new()
var _vitals := Label.new()
var _stats := Label.new()

@onready var _list: VBoxContainer = $List


func _ready() -> void:
	add_to_group("windows")
	_portrait.custom_minimum_size = Vector2(80, 80)
	_sprite.texture = BODY
	_sprite.position = _portrait.custom_minimum_size / 2
	_sprite.scale = Vector2.ONE * SPRITE_SCALE
	_portrait.add_child(_sprite)
	for child: Control in [_title, _portrait, _slots, _vitals, _stats]:
		_list.add_child(child)
	clear()


func setup(character_name: String, info: GameV1.ClassInfo) -> void:
	_name = character_name
	_info = info
	_title.text = "%s · %s · level %d" % [_name, _info.get_name() if _info else "", LEVEL]


func clear() -> void:
	_name = ""
	_info = null
	_title.text = ""
	_vitals.text = ""
	_stats.text = ""
	var none: Array[GameV1.Item] = []
	show_equipment(none)


func show_equipment(items: Array[GameV1.Item]) -> void:
	_equipment.clear()
	for item in items:
		_equipment[item.get_slot()] = item
	var weapon: GameV1.Item = _equipment.get("weapon")
	_sprite.weapon = Player.weapon_texture(weapon.get_item_def_id() if weapon else "")

	for child in _slots.get_children():
		_slots.remove_child(child)
		child.queue_free()
	for slot: String in SLOTS:
		var item: GameV1.Item = _equipment.get(slot)
		var row: HBoxContainer
		if item:
			row = ItemRow.make(item, {"Unequip": _unequip.bind(slot)})
		else:
			row = HBoxContainer.new()
			var empty := Label.new()
			empty.text = "empty"
			empty.modulate = Color(1, 1, 1, 0.5)
			row.add_child(empty)
		var label := Label.new()
		label.text = slot.capitalize()
		label.custom_minimum_size.x = 64
		row.add_child(label)
		row.move_child(label, 0)
		_slots.add_child(row)


func show_stats(stats: Array[GameV1.ItemStat]) -> void:
	var lines: Array[String] = []
	for s in stats:
		var percent := "%" if s.get_stat() in ItemText.PERCENT_STATS else ""
		lines.append("%s %d%s" % [s.get_stat().capitalize(), s.get_value(), percent])
	_stats.text = "\n".join(lines)


# Health and resource come in every snapshot, so they get their own label
# instead of rebuilding the slot buttons under the mouse.
func set_vitals(health: int, max_health: int, resource: int, max_resource: int) -> void:
	var resource_name := _info.get_resource().capitalize() if _info else "Resource"
	_vitals.text = "Health %d/%d\n%s %d/%d" % [health, max_health, resource_name, resource, max_resource]


func slot_item(slot: String) -> GameV1.Item:
	return _equipment.get(slot)


func _unequip(slot: String) -> void:
	send_unequip.call(slot)
