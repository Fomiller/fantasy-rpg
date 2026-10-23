extends StaticBody2D

signal opened

var is_open := false


func interact(_by: Node) -> void:
	if is_open:
		return
	is_open = true
	$Body.color = Color(0.85, 0.7, 0.2)
	opened.emit()
	print("chest opened")
