class_name ServerConnection
extends Node

signal joined(player_id: int)
signal disconnected
signal snapshot_received(snapshot: GameV1.WorldSnapshot)
signal damage_received(event: GameV1.DamageEvent)

const DEFAULT_URL := "ws://127.0.0.1:8080/ws"
const PROTOCOL_VERSION := 1
const RETRY_SECONDS := 2.0

@export var player_name := "player"

var player_id := 0

var _socket := WebSocketPeer.new()
var _url := DEFAULT_URL
var _was_open := false
var _retry_in := 0.0
var _move_seq := 0
var _attack_seq := 0
var _last_direction := Vector2.INF


func _ready() -> void:
	_url = _url_from_args()
	_connect()


func _exit_tree() -> void:
	_socket.close()


func is_joined() -> bool:
	return player_id != 0


# Only sends when the direction changes. The server keeps applying the last
# input it got, so repeating it every frame would be wasted bandwidth.
func send_move(direction: Vector2) -> void:
	if not is_joined() or direction == _last_direction:
		return
	_last_direction = direction
	_move_seq += 1
	var msg := _new_message()
	var move := msg.new_move()
	move.set_seq(_move_seq)
	var d := move.new_direction()
	d.set_x(direction.x)
	d.set_y(direction.y)
	_send(msg)


func send_attack(target_id: int) -> void:
	if not is_joined():
		return
	_attack_seq += 1
	var msg := _new_message()
	var attack := msg.new_attack()
	attack.set_seq(_attack_seq)
	attack.set_target_id(target_id)
	attack.set_ability_id("basic")
	_send(msg)


func _process(delta: float) -> void:
	_socket.poll()
	match _socket.get_ready_state():
		WebSocketPeer.STATE_OPEN:
			if not _was_open:
				_was_open = true
				_send_join()
			while _socket.get_available_packet_count() > 0:
				_handle(_socket.get_packet())
		WebSocketPeer.STATE_CLOSED:
			if _was_open:
				_was_open = false
				player_id = 0
				_last_direction = Vector2.INF
				print("disconnected from %s (%d)" % [_url, _socket.get_close_code()])
				disconnected.emit()
			_retry_in -= delta
			if _retry_in <= 0.0:
				_connect()


func _connect() -> void:
	_retry_in = RETRY_SECONDS
	_socket = WebSocketPeer.new()
	var err := _socket.connect_to_url(_url)
	if err != OK:
		push_warning("cannot connect to %s: %s" % [_url, error_string(err)])


func _send_join() -> void:
	var msg := _new_message()
	msg.new_join().set_name(player_name)
	_send(msg)


func _handle(bytes: PackedByteArray) -> void:
	var msg := GameV1.ServerMessage.new()
	if msg.from_bytes(bytes) != GameV1.PB_ERR.NO_ERRORS:
		push_warning("dropped a server message that did not decode")
		return
	if msg.has_joined():
		player_id = msg.get_joined().get_player_id()
		print("joined %s as player %d" % [_url, player_id])
		joined.emit(player_id)
	elif msg.has_snapshot():
		snapshot_received.emit(msg.get_snapshot())
	elif msg.has_damage():
		damage_received.emit(msg.get_damage())


func _new_message() -> GameV1.ClientMessage:
	var msg := GameV1.ClientMessage.new()
	msg.set_protocol_version(PROTOCOL_VERSION)
	return msg


func _send(msg: GameV1.ClientMessage) -> void:
	_socket.send(msg.to_bytes(), WebSocketPeer.WRITE_MODE_BINARY)


# Run with `-- --server=ws://host:port/ws` to point at another server.
func _url_from_args() -> String:
	for arg in OS.get_cmdline_user_args():
		if arg.begins_with("--server="):
			return arg.trim_prefix("--server=")
	return DEFAULT_URL
