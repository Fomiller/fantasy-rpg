class_name ProjectileView
extends EntityView

var velocity := Vector2.ZERO


# Shots fly in a straight line, so moving by velocity between snapshots is exact.
func apply(state: GameV1.EntityState) -> void:
	entity_id = state.get_id()
	var pos := state.get_position()
	var vel := state.get_velocity()
	position = Vector2(pos.get_x(), pos.get_y())
	velocity = Vector2(vel.get_x(), vel.get_y()) if vel != null else Vector2.ZERO
	# The bolt art points up.
	if velocity != Vector2.ZERO:
		_body.rotation = velocity.angle() + PI / 2


func _process(delta: float) -> void:
	position += velocity * delta
