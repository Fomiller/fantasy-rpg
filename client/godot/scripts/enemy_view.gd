class_name EnemyView
extends EntityView

# Placeholder looks until real art lands. Reach mirrors sim.defaultArchetypes,
# so the telegraph shows how far the hit or dash goes.
const LOOKS := {
	"melee": {"color": Color(0.8, 0.2, 0.2), "points": [Vector2(-16, -16), Vector2(16, -16), Vector2(16, 16), Vector2(-16, 16)], "reach": 40.0},
	"ranged": {"color": Color(0.6, 0.3, 0.85), "points": [Vector2(0, -18), Vector2(16, 0), Vector2(0, 18), Vector2(-16, 0)], "reach": 240.0},
	"elite": {"color": Color(0.95, 0.45, 0.1), "points": [Vector2(-10, -20), Vector2(10, -20), Vector2(20, 0), Vector2(10, 20), Vector2(-10, 20), Vector2(-20, 0)], "reach": 44.0},
}
const CHARGE_REACH := 168.0
const TELEGRAPH_COLOR := Color(1, 0.9, 0.3, 0.8)
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
	var vel := state_msg.get_velocity()
	moving = vel != null and Vector2(vel.get_x(), vel.get_y()).length() > 1.0
	if state_msg.get_archetype() != archetype:
		archetype = state_msg.get_archetype()
		var look: Dictionary = LOOKS.get(archetype, LOOKS.melee)
		_body.polygon = PackedVector2Array(look.points)
	_body.color = _color()
	_health.text = _label()
	queue_redraw()


func is_telegraphing() -> bool:
	return is_alive() and (state == GameV1.EnemyState.ATTACK or (state == GameV1.EnemyState.CHARGE and not moving))


func _color() -> Color:
	if not is_alive():
		return dead_color
	var base: Color = LOOKS.get(archetype, LOOKS.melee).color
	return base.lightened(0.45) if is_telegraphing() or state == GameV1.EnemyState.CHARGE else base


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
