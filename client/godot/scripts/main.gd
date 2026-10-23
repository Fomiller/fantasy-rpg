extends Node2D

# Mirror sim.DefaultConfig and the slash data. They only pick a target; the
# server decides.
const ATTACK_RANGE := 64.0
const PICKUP_RANGE := 48.0

const ENEMY_SCENE := preload("res://scenes/enemy.tscn")
const REMOTE_PLAYER_SCENE := preload("res://scenes/remote_player.tscn")
const LOOT_SCENE := preload("res://scenes/loot.tscn")
const PROJECTILE_SCENE := preload("res://scenes/projectile.tscn")
const EXTRACTED_TEXT := "extracted · press E to go again"
# Slot 0 is the basic attack.
const ABILITY_ACTIONS := ["attack", "ability_1", "ability_2", "ability_3"]
# Any Control in this group is a window that Escape closes.
const WINDOWS_GROUP := "windows"

@onready var _connection: ServerConnection = $ServerConnection
@onready var _cave: Cave = $Cave
@onready var _player: Player = $Player
@onready var _ghost: Node2D = $ServerGhost
@onready var _entities: Node2D = $Entities
@onready var _status: Label = $HUD/Status
@onready var _equipment: RichTextLabel = $HUD/Equipment
@onready var _inventory: InventoryPanel = $HUD/Inventory
@onready var _abilities: AbilityBar = $HUD/Abilities

var _views: Dictionary[int, EntityView] = {}
var _health := 0
var _max_health := 0
var _extract_progress := 0.0


func _ready() -> void:
	print("main scene loaded")
	_connection.joined.connect(_on_joined)
	_connection.disconnected.connect(_on_disconnected)
	_connection.snapshot_received.connect(_on_snapshot)
	_connection.damage_received.connect(_on_damage)
	_connection.ability_used.connect(_on_ability_used)
	_connection.ability_rejected.connect(_on_ability_rejected)
	_connection.extracted.connect(_on_extracted)
	_connection.equipment_received.connect(_on_equipment)
	_connection.equipment_received.connect(_inventory.show_equipment)
	_connection.inventory_received.connect(_inventory.show_inventory)
	_inventory.connection = _connection
	_player.cave = _cave
	_player.send_move = _connection.send_move
	_on_disconnected()


func _physics_process(_delta: float) -> void:
	if Input.is_action_just_pressed("close_window"):
		close_top_window()
	if _connection.has_extracted:
		if Input.is_action_just_pressed("interact"):
			_connection.rejoin()
		return
	if Input.is_action_just_pressed("inventory"):
		_inventory.visible = not _inventory.visible
	if Input.is_action_just_pressed("interact"):
		_pickup()
	for i in ABILITY_ACTIONS.size():
		# A click on an open window is for its buttons, not an attack.
		if Input.is_action_just_pressed(ABILITY_ACTIONS[i]) and not (i == 0 and _top_window() != null):
			use_ability(i)


# Closes one window per press, the one drawn on top. Does nothing when none is open.
func close_top_window() -> void:
	var window := _top_window()
	if window:
		window.visible = false


func _top_window() -> Control:
	var top: Control = null
	for window: Control in get_tree().get_nodes_in_group(WINDOWS_GROUP):
		if window.is_visible_in_tree():
			top = window
	return top


# Sends the ability in a slot, aimed at the mouse. A cooldown the server
# already reported holds the key back.
func use_ability(index: int) -> void:
	var ability := _abilities.slot(index)
	if ability == null or not _connection.is_joined() or _abilities.cooldown_left(ability.get_id()) > 0.0:
		return
	var target := nearest_enemy(_ghost.position)
	var aim := aim_direction(target)
	_player.cast(aim, ability.get_windup_ms() / 1000.0, ability.get_root())
	_connection.send_attack(target.entity_id if target else 0, ability.get_id(), aim)


# Toward the mouse. Without one, like in a headless test, toward the nearest
# enemy, then the way the player is moving.
func aim_direction(target: EntityView) -> Vector2:
	if DisplayServer.get_name() != "headless":
		var to_mouse := get_global_mouse_position() - _player.position
		if to_mouse.length() > 1.0:
			return to_mouse.normalized()
	if target:
		return (target.position - _ghost.position).normalized()
	var move := _player.get_move_input()
	return move.normalized() if move != Vector2.ZERO else Vector2.RIGHT


func _pickup() -> void:
	var target := nearest_loot(_ghost.position)
	if target:
		_connection.send_pickup(target.entity_id)


# Both use the server position, since that is where the server measures range from.
func nearest_enemy(from: Vector2) -> EntityView:
	return _nearest(from, ATTACK_RANGE, func(view: EntityView) -> bool: return view.is_in_group("enemies") and view.is_alive())


func nearest_loot(from: Vector2) -> EntityView:
	return _nearest(from, PICKUP_RANGE, func(view: EntityView) -> bool: return view is LootView and (view as LootView).item_count > 0)


