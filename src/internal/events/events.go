// Package events produces facts about what happened to Redpanda. Topics and
// keys are in docs/src/design/instance-manager.md. Postgres stays the source
// of truth, so nothing here waits on a produce.
package events

import (
	"log/slog"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/protobuf/proto"
)

const (
	TopicPlayerSession     = "player.session"
	TopicInstanceJoin      = "instance.join"
	TopicInstanceLifecycle = "instance.lifecycle"
	TopicRunEnded          = "run.ended"
	TopicInventoryApplied  = "inventory.applied"
)

// Version goes in every payload's version field.
const Version = 1

// Producer sends msg on topic under key. Produce never blocks on the broker.
// A failure is logged and counted, not returned.
type Producer interface {
	Produce(topic, key string, msg proto.Message)
	// Close waits a short time for queued records, then stops.
	Close()
}

// Open picks the producer for brokers, a comma-separated list. Empty means
// events are off.
func Open(brokers string, reg prometheus.Registerer, log *slog.Logger) (Producer, string, error) {
	if brokers == "" {
		return Nop{}, "off", nil
	}
	k, err := NewKafka(strings.Split(brokers, ","), reg, log)
	if err != nil {
		return nil, "", err
	}
	return k, "kafka", nil
}

// Nop drops every event.
type Nop struct{}

func (Nop) Produce(string, string, proto.Message) {}
func (Nop) Close()                                {}

// Record is one event a Memory producer kept.
type Record struct {
	Topic, Key string
	Msg        proto.Message
}

// Memory keeps every event, for tests.
type Memory struct {
	mu      sync.Mutex
	records []Record
}

func NewMemory() *Memory { return &Memory{} }

func (m *Memory) Produce(topic, key string, msg proto.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, Record{Topic: topic, Key: key, Msg: proto.Clone(msg)})
}

func (m *Memory) Close() {}

// Records returns what was produced on topic, in order.
func (m *Memory) Records(topic string) []Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Record
	for _, r := range m.records {
		if r.Topic == topic {
			out = append(out, r)
		}
	}
	return out
}
