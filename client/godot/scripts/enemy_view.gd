class_name EnemyView
extends CharacterView

# Reach mirrors sim.defaultArchetypes, so the telegraph shows how far the hit
# or dash goes.
const LOOKS := {
	"melee": {"texture": preload("res://assets/kenney_tiny_dungeon/tile_0110.png"), "scale": 2.0, "reach": 40.0},
	"ranged": {"texture": preload("res://assets/kenney_tiny_dungeon/tile_0084.png"), "scale": 2.0, "reach": 240.0},
	"elite": {"texture": preload("res://assets/kenney_tiny_dungeon/tile_0109.png"), "scale": 2.5, "reach": 44.0},
}
const CHARGE_REACH := 168.0
const TELEGRAPH_COLOR := Color(1, 0.9, 0.3, 0.8)
# Above 1 so the sprite glows during the windup.
const WINDUP_TINT := Color(1.5, 1.35, 0.9)
const BAR_SIZE := Vector2(32, 4)
const BAR_OFFSET := Vector2(-16, -26)

var archetype := ""
var max_health := 0
var state := GameV1.EnemyState.UNSPECIFIED
var aim := Vector2.ZERO
var moving := false


func apply(state_msg: GameV1.EntityState) -> void:
	super.apply(state_msg)
	max_health = state_msg.get_max_health()
	state = state_msg.get_enemy_state()
	aim = Vector2(state_msg.get_aim().get_x(), state_msg.get_aim().get_y()) if state_msg.has_aim() else Vector2.ZERO
	moving = velocity.length() > 1.0
	if state_msg.get_archetype() != archetype:
		archetype = state_msg.get_archetype()
		var look: Dictionary = LOOKS.get(archetype, LOOKS.melee)
		_sprite.texture = look.texture
		_sprite.scale = Vector2.ONE * look.scale
	if aim != Vector2.ZERO:
		_sprite.face(aim)
	_sprite.windup(is_telegraphing())
	_body.modulate = _tint()
	_health.text = _label()
	queue_redraw()


func is_telegraphing() -> bool:
	return is_alive() and (state == GameV1.EnemyState.ATTACK or (state == GameV1.EnemyState.CHARGE and not moving))


func _tint() -> Color:
	if not is_alive():
		return dead_color
	return WINDUP_TINT if is_telegraphing() or state == GameV1.EnemyState.CHARGE else Color.WHITE


func _label() -> String:
	if not is_alive():
		return "dead"
	match state:
		GameV1.EnemyState.DETECT:
			return "!"
		GameV1.EnemyState.SEARCH:
			return "?"
	return ""


func _draw() -> void:
	if not is_alive():
		return
	if max_health > 0:
		draw_rect(Rect2(BAR_OFFSET, BAR_SIZE), Color(0, 0, 0, 0.6))
		draw_rect(Rect2(BAR_OFFSET, Vector2(BAR_SIZE.x * clampf(float(health) / max_health, 0, 1), BAR_SIZE.y)), Color(0.3, 0.9, 0.3))
	if not is_telegraphing() or aim == Vector2.ZERO:
		return
	if state == GameV1.EnemyState.CHARGE:
		draw_line(Vector2.ZERO, aim * CHARGE_REACH, Color(TELEGRAPH_COLOR, 0.35), 32.0)
	else:
		draw_line(Vector2.ZERO, aim * float(LOOKS.get(archetype, LOOKS.melee).reach), TELEGRAPH_COLOR, 2.0)
