// Package bot plays the game headless over the real protocol. A Bot joins
// the town, walks into the entrance, plays a run in the cave and comes back,
// over and over. A Worker runs as many bots as the bot director asks for.
package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	gamev1 "github.com/Fomiller/fantasy-rpg/gen/go/game/v1"
	"github.com/Fomiller/fantasy-rpg/src/internal/chat"
)

type Config struct {
	// URL is the world server's WebSocket URL.
	URL string
	// Via is dialed instead of every URL's scheme and host, keeping the Host
	// header. Empty dials the URLs as they are.
	Via      string
	TownTime time.Duration
	Fight    bool
	// RunTimeout gives up on a cave the bot cannot finish, like one it is
	// stuck in.
	RunTimeout time.Duration
	// Human joins without the bot flag, as a stand-in player in tests.
	Human bool
}

func New(name string, cfg Config, m *Metrics) *Bot {
	return &Bot{name: name, cfg: cfg, m: m, log: slog.Default(), rng: rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))}
}

type Bot struct {
	name string
	cfg  Config
	m    *Metrics
	log  *slog.Logger
	rng  *rand.Rand

	// For tests: runs ended, and items in the stash as the town last said.
	runs  atomic.Int64
	stash atomic.Int64

	seq       uint32
	attackSeq uint32
}

// stageError is a failure the bot counts and retries after.
type stageError struct {
	stage string
	err   error
}

func (e *stageError) Error() string { return e.stage + ": " + e.err.Error() }
func (e *stageError) Unwrap() error { return e.err }

func fail(stage string, err error) error { return &stageError{stage, err} }

// Run plays until ctx is done. A cancel closes the socket the bot is on, so
// a bot in a cave abandons the run as a disconnect does.
func (b *Bot) Run(ctx context.Context) {
	b.m.active.Inc()
	defer b.m.active.Dec()
	returning := false
	for ctx.Err() == nil {
		err := b.cycle(ctx, &returning)
		if err == nil || ctx.Err() != nil {
			continue
		}
		var se *stageError
		stage := "unknown"
		if errors.As(err, &se) {
			stage = se.stage
		}
		b.m.errors.WithLabelValues(stage).Inc()
		b.log.Warn("bot error", "name", b.name, "err", err)
		select {
		case <-ctx.Done():
		case <-time.After(time.Second + time.Duration(b.rng.Int64N(int64(time.Second)))):
		}
	}
}

// cycle is one loop: the town, a ticket, the cave, and back.
func (b *Bot) cycle(ctx context.Context, returning *bool) error {
	ticket, err := b.town(ctx, *returning)
	if err != nil {
		return err
	}
	// Whatever happens in the cave, the next join comes back from a run.
	*returning = true
	return b.cave(ctx, ticket)
}

// player is what the bot knows on one socket.
type player struct {
	id       uint64
	grid     *grid
	layout   *gamev1.InstanceLayout
	tickRate uint32
	pos      point
	seen     bool
	snapshot *gamev1.WorldSnapshot
	lastSnap time.Time
}

func (b *Bot) joined(j *gamev1.JoinAccepted) *player {
	return &player{
		id:       j.GetPlayerId(),
		grid:     newGrid(j.GetLayout()),
		layout:   j.GetLayout(),
		tickRate: max(j.GetTickRate(), 1),
		pos:      point{float64(j.GetSpawn().GetX()), float64(j.GetSpawn().GetY())},
	}
}

func (b *Bot) snapshot(p *player, s *gamev1.WorldSnapshot, where string) {
	now := time.Now()
	if !p.lastSnap.IsZero() {
		b.m.snapshotGap.WithLabelValues(where).Observe(now.Sub(p.lastSnap).Seconds())
	}
	p.lastSnap = now
	p.snapshot = s
	for _, e := range s.GetEntities() {
		if e.GetId() == p.id {
			p.pos = point{float64(e.GetPosition().GetX()), float64(e.GetPosition().GetY())}
			p.seen = true
		}
	}
}

