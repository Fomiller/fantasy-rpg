class_name InventoryPanel
extends PanelContainer

# Every button sends a request. The panel only changes when the server
# answers with a new inventory.
var connection: ServerConnection

var _stash_open := false
var _items: Array[GameV1.Item] = []
var _capacity := 0

@onready var _list: VBoxContainer = $List


func _ready() -> void:
	add_to_group("windows")


func set_stash_open(is_open: bool) -> void:
	_stash_open = is_open
	_rebuild()


func show_inventory(items: Array[GameV1.Item], capacity: int) -> void:
	_items = items
	_capacity = capacity
	_rebuild()


func clear() -> void:
	_items = []
	_capacity = 0
	_rebuild()


func _rebuild() -> void:
	for child in _list.get_children():
		_list.remove_child(child)
		child.queue_free()

	# A potion is for a run, and nothing can be dropped in the town.
	var in_run := not connection.in_town
	_heading("Bag %d/%d" % [_items.size(), _capacity])
	for item in _items:
		var actions := {}
		if item.get_slot() == "consumable":
			if in_run:
				actions["Use"] = connection.send_use.bind(item.get_id())
		elif item.get_slot() != "material":
			actions["Equip"] = connection.send_equip.bind(item.get_id())
		if in_run:
			actions["Drop"] = connection.send_drop.bind(item.get_id())
		if _stash_open:
			actions["To stash"] = connection.send_stash_move.bind(item.get_id(), GameV1.ItemPlace.STASH)
		_row(item, actions)


func _heading(text: String) -> void:
	var label := Label.new()
	label.text = text
	_list.add_child(label)


func _row(item: GameV1.Item, actions: Dictionary) -> void:
	_list.add_child(ItemRow.make(item, actions))