func _nearest(from: Vector2, max_distance: float, wanted: Callable) -> EntityView:
	var nearest: EntityView = null
	var nearest_distance := max_distance
	for view: EntityView in _views.values():
		if not wanted.call(view):
			continue
		var distance := from.distance_to(view.position)
		if distance <= nearest_distance:
			nearest = view
			nearest_distance = distance
	return nearest


func _on_joined(player_id: int) -> void:
	_cave.build(_connection.cave)
	_player.tick_seconds = 1.0 / _connection.tick_rate
	_player.place(_connection.spawn)
	_player.visible = true
	_player.revive()
	if _connection.class_info:
		_abilities.setup(_connection.class_info)
	_status.text = "player %d" % player_id


func _on_disconnected() -> void:
	_ghost.visible = false
	for view: EntityView in _views.values():
		view.queue_free()
	_views.clear()
	_equipment.text = ""
	_inventory.clear()
	_abilities.clear()
	_status.text = EXTRACTED_TEXT if _connection.has_extracted else "offline"


# The ghost marks where the server thinks this player is. With prediction it
# should sit just behind the player, about one round trip back.
func _on_snapshot(snapshot: GameV1.WorldSnapshot) -> void:
	var seen := {}
	for entity: GameV1.EntityState in snapshot.get_entities():
		var id := entity.get_id()
		if id == _connection.player_id:
			var pos := entity.get_position()
			var vel := entity.get_velocity()
			_ghost.position = Vector2(pos.get_x(), pos.get_y())
			_ghost.visible = true
			if entity.get_max_speed() > 0.0:
				_player.max_speed = entity.get_max_speed()
			_player.reconcile(entity.get_last_input_seq(), _ghost.position, Vector2(vel.get_x(), vel.get_y()))
			_health = entity.get_health()
			_max_health = entity.get_max_health()
			_extract_progress = entity.get_extract_progress()
			_abilities.set_resource(entity.get_resource(), entity.get_max_resource())
			if _health <= 0 and _player.alive:
				_player.die()
			continue
		seen[id] = true
		var view: EntityView = _views.get(id)
		if view == null:
			view = _spawn_view(entity.get_kind())
			_views[id] = view
		view.apply(entity)

	for id: int in _views.keys():
		if not seen.has(id):
			_views[id].queue_free()
			_views.erase(id)
	var hp := "%d/%d hp" % [_health, _max_health] if _health > 0 else "dead"
	_status.text = "player %d · %s · tick %d" % [_connection.player_id, hp, snapshot.get_tick()]
	if _extract_progress > 0.0:
		_status.text += " · extracting %d%%" % int(_extract_progress * 100)


func _spawn_view(kind: int) -> EntityView:
	var view: EntityView
	if kind == GameV1.EntityKind.ENEMY:
		view = ENEMY_SCENE.instantiate()
		view.add_to_group("enemies")
	elif kind == GameV1.EntityKind.CHEST or kind == GameV1.EntityKind.LOOT:
		view = LOOT_SCENE.instantiate()
	elif kind == GameV1.EntityKind.PROJECTILE:
		view = PROJECTILE_SCENE.instantiate()
	else:
		view = REMOTE_PLAYER_SCENE.instantiate()
	_entities.add_child(view)
	return view


func _on_damage(event: GameV1.DamageEvent) -> void:
	if event.get_target_id() == _connection.player_id:
		_player.flash()
	var view: EntityView = _views.get(event.get_target_id())
	if view:
		view.flash()
	if event.get_killed():
		print("player %d killed %d" % [event.get_source_id(), event.get_target_id()])


# The caster already played its own windup when the key went down.
func _on_ability_used(event: GameV1.AbilityUsed) -> void:
	if event.get_source_id() == _connection.player_id:
		_abilities.started(event.get_ability_id(), event.get_cooldown_ms())
		return
	var view: EntityView = _views.get(event.get_source_id())
	if view is CharacterView:
		var aim := Vector2(event.get_aim().get_x(), event.get_aim().get_y()) if event.has_aim() else Vector2.ZERO
		(view as CharacterView).cast(aim, event.get_windup_ms() / 1000.0)


# Basic attacks are pressed often, so their rejections would only be noise.
func _on_ability_rejected(event: GameV1.AbilityRejected) -> void:
	var basic := _abilities.slot(0)
	if basic == null or event.get_ability_id() != basic.get_id():
		_abilities.rejected(event.get_ability_id(), event.get_reason())


func _on_extracted(player_id: int) -> void:
	if player_id != _connection.player_id:
		print("player %d extracted" % player_id)
		return
	_player.alive = false
	_player.visible = false
	_ghost.visible = false
	_status.text = EXTRACTED_TEXT


func _on_equipment(items: Array[GameV1.Item]) -> void:
	var lines: Array[String] = []
	for item in items:
		lines.append(ItemText.line(item))
	_equipment.text = "\n".join(lines)
	var weapon := ""
	for item in items:
		if item.get_slot() == "weapon":
			weapon = item.get_item_def_id()
	_player.equip_weapon(weapon)
