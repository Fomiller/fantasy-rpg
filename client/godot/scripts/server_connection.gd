class_name ServerConnection
extends Node

# The player entered the town or the cave. in_town says which.
signal joined(player_id: int)
# The town state changed: the character, the class or saving. It follows a
# town join, and comes again when the last run lands. Gear, bag and stash
# follow it.
signal town_state_changed
# Any socket closed, including on the way to or from an instance.
signal left
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
signal enter_instance_rejected(event: GameV1.EnterInstanceRejected)
signal chat_received(message: GameV1.ChatMessage)

# The world server's front door. Every session starts here and comes back here
# after a run or a drop. It may send the player on to a shard's own path.
const DEFAULT_URL := "ws://127.0.0.1:8080/ws"
const PROTOCOL_VERSION := 1
# Why an instance turned the player away or the run ended early, as the town
# says it. The empty reason is a dropped connection.
const INSTANCE_CLOSE_TEXT := {
	"": "The run ended when the connection dropped.",
	"token expired": "Took too long to join. Try again.",
	"instance full": "That instance filled up. Try again.",
	"already in a run": "Your character is in a run",
	"token used": "Couldn't join the instance. Try again.",
	"bad token": "Couldn't join the instance. Try again.",
	"wrong instance": "Couldn't join the instance. Try again.",
	"characters unavailable": "Can't load your character. Try again in a minute.",
}
# The channels /join and /leave take, as the server names them.
const CHAT_CHANNELS: Array[String] = ["general", "trade", "lfg"]
const RETRY_SECONDS := 2.0
# Closes from a shard's path that send the player back to the front door.
const SHARD_TURNED_AWAY := ["town full", "wrong town"]

# The server keeps items by name, so the same name gets them back on reconnect.
# Random by default so two local windows do not fight over one name.
@export var player_name := ""
# Empty plays the server's default class.
@export var class_id := ""

var player_id := 0
var layout: GameV1.InstanceLayout
var spawn := Vector2.ZERO
var tick_rate := 20
var class_info: GameV1.ClassInfo
# Level and XP as the server last sent them. A run summary replaces it.
var character: GameV1.CharacterInfo
# In the world server's town, where only moves and item moves work.
var in_town := false
# The last run is not in the database yet, so the server turns away stash
# moves and the entrance.
var saving := false
# The chat channels the server last said the player left. Every join sends
# them again, so a move to another town or a cave keeps them.
var chat_left: Array[String] = []
# Why the server last closed the socket. Empty when it gave no reason.
var close_reason := ""
# Why the last trip to an instance ended early, for the town to say. Main
# clears it once shown.
var notice := ""

var _socket := WebSocketPeer.new()
var _world_url := DEFAULT_URL
var _url := DEFAULT_URL
# Set from InstanceTicket until the instance connection ends. While set, the
# socket is on, or on its way to, an instance server.
var _ticket_url := ""
var _token := ""
# Set from TownTicket until its socket closes. The front door and a draining
# shard send it.
var _town_url := ""
# Set once a shard turned the player away, so a second time goes offline.
# Cleared by a town join.
var _fell_back := false
# Set when this player's run summary came, so the close that follows is the
# run ending, not a drop.
var _run_over := false
# Set on the way back from an instance, so the town spawns the player at the
# entrance. Cleared once the town takes them.
var _returning := false
var _was_open := false
var _retry_in := 0.0
var _move_seq := 0
var _attack_seq := 0


func _ready() -> void:
	_world_url = _arg("--server=", DEFAULT_URL)
	_url = _world_url
	if player_name == "":
		player_name = _arg("--name=", "player-%04x" % (randi() % 0x10000))
	_connect()


func _exit_tree() -> void:
	_socket.close()


func is_joined() -> bool:
	return player_id != 0


# to is a GameV1.ItemPlace.
func send_stash_move(item_id: String, to: int) -> void:
	if not in_town:
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


# With no aim the server aims at target_id. Returns the seq, or 0 when not
# in the cave.
func send_attack(target_id: int, ability_id := "basic", aim := Vector2.ZERO) -> int:
	if not is_joined() or in_town:
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
	if not is_joined() or in_town:
		return
	var msg := _new_message()
	var pickup := msg.new_pickup()
	pickup.set_target_id(target_id)
	pickup.set_item_id(item_id)
	_send(msg)


func send_loot_open(target_id: int) -> void:
	if not is_joined() or in_town:
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
	if not is_joined() or in_town:
		return
	var msg := _new_message()
	msg.new_drop().set_item_id(item_id)
	_send(msg)


func send_use(item_id: String) -> void:
	if not is_joined() or in_town:
		return
	var msg := _new_message()
	msg.new_use().set_item_id(item_id)
	_send(msg)


# The server reads the slash commands, so this sends the line as typed.
func send_chat(text: String) -> void:
	if not is_joined():
		return
	var msg := _new_message()
	msg.new_chat().set_text(text)
	_send(msg)


func _process(delta: float) -> void:
	_socket.poll()
	var state := _socket.get_ready_state()
	if state == WebSocketPeer.STATE_OPEN and not _was_open:
		_was_open = true
		if _token != "":
			_send_instance_join()
		else:
			_send_join()
	if _was_open:
		while _socket.get_available_packet_count() > 0:
			_handle(_socket.get_packet())
	match state:
		WebSocketPeer.STATE_CLOSED:
			if _was_open:
				_closed(_socket.get_close_code(), _socket.get_close_reason())
			elif _token != "" and _url == _ticket_url:
				_back_to_world("Couldn't reach the instance. Try again.")
			elif _token == "":
				_url = _world_url
			_retry_in -= delta
			if _retry_in <= 0.0:
				_connect()


