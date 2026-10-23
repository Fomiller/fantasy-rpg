class_name CharacterView
extends EntityView

var velocity := Vector2.ZERO

@onready var _sprite: CharacterSprite = $Body


func apply(state: GameV1.EntityState) -> void:
	super.apply(state)
	var vel := state.get_velocity()
	velocity = Vector2(vel.get_x(), vel.get_y()) if vel != null else Vector2.ZERO
	_sprite.set_dead(not is_alive())
	_sprite.move(velocity)
