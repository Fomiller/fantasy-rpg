package sim

import (
	"fmt"
	"maps"
	"slices"

	"github.com/Fomiller/fantasy-rpg/src/internal/items"
)

// Where run XP came from.
const (
	XPKill  = "kill"
	XPChest = "chest"
)

// XPEvent is run XP one player just earned. RunXP is their run total after it.
type XPEvent struct {
	Player EntityID
	Amount int
	Source string
	RunXP  int
}

// RunEnd is the XP a player banked when their run ended. XP and Level are
// the totals after banking.
type RunEnd struct {
	Player    EntityID
	Class     string
	Extracted bool
	RunXP     int
	Banked    int
	XP        int
	Level     int
	Levels    int
	// The bag the player left with. Nil on death, since it all drops.
	Kept []items.Instance
}

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
	levels := w.bank(p, amount)
	w.applyGear(p)
	return levels, nil
}

func (w *World) bank(p *Player, amount int) int {
	p.XP += amount
	p.RunXP = 0
	old := p.Level
	p.Level = w.cfg.Classes.Progression.LevelFor(p.XP)
	return p.Level - old
}

// endRun banks run XP by the extraction or death rule. It leaves stats to
// the caller, since a dead player must not be healed.
func (w *World) endRun(p *Player, extracted bool) (RunEnd, bool) {
	if p.runOver {
		return RunEnd{}, false
	}
	p.runOver = true
	rules := w.cfg.Classes.Progression
	run := p.RunXP
	banked := run * rules.DeathKeepPercent / 100
	if extracted {
		banked = run + rules.ExtractBonus + run*rules.ExtractBonusPercent/100
	}
	levels := w.bank(p, banked)
	end := RunEnd{Player: p.ID, Class: p.Class, Extracted: extracted, RunXP: run, Banked: banked, XP: p.XP, Level: p.Level, Levels: levels}
	if extracted {
		end.Kept = slices.Clone(p.Inventory.Items)
	}
	return end, true
}

func (w *World) grantXP(p *Player, amount int, source string, ev *Events) {
	if p.runOver || amount <= 0 {
		return
	}
	p.RunXP += amount
	ev.XP = append(ev.XP, XPEvent{Player: p.ID, Amount: amount, Source: source, RunXP: p.RunXP})
}

// grantKillXP gives the full XP to every player who hit the enemy within the
// assist window. No split, so co-op players never fight over the last hit.
func (w *World) grantKillXP(e *Enemy, ev *Events) {
	xp := w.cfg.Archetypes[e.Archetype].XP
	window := w.cfg.Classes.Progression.AssistWindow()
	for _, id := range slices.Sorted(maps.Keys(e.hits)) {
		if p, ok := w.players[id]; ok && w.now-e.hits[id] <= window {
			w.grantXP(p, xp, XPKill, ev)
		}
	}
}
