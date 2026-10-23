extends Node2D

@onready var _connection: ServerConnection = $ServerConnection
@onready var _player: Player = $Player
@onready var _ghost: Node2D = $ServerGhost
@onready var _status: Label = $HUD/Status


func _ready() -> void:
	print("main scene loaded")
	_connection.joined.connect(_on_joined)
	_connection.disconnected.connect(_on_disconnected)
	_connection.snapshot_received.connect(_on_snapshot)
	_on_disconnected()


func _physics_process(_delta: float) -> void:
	_connection.send_move(_player.get_move_input())


func _on_joined(player_id: int) -> void:
	_status.text = "player %d" % player_id


func _on_disconnected() -> void:
	_ghost.visible = false
	_status.text = "offline"


# The ghost marks where the server thinks this player is. Until prediction and
# reconciliation land, it shows how far the client and server drift apart.
func _on_snapshot(snapshot: GameV1.WorldSnapshot) -> void:
	for entity: GameV1.EntityState in snapshot.get_entities():
		if entity.get_id() == _connection.player_id:
			var pos := entity.get_position()
			_ghost.position = Vector2(pos.get_x(), pos.get_y())
			_ghost.visible = true
	_status.text = "player %d · tick %d" % [_connection.player_id, snapshot.get_tick()]
