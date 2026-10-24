extends Node2D

# Mirrors sim.DefaultConfig. It only picks a target; the server decides.
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
@onready var _debug_overlay: Label = $HUD/DebugOverlay
@onready var _equipment: RichTextLabel = $HUD/Equipment
@onready var _inventory: InventoryPanel = $HUD/Inventory
@onready var _abilities: AbilityBar = $HUD/Abilities
@onready var _loot: LootWindow = $HUD/Loot
@onready var _character: CharacterScreen = $HUD/Character

# Set by main. Tests swap it for a fake.
var send_attack: Callable
# Shows the server ghost, the overlay and the labels with server state.
var debug := false

var _last_tick := 0
var _last_snapshot_ms := 0
var _snapshot_rate := 0.0

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
	_connection.equipment_received.connect(_character.show_equipment)
	_connection.stats_received.connect(_character.show_stats)
	_character.send_unequip = _connection.send_unequip
	_connection.loot_received.connect(_loot.show_contents)
	_connection.pickup_rejected.connect(_loot.show_rejected)
	_loot.send_open = _connection.send_loot_open
	_loot.send_close = _connection.send_loot_close
	_loot.send_pickup = _connection.send_pickup
	_player.cave = _cave
	_player.send_move = _connection.send_move
	send_attack = _connection.send_attack
	_on_disconnected()


func _process(_delta: float) -> void:
	if debug:
		_debug_overlay.text = debug_text()


func _physics_process(_delta: float) -> void:
	if Input.is_action_just_pressed("debug_overlay"):
		set_debug(not debug)
	if Input.is_action_just_pressed("close_window"):
		close_top_window()
	if _connection.has_extracted:
		if Input.is_action_just_pressed("interact"):
			_connection.rejoin()
		return
	if Input.is_action_just_pressed("inventory"):
		_inventory.visible = not _inventory.visible
	if Input.is_action_just_pressed("character"):
		_character.visible = not _character.visible
	if Input.is_action_just_pressed("interact"):
		_open_loot()
	_close_loot_out_of_range()


# A click on a window is taken by its Controls and never gets here, so the
# keys and clicks outside a window still attack.
func _unhandled_input(event: InputEvent) -> void:
	if _connection.has_extracted:
		return
	for i in ABILITY_ACTIONS.size():
		if event.is_action_pressed(ABILITY_ACTIONS[i]):
			use_ability(i)


func set_debug(on: bool) -> void:
	debug = on
	_debug_overlay.visible = on
	_debug_overlay.text = debug_text()
	_ghost.visible = on and _connection.is_joined() and not _connection.has_extracted
	for view: EntityView in _views.values():
		view.debug = on


func debug_text() -> String:
	var age := Time.get_ticks_msec() - _last_snapshot_ms if _last_snapshot_ms > 0 else -1
	return "\n".join([
		"fps %d" % Engine.get_frames_per_second(),
		"player %d" % _connection.player_id,
		"pos %d, %d · server %d, %d" % [_player.position.x, _player.position.y, _ghost.position.x, _ghost.position.y],
		"tick %d · %d Hz · seen %.1f/s" % [_last_tick, _connection.tick_rate, _snapshot_rate],
		"snapshot age %d ms" % age,
	])


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


# Sends the ability in a slot, aimed where the player faces. A cooldown the
# server already reported holds the key back.
func use_ability(index: int) -> void:
	var ability := _abilities.slot(index)
	if ability == null or not _connection.is_joined() or _abilities.cooldown_left(ability.get_id()) > 0.0:
		return
	var aim := _player.facing
	_player.cast(aim, ability.get_windup_ms() / 1000.0, ability.get_root())
	send_attack.call(0, ability.get_id(), aim)


func _open_loot() -> void:
	var target := nearest_loot(_ghost.position)
	if target:
		_loot.open(target.entity_id)


# The server closes it too, but this does not wait a round trip for that.
func _close_loot_out_of_range() -> void:
	if _loot.target_id == 0:
		return
	var view: EntityView = _views.get(_loot.target_id)
	if view == null or _ghost.position.distance_to(view.position) > PICKUP_RANGE:
		_loot.close()


# Uses the server position, since that is where the server measures range from.
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
	_character.setup(_connection.player_name, _connection.class_info)
	_status.text = "joined"


func _on_disconnected() -> void:
	_ghost.visible = false
	for view: EntityView in _views.values():
		view.queue_free()
	_views.clear()
	_equipment.text = ""
	_inventory.clear()
	_character.clear()
	_loot.close()
	_abilities.clear()
	_status.text = EXTRACTED_TEXT if _connection.has_extracted else "offline"


# The ghost marks where the server thinks this player is. With prediction it
# should sit just behind the player, about one round trip back.
func _on_snapshot(snapshot: GameV1.WorldSnapshot) -> void:
	_track_snapshot(snapshot.get_tick())
	var seen := {}
	for entity: GameV1.EntityState in snapshot.get_entities():
		var id := entity.get_id()
		if id == _connection.player_id:
			var pos := entity.get_position()
			var vel := entity.get_velocity()
			_ghost.position = Vector2(pos.get_x(), pos.get_y())
			_ghost.visible = debug
			if entity.get_max_speed() > 0.0:
				_player.max_speed = entity.get_max_speed()
			_player.reconcile(entity.get_last_input_seq(), _ghost.position, Vector2(vel.get_x(), vel.get_y()))
			_health = entity.get_health()
			_max_health = entity.get_max_health()
			_extract_progress = entity.get_extract_progress()
			_abilities.set_resource(entity.get_resource(), entity.get_max_resource())
			_abilities.set_level(entity.get_level())
			_character.set_vitals(_health, _max_health, entity.get_resource(), entity.get_max_resource())
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
	_status.text = hp
	if _extract_progress > 0.0:
		_status.text += " · extracting %d%%" % int(_extract_progress * 100)


# Smoothed so one late packet does not make the rate jump.
func _track_snapshot(tick: int) -> void:
	var now := Time.get_ticks_msec()
	if _last_snapshot_ms > 0 and now > _last_snapshot_ms:
		var rate := 1000.0 / (now - _last_snapshot_ms)
		_snapshot_rate = rate if _snapshot_rate == 0.0 else lerpf(_snapshot_rate, rate, 0.1)
	_last_snapshot_ms = now
	_last_tick = tick


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
	view.debug = debug
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
	# Busy means the server is still winding up an earlier cast.
	if event.get_reason() != "busy":
		_player.cancel_cast()
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
