extends SceneTree

# Names and health bars follow the settings at once, on views that exist and
# views that spawn later, and the settings survive a restart.
const TEMP_PATH := "user://settings_test.cfg"

var _failures := 0


func _initialize() -> void:
	_run.call_deferred()


func _run() -> void:
	DirAccess.remove_absolute(TEMP_PATH)
	var main: Node = load("res://scenes/main.tscn").instantiate()
	root.add_child(main)
	await process_frame
	main.settings.path = TEMP_PATH
	main._connection.player_id = 1

	var snapshot := GameV1.WorldSnapshot.new()
	_fill(snapshot.add_entities(), 1, GameV1.EntityKind.PLAYER, "ann", 50, 100)
	_fill(snapshot.add_entities(), 2, GameV1.EntityKind.PLAYER, "bob", 100, 100)
	_fill(snapshot.add_entities(), 3, GameV1.EntityKind.ENEMY, "", 10, 20)
	main._on_snapshot(snapshot)
	var me: Overhead = main._player.overhead
	var bob: Overhead = main._views[2].overhead
	var enemy: Overhead = main._views[3].overhead

	_check(me.show_name and bob.show_name and me._name.text == "ann" and bob._name.text == "bob", "names show over every player, the own one too")
	_check(me._name.visible and bob._name.visible and not enemy._name.visible, "enemies have no name")
	_check(me._bar.visible and bob._bar.visible and enemy._bar.visible, "health bars show over players and enemies")
	_check(me._bar.value == 50 and me._bar.max_value == 100 and enemy._bar.value == 10 and enemy._bar.max_value == 20, "bars show health over max health")

	main.settings.set_show_names(false)
	_check(not me._name.visible and not bob._name.visible and enemy._bar.visible, "hiding names leaves the bars")
	main.settings.set_show_bars(false)
	_check(not me._bar.visible and not bob._bar.visible and not enemy._bar.visible, "hiding bars hides them at once")

	_fill(snapshot.add_entities(), 4, GameV1.EntityKind.PLAYER, "cat", 80, 100)
	main._on_snapshot(snapshot)
	var cat: Overhead = main._views[4].overhead
	_check(not cat._name.visible and not cat._bar.visible, "views spawned later follow the settings")

	var window: SettingsWindow = main.get_node("HUD/Windows/Right/Column/Settings")
	window.visible = true
	_check(not window._names.button_pressed and not window._bars.button_pressed, "the window shows the settings")
	window._names.button_pressed = true
	window._bars.button_pressed = true
	_check(main.settings.show_names and main.settings.show_bars and me._name.visible and cat._bar.visible, "the toggles apply at once")

	var dead_snapshot := GameV1.WorldSnapshot.new()
	_fill(dead_snapshot.add_entities(), 2, GameV1.EntityKind.PLAYER, "bob", 0, 100)
	main._on_snapshot(dead_snapshot)
	_check(not bob._bar.visible and main._views[2]._health.text == "dead", "a dead player has no bar and says dead")

	main.settings.set_show_names(false)
	var reloaded := GameSettings.new()
	reloaded.path = TEMP_PATH
	reloaded.load_file()
	_check(not reloaded.show_names and reloaded.show_bars, "settings survive a restart")
	var fresh := GameSettings.new()
	fresh.path = "user://missing_settings.cfg"
	fresh.load_file()
	_check(fresh.show_names and fresh.show_bars, "both are on by default")

	DirAccess.remove_absolute(TEMP_PATH)
	print("settings_test: %s" % ("PASS" if _failures == 0 else "FAIL (%d)" % _failures))
	quit(1 if _failures > 0 else 0)


func _fill(msg: GameV1.EntityState, id: int, kind: int, player_name: String, health: int, max_health: int) -> void:
	msg.set_id(id)
	msg.set_kind(kind)
	msg.set_name(player_name)
	msg.set_health(health)
	msg.set_max_health(max_health)
	var pos := msg.new_position()
	pos.set_x(0)
	pos.set_y(0)
	var vel := msg.new_velocity()
	vel.set_x(0)
	vel.set_y(0)


func _check(ok: bool, name: String) -> void:
	print("%s %s" % ["ok  " if ok else "FAIL", name])
	if not ok:
		_failures += 1
