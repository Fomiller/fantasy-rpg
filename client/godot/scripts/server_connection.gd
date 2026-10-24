class_name ServerConnection
extends Node

# The player entered the cave.
signal joined(player_id: int)
# On join and after every run. Gear, bag and stash follow.
signal lobby_entered
signal disconnected
signal extracted(player_id: int)
signal equipment_received(items: Array[GameV1.Item])
signal stats_received(stats: Array[GameV1.ItemStat])
signal inventory_received(items: Array[GameV1.Item], capacity: int)
signal snapshot_received(snapshot: GameV1.WorldSnapshot)
signal damage_received(event: GameV1.DamageEvent)
signal ability_used(event: GameV1.AbilityUsed)
signal ability_rejected(event: GameV1.AbilityRejected)
signal loot_received(contents: GameV1.LootContents)
signal pickup_rejected(event: GameV1.PickupRejected)
signal xp_gained(event: GameV1.XpGained)
signal level_up(event: GameV1.LevelUp)
# After a death, and on this player's own extraction just before extracted.
signal run_ended(summary: GameV1.RunSummary)
signal stash_received(items: Array[GameV1.Item], capacity: int)
signal stash_move_rejected(event: GameV1.StashMoveRejected)

const DEFAULT_URL := "ws://127.0.0.1:8080/ws"
const PROTOCOL_VERSION := 1
const RETRY_SECONDS := 2.0

# The server keeps items by name, so the same name gets them back on reconnect.
# Random by default so two local windows do not fight over one name.
@export var player_name := ""
# Empty plays the server's default class.
@export var class_id := ""

var player_id := 0
var cave: GameV1.CaveLayout
var spawn := Vector2.ZERO
var tick_rate := 20
var class_info: GameV1.ClassInfo
# Level and XP as the server last sent them. A run summary replaces it.
var character: GameV1.CharacterInfo
# Between runs. player_id is 0 while it is set.
var in_lobby := false

var _socket := WebSocketPeer.new()
var _url := DEFAULT_URL
var _was_open := false
var _retry_in := 0.0
var _move_seq := 0
var _attack_seq := 0


func _ready() -> void:
	_url = _arg("--server=", DEFAULT_URL)
	if player_name == "":
		player_name = _arg("--name=", "player-%04x" % (randi() % 0x10000))
	_connect()


func _exit_tree() -> void:
	_socket.close()


func is_joined() -> bool:
	return player_id != 0


func send_enter_cave() -> void:
	if not in_lobby:
		return
	var msg := _new_message()
	msg.new_enter_cave()
	_send(msg)


# to is a GameV1.ItemPlace.
func send_stash_move(item_id: String, to: int) -> void:
	if not in_lobby:
		return
	var msg := _new_message()
	var move := msg.new_stash_move()
	move.set_item_id(item_id)
	move.set_to(to)
	_send(msg)


# Returns the seq it sent, or 0 when not joined.
func send_move(direction: Vector2) -> int:
	if not is_joined():
		return 0
	_move_seq += 1
	var msg := _new_message()
	var move := msg.new_move()
	move.set_seq(_move_seq)
	var d := move.new_direction()
	d.set_x(direction.x)
	d.set_y(direction.y)
	_send(msg)
	return _move_seq


# With no aim the server aims at target_id. Returns the seq, or 0 when not joined.
func send_attack(target_id: int, ability_id := "basic", aim := Vector2.ZERO) -> int:
	if not is_joined():
		return 0
	_attack_seq += 1
	var msg := _new_message()
	var attack := msg.new_attack()
	attack.set_seq(_attack_seq)
	attack.set_target_id(target_id)
	attack.set_ability_id(ability_id)
	if aim != Vector2.ZERO:
		var a := attack.new_aim()
		a.set_x(aim.x)
		a.set_y(aim.y)
	_send(msg)
	return _attack_seq


# An empty item_id takes everything that fits.
func send_pickup(target_id: int, item_id := "") -> void:
	if not is_joined():
		return
	var msg := _new_message()
	var pickup := msg.new_pickup()
	pickup.set_target_id(target_id)
	pickup.set_item_id(item_id)
	_send(msg)


func send_loot_open(target_id: int) -> void:
	if not is_joined():
		return
	var msg := _new_message()
	msg.new_loot_open().set_target_id(target_id)
	_send(msg)


