package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// Postgres comes from TEST_DATABASE_URL, else a testcontainers postgres:18.
// Without Docker the tests skip, unless CI is set.
var (
	adminURL  string
	setupErr  error
	noDocker  bool
	template  string
	createMu  sync.Mutex
	dbCounter atomic.Int64
)

func TestMain(m *testing.M) {
	os.Exit(setup(m))
}

func setup(m *testing.M) int {
	ctx := context.Background()
	adminURL = os.Getenv("TEST_DATABASE_URL")
	if adminURL == "" {
		// The images are public. Without this, testcontainers asks every
		// credential helper in ~/.docker/config.json, and a locked one hangs.
		if os.Getenv("DOCKER_AUTH_CONFIG") == "" {
			os.Setenv("DOCKER_AUTH_CONFIG", "{}")
		}
		startCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		c, err := postgres.Run(startCtx, "postgres:18",
			postgres.WithDatabase("fantasy"),
			postgres.WithUsername("fantasy"),
			postgres.WithPassword("fantasy"),
			postgres.BasicWaitStrategies(),
		)
		if c != nil {
			defer c.Terminate(context.Background())
		}
		if err != nil {
			setupErr = fmt.Errorf("start postgres container: %w", err)
			noDocker = true
			return m.Run()
		}
		adminURL, err = c.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			setupErr = err
			return m.Run()
		}
	}

	// Migrate once into a template. Each test copies it, so tests share no rows.
	template = fmt.Sprintf("fantasy_template_%d", os.Getpid())
	if setupErr = createDB(ctx, template, "template0"); setupErr != nil {
		return m.Run()
	}
	defer dropDB(template)
	conn, err := Open(dbURL(template))
	if err != nil {
		setupErr = err
		return m.Run()
	}
	if _, err := Migrate(ctx, conn); err != nil {
		setupErr = fmt.Errorf("migrate template: %w", err)
	}
	// CREATE DATABASE ... TEMPLATE fails while anyone is connected to it.
	conn.Close()
	return m.Run()
}

func dbURL(name string) string {
	u, err := url.Parse(adminURL)
	if err != nil {
		panic(err)
	}
	u.Path = "/" + name
	return u.String()
}

func admin(ctx context.Context, query string) error {
	conn, err := Open(adminURL)
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.ExecContext(ctx, query)
	return err
}

func createDB(ctx context.Context, name, from string) error {
	createMu.Lock()
	defer createMu.Unlock()
	return admin(ctx, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", name, from))
}

func dropDB(name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	admin(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", name))
}

// newDB returns a fresh database. Pass migrated false for an empty one.
func newDB(t *testing.T, migrated bool) *sql.DB {
	t.Helper()
	if setupErr != nil {
		if !noDocker || os.Getenv("CI") != "" {
			t.Fatalf("no postgres for tests: %v", setupErr)
		}
		t.Skipf("no postgres for tests: %v", setupErr)
	}
	from := "template0"
	if migrated {
		from = template
	}
	name := fmt.Sprintf("fantasy_test_%d_%d", os.Getpid(), dbCounter.Add(1))
	if err := createDB(t.Context(), name, from); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dropDB(name) })
	conn, err := Open(dbURL(name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestMigrateEmptyDatabase(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn := newDB(t, false)

	if err := CheckVersion(ctx, conn); err == nil {
		t.Fatal("CheckVersion passed on an empty database")
	}
	results, err := Migrate(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	files, err := embedded.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != len(files) {
		t.Fatalf("applied %d migrations, want %d", len(results), len(files))
	}
	if err := CheckVersion(ctx, conn); err != nil {
		t.Fatal(err)
	}

	results, err = Migrate(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("second migrate applied %d migrations, want 0", len(results))
	}
}

func TestCheckVersionOlderSchema(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn := newDB(t, false)
	p, err := provider(conn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, 1); err != nil {
		t.Fatal(err)
	}
	err = CheckVersion(ctx, conn)
	if err == nil || !strings.Contains(err.Error(), "older") {
		t.Fatalf("CheckVersion on version 1 = %v, want an older schema error", err)
	}
}

func TestCheckVersionNewerSchema(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn := newDB(t, true)
	if _, err := conn.ExecContext(ctx, "INSERT INTO goose_db_version (version_id, is_applied) VALUES (9999, true)"); err != nil {
		t.Fatal(err)
	}
	if err := CheckVersion(ctx, conn); err != nil {
		t.Fatalf("CheckVersion on a newer schema = %v, want nil", err)
	}
}

func TestSchemaLimits(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn := newDB(t, true)

	var account int64
	if err := conn.QueryRowContext(ctx, "INSERT INTO accounts (name) VALUES ('ada') RETURNING id").Scan(&account); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO characters (account_id, class_id) VALUES ($1, 'warrior')", account); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		query string
		ok    bool
	}{
		{"stash at limit", "UPDATE accounts SET stash = (SELECT jsonb_agg(i) FROM generate_series(1, 100) i)", true},
		{"stash over limit", "UPDATE accounts SET stash = (SELECT jsonb_agg(i) FROM generate_series(1, 101) i)", false},
		{"stash not an array", `UPDATE accounts SET stash = '{}'`, false},
		{"bag at limit", "UPDATE characters SET bag = (SELECT jsonb_agg(i) FROM generate_series(1, 20) i)", true},
		{"bag over limit", "UPDATE characters SET bag = (SELECT jsonb_agg(i) FROM generate_series(1, 21) i)", false},
		{"equipment not an array", `UPDATE characters SET equipment = '{}'`, false},
		{"negative xp", "UPDATE characters SET xp = -1", false},
		{"second warrior", fmt.Sprintf("INSERT INTO characters (account_id, class_id) VALUES (%d, 'warrior')", account), false},
		{"duplicate name", "INSERT INTO accounts (name) VALUES ('ada')", false},
	}
	for _, tt := range tests {
		_, err := conn.ExecContext(ctx, tt.query)
		if tt.ok && err != nil {
			t.Errorf("%s: %v", tt.name, err)
		}
		if !tt.ok && err == nil {
			t.Errorf("%s: no error, want a constraint violation", tt.name)
		}
	}

	if _, err := conn.ExecContext(ctx, "DELETE FROM accounts"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM characters").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d characters left after deleting the account, want 0", n)
	}
}
