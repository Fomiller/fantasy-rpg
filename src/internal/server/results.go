package server

import (
	"context"

	"google.golang.org/protobuf/proto"

	"github.com/Fomiller/fantasy-rpg/src/internal/events"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
)

// RunResults takes a run's result. Nil from EndRun means it is durable, and
// the seat it held can go.
type RunResults interface {
	EndRun(ctx context.Context, end inventory.RunEnd) error
}

// StoreResults writes results straight to the store. It is the path with no
// Redpanda or no inventory service to consume run.ended.
type StoreResults struct{ Store inventory.Store }

func (r StoreResults) EndRun(ctx context.Context, end inventory.RunEnd) error {
	return r.Store.EndRun(ctx, end.RunID, end.Character)
}

// EventResults produces run.ended and waits for the ack. inventory applies it.
type EventResults struct{ Sender events.Sender }

func (r EventResults) EndRun(ctx context.Context, end inventory.RunEnd) error {
	payload, err := proto.Marshal(end.Event())
	if err != nil {
		return err
	}
	return r.Sender.Send(ctx, events.TopicRunEnded, end.RunID, payload)
}