func send_loot_close(target_id: int) -> void:
	if not is_joined():
		return
	var msg := _new_message()
	msg.new_loot_close().set_target_id(target_id)
	_send(msg)


func send_equip(item_id: String) -> void:
	if not is_joined():
		return
	var msg := _new_message()
	msg.new_equip().set_item_id(item_id)
	_send(msg)


func send_unequip(slot: String) -> void:
	if not is_joined():
		return
	var msg := _new_message()
	msg.new_unequip().set_slot(slot)
	_send(msg)


func send_drop(item_id: String) -> void:
	if not is_joined():
		return
	var msg := _new_message()
	msg.new_drop().set_item_id(item_id)
	_send(msg)


func send_use(item_id: String) -> void:
	if not is_joined():
		return
	var msg := _new_message()
	msg.new_use().set_item_id(item_id)
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
				in_lobby = false
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
	var join := msg.new_join()
	join.set_name(player_name)
	join.set_class_id(class_id)
	_send(msg)


func _handle(bytes: PackedByteArray) -> void:
	var msg := GameV1.ServerMessage.new()
	if msg.from_bytes(bytes) != GameV1.PB_ERR.NO_ERRORS:
		push_warning("dropped a server message that did not decode")
		return
	if msg.has_lobby():
		var lobby := msg.get_lobby()
		player_id = 0
		in_lobby = true
		class_info = lobby.get_class_info()
		character = lobby.get_character() if lobby.has_character() else null
		lobby_entered.emit()
	elif msg.has_stash():
		stash_received.emit(msg.get_stash().get_items(), msg.get_stash().get_capacity())
	elif msg.has_stash_move_rejected():
		stash_move_rejected.emit(msg.get_stash_move_rejected())
	elif msg.has_joined():
		var accepted := msg.get_joined()
		in_lobby = false
		player_id = accepted.get_player_id()
		cave = accepted.get_cave()
		spawn = Vector2(accepted.get_spawn().get_x(), accepted.get_spawn().get_y())
		if accepted.get_tick_rate() > 0:
			tick_rate = accepted.get_tick_rate()
		class_info = accepted.get_class_info()
		character = accepted.get_character() if accepted.has_character() else null
		print("joined %s as player %d in %s" % [_url, player_id, cave.get_name()])
		joined.emit(player_id)
	elif msg.has_snapshot():
		snapshot_received.emit(msg.get_snapshot())
	elif msg.has_damage():
		damage_received.emit(msg.get_damage())
	elif msg.has_ability_used():
		ability_used.emit(msg.get_ability_used())
	elif msg.has_ability_rejected():
		ability_rejected.emit(msg.get_ability_rejected())
	elif msg.has_equipment():
		equipment_received.emit(msg.get_equipment().get_items())
		stats_received.emit(msg.get_equipment().get_stats())
	elif msg.has_inventory():
		var inventory := msg.get_inventory()
		inventory_received.emit(inventory.get_items(), inventory.get_capacity())
	elif msg.has_loot():
		loot_received.emit(msg.get_loot())
	elif msg.has_pickup_rejected():
		pickup_rejected.emit(msg.get_pickup_rejected())
	elif msg.has_xp_gained():
		xp_gained.emit(msg.get_xp_gained())
	elif msg.has_level_up():
		var up := msg.get_level_up()
		if up.has_class_info():
			class_info = up.get_class_info()
		level_up.emit(up)
	elif msg.has_run_ended():
		_end_run(msg.get_run_ended().get_summary())
	elif msg.has_extracted():
		var event := msg.get_extracted()
		var id := event.get_player_id()
		if id == player_id and event.has_summary():
			_end_run(event.get_summary())
		extracted.emit(id)


func _end_run(summary: GameV1.RunSummary) -> void:
	if summary.has_character():
		character = summary.get_character()
	run_ended.emit(summary)


func _new_message() -> GameV1.ClientMessage:
	var msg := GameV1.ClientMessage.new()
	msg.set_protocol_version(PROTOCOL_VERSION)
	return msg


func _send(msg: GameV1.ClientMessage) -> void:
	_socket.send(msg.to_bytes(), WebSocketPeer.WRITE_MODE_BINARY)


# Run with `-- --server=ws://host:port/ws` to point at another server, and
# `-- --name=ann` to play as a set name.
func _arg(prefix: String, default: String) -> String:
	for arg in OS.get_cmdline_user_args():
		if arg.begins_with(prefix):
			return arg.trim_prefix(prefix)
	return default
