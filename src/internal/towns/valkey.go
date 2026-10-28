package towns

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/valkey-io/valkey-go"
)

// index is a set of every published shard id. Towns reads it and MGETs the
// town keys, two round trips for any number of shards. A SCAN would also walk
// every player key.
const index = "towns"

func townKey(id string) string     { return "town:" + id }
func playerKey(name string) string { return "player:" + name }

// Open is a Valkey directory at addr, or Off when addr is empty. It connects
// on first use, so a Valkey that is down at start does not stop the server.
func Open(addr string, c Caps, reg prometheus.Registerer) (Directory, string) {
	if addr == "" {
		return Off{Caps: c}, "off"
	}
	v := NewValkey(addr, c)
	reg.MustRegister(v.Errors)
	return v, "valkey"
}

// Valkey is the directory in one Valkey.
type Valkey struct {
	addr string
	caps Caps
	// Errors counts failed calls by call. Register it to export it.
	Errors *prometheus.CounterVec

	mu     sync.Mutex
	client valkey.Client
}

func NewValkey(addr string, c Caps) *Valkey {
	return &Valkey{
		addr: addr,
		caps: c,
		Errors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "towns_errors_total",
			Help: "Town directory calls to Valkey that failed, by call.",
		}, []string{"call"}),
	}
}

func (v *Valkey) conn() (valkey.Client, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.client != nil {
		return v.client, nil
	}
	c, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:       []string{v.addr},
		ForceSingleClient: true,
		DisableCache:      true,
		Dialer:            net.Dialer{Timeout: time.Second},
	})
	if err != nil {
		return nil, fmt.Errorf("valkey %s: %w", v.addr, err)
	}
	v.client = c
	return c, nil
}

func (v *Valkey) count(call string, err error) error {
	if err != nil {
		v.Errors.WithLabelValues(call).Inc()
	}
	return err
}

func (v *Valkey) Publish(ctx context.Context, t Town) error {
	return v.count("publish", v.publish(ctx, t))
}

func (v *Valkey) publish(ctx context.Context, t Town) error {
	c, err := v.conn()
	if err != nil {
		return err
	}
	t.UpdatedAt = time.Now()
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	for _, r := range c.DoMulti(ctx,
		c.B().Set().Key(townKey(t.ID)).Value(string(b)).Ex(TownTTL).Build(),
		c.B().Sadd().Key(index).Member(t.ID).Build(),
	) {
		if err := r.Error(); err != nil {
			return err
		}
	}
	return nil
}

func (v *Valkey) Towns(ctx context.Context) ([]Town, error) {
	ts, _, err := v.read(ctx, "")
	return ts, v.count("towns", err)
}

// read is every live shard, and name's last shard when name is set.
func (v *Valkey) read(ctx context.Context, name string) ([]Town, string, error) {
	c, err := v.conn()
	if err != nil {
		return nil, "", err
	}
	cmds := valkey.Commands{c.B().Smembers().Key(index).Build()}
	if name != "" {
		cmds = append(cmds, c.B().Get().Key(playerKey(name)).Build())
	}
	rs := c.DoMulti(ctx, cmds...)
	ids, err := rs[0].AsStrSlice()
	if err != nil {
		return nil, "", err
	}
	var last string
	if name != "" {
		if last, err = rs[1].ToString(); err != nil && !valkey.IsValkeyNil(err) {
			return nil, "", err
		}
	}
	if len(ids) == 0 {
		return nil, last, nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = townKey(id)
	}
	msgs, err := c.Do(ctx, c.B().Mget().Key(keys...).Build()).ToArray()
	if err != nil {
		return nil, "", err
	}
	var ts []Town
	var gone []string
	for i, m := range msgs {
		s, err := m.ToString()
		if valkey.IsValkeyNil(err) {
			gone = append(gone, ids[i])
			continue
		}
		if err != nil {
			return nil, "", err
		}
		var t Town
		if err := json.Unmarshal([]byte(s), &t); err != nil {
			return nil, "", fmt.Errorf("town %s: %w", ids[i], err)
		}
		ts = append(ts, t)
	}
	// A shard dropped here by a race is back on its next beat.
	if len(gone) > 0 {
		c.Do(ctx, c.B().Srem().Key(index).Member(gone...).Build())
	}
	return ts, last, nil
}

func (v *Valkey) Remember(ctx context.Context, name, shard string) error {
	c, err := v.conn()
	if err == nil {
		err = c.Do(ctx, c.B().Set().Key(playerKey(name)).Value(shard).Ex(PlayerTTL).Build()).Error()
	}
	return v.count("remember", err)
}

// Pick reads the directory and chooses. A party's shard goes first once
// parties exist, from party:{id}.
func (v *Valkey) Pick(ctx context.Context, name string, self Town) (Town, error) {
	ts, last, err := v.read(ctx, name)
	if v.count("pick", err) != nil {
		return failOpen(self, v.caps, err)
	}
	return Choose(ts, self, last, v.caps)
}

func (v *Valkey) Close() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.client != nil {
		v.client.Close()
		v.client = nil
	}
}