func (b *Bot) move(ctx context.Context, c *conn, d point) error {
	b.seq++
	return c.send(ctx, &gamev1.ClientMessage{Payload: &gamev1.ClientMessage_Move{Move: &gamev1.MoveInput{
		Seq: b.seq, Direction: &gamev1.Vec2{X: float32(d.x), Y: float32(d.y)},
	}}})
}

// steer is the move toward the next waypoint, dropping waypoints reached.
// Inputs shrink near a turn so the bot does not overshoot into a wall.
func steer(pos point, route *[]point) point {
	for len(*route) > 0 && pos.dist((*route)[0]) < 6 {
		*route = (*route)[1:]
	}
	if len(*route) == 0 {
		return point{}
	}
	next := (*route)[0]
	d := pos.dist(next)
	scale := 1 / math.Max(d, 32)
	return point{(next.x - pos.x) * scale, (next.y - pos.y) * scale}
}

// stuck tracks progress, so a bot pressed into a wall or another body
// replans instead of pushing forever.
type stuck struct {
	at    point
	since time.Time
}

func (s *stuck) check(pos point, now time.Time) bool {
	if s.since.IsZero() || pos.dist(s.at) > 8 {
		s.at, s.since = pos, now
		return false
	}
	if now.Sub(s.since) > 2*time.Second {
		s.since = now
		return true
	}
	return false
}

// town joins through the front door and follows each TownTicket with the
// same JoinRequest. A full or wrong shard sends it back to the front door
// once.
func (b *Bot) town(ctx context.Context, returning bool) (*gamev1.InstanceTicket, error) {
	start := time.Now()
	url, fellBack := b.cfg.URL, false
	for {
		ticket, next, err := b.townOn(ctx, url, returning, start)
		switch {
		case next != "":
			b.m.townMoves.WithLabelValues("ticket").Inc()
			url = next
		case err != nil && url != b.cfg.URL && !fellBack && shardTurnedAway(err):
			b.m.townMoves.WithLabelValues("fallback").Inc()
			url, fellBack = b.cfg.URL, true
		default:
			return ticket, err
		}
	}
}

// shardTurnedAway is a shard path's close that means try the front door.
func shardTurnedAway(err error) bool {
	var ce websocket.CloseError
	return errors.As(err, &ce) && ce.Code == websocket.StatusTryAgainLater && (ce.Reason == "town full" || ce.Reason == "wrong town")
}

