class_name LootWindow
extends PanelContainer

const BAG_FULL_TEXT := "bag full"
const GONE_TEXT := "already taken"

# Set by main. Tests swap them for fakes.
var send_open: Callable
var send_close: Callable
var send_pickup: Callable

# The pile this window shows, or 0 when closed.
var target_id := 0
var message := ""

var _items: Array[GameV1.Item] = []

@onready var _list: VBoxContainer = $List


func _ready() -> void:
	add_to_group("windows")
	visibility_changed.connect(_on_visibility_changed)


# Asks the server for the pile. The window shows once the contents arrive.
func open(id: int) -> void:
	if target_id != 0 and target_id != id:
		close()
	target_id = id
	message = ""
	send_open.call(id)


func close() -> void:
	visible = false
	_on_visibility_changed()


func show_contents(contents: GameV1.LootContents) -> void:
	if contents.get_target_id() != target_id:
		return
	_items = contents.get_items()
	if contents.get_closed() or _items.is_empty():
		target_id = 0
		visible = false
		return
	_rebuild()
	visible = true


func show_rejected(event: GameV1.PickupRejected) -> void:
	if event.get_target_id() != target_id:
		return
	message = BAG_FULL_TEXT if event.get_reason() == "bag_full" else GONE_TEXT
	_rebuild()


func _take(item_id: String) -> void:
	message = ""
	send_pickup.call(target_id, item_id)


# Escape hides the window without calling close(), so this is where the
# server hears about it.
func _on_visibility_changed() -> void:
	if visible:
		return
	_clear()
	if target_id != 0:
		send_close.call(target_id)
		target_id = 0


func _clear() -> void:
	for child in _list.get_children():
		_list.remove_child(child)
		child.queue_free()


func _rebuild() -> void:
	_clear()

	var heading := HBoxContainer.new()
	var label := Label.new()
	label.text = "Loot" if message == "" else "Loot · %s" % message
	label.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	heading.add_child(label)
	heading.add_child(ItemRow.button("Take all", _take.bind("")))
	_list.add_child(heading)
	for item in _items:
		_list.add_child(ItemRow.make(item, {"Take": _take.bind(item.get_id())}))
