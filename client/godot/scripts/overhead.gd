class_name Overhead
extends Node2D

var show_name := true:
	set(value):
		show_name = value
		_refresh()
var show_bar := true:
	set(value):
		show_bar = value
		_refresh()

var player_name := ""
var health := 0
var max_health := 0

@onready var _name: Label = $Name
@onready var _bar: ProgressBar = $Bar


func _ready() -> void:
	_refresh()


func set_vitals(new_health: int, new_max_health: int) -> void:
	health = new_health
	max_health = new_max_health
	_refresh()


func set_player_name(value: String) -> void:
	player_name = value
	_refresh()


func _refresh() -> void:
	if _bar == null:
		return
	_name.text = player_name
	_name.visible = show_name and player_name != ""
	_bar.max_value = maxf(max_health, 1)
	_bar.value = health
	_bar.visible = show_bar and max_health > 0 and health > 0
