package director

import "time"

// FillRule is how fill mode moves the bot count toward the target minus the
// humans. A change has to hold for its delay first, so a player crossing
// from the town to a cave, and out of both counts for a moment, moves
// nothing.
type FillRule struct {
	// Max caps the bots in fill mode. Keep it under TOWN_MAX_PLAYERS and the
	// Fleet's seats, so bots alone never fill the town or the caves.
	Max int
	// Slack is how many bots over the wanted count may wait out DownDelay.
	// Past it they leave at once, so a crowd of humans is never kept out.
	Slack int
	// Step caps the bots added or removed per Next.
	Step      int
	UpDelay   time.Duration
	DownDelay time.Duration

	desired int
	// side is which way the wanted count has been from desired since since.
	side  int
	since time.Time
}

// Reset starts from n bots, like the bots already playing after a restart.
func (f *FillRule) Reset(n int) {
	f.desired, f.side = max(0, n), 0
}

// Next is the bot count for target and humans at now.
func (f *FillRule) Next(target, humans int, now time.Time) int {
	want := min(max(target-humans, 0), f.Max)
	cur := min(f.desired, f.Max, want+f.Slack)

	side := 0
	switch {
	case want > cur:
		side = 1
	case want < cur:
		side = -1
	}
	if side != f.side {
		f.side, f.since = side, now
	}
	step := max(f.Step, 1)
	switch {
	case side > 0 && now.Sub(f.since) >= f.UpDelay:
		cur = min(want, cur+step)
	case side < 0 && now.Sub(f.since) >= f.DownDelay:
		cur = max(want, cur-step)
	}
	f.desired = cur
	return cur
}
