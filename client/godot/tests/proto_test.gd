extends SceneTree

# Shares fixtures with src/internal/protocol/protocol_test.go so Go and
# GDScript are checked against each other, not just against themselves.
const TESTDATA := "res://../../proto/testdata"

var _failures := 0


func _initialize() -> void:
	var testdata := ProjectSettings.globalize_path(TESTDATA).simplify_path()
	_test_decode_server_snapshot(testdata.path_join("server_snapshot.bin"))
	_test_encode_client_move(testdata.path_join("client_move.bin"), OS.get_environment("UPDATE") == "1")

	print("proto_test: %s" % ("PASS" if _failures == 0 else "FAIL (%d)" % _failures))
	quit(1 if _failures > 0 else 0)


func _test_decode_server_snapshot(path: String) -> void:
	var bytes := FileAccess.get_file_as_bytes(path)
	_check(bytes.size() > 0, "read server_snapshot.bin")

	var msg := GameV1.ServerMessage.new()
	_check(msg.from_bytes(bytes) == GameV1.PB_ERR.NO_ERRORS, "decode ServerMessage")
	_check(msg.get_protocol_version() == 1, "protocol version is 1")
	_check(msg.has_snapshot(), "payload is snapshot")

	var snapshot: GameV1.WorldSnapshot = msg.get_snapshot()
	_check(snapshot.get_tick() == 42, "tick is 42")
	var entities: Array = snapshot.get_entities()
	_check(entities.size() == 3, "three entities")
	if entities.size() != 3:
		return

	var player: GameV1.EntityState = entities[0]
	_check(player.get_id() == 7 and player.get_kind() == GameV1.EntityKind.PLAYER, "first entity is player 7")
	_check(is_equal_approx(player.get_position().get_x(), 1.5) and is_equal_approx(player.get_position().get_y(), -2.0), "player position is (1.5, -2)")
	_check(player.get_health() == 100, "player health is 100")
	_check(player.get_class_id() == "warrior" and player.get_resource() == 42 and player.get_max_resource() == 100, "player is a warrior with 42/100 rage")

	var enemy: GameV1.EntityState = entities[1]
	_check(enemy.get_id() == 9 and enemy.get_kind() == GameV1.EntityKind.ENEMY and enemy.get_health() == 30, "second entity is enemy 9 with 30 health")
	_check(enemy.get_max_health() == 90 and enemy.get_archetype() == "elite", "enemy is an elite with 90 max health")
	_check(enemy.get_enemy_state() == GameV1.EnemyState.CHARGE and is_equal_approx(enemy.get_aim().get_y(), -1.0), "enemy is charging upward")

	var shot: GameV1.EntityState = entities[2]
	_check(shot.get_kind() == GameV1.EntityKind.PROJECTILE and is_equal_approx(shot.get_velocity().get_x(), 320.0), "third entity is a projectile moving right")


func _test_encode_client_move(path: String, update: bool) -> void:
	var msg := GameV1.ClientMessage.new()
	msg.set_protocol_version(1)
	var move := msg.new_move()
	move.set_seq(3)
	var direction := move.new_direction()
	direction.set_x(1.0)
	direction.set_y(-0.5)
	var bytes := msg.to_bytes()

	if update:
		var file := FileAccess.open(path, FileAccess.WRITE)
		file.store_buffer(bytes)
		file.close()
	_check(bytes == FileAccess.get_file_as_bytes(path), "encoded ClientMessage matches client_move.bin")


func _check(ok: bool, name: String) -> void:
	print("%s %s" % ["ok  " if ok else "FAIL", name])
	if not ok:
		_failures += 1
