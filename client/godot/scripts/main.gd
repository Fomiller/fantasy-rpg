extends Node2D

# Mirror sim.DefaultConfig. They only pick a target; the server decides.
const ATTACK_RANGE := 64.0
const PICKUP_RANGE := 48.0

const ENEMY_SCENE := preload("res://scenes/enemy.tscn")
const REMOTE_PLAYER_SCENE := preload("res://scenes/remote_player.tscn")
const LOOT_SCENE := preload("res://scenes/loot.tscn")
const EXTRACTED_TEXT := "extracted · press E to go again"

@onready var _connection: ServerConnection = $ServerConnection
@onready var _cave: Cave = $Cave
@onready var _player: Player = $Player
@onready var _ghost: Node2D = $ServerGhost
@onready var _entities: Node2D = $Entities
@onready var _status: Label = $HUD/Status
@onready var _equipment: RichTextLabel = $HUD/Equipment
@onready var _inventory: InventoryPanel = $HUD/Inventory

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
	_connection.extracted.connect(_on_extracted)
	_connection.equipment_received.connect(_on_equipment)
	_connection.equipment_received.connect(_inventory.show_equipment)
	_connection.inventory_received.connect(_inventory.show_inventory)
	_inventory.connection = _connection
	_player.cave = _cave
	_player.send_move = _connection.send_move
	_on_disconnected()


func _physics_process(_delta: float) -> void:
	if _connection.has_extracted:
		if Input.is_action_just_pressed("interact"):
			_connection.rejoin()
		return
	if Input.is_action_just_pressed("inventory"):
		_inventory.visible = not _inventory.visible
	if Input.is_action_just_pressed("interact"):
		_pickup()
	# A click on the open panel is for its buttons, not an attack.
	if Input.is_action_just_pressed("attack") and not _inventory.visible:
		_attack()


func _attack() -> void:
	var target := nearest_enemy(_ghost.position)
	if target:
		_connection.send_attack(target.entity_id)


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
	_status.text = "player %d" % player_id


func _on_disconnected() -> void:
	_ghost.visible = false
	for view: EntityView in _views.values():
		view.queue_free()
	_views.clear()
	_equipment.text = ""
	_inventory.clear()
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
