class_name LootView
extends EntityView

const CHEST := preload("res://assets/kenney_tiny_dungeon/tile_0089.png")
const OPEN_CHEST := preload("res://assets/kenney_tiny_dungeon/tile_0091.png")
const LOOT := preload("res://assets/kenney_tiny_dungeon/tile_0066.png")

var item_count := 0

@onready var _sprite: Sprite2D = $Body


func apply(state: GameV1.EntityState) -> void:
	super.apply(state)
	item_count = state.get_item_count()
	_health.text = str(item_count) if item_count > 0 else ""
	# Chests and loot have no health, so undo the dead tint from super.
	_body.modulate = Color.WHITE
	if state.get_kind() == GameV1.EntityKind.CHEST:
		_sprite.texture = CHEST if item_count > 0 else OPEN_CHEST
	else:
		_sprite.texture = LOOT


func _show_debug() -> void:
	_health.visible = debug
