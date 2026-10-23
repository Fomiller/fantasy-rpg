class_name InventoryPanel
extends PanelContainer

# Every button sends a request. The panel only changes when the server
# answers with new equipment or inventory.
var connection: ServerConnection

var _equipment: Array[GameV1.Item] = []
var _items: Array[GameV1.Item] = []
var _capacity := 0

@onready var _list: VBoxContainer = $List


func _ready() -> void:
	add_to_group("windows")


func show_equipment(items: Array[GameV1.Item]) -> void:
	_equipment = items
	_rebuild()


func show_inventory(items: Array[GameV1.Item], capacity: int) -> void:
	_items = items
	_capacity = capacity
	_rebuild()


func clear() -> void:
	_equipment = []
	_items = []
	_capacity = 0
	_rebuild()


func _rebuild() -> void:
	for child in _list.get_children():
		_list.remove_child(child)
		child.queue_free()

	_heading("Equipped")
	for item in _equipment:
		_row(item, {"Unequip": connection.send_unequip.bind(item.get_slot())})

	_heading("Bag %d/%d" % [_items.size(), _capacity])
	for item in _items:
		var actions := {}
		if item.get_slot() == "consumable":
			actions["Use"] = connection.send_use.bind(item.get_id())
		elif item.get_slot() != "material":
			actions["Equip"] = connection.send_equip.bind(item.get_id())
		actions["Drop"] = connection.send_drop.bind(item.get_id())
		_row(item, actions)


func _heading(text: String) -> void:
	var label := Label.new()
	label.text = text
	_list.add_child(label)


func _row(item: GameV1.Item, actions: Dictionary) -> void:
	var row := HBoxContainer.new()
	var label := RichTextLabel.new()
	label.bbcode_enabled = true
	label.fit_content = true
	label.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	label.text = ItemText.line(item)
	if item.get_quantity() > 1:
		label.text += "  x%d" % item.get_quantity()
	row.add_child(label)
	for text: String in actions:
		var button := Button.new()
		button.text = text
		# Space is attack. A focused button would take it too.
		button.focus_mode = Control.FOCUS_NONE
		button.pressed.connect(actions[text])
		row.add_child(button)
	_list.add_child(row)
