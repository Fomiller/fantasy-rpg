package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
)

// Memory is the STORE value that allows the memory store. Nothing falls back
// to it, so a pod missing its store config crash-loops instead of losing loot.
const Memory = "memory"

// ErrNoStore is a start with no store configured.
var ErrNoStore = errors.New("no character store")

// OpenStore is the Postgres store when url is set, else the memory store when
// store is Memory, metered on reg. inventory uses it, and so do world and
// instance servers when INVENTORY_URL is empty.
func OpenStore(ctx context.Context, url, store string, reg prometheus.Registerer) (inventory.Store, string, func(), error) {
	if store != "" && store != Memory {
		return nil, "", nil, fmt.Errorf("STORE %q is not %q", store, Memory)
	}
	if url == "" {
		if store != Memory {
			return nil, "", nil, fmt.Errorf("%w: set DATABASE_URL, or STORE=memory for local dev", ErrNoStore)
		}
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
