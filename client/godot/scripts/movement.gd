class_name Movement
extends RefCounted

# Same math as movePlayer and slide in src/internal/sim/world.go, so the
# client predicts where the server will put the player.
# tests/movement_test.gd replays the server's fixture to keep them in sync.
const ACCELERATION := 1600.0
const FRICTION := 2000.0
const HALF_SIZE := 16.0

var position := Vector2.ZERO
var velocity := Vector2.ZERO


func step(direction: Vector2, max_speed: float, walls: Array[Rect2], dt: float) -> void:
	var length := direction.length()
	if length > 1:
		direction *= 1.0 / length
	if direction != Vector2.ZERO:
		velocity = _move_toward(velocity, direction * max_speed, ACCELERATION * dt)
	else:
		velocity = _move_toward(velocity, Vector2.ZERO, FRICTION * dt)

	position.x += velocity.x * dt
	for wall in walls:
		if _box().intersects(wall):
			position.x = wall.position.x - HALF_SIZE if velocity.x > 0 else wall.end.x + HALF_SIZE
			velocity.x = 0
	position.y += velocity.y * dt
	for wall in walls:
		if _box().intersects(wall):
			position.y = wall.position.y - HALF_SIZE if velocity.y > 0 else wall.end.y + HALF_SIZE
			velocity.y = 0


func _box() -> Rect2:
	return Rect2(position - Vector2(HALF_SIZE, HALF_SIZE), Vector2(HALF_SIZE, HALF_SIZE) * 2)


# Written like sim.Vec2.MoveToward rather than Vector2.move_toward, so rounding matches.
static func _move_toward(from: Vector2, to: Vector2, delta: float) -> Vector2:
	var diff := to - from
	var dist := diff.length()
	if dist <= delta or dist < 1e-9:
		return to
	return from + diff * (delta / dist)
