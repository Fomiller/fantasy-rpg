package db

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/protobuf/proto"

	"github.com/Fomiller/fantasy-rpg/src/internal/events"
)

const (
	relayBatch = 100
	relayEvery = 500 * time.Millisecond
)

// writeOutbox queues msg in tx, so the event exists only if the write does.
func writeOutbox(ctx context.Context, tx pgx.Tx, topic, key string, msg proto.Message) error {
	payload, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO outbox (topic, key, payload) VALUES ($1, $2, $3)`, topic, key, payload)
	return err
}

// Relay produces outbox rows in id order and deletes each once the bus acks
// it. Delivery is at least once.
type Relay struct {
	pool    *pgxpool.Pool
	bus     events.Sender
	log     *slog.Logger
	pending prometheus.Gauge
	every   time.Duration
}

// NewRelay does not connect until first use.
func NewRelay(ctx context.Context, url string, bus events.Sender, reg prometheus.Registerer, log *slog.Logger) (*Relay, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	r := &Relay{
		pool: pool,
		bus:  bus,
		log:  log,
		pending: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "outbox_pending",
			Help: "Outbox rows not yet produced.",
		}),
		every: relayEvery,
	}
	if err := reg.Register(r.pending); err != nil {
		pool.Close()
		return nil, err
	}
	return r, nil
}

func (r *Relay) Close() { r.pool.Close() }

// Run relays until ctx is done. A full batch goes again at once.
func (r *Relay) Run(ctx context.Context) {
	for {
		n, err := r.flush(ctx)
		if err != nil && ctx.Err() == nil {
			r.log.Warn("relay outbox", "sent", n, "err", err)
		}
		r.count(ctx)
		if err == nil && n == relayBatch {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(r.every):
		}
	}
}

type outboxRow struct {
	ID      int64
	Topic   string
	Key     string
	Payload []byte
}

// flush sends one batch and deletes the rows that were acked. It stops at the
// first failure, so a row is never sent before the ones ahead of it.
func (r *Relay) flush(ctx context.Context) (int, error) {
	var sent []int64
	var sendErr error
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		// The lock keeps a second relay, say an old pod still stopping, off
		// the rows this one is sending.
		rows, err := tx.Query(ctx, `
			SELECT id, topic, key, payload FROM outbox
			ORDER BY id LIMIT $1
			FOR UPDATE`, relayBatch)
		if err != nil {
			return err
		}
		batch, err := pgx.CollectRows(rows, pgx.RowToStructByPos[outboxRow])
		if err != nil {
			return err
		}
		for _, row := range batch {
			if sendErr = r.bus.Send(ctx, row.Topic, row.Key, row.Payload); sendErr != nil {
				break
			}
			sent = append(sent, row.ID)
		}
		if len(sent) == 0 {
			return nil
		}
		_, err = tx.Exec(ctx, `DELETE FROM outbox WHERE id = ANY($1)`, sent)
		return err
	})
	if err != nil {
		return 0, err
	}
	return len(sent), sendErr
}

func (r *Relay) count(ctx context.Context) {
	var n int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&n); err != nil {
		return
	}
	r.pending.Set(float64(n))
}
