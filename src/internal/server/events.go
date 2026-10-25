package server

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	eventsv1 "github.com/Fomiller/fantasy-rpg/gen/go/events/v1"
	"github.com/Fomiller/fantasy-rpg/src/internal/events"
)

const (
	reasonUnavailable = "characters unavailable"
	// reasonLeft is a player who left while the lease was taken.
	reasonLeft = "left"
)

func (s *Lobby) sessionEvent(c *client, kind eventsv1.PlayerSessionKind) {
	s.Events.Produce(events.TopicPlayerSession, c.name, &eventsv1.PlayerSession{
		Version: events.Version,
		Account: c.name,
		Kind:    kind,
		World:   s.WorldID,
		Time:    timestamppb.Now(),
	})
}

// joinEvent says how a join with a valid token ended. An empty reason is accepted.
func (s *Server) joinEvent(c *client, reason string) {
	result := eventsv1.InstanceJoinResult_INSTANCE_JOIN_RESULT_ACCEPTED
	if reason != "" {
		result = eventsv1.InstanceJoinResult_INSTANCE_JOIN_RESULT_REJECTED
	}
	s.Events.Produce(events.TopicInstanceJoin, c.runID, &eventsv1.InstanceJoin{
		Version:  events.Version,
		RunId:    c.runID,
		Account:  c.name,
		Result:   result,
		Reason:   reason,
		Instance: s.Instance(),
		Time:     timestamppb.Now(),
	})
}
