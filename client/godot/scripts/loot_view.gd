class_name LootView
extends EntityView

const CHEST_COLOR := Color(0.55, 0.35, 0.18)
const EMPTY_CHEST_COLOR := Color(0.3, 0.22, 0.15)
const LOOT_COLOR := Color(0.9, 0.75, 0.25)

var item_count := 0


func apply(state: GameV1.EntityState) -> void:
	super.apply(state)
	item_count = state.get_item_count()
	_health.text = str(item_count) if item_count > 0 else ""
	if state.get_kind() == GameV1.EntityKind.CHEST:
		_body.color = CHEST_COLOR if item_count > 0 else EMPTY_CHEST_COLOR
	else:
		_body.color = LOOT_COLOR
