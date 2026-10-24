class_name RunSummaryWindow
extends PanelContainer

@onready var _list: VBoxContainer = $List


func _ready() -> void:
	add_to_group("windows")


func show_summary(summary: GameV1.RunSummary) -> void:
	for child in _list.get_children():
		_list.remove_child(child)
		child.queue_free()

	var character := summary.get_character()
	_label("Extracted" if summary.get_extracted() else "You died")
	_label("Run XP %d" % summary.get_run_xp())
	_label("Banked %d XP" % summary.get_banked_xp())
	_label("Levels gained %d · level %d" % [summary.get_levels_gained(), character.get_level() if character else 0])
	_label("Loot kept")
	for item: GameV1.Item in summary.get_kept():
		_list.add_child(ItemRow.make(item, {}))
	if summary.get_kept().is_empty():
		_label("nothing").modulate = Color(1, 1, 1, 0.5)
	_list.add_child(ItemRow.button("Close", hide))
	visible = true


func _label(text: String) -> Label:
	var label := Label.new()
	label.text = text
	_list.add_child(label)
	return label
