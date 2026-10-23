extends SceneTree

# Needs a fresh cave-server on 127.0.0.1:8080 with one enemy above spawn.
# `just net-test` starts one.
const TIMEOUT_MSEC := 10000
const ATTACK_EVERY_MSEC := 600

var _failures := 0


class Client:
	var connection := ServerConnection.new()
	var snapshot: GameV1.WorldSnapshot
	var hits: Array[String] = []

	func _init(tree: SceneTree, name: String) -> void:
		connection.player_name = name
		connection.snapshot_received.connect(func(s: GameV1.WorldSnapshot) -> void: snapshot = s)
		connection.damage_received.connect(func(d: GameV1.DamageEvent) -> void:
			hits.append("%d->%d:%d%s" % [d.get_source_id(), d.get_target_id(), d.get_target_health(), " killed" if d.get_killed() else ""]))
		tree.root.add_child(connection)

	func entity(kind: int) -> GameV1.EntityState:
		if snapshot == null:
			return null
		for e: GameV1.EntityState in snapshot.get_entities():
			if (kind == GameV1.EntityKind.PLAYER and e.get_id() == connection.player_id) or (kind == GameV1.EntityKind.ENEMY and e.get_kind() == kind):
				return e
		return null

	func me() -> GameV1.EntityState:
		return entity(GameV1.EntityKind.PLAYER)

	func enemy() -> GameV1.EntityState:
		return entity(GameV1.EntityKind.ENEMY)


func _initialize() -> void:
	_run.call_deferred()


func _run() -> void:
	var a := Client.new(self, "a")
	var b := Client.new(self, "b")
	var clients: Array[Client] = [a, b]

	var joined := await _until(func() -> bool: return a.me() != null and b.me() != null)
	_check(joined, "both clients joined and see themselves")
	if not joined:
		_finish()
		return
	_check(a.enemy() != null and a.enemy().get_health() > 0, "enemy is alive")

	for c in clients:
		c.connection.send_move(Vector2.UP)
	var in_range := await _until(func() -> bool: return a.me().get_position().get_y() < -100.0 and b.me().get_position().get_y() < -100.0)
	for c in clients:
		c.connection.send_move(Vector2.ZERO)
	_check(in_range, "both clients walked up to the enemy")

	var enemy_id := a.enemy().get_id()
	var next_attack := 0
	var dead := await _until(func() -> bool:
		if Time.get_ticks_msec() >= next_attack:
			next_attack = Time.get_ticks_msec() + ATTACK_EVERY_MSEC
			for c in clients:
				c.connection.send_attack(enemy_id)
		return a.enemy().get_health() == 0 and b.enemy().get_health() == 0)
	_check(dead, "both clients see the enemy die")

	var sources := {}
	for hit in a.hits:
		sources[hit.get_slice("->", 0)] = true
	_check(sources.size() == 2, "both players landed hits")
	_check(a.hits == b.hits, "both clients saw the same hits")
	_check(not a.hits.is_empty() and a.hits.back().ends_with("killed"), "last hit killed the enemy")
	print("hits: %s" % [a.hits])
	_finish()


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
