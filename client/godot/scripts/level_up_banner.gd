class_name LevelUpBanner
extends Label

const POP_SCALE := 1.8
const POP_SECONDS := 0.3
const HOLD_SECONDS := 2.5
const FADE_SECONDS := 0.6

var _tween: Tween


func _ready() -> void:
	mouse_filter = Control.MOUSE_FILTER_IGNORE
	visible = false


func play(level: int) -> void:
	text = "Level up! You are level %d" % level
	visible = true
	modulate = Color.WHITE
	pivot_offset = size / 2
	scale = Vector2.ONE * POP_SCALE
	if _tween:
		_tween.kill()
	_tween = create_tween()
	_tween.tween_property(self, "scale", Vector2.ONE, POP_SECONDS).set_trans(Tween.TRANS_BACK).set_ease(Tween.EASE_OUT)
	_tween.tween_interval(HOLD_SECONDS)
	_tween.tween_property(self, "modulate:a", 0.0, FADE_SECONDS)
	_tween.tween_callback(hide)
