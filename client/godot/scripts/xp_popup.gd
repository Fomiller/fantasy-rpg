class_name XpPopup
extends Label

const COLOR := Color(0.95, 0.78, 0.3)
const RISE := 40.0
const SECONDS := 1.2


static func make(amount: int) -> XpPopup:
	var popup := XpPopup.new()
	popup.text = "+%d XP" % amount
	return popup


func _ready() -> void:
	modulate = COLOR
	var tween := create_tween().set_parallel()
	tween.tween_property(self, "position:y", position.y - RISE, SECONDS)
	tween.tween_property(self, "modulate:a", 0.0, SECONDS).set_ease(Tween.EASE_IN)
	tween.chain().tween_callback(queue_free)
