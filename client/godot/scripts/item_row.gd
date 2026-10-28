class_name ItemRow


# One item with a button per action. actions maps button text to a Callable.
static func make(item: GameV1.Item, actions: Dictionary) -> HBoxContainer:
	var row := HBoxContainer.new()
	var label := RichTextLabel.new()
	label.bbcode_enabled = true
	label.fit_content = true
	label.scroll_active = false
	label.custom_minimum_size.x = 150
	label.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	label.text = ItemText.line(item)
	if item.get_quantity() > 1:
		label.text += "  x%d" % item.get_quantity()
	row.add_child(label)
	for text: String in actions:
		row.add_child(button(text, actions[text]))
	return row


static func button(text: String, action: Callable) -> Button:
	var b := Button.new()
	b.text = text
	# Space is attack. A focused button would take it too.
	b.focus_mode = Control.FOCUS_NONE
	b.pressed.connect(action)
	return b
