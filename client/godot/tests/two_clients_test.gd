extends SceneTree

# Needs a world-server on 127.0.0.1:8080 with instance servers running the
# loop cave. Both clients join the town and see each other there. They walk
# into the entrance and follow their tickets, and the world puts them in the
# same instance. The enemy fights back, so the players must win before they
# run out of health. `just net-test` starts them.
const TIMEOUT_MSEC := 10000
const ATTACK_EVERY_MSEC := 600
const CORRIDOR_HALF_HEIGHT := 32.0
# The town's row just below the entrance corridor, from the top of the map.
const TOWN_OPEN_ROW_Y := 224.0
# One move per server tick, so the queue on the server does not lag the walk.
const MOVE_EVERY_MSEC := 50

var _failures := 0


class Client:
	var connection := ServerConnection.new()
	var snapshot: GameV1.WorldSnapshot
	var hits: Array[String] = []

	func _init(tree: SceneTree, name: String) -> void:
		connection.player_name = name
		connection.joined.connect(func(_id: int) -> void: snapshot = null)
		connection.snapshot_received.connect(func(s: GameV1.WorldSnapshot) -> void: snapshot = s)
		connection.damage_received.connect(func(d: GameV1.DamageEvent) -> void:
			hits.append("%d->%d:%d%s" % [d.get_source_id(), d.get_target_id(), d.get_target_health(), " killed" if d.get_killed() else ""]))
		tree.root.add_child(connection)

	var enemy_id := 0
	var _next_move := 0

	# Up out of the spawn, across under the corridor, and up into the entrance.
	func walk_to_entrance() -> void:
		var me_now := me()
		if me_now == null or Time.get_ticks_msec() < _next_move:
			return
		_next_move = Time.get_ticks_msec() + MOVE_EVERY_MSEC
		var p := pos(me_now)
		var door := connection.layout.get_entrance()
		var door_x := (door.get_min().get_x() + door.get_max().get_x()) / 2.0
		var dir := Vector2.UP
		if p.y <= TOWN_OPEN_ROW_Y + 8.0 and absf(p.x - door_x) > 8.0:
			dir = Vector2(signf(door_x - p.x), 0)
		connection.send_move(dir)

	func by_id(id: int) -> GameV1.EntityState:
		if snapshot == null:
			return null
		for e: GameV1.EntityState in snapshot.get_entities():
			if e.get_id() == id:
				return e
		return null

	func me() -> GameV1.EntityState:
		return by_id(connection.player_id)

	func enemy() -> GameV1.EntityState:
		return by_id(enemy_id)

	# The live enemy closest to this player along the spawn corridor. The walk is
	# a straight line, so an enemy past a wall can be nearer and still unreachable.
	func nearest_enemy() -> GameV1.EntityState:
		var best: GameV1.EntityState = null
		var best_distance := INF
		for e: GameV1.EntityState in snapshot.get_entities():
			if e.get_kind() != GameV1.EntityKind.ENEMY or e.get_health() <= 0:
				continue
			if absf(pos(e).y - pos(me()).y) > CORRIDOR_HALF_HEIGHT:
				continue
			var d := pos(e).distance_to(pos(me()))
			if d < best_distance:
				best = e
				best_distance = d
		return best

	func saw_kill(id: int) -> bool:
		return hits.any(func(hit: String) -> bool: return hit.get_slice("->", 1).begins_with("%d:" % id) and hit.ends_with("killed"))

	static func pos(e: GameV1.EntityState) -> Vector2:
		return Vector2(e.get_position().get_x(), e.get_position().get_y())


func _initialize() -> void:
	_run.call_deferred()


func _run() -> void:
	var a := Client.new(self, "a")
	var b := Client.new(self, "b")
	var clients: Array[Client] = [a, b]

	var in_town := await _until(func() -> bool: return a.connection.in_town and b.connection.in_town and a.me() != null and b.me() != null)
	_check(in_town, "both clients joined the town")
	if not in_town:
		_finish()
		return
	_check(await _until(func() -> bool: return a.by_id(b.connection.player_id) != null and b.by_id(a.connection.player_id) != null), "each sees the other in the town")
	var start_x := Client.pos(b.by_id(a.connection.player_id)).x
	a.connection.send_move(Vector2.RIGHT)
	_check(await _until(func() -> bool:
		var other := b.by_id(a.connection.player_id)
		return other != null and Client.pos(other).x > start_x + 20.0), "b sees a walk")
	a.connection.send_move(Vector2.ZERO)

	var joined := await _until(func() -> bool:
		for c in clients:
			if c.connection.in_town:
				c.walk_to_entrance()
		return not a.connection.in_town and not b.connection.in_town and a.me() != null and b.me() != null)
	_check(joined, "both clients walked into the entrance and joined the cave")
	if not joined:
		_finish()
		return
	_check(a.connection._url == b.connection._url and "/instances/" in a.connection._url, "the world put both clients in one instance")
	var target := a.nearest_enemy()
	_check(target != null, "an enemy is alive")
	if target == null:
		_finish()
		return
	var enemy_id := target.get_id()
	a.enemy_id = enemy_id
	b.enemy_id = enemy_id

	# Walk toward the enemy until it is close. It also comes to us once it sees us.
	var in_range := await _until(func() -> bool:
		var done := true
		for c in clients:
			if _near(c):
				c.connection.send_move(Vector2.ZERO)
			else:
				done = false
				c.connection.send_move((Client.pos(c.enemy()) - Client.pos(c.me())).normalized())
		return done)
	for c in clients:
		c.connection.send_move(Vector2.ZERO)
	_check(in_range, "both clients reached the enemy")

	var next_attack := 0
	var dead := await _until(func() -> bool:
		if Time.get_ticks_msec() >= next_attack:
			next_attack = Time.get_ticks_msec() + ATTACK_EVERY_MSEC
			for c in clients:
				c.connection.send_attack(enemy_id)
		# The kill's DamageEvent comes after the snapshot that shows the enemy at 0.
		return a.saw_kill(enemy_id) and b.saw_kill(enemy_id))
	_check(dead, "both clients see the enemy die")

	var sources := {}
	var on_enemy: Array[String] = []
	var on_players := 0
	for hit in a.hits:
		if hit.get_slice("->", 1).begins_with("%d:" % enemy_id):
			sources[hit.get_slice("->", 0)] = true
			on_enemy.append(hit)
		else:
			on_players += 1
	_check(sources.size() == 2, "both players landed hits")
	_check(on_players > 0, "enemy hit back")
	_check(a.hits == b.hits, "both clients saw the same hits")
	_check(not on_enemy.is_empty() and on_enemy.back().ends_with("killed"), "last hit on the enemy killed it")
	print("hits: %s" % [a.hits])
	_finish()


func _near(c: Client) -> bool:
	return Client.pos(c.me()).distance_to(Client.pos(c.enemy())) < 50.0


func _until(condition: Callable) -> bool:
	var deadline := Time.get_ticks_msec() + TIMEOUT_MSEC
	while Time.get_ticks_msec() < deadline:
		if condition.call():
			return true
		await process_frame
	return condition.call()


func _finish() -> void:
	print("two_clients_test: %s" % ("PASS" if _failures == 0 else "FAIL (%d)" % _failures))
	quit(1 if _failures > 0 else 0)


func _check(ok: bool, name: String) -> void:
	print("%s %s" % ["ok  " if ok else "FAIL", name])
	if not ok:
		_failures += 1
