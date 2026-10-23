extends SceneTree

# Every character scene owns a sprite with its animations, and props get art.
const CHARACTER_ANIMATIONS := ["idle", "walk", "swing", "windup"]

var _failures := 0


func _initialize() -> void:
	_run.call_deferred()


func _run() -> void:
	var player: Player = load("res://scenes/player.tscn").instantiate()
	root.add_child(player)
	await process_frame
	_check_character(player.get_node("Body"), "player")
	_check(player._sprite.weapon != null, "player starts with a weapon")
	player.equip_weapon("war_axe")
	_check(player._sprite.weapon == Player.WEAPONS.war_axe, "the equipped weapon shows")
	player.equip_weapon("unknown_blade")
	_check(player._sprite.weapon == Player.DEFAULT_WEAPON, "weapons without art fall back to a sword")
	player.equip_weapon("")
	_check(not player._sprite._weapon.visible, "no weapon hides the weapon sprite")

	var sprite: CharacterSprite = player._sprite
	sprite.move(Vector2(-100, 0))
	_check(sprite.animation() == "walk" and sprite._rig.scale.x < 0, "walking left plays walk and faces left")
	sprite.move(Vector2.ZERO)
	_check(sprite.animation() == "idle" and sprite._rig.scale.x < 0, "standing still idles and keeps facing")
	player.cast(Vector2.RIGHT, 0.0, false)
	_check(sprite.animation() == "swing" and sprite._rig.scale.x > 0, "attacking swings toward the target")
	player.die()
	_check(sprite.dead and sprite.animation() != "walk", "dead player lies still")
	sprite.move(Vector2(100, 0))
	_check(sprite.animation() != "walk", "dead player does not walk")
	player.revive()
	_check(not sprite.dead and sprite.animation() == "idle", "revive stands the player up")

	for archetype in ["melee", "ranged", "elite"]:
		var enemy: EnemyView = load("res://scenes/enemy.tscn").instantiate()
		root.add_child(enemy)
		await process_frame
		var msg := GameV1.EntityState.new()
		msg.set_id(1)
		msg.set_kind(GameV1.EntityKind.ENEMY)
		msg.set_archetype(archetype)
		msg.set_health(10)
		msg.set_max_health(10)
		msg.new_position()
		msg.new_velocity()
		enemy.apply(msg)
		_check_character(enemy._sprite, archetype)
		_check(enemy._sprite.texture == EnemyView.LOOKS[archetype].texture, "%s uses its own art" % archetype)

	var remote: CharacterView = load("res://scenes/remote_player.tscn").instantiate()
	root.add_child(remote)
	await process_frame
	_check_character(remote._sprite, "remote player")

	for path in ["res://scenes/loot.tscn", "res://scenes/projectile.tscn", "res://scenes/chest.tscn"]:
		var prop: Node = load(path).instantiate()
		var body := prop.get_node("Body") as Sprite2D
		_check(body != null and body.texture != null, "%s has a sprite" % path.get_file())
		prop.free()

	print("sprites_test: %s" % ("PASS" if _failures == 0 else "FAIL (%d)" % _failures))
	quit(1 if _failures > 0 else 0)


func _check_character(sprite: CharacterSprite, name: String) -> void:
	var anim: AnimationPlayer = sprite.get_node("AnimationPlayer")
	var missing := CHARACTER_ANIMATIONS.filter(func(a: String) -> bool: return not anim.has_animation(a))
	_check(sprite.texture != null and sprite._body.texture == sprite.texture, "%s has a sprite" % name)
	_check(missing.is_empty(), "%s has idle, walk, swing and windup animations" % name)
	_check(anim.is_playing(), "%s is animated" % name)


func _check(ok: bool, name: String) -> void:
	print("%s %s" % ["ok  " if ok else "FAIL", name])
	if not ok:
		_failures += 1
