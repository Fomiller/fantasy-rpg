package server

import (
	"context"
	"time"

	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
)

// Session claims: one session per name across every shard. Only the shard
// that seats a player claims the name. A front door that sends the player on
// with a TownTicket never claims, and a move off this shard releases first.
// Inventory fences each lobby save on the claim, in the save's transaction.

const (
	// A shard's claims live for shardTTL past its last heartbeat, so a
	// crashed shard's players can join elsewhere after that.
	shardBeat = 5 * time.Second
	shardTTL  = 15 * time.Second
	// reasonPlaying is a name claimed by a session on another shard.
	reasonPlaying = "already playing"
)

// claim takes name for a new session on this shard. It runs on the
// connection goroutine.
func (s *Lobby) claim(ctx context.Context, name string) (inventory.Session, error) {
	sess := inventory.Session{Name: name, ID: items.NewID(), Shard: s.WorldID, Boot: s.boot}
	ctx, cancel := context.WithTimeout(ctx, storeTimeout)
	defer cancel()
	return sess, s.Store.Claim(ctx, sess)
}

// noSession is a client with no claim, as after a move to another shard.
var noSession inventory.Session

// release queues the claim's release behind the player's other writes.
func (s *Lobby) release(c *client) {
	s.releaseThen(c, nil)
}

// releaseThen is release with done run on the loop once it lands. A move to
// another shard sends its TownTicket from done.
func (s *Lobby) releaseThen(c *client, done func(*client, error)) {
	sess := c.session
	c.writes.push(storeWrite{op: "release session", patience: releasePatience, call: func(ctx context.Context) error {
		return s.Store.Release(ctx, sess)
	}, done: done})
}

// RunHeartbeat keeps this shard's session claims alive until ctx ends.
func (s *Lobby) RunHeartbeat(ctx context.Context) {
	tick := time.NewTicker(shardBeat)
	defer tick.Stop()
	for {
		hctx, cancel := context.WithTimeout(ctx, storeTimeout)
		if err := s.Store.Heartbeat(hctx, s.WorldID, s.boot, shardTTL); err != nil && ctx.Err() == nil {
			s.log.Warn("shard heartbeat", "err", err)
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
