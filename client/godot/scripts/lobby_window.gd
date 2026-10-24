class_name LobbyWindow
extends PanelContainer

const REJECTED_TEXT := {
	"full": "no room",
	"cannot_equip": "cannot equip that",
	"unknown_item": "item is gone",
	"not_in_lobby": "not in the lobby",
	"saving": "Couldn't save, try again",
}
const ENTER_REJECTED_TEXT := {
	"saving": "Still saving your last run",
}

# Set by main. Tests swap them for fakes. Every button sends a request, and
# the window only changes when the server answers.
var send_move: Callable
var send_enter: Callable

var message := ""
# Enter Cave stays off until the server says the last run is saved.
var saving := false

var _character: GameV1.CharacterInfo
var _info: GameV1.ClassInfo
var _gear: Array[GameV1.Item] = []
var _bag: Array[GameV1.Item] = []
var _bag_capacity := 0
var _stash: Array[GameV1.Item] = []
var _stash_capacity := 0

@onready var _list: VBoxContainer = $Scroll/List


# Not in the windows group: Escape must not close the only way into the cave.
func _ready() -> void:
	_rebuild()


func show_lobby(character: GameV1.CharacterInfo, info: GameV1.ClassInfo, is_saving := false) -> void:
	_character = character
	_info = info
	saving = is_saving
	message = ""
	_rebuild()
	visible = true


func show_equipment(items: Array[GameV1.Item]) -> void:
	_gear = items
	_rebuild()


func show_inventory(items: Array[GameV1.Item], capacity: int) -> void:
	_bag = items
	_bag_capacity = capacity
	_rebuild()


func show_stash(items: Array[GameV1.Item], capacity: int) -> void:
	_stash = items
	_stash_capacity = capacity
	message = ""
	_rebuild()


func show_rejected(event: GameV1.StashMoveRejected) -> void:
	message = REJECTED_TEXT.get(event.get_reason(), event.get_reason())
	_rebuild()


func show_enter_rejected(event: GameV1.EnterCaveRejected) -> void:
	message = ENTER_REJECTED_TEXT.get(event.get_reason(), event.get_reason())
	_rebuild()


func _move(item_id: String, to: int) -> void:
	send_move.call(item_id, to)


func _rebuild() -> void:
	if _list == null:
		return
	for child in _list.get_children():
		_list.remove_child(child)
		child.queue_free()

	var title := "Lobby"
	if _character:
		title = "%s · %s · level %d" % [_character.get_name(), _info.get_name() if _info else "", _character.get_level()]
	_label(title)
	if message != "":
		_label(message).modulate = Color(1, 0.6, 0.5)

	_label("Gear")
	for item in _gear:
		_row(item, {"To bag": GameV1.ItemPlace.BAG, "To stash": GameV1.ItemPlace.STASH})
	_label("Bag %d/%d" % [_bag.size(), _bag_capacity])
	for item in _bag:
		_row(item, _equip_or({"To stash": GameV1.ItemPlace.STASH}, item))
	_label("Stash %d/%d" % [_stash.size(), _stash_capacity])
	for item in _stash:
		_row(item, _equip_or({"To bag": GameV1.ItemPlace.BAG}, item))

	if saving:
		_label("Saving...")
	var enter := ItemRow.button("Enter cave", func() -> void: send_enter.call())
	enter.disabled = saving
	_list.add_child(enter)


func _equip_or(places: Dictionary, item: GameV1.Item) -> Dictionary:
	if item.get_slot() not in ["consumable", "material"]:
		places["Equip"] = GameV1.ItemPlace.GEAR
	return places


func _row(item: GameV1.Item, places: Dictionary) -> void:
	var actions := {}
	for text: String in places:
		actions[text] = _move.bind(item.get_id(), places[text])
	_list.add_child(ItemRow.make(item, actions))


func _label(text: String) -> Label:
	var label := Label.new()
	label.text = text
	_list.add_child(label)
	return label