// townOn joins the town at url, walks around for townTime, then walks into
// the entrance until a ticket comes. A TownTicket returns its URL instead.
func (b *Bot) townOn(ctx context.Context, url string, returning bool, start time.Time) (*gamev1.InstanceTicket, string, error) {
	c, err := dial(ctx, url, b.cfg.Via, "town", b.m)
	if err != nil {
		return nil, "", fail("world_dial", err)
	}
	defer c.close("bot leaving")
	if err := c.send(ctx, &gamev1.ClientMessage{Payload: &gamev1.ClientMessage_Join{Join: &gamev1.JoinRequest{
		Name: b.name, Returning: returning, Bot: !b.cfg.Human, ChatLeft: chat.Shared,
	}}}); err != nil {
		return nil, "", fail("world_join", err)
	}

	var p *player
	saving := true
	var joinedAt time.Time
	wanderUntil := time.Time{}
	var route []point
	var entering time.Time
	// leaving is set by a rejection, so the bot walks out and steps in again.
	var leaving bool
	var st stuck
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		case msg, ok := <-c.msgs:
			if !ok {
				stage := "town"
				if p == nil {
					stage = "world_join"
				}
				return nil, "", fail(stage, closedErr(c))
			}
			switch {
			case msg.GetJoined() != nil:
				p = b.joined(msg.GetJoined())
				joinedAt = time.Now()
				b.m.join.Observe(joinedAt.Sub(start).Seconds())
				wanderUntil = joinedAt.Add(b.cfg.TownTime)
				tick.Reset(time.Second / time.Duration(p.tickRate))
			case msg.GetLobby() != nil:
				was := saving
				saving = msg.GetLobby().GetSaving()
				if was && !saving && returning && !joinedAt.IsZero() {
					b.m.saving.Observe(time.Since(joinedAt).Seconds())
				}
			case msg.GetStash() != nil:
				b.stash.Store(int64(len(msg.GetStash().GetItems())))
			case msg.GetSnapshot() != nil && p != nil:
				b.snapshot(p, msg.GetSnapshot(), "town")
			case msg.GetEnterInstanceRejected() != nil:
				// Saving and in_run clear on their own. Full needs a fresh step in.
				b.m.errors.WithLabelValues("enter_" + msg.GetEnterInstanceRejected().GetReason()).Inc()
				entering, leaving = time.Time{}, true
				route = nil
				wanderUntil = time.Now().Add(time.Second)
			case msg.GetTownTicket() != nil:
				return nil, msg.GetTownTicket().GetUrl(), nil
			case msg.GetTicket() != nil:
				if !entering.IsZero() {
					b.m.ticket.Observe(time.Since(entering).Seconds())
				}
				return msg.GetTicket(), "", nil
			}
		case now := <-tick.C:
			if p == nil || !p.seen {
				continue
			}
			ent := p.layout.GetEntrance()
			in := inRect(ent, p.pos)
			if !in {
				leaving = false
			}
			if in && !leaving {
				if entering.IsZero() {
					entering = now
				}
				if err := b.move(ctx, c, point{}); err != nil {
					return nil, "", fail("town", err)
				}
				continue
			}
			if st.check(p.pos, now) {
				route = nil
			}
			if len(route) == 0 {
				if now.Before(wanderUntil) || saving {
					route = p.grid.path(p.pos, []tile{b.randomTile(p.grid, ent)})
				} else {
					route = p.grid.path(p.pos, p.grid.floors(ent))
					if route == nil {
						return nil, "", fail("town", errors.New("no path to the entrance"))
					}
				}
			}
			if err := b.move(ctx, c, steer(p.pos, &route)); err != nil {
				return nil, "", fail("town", err)
			}
		}
	}
}

// randomTile is anywhere but the entrance.
func (b *Bot) randomTile(g *grid, entrance *gamev1.Rect) tile {
	fl := g.floors(nil)
	for {
		t := fl[b.rng.IntN(len(fl))]
		if !inRect(entrance, g.center(t)) {
			return t
		}
	}
}

