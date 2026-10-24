package db

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
)

// OpenStore is the Postgres store when url is set and the memory store when
// it is not, metered on reg. Both servers pick their store this way.
func OpenStore(ctx context.Context, url string, reg prometheus.Registerer) (store inventory.Store, name string, closeFn func(), err error) {
	if url == "" {
		return inventory.Metered(inventory.NewMemory(), reg), "memory", func() {}, nil
	}
	if err := checkSchema(ctx, url); err != nil {
		return nil, "", nil, err
	}
	pg, err := NewStore(ctx, url)
	if err != nil {
		return nil, "", nil, err
	}
	return inventory.Metered(pg, reg), "postgres", pg.Close, nil
}

// checkSchema refuses a schema older than this binary, so a skipped migrate
// fails at start instead of on the first query.
func checkSchema(ctx context.Context, url string) error {
	conn, err := Open(url)
	if err != nil {
		return err
	}
	defer conn.Close()

	return Retry(ctx, 30*time.Second, func(ctx context.Context) error {
		if err := conn.PingContext(ctx); err != nil {
			return err
		}
		return CheckVersion(ctx, conn)
	})
}
