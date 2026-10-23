package sim

import "time"

type ArchetypeID string

const (
	Melee  ArchetypeID = "melee"
	Ranged ArchetypeID = "ranged"
	Elite  ArchetypeID = "elite"
)

type Special int

const (
	SpecialNone Special = iota
	SpecialCharge
)

// Archetype is one kind of enemy. Archetypes differ by these numbers, plus a
// Special for behavior the numbers cannot express.
type Archetype struct {
	Health        int
	Speed         float64
	PatrolSpeed   float64
	PatrolRadius  float64
	DetectRadius  float64
	LoseRadius    float64
	LeashRadius   float64
	ReactTime     time.Duration
	IdleTime      time.Duration
	SearchTime    time.Duration
	AttackRange   float64
	Damage        int
	Windup        time.Duration
	Cooldown      time.Duration
	RetreatHealth float64 // fraction of max health. 0 never retreats
	KeepAway      float64 // 0 means no kiting
	Projectile    *ProjectileSpec
	Special       Special
	Loot          string // drop table id

	// Charge starts when the target is between ChargeMin and ChargeMax away.
	ChargeMin, ChargeMax float64
	ChargeWindup         time.Duration
	ChargeTime           time.Duration
	ChargeSpeed          float64
	ChargeDamage         int
	ChargeCooldown       time.Duration
}

type ProjectileSpec struct {
	Speed  float64
	Radius float64
	Range  float64
}

// Every Speed is under the player's 220, so a player can always run away.
func defaultArchetypes() map[ArchetypeID]Archetype {
	return map[ArchetypeID]Archetype{
		Melee: {
			Health:        30,
			Speed:         140,
			PatrolSpeed:   60,
			PatrolRadius:  80,
			DetectRadius:  200,
			LoseRadius:    320,
			LeashRadius:   450,
			ReactTime:     400 * time.Millisecond,
			IdleTime:      time.Second,
			SearchTime:    3 * time.Second,
			AttackRange:   40,
			Damage:        10,
			Windup:        250 * time.Millisecond,
			Cooldown:      time.Second,
			RetreatHealth: 0.25,
			Loot:          "cave1_grunt",
		},
		Ranged: {
			Health:       20,
			Speed:        110,
			PatrolSpeed:  50,
			PatrolRadius: 64,
			DetectRadius: 260,
			LoseRadius:   380,
			LeashRadius:  450,
			ReactTime:    400 * time.Millisecond,
			IdleTime:     1500 * time.Millisecond,
			SearchTime:   3 * time.Second,
			AttackRange:  240,
			Damage:       8,
			Windup:       400 * time.Millisecond,
			Cooldown:     1600 * time.Millisecond,
			// Well under AttackRange, so it backs off to where it can still shoot.
			KeepAway:   140,
			Projectile: &ProjectileSpec{Speed: 320, Radius: 6, Range: 320},
			Loot:       "cave1_grunt",
		},
		Elite: {
			Health:         90,
			Speed:          150,
			PatrolSpeed:    60,
			PatrolRadius:   80,
			DetectRadius:   220,
			LoseRadius:     360,
			LeashRadius:    600,
			ReactTime:      250 * time.Millisecond,
			IdleTime:       time.Second,
			SearchTime:     5 * time.Second,
			AttackRange:    44,
			Damage:         18,
			Windup:         300 * time.Millisecond,
			Cooldown:       1200 * time.Millisecond,
			Special:        SpecialCharge,
			Loot:           "cave1_elite",
			ChargeMin:      100,
			ChargeMax:      260,
			ChargeWindup:   600 * time.Millisecond,
			ChargeTime:     400 * time.Millisecond,
			ChargeSpeed:    420,
			ChargeDamage:   25,
			ChargeCooldown: 6 * time.Second,
		},
	}
}
