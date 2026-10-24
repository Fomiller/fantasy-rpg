// Package db owns the Postgres schema, its migrations and the Postgres
// inventory store.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed migrations/*.sql
var embedded embed.FS

// Open returns a handle for goose. It does not connect until first use.
func Open(url string) (*sql.DB, error) {
	return sql.Open("pgx", url)
}

func provider(db *sql.DB) (*goose.Provider, error) {
	migrations, err := fs.Sub(embedded, "migrations")
	if err != nil {
		return nil, err
	}
	// A session lock keeps two migrate runs, say a retried Job, from racing.
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return nil, err
	}
	return goose.NewProvider(goose.DialectPostgres, db, migrations, goose.WithSessionLocker(locker))
}

// Migrate applies every pending migration and returns the ones it applied.
func Migrate(ctx context.Context, db *sql.DB) ([]*goose.MigrationResult, error) {
	p, err := provider(db)
	if err != nil {
		return nil, err
	}
	return p.Up(ctx)
}

// CheckVersion fails if the schema is older than the newest embedded
// migration. A newer schema is fine, so an older server can run on it.
func CheckVersion(ctx context.Context, db *sql.DB) error {
	p, err := provider(db)
	if err != nil {
		return err
	}
	current, target, err := p.GetVersions(ctx)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if current < target {
		return fmt.Errorf("schema version %d is older than %d, run instance-server migrate", current, target)
	}
	return nil
}

// Retry calls fn once a second until it succeeds or timeout passes, and then
// returns the last error.
func Retry(ctx context.Context, timeout time.Duration, fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		err := fn(ctx)
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(time.Second):
		}
	}
}
