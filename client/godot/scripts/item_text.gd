class_name ItemText

# Colors match the rarity table in docs/src/design/loot.md.
const RARITY_COLORS := {
	GameV1.Rarity.COMMON: Color(0.75, 0.75, 0.75),
	GameV1.Rarity.UNCOMMON: Color(0.35, 0.85, 0.35),
	GameV1.Rarity.RARE: Color(0.35, 0.6, 1.0),
	GameV1.Rarity.EPIC: Color(0.7, 0.4, 0.95),
	GameV1.Rarity.UNIQUE: Color(1.0, 0.6, 0.2),
}
# Stats that are a percent bonus, not a flat number.
const PERCENT_STATS := ["attack_speed", "move_speed", "extract_speed", "detect_radius"]


static func color(item: GameV1.Item) -> Color:
	return RARITY_COLORS.get(item.get_rarity(), Color.WHITE)


# One line per item: name in its rarity color, then every stat it gives.
static func line(item: GameV1.Item) -> String:
	var parts: Array[String] = []
	for s: GameV1.ItemStat in item.get_base():
		parts.append(_stat(s.get_stat(), s.get_value()))
	for a: GameV1.ItemAffix in item.get_affixes():
		parts.append(_stat(a.get_stat(), a.get_value()))
	return "[color=#%s]%s[/color]  %s" % [color(item).to_html(false), item.get_name(), ", ".join(parts)]


static func _stat(stat: String, value: int) -> String:
	var percent := stat in PERCENT_STATS
	return "+%d%s %s" % [value, "%" if percent else "", stat.replace("_", " ")]
