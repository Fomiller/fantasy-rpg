extends SceneTree

# Replays proto/testdata/movement.json, written by TestMovementFixture in
# src/internal/sim. Vector2 is 32-bit and the server is 64-bit, so allow a little drift.
const FIXTURE := "res://../../proto/testdata/movement.json"
const TOLERANCE := 0.05


func _initialize() -> void:
	var path := ProjectSettings.globalize_path(FIXTURE).simplify_path()
	var f: Dictionary = JSON.parse_string(FileAccess.get_file_as_string(path))
	var walls: Array[Rect2] = []
	for w: Array in f.walls:
		walls.append(Rect2(Vector2(w[0], w[1]), Vector2(w[2] - w[0], w[3] - w[1])))

	var body := Movement.new()
	body.position = Vector2(f.start[0], f.start[1])
	var worst := 0.0
	var worst_tick := 0
	for i in f.inputs.size():
		body.step(Vector2(f.inputs[i][0], f.inputs[i][1]), f.max_speed, walls, f.dt)
		var err := body.position.distance_to(Vector2(f.positions[i][0], f.positions[i][1]))
		if err > worst:
			worst = err
			worst_tick = i + 1

	var ok := worst <= TOLERANCE
	print("%s client matches server movement over %d ticks (worst %.4f at tick %d)" % ["ok  " if ok else "FAIL", f.inputs.size(), worst, worst_tick])
	print("movement_test: %s" % ("PASS" if ok else "FAIL"))
	quit(0 if ok else 1)
