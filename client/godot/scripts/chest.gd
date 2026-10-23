extends StaticBody2D

signal opened

const OPEN := preload("res://assets/kenney_tiny_dungeon/tile_0091.png")

var is_open := false


func interact(_by: Node) -> void:
	if is_open:
		return
	is_open = true
	$Body.texture = OPEN
	opened.emit()
	print("chest opened")