// cave follows the ticket, walks to a chest and loots it, then walks to the
// exit. With fight on it swings at anything in reach on the way.
func (b *Bot) cave(ctx context.Context, ticket *gamev1.InstanceTicket) error {
	start := time.Now()
	c, err := dial(ctx, ticket.GetUrl(), b.cfg.Via, "cave", b.m)
	if err != nil {
		return fail("instance_dial", err)
	}
	defer c.close("bot leaving")
	if err := c.send(ctx, &gamev1.ClientMessage{Payload: &gamev1.ClientMessage_InstanceJoin{InstanceJoin: &gamev1.InstanceJoinRequest{
		Token: ticket.GetToken(),
	}}}); err != nil {
		return fail("instance_join", err)
	}

	var p *player
	var basic string
	var cooldown time.Duration
	var lastSwing time.Time
	var route []point
	var looted bool
	var st stuck
	deadline := time.NewTimer(b.cfg.RunTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fail("run", fmt.Errorf("no extraction in %s", b.cfg.RunTimeout))
		case msg, ok := <-c.msgs:
			if !ok {
				stage := "run"
				if p == nil {
					stage = "instance_join"
				}
				return fail(stage, closedErr(c))
			}
			switch {
			case msg.GetJoined() != nil:
				p = b.joined(msg.GetJoined())
				b.m.instanceJoin.Observe(time.Since(start).Seconds())
				tick.Reset(time.Second / time.Duration(p.tickRate))
				ci := msg.GetJoined().GetClassInfo()
				basic = ci.GetBasic()
				for _, a := range ci.GetAbilities() {
					if a.GetId() == basic {
						cooldown = time.Duration(a.GetCooldownMs()) * time.Millisecond
					}
				}
			case msg.GetSnapshot() != nil && p != nil:
				b.snapshot(p, msg.GetSnapshot(), "cave")
			case msg.GetExtracted() != nil && p != nil && msg.GetExtracted().GetPlayerId() == p.id && msg.GetExtracted().GetSummary() != nil:
				b.runs.Add(1)
				b.m.runs.WithLabelValues("extracted").Inc()
				c.close("run over")
				return nil
			case msg.GetRunEnded() != nil:
				b.runs.Add(1)
				b.m.runs.WithLabelValues("died").Inc()
				c.close("run over")
				return nil
			}
		case now := <-tick.C:
			if p == nil || !p.seen {
				continue
			}
			if b.cfg.Fight && now.Sub(lastSwing) >= cooldown {
				if e := nearest(p, gamev1.EntityKind_ENTITY_KIND_ENEMY, 64); e != nil {
					lastSwing = now
					b.attackSeq++
					aim := &gamev1.Vec2{X: e.GetPosition().GetX() - float32(p.pos.x), Y: e.GetPosition().GetY() - float32(p.pos.y)}
					if err := c.send(ctx, &gamev1.ClientMessage{Payload: &gamev1.ClientMessage_Attack{Attack: &gamev1.AttackRequest{
						Seq: b.attackSeq, TargetId: e.GetId(), AbilityId: "basic", Aim: aim,
					}}}); err != nil {
						return fail("run", err)
					}
				}
			}
			if !looted {
				if e := nearest(p, gamev1.EntityKind_ENTITY_KIND_CHEST, 40); e != nil && e.GetItemCount() > 0 {
					looted, route = true, nil
					if err := c.send(ctx, &gamev1.ClientMessage{Payload: &gamev1.ClientMessage_Pickup{Pickup: &gamev1.PickupRequest{TargetId: e.GetId()}}}); err != nil {
						return fail("run", err)
					}
				}
			}
			if st.check(p.pos, now) {
				route = nil
			}
			if len(route) == 0 {
				route = b.caveRoute(p, looted)
				if route == nil {
					return fail("run", errors.New("no path to the exit"))
				}
			}
			if err := b.move(ctx, c, steer(p.pos, &route)); err != nil {
				return fail("run", err)
			}
		}
	}
}

// caveRoute is to the nearest chest with items, until one is looted, then
// to the exit.
func (b *Bot) caveRoute(p *player, looted bool) []point {
	if !looted && p.snapshot != nil {
		var chests []tile
		for _, e := range p.snapshot.GetEntities() {
			if e.GetKind() == gamev1.EntityKind_ENTITY_KIND_CHEST && e.GetItemCount() > 0 {
				chests = append(chests, p.grid.tileAt(point{float64(e.GetPosition().GetX()), float64(e.GetPosition().GetY())}))
			}
		}
		if r := p.grid.path(p.pos, chests); r != nil {
			return r
		}
	}
	return p.grid.path(p.pos, p.grid.floors(p.layout.GetExtraction()))
}

func nearest(p *player, kind gamev1.EntityKind, within float64) *gamev1.EntityState {
	var best *gamev1.EntityState
	for _, e := range p.snapshot.GetEntities() {
		if e.GetKind() != kind || (kind == gamev1.EntityKind_ENTITY_KIND_ENEMY && e.GetHealth() <= 0) {
			continue
		}
		d := p.pos.dist(point{float64(e.GetPosition().GetX()), float64(e.GetPosition().GetY())})
		if d <= within {
			within, best = d, e
		}
	}
	return best
}

func closedErr(c *conn) error {
	if c.err == nil {
		return errors.New("closed")
	}
	return c.err
}