# One socket at a time: the world closes it to send the player to the
# ticket's instance or another town, and the instance closes it to send them
# back.
func _closed(code: int, reason: String) -> void:
	_was_open = false
	player_id = 0
	in_town = false
	saving = false
	close_reason = reason
	print("disconnected from %s (%d %s)" % [_url, code, reason])
	left.emit()
	if _town_url != "":
		_url = _town_url
		_town_url = ""
		_retry_in = 0.0
		return
	# A token while in a town means a ticket came, whoever closed first.
	if _token != "" and _url != _ticket_url:
		_url = _ticket_url
		_retry_in = 0.0
		return
	if _token != "":
		var why := "run over" if _run_over else reason
		_run_over = false
		_back_to_world(why)
		if why == "run over":
			return
	# A shard's path is good for one join. Any close there goes back to the
	# front door, at once the first time the shard turned the player away.
	if _url != _world_url:
		_url = _world_url
		if code == 1013 and reason in SHARD_TURNED_AWAY and not _fell_back:
			_fell_back = true
			_retry_in = 0.0
			return
	disconnected.emit()


func _back_to_world(why: String) -> void:
	_token = ""
	_ticket_url = ""
	_url = _world_url
	_retry_in = 0.0
	_returning = true
	if why != "run over":
		notice = INSTANCE_CLOSE_TEXT.get(why, why)


func _connect() -> void:
	_retry_in = RETRY_SECONDS
	_socket = WebSocketPeer.new()
	var err := _socket.connect_to_url(_url)
	if err != OK:
		push_warning("cannot connect to %s: %s" % [_url, error_string(err)])


func _send_instance_join() -> void:
	var msg := _new_message()
	msg.new_instance_join().set_token(_token)
	_send(msg)


func _send_join() -> void:
	var msg := _new_message()
	var join := msg.new_join()
	join.set_name(player_name)
	join.set_class_id(class_id)
	join.set_returning(_returning)
	for channel in chat_left:
		join.add_chat_left(channel)
	_send(msg)


func _handle(bytes: PackedByteArray) -> void:
	var msg := GameV1.ServerMessage.new()
	if msg.from_bytes(bytes) != GameV1.PB_ERR.NO_ERRORS:
		push_warning("dropped a server message that did not decode")
		return
	if msg.has_lobby():
		var lobby := msg.get_lobby()
		saving = lobby.get_saving()
		class_info = lobby.get_class_info()
		character = lobby.get_character() if lobby.has_character() else null
		town_state_changed.emit()
	elif msg.has_stash():
		stash_received.emit(msg.get_stash().get_items(), msg.get_stash().get_capacity())
	elif msg.has_stash_move_rejected():
		stash_move_rejected.emit(msg.get_stash_move_rejected())
	elif msg.has_enter_instance_rejected():
		enter_instance_rejected.emit(msg.get_enter_instance_rejected())
	elif msg.has_town_ticket():
		# Closed here for the same reason as the instance ticket below.
		_town_url = msg.get_town_ticket().get_url()
		_socket.close(1000, "changing town")
	elif msg.has_ticket():
		# Closing the world's socket here, not waiting for the world to, keeps
		# the ticket: a close that lands with it empties the read buffer.
		_ticket_url = msg.get_ticket().get_url()
		_token = msg.get_ticket().get_token()
		_socket.close(1000, "entering instance")
	elif msg.has_joined():
		var accepted := msg.get_joined()
		in_town = accepted.get_town()
		if in_town:
			_returning = false
			_fell_back = false
		player_id = accepted.get_player_id()
		layout = accepted.get_layout()
		spawn = Vector2(accepted.get_spawn().get_x(), accepted.get_spawn().get_y())
		if accepted.get_tick_rate() > 0:
			tick_rate = accepted.get_tick_rate()
		class_info = accepted.get_class_info()
		character = accepted.get_character() if accepted.has_character() else null
		print("joined %s as player %d in %s" % [_url, player_id, layout.get_name()])
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
	elif msg.has_chat():
		chat_received.emit(msg.get_chat())
	elif msg.has_chat_channels():
		var joined := msg.get_chat_channels().get_joined()
		chat_left.clear()
		for channel in CHAT_CHANNELS:
			if not joined.has(channel):
				chat_left.append(channel)


func _end_run(summary: GameV1.RunSummary) -> void:
	if summary.has_character():
		character = summary.get_character()
	run_ended.emit(summary)
	# The instance closes too, but closing here keeps the summary for the
	# same reason as the ticket.
	if _token != "":
		_run_over = true
		_socket.close(1000, "run over")


func _new_message() -> GameV1.ClientMessage:
	var msg := GameV1.ClientMessage.new()
	msg.set_protocol_version(PROTOCOL_VERSION)
	return msg


# A key held through a ticket or a run's end would send on a closing socket.
func _send(msg: GameV1.ClientMessage) -> void:
	if _socket.get_ready_state() != WebSocketPeer.STATE_OPEN:
		return
	_socket.send(msg.to_bytes(), WebSocketPeer.WRITE_MODE_BINARY)


# Run with `-- --server=ws://host:port/ws` to point at another server, and
# `-- --name=ann` to play as a set name.
func _arg(prefix: String, default: String) -> String:
	for arg in OS.get_cmdline_user_args():
		if arg.begins_with(prefix):
			return arg.trim_prefix(prefix)
	return default
