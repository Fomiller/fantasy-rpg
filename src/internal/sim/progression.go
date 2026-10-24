package sim

import "fmt"

// AddRunXP gives a player XP for this run. It does not change the level, so
// stats never move mid-fight.
func (w *World) AddRunXP(id EntityID, amount int) (int, error) {
	p, ok := w.players[id]
	if !ok {
		return 0, errNoPlayer
	}
	if amount < 0 {
		return 0, fmt.Errorf("xp is %d", amount)
	}
	p.RunXP += amount
	return p.RunXP, nil
}

// BankXP ends the run's XP: it adds amount to the banked total, clears run
// XP and applies any new levels. It returns how many levels were gained.
// How much of the run XP to bank is the caller's rule.
func (w *World) BankXP(id EntityID, amount int) (int, error) {
	p, ok := w.players[id]
	if !ok {
		return 0, errNoPlayer
	}
	if amount < 0 {
		return 0, fmt.Errorf("xp is %d", amount)
	}
	p.XP += amount
	p.RunXP = 0
	old := p.Level
	p.Level = w.cfg.Classes.Progression.LevelFor(p.XP)
	w.applyGear(p)
	return p.Level - old, nil
}
