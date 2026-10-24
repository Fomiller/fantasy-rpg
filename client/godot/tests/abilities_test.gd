extends SceneTree

var _failures := 0


func _initialize() -> void:
	_run.call_deferred()


func _run() -> void:
	var bar := AbilityBar.new()
	root.add_child(bar)
	await process_frame
	bar.setup(_warrior())
	bar.set_level(3)
	_check(bar.slot(0).get_id() == "slash" and bar.slot(1).get_id() == "cleave" and bar.slot(2).get_id() == "brace", "basic is slot 0, then the rest in order")
	_check(bar.slot(3) == null, "an empty slot has no ability")

	bar.set_resource(10, 100)
	bar.started("cleave", 4000)
	await process_frame
	_check(bar.cooldown_left("cleave") > 3.5 and bar.cooldown_left("slash") == 0.0, "cooldown starts from AbilityUsed")
	_check("Cleave (30)" in bar._list.text and "rage 10/100" in bar._list.text, "bar lists abilities and rage")
	bar.rejected("brace", "resource")
	_check(bar.rejection() == "Brace: not enough rage", "rejection says why")

	bar.set_level(1)
	await process_frame
	var text := bar._list.get_parsed_text()
	_check(bar.is_locked("cleave") and bar.is_locked("brace") and not bar.is_locked("slash"), "level 1 has only slash")
	_check("Cleave (30)  Lv 2" in text and "Brace (25)  Lv 3" in text and not "Slash  Lv" in text, "locked abilities show their level")
	_check("[color=%s][lb]1/Q] Cleave" % AbilityBar.LOCKED_COLOR in bar._list.text, "locked abilities are dimmed")
	bar.rejected("cleave", "locked")
	_check(bar.rejection() == "Cleave: unlocks at Lv 2", "a locked rejection says the level")
	bar.set_level(2)
	_check(not bar.is_locked("cleave") and bar.is_locked("brace"), "level 2 unlocks cleave")

	var sprite: CharacterSprite = load("res://scenes/character_sprite.tscn").instantiate()
	root.add_child(sprite)
	await process_frame
	sprite.cast(Vector2.LEFT, 0.1)
	_check(sprite.animation() == "windup" and sprite._rig.scale.x < 0, "cast winds up facing the aim")
	await create_timer(0.15).timeout
	_check(sprite.animation() == "swing", "cast swings when the windup ends")
	sprite.cast(Vector2.RIGHT, 0.0)
	_check(sprite.animation() == "swing" and sprite._rig.scale.x > 0, "an instant cast swings at once")

	var player: Player = load("res://scenes/player.tscn").instantiate()
	root.add_child(player)
	await process_frame
	Input.action_press("move_right")
	player.cast(Vector2.RIGHT, 0.25, true)
	_check(player.get_move_input() == Vector2.ZERO, "a rooting windup holds the player still")
	await create_timer(0.3).timeout
	_check(player.get_move_input() != Vector2.ZERO, "the player can move once the windup ends")
	Input.action_release("move_right")

	var main: Node = load("res://scenes/main.tscn").instantiate()
	root.add_child(main)
	await process_frame
	main.get_node("ServerConnection").player_id = 1
	main.get_node("Player").send_move = func(_direction: Vector2) -> int: return 0
	main._on_snapshot(_snapshot(1, 3))
	_check(main.get_node("HUD/Abilities").level == 3, "the level comes from the player's snapshot")
	var caster: Player = main.get_node("Player")
	caster.cast(Vector2.DOWN, 0.5, true)
	main._on_ability_rejected(_rejection("busy"))
	_check(caster._rooted > 0.0, "a busy rejection keeps the earlier windup")
	main._on_ability_rejected(_rejection("resource"))
	caster.face(Vector2.LEFT)
	_check(caster._rooted == 0.0 and caster.facing == Vector2.LEFT, "a rejected cast frees movement and facing")
	_check(caster._sprite.animation() != "windup", "a rejected cast stops the windup")
	await create_timer(0.6).timeout
	_check(caster._sprite.animation() != "swing", "a rejected cast never swings")

	print("abilities_test: %s" % ("PASS" if _failures == 0 else "FAIL (%d)" % _failures))
	quit(1 if _failures > 0 else 0)


func _rejection(reason: String) -> GameV1.AbilityRejected:
	var event := GameV1.AbilityRejected.new()
	event.set_ability_id("cleave")
	event.set_reason(reason)
	return event


func _warrior() -> GameV1.ClassInfo:
	var info := GameV1.ClassInfo.new()
	info.set_id("warrior")
	info.set_name("Warrior")
	info.set_resource("rage")
	info.set_max_resource(100)
	info.set_basic("slash")
	for a in [["cleave", "Cleave", 30, 4000, 2], ["slash", "Slash", 0, 500, 1], ["brace", "Brace", 25, 12000, 3]]:
		var ability := info.add_abilities()
		ability.set_id(a[0])
		ability.set_name(a[1])
		ability.set_cost(a[2])
		ability.set_cooldown_ms(a[3])
		ability.set_unlock_level(a[4])
	return info


func _snapshot(id: int, level: int) -> GameV1.WorldSnapshot:
	var snapshot := GameV1.WorldSnapshot.new()
	var state := snapshot.add_entities()
	state.set_id(id)
	state.set_kind(GameV1.EntityKind.PLAYER)
	state.new_position()
	state.new_velocity()
	state.set_health(100)
	state.set_max_health(100)
	state.set_level(level)
	return snapshot


func _check(ok: bool, name: String) -> void:
	print("%s %s" % ["ok  " if ok else "FAIL", name])
	if not ok:
		_failures += 1
