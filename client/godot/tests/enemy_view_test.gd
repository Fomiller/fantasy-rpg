extends SceneTree

# Enemy views must tell the archetypes apart and show the windup and charge
# telegraphs from the snapshot alone.
var _failures := 0


func _initialize() -> void:
	_run.call_deferred()


func _run() -> void:
	var main: Node = load("res://scenes/main.tscn").instantiate()
	root.add_child(main)
	await process_frame

	var views := {}
	for archetype in ["melee", "ranged", "elite"]:
		var view: EnemyView = main._spawn_view(GameV1.EntityKind.ENEMY)
		view.apply(_enemy(archetype, GameV1.EnemyState.PATROL, 20, 30, Vector2.ZERO, Vector2(60, 0)))
		main._views[view.entity_id] = view
		views[archetype] = view
	var melee: EnemyView = views.melee
	var ranged: EnemyView = views.ranged
	var elite: EnemyView = views.elite
	_check(melee.is_in_group("enemies") and main.nearest_enemy(melee.position) is EnemyView, "enemies are attack targets")
	_check(melee._sprite.texture != ranged._sprite.texture and ranged._sprite.texture != elite._sprite.texture and melee._sprite.texture != elite._sprite.texture, "each archetype has its own sprite")
	_check(elite._sprite.scale.x > melee._sprite.scale.x, "elite is drawn bigger")
	_check(melee._sprite.animation() == "walk", "a moving enemy walks")
	_check(melee.max_health == 30 and melee._health.text == "", "health bar uses max_health, no number")

	melee.apply(_enemy("melee", GameV1.EnemyState.DETECT, 20, 30, Vector2.RIGHT, Vector2.ZERO))
	_check(melee._health.text == "!" and not melee.is_telegraphing(), "detect shows an alert")
	var calm := melee._body.modulate
	melee.apply(_enemy("melee", GameV1.EnemyState.ATTACK, 20, 30, Vector2.RIGHT, Vector2.ZERO))
	_check(melee.is_telegraphing() and melee._body.modulate != calm and melee._sprite.animation() == "windup", "windup lights up the enemy")
	_check(melee._body.show_behind_parent, "telegraphs draw over the sprite")

	elite.apply(_enemy("elite", GameV1.EnemyState.CHARGE, 90, 90, Vector2.UP, Vector2.ZERO))
	_check(elite.is_telegraphing() and elite.aim == Vector2.UP, "charge windup telegraphs along the aim")
	elite.apply(_enemy("elite", GameV1.EnemyState.CHARGE, 90, 90, Vector2.UP, Vector2(0, -420)))
	_check(not elite.is_telegraphing(), "the dash itself is not a telegraph")

	ranged.apply(_enemy("ranged", GameV1.EnemyState.DEAD, 0, 20, Vector2.ZERO, Vector2.ZERO))
	_check(ranged._health.text == "dead" and ranged._body.modulate == ranged.dead_color and ranged._sprite.dead, "dead enemy is gray and lying down")

	var shot: ProjectileView = main._spawn_view(GameV1.EntityKind.PROJECTILE)
	var msg := GameV1.EntityState.new()
	msg.set_id(99)
	msg.set_kind(GameV1.EntityKind.PROJECTILE)
	_vec(msg.new_position(), Vector2(10, 0))
	_vec(msg.new_velocity(), Vector2(320, 0))
	shot.apply(msg)
	main._views[99] = shot
	shot._process(0.5)
	_check(shot.position.is_equal_approx(Vector2(170, 0)), "projectile flies by its velocity between snapshots")
	_check(main.nearest_enemy(shot.position) == null, "projectiles are not attack targets")

	print("enemy_view_test: %s" % ("PASS" if _failures == 0 else "FAIL (%d)" % _failures))
	quit(1 if _failures > 0 else 0)


func _enemy(archetype: String, state: int, health: int, max_health: int, aim: Vector2, vel: Vector2) -> GameV1.EntityState:
	var msg := GameV1.EntityState.new()
	msg.set_id(["melee", "ranged", "elite"].find(archetype) + 1)
	msg.set_kind(GameV1.EntityKind.ENEMY)
	msg.set_archetype(archetype)
	msg.set_enemy_state(state)
	msg.set_health(health)
	msg.set_max_health(max_health)
	_vec(msg.new_position(), Vector2.ZERO)
	_vec(msg.new_velocity(), vel)
	_vec(msg.new_aim(), aim)
	return msg


func _vec(v: GameV1.Vec2, value: Vector2) -> void:
	v.set_x(value.x)
	v.set_y(value.y)


func _check(ok: bool, name: String) -> void:
	print("%s %s" % ["ok  " if ok else "FAIL", name])
	if not ok:
		_failures += 1
