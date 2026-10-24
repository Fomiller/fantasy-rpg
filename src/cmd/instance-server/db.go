package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/Fomiller/fantasy-rpg/src/internal/db"
)

// migrate waits for the database, since in Compose and on first install it
// can start after this does.
func migrate(ctx context.Context) error {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is not set")
	}
	conn, err := db.Open(url)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := db.Retry(ctx, 2*time.Minute, conn.PingContext); err != nil {
		return err
	}
	results, err := db.Migrate(ctx, conn)
	if err != nil {
		return err
	}
	for _, r := range results {
		slog.Info("migration applied", "version", r.Source.Version, "file", r.Source.Path, "duration", r.Duration)
	}
	slog.Info("migrations done", "applied", len(results))
	return nil
}
