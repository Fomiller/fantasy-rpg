extends Node2D

# Mirrors sim.DefaultConfig. Only picks a target; the server decides if it hits.
const ATTACK_RANGE := 64.0

const ENEMY_SCENE := preload("res://scenes/enemy.tscn")
const REMOTE_PLAYER_SCENE := preload("res://scenes/remote_player.tscn")

@onready var _connection: ServerConnection = $ServerConnection
@onready var _player: Player = $Player
@onready var _ghost: Node2D = $ServerGhost
@onready var _entities: Node2D = $Entities
@onready var _status: Label = $HUD/Status

var _views: Dictionary[int, EntityView] = {}
var _health := 0


func _ready() -> void:
	print("main scene loaded")
	_connection.joined.connect(_on_joined)
	_connection.disconnected.connect(_on_disconnected)
	_connection.snapshot_received.connect(_on_snapshot)
	_connection.damage_received.connect(_on_damage)
	_on_disconnected()


func _physics_process(_delta: float) -> void:
	_connection.send_move(_player.get_move_input())
	if Input.is_action_just_pressed("attack"):
		_attack()


func _attack() -> void:
	var target := nearest_enemy(_ghost.position)
	if target:
		_connection.send_attack(target.entity_id)


# Uses the server position, since that is where the server measures range from.
func nearest_enemy(from: Vector2) -> EntityView:
	var nearest: EntityView = null
	var nearest_distance := ATTACK_RANGE
	for view: EntityView in _views.values():
		if not view.is_in_group("enemies") or not view.is_alive():
			continue
		var distance := from.distance_to(view.position)
		if distance <= nearest_distance:
			nearest = view
			nearest_distance = distance
	return nearest


func _on_joined(player_id: int) -> void:
	_player.revive()
	_status.text = "player %d" % player_id


func _on_disconnected() -> void:
	_ghost.visible = false
	for view: EntityView in _views.values():
		view.queue_free()
	_views.clear()
	_status.text = "offline"


# The ghost marks where the server thinks this player is. Until prediction and
# reconciliation land, it shows how far the client and server drift apart.
func _on_snapshot(snapshot: GameV1.WorldSnapshot) -> void:
	var seen := {}
	for entity: GameV1.EntityState in snapshot.get_entities():
		var id := entity.get_id()
		if id == _connection.player_id:
			var pos := entity.get_position()
			_ghost.position = Vector2(pos.get_x(), pos.get_y())
			_ghost.visible = true
			_health = entity.get_health()
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
	var hp := "%d hp" % _health if _health > 0 else "dead"
	_status.text = "player %d · %s · tick %d" % [_connection.player_id, hp, snapshot.get_tick()]


func _spawn_view(kind: int) -> EntityView:
	var view: EntityView
	if kind == GameV1.EntityKind.ENEMY:
		view = ENEMY_SCENE.instantiate()
		view.add_to_group("enemies")
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
