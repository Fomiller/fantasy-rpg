package towns

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Valkey comes from TEST_VALKEY_ADDR, else a testcontainers Valkey. Without
// Docker the tests that need it skip, unless CI is set.
var (
	valkeyAddr string
	setupErr   error
)

func TestMain(m *testing.M) {
	os.Exit(setup(m))
}

func setup(m *testing.M) int {
	valkeyAddr = os.Getenv("TEST_VALKEY_ADDR")
	if valkeyAddr != "" {
		return m.Run()
	}
	// As in the db tests: a locked credential helper would hang the pull.
	if os.Getenv("DOCKER_AUTH_CONFIG") == "" {
		os.Setenv("DOCKER_AUTH_CONFIG", "{}")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "valkey/valkey:8.1.11-alpine",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForListeningPort("6379/tcp"),
		},
		Started: true,
	})
	if c != nil {
		defer c.Terminate(context.Background())
	}
	if err != nil {
		setupErr = fmt.Errorf("start valkey container: %w", err)
		return m.Run()
	}
	valkeyAddr, setupErr = c.PortEndpoint(ctx, "6379/tcp", "")
	return m.Run()
}

// newValkey is a directory on a flushed Valkey.
func newValkey(t *testing.T, c Caps) *Valkey {
	t.Helper()
	if setupErr != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("no valkey for tests: %v", setupErr)
		}
		t.Skipf("no valkey for tests: %v", setupErr)
	}
	v := NewValkey(valkeyAddr, c)
	t.Cleanup(v.Close)
	cl, err := v.conn()
	if err != nil {
		t.Fatal(err)
	}
	if err := cl.Do(context.Background(), cl.B().Flushall().Build()).Error(); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestValkeyPublishAndTowns(t *testing.T) {
	ctx := context.Background()
	v := newValkey(t, NewCaps(8, 75))
	for _, tw := range []Town{{ID: "world-0", URL: "ws://a/ws", Players: 3, Revision: "r1"}, {ID: "world-1", Players: 1, Draining: true}} {
		if err := v.Publish(ctx, tw); err != nil {
			t.Fatal(err)
		}
	}
	ts, err := v.Towns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Town{}
	for _, tw := range ts {
		got[tw.ID] = tw
	}
	if len(got) != 2 || got["world-0"].URL != "ws://a/ws" || got["world-0"].Players != 3 || !got["world-1"].Draining {
		t.Fatalf("Towns = %+v", ts)
	}
	if got["world-0"].UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt not set")
	}

	cl, _ := v.conn()
	ttl, err := cl.Do(ctx, cl.B().Ttl().Key(townKey("world-0")).Build()).AsInt64()
	if err != nil || ttl <= 0 || ttl > int64(TownTTL/time.Second) {
		t.Fatalf("town TTL = %d, %v", ttl, err)
	}
}

func TestValkeyDropsExpiredTowns(t *testing.T) {
	ctx := context.Background()
	v := newValkey(t, NewCaps(8, 75))
	if err := v.Publish(ctx, Town{ID: "world-0"}); err != nil {
		t.Fatal(err)
	}
	cl, _ := v.conn()
	// A town key that expired leaves its id in the index.
	if err := cl.Do(ctx, cl.B().Sadd().Key(index).Member("world-9").Build()).Error(); err != nil {
		t.Fatal(err)
	}
	ts, err := v.Towns(ctx)
	if err != nil || len(ts) != 1 || ts[0].ID != "world-0" {
		t.Fatalf("Towns = %+v, %v", ts, err)
	}
	ids, _ := cl.Do(ctx, cl.B().Smembers().Key(index).Build()).AsStrSlice()
	if len(ids) != 1 || ids[0] != "world-0" {
		t.Fatalf("index = %v, want the expired id gone", ids)
	}
}

func TestValkeyRememberAndPick(t *testing.T) {
	ctx := context.Background()
	v := newValkey(t, NewCaps(8, 75))
	self := Town{ID: "world-0", Players: 1}
	if err := v.Publish(ctx, Town{ID: "world-1", Players: 7}); err != nil {
		t.Fatal(err)
	}
	got, err := v.Pick(ctx, "ada", self)
	if err != nil || got.ID != "world-0" {
		t.Fatalf("first pick = %q, %v, want world-0", got.ID, err)
	}
	if err := v.Remember(ctx, "ada", "world-1"); err != nil {
		t.Fatal(err)
	}
	got, err = v.Pick(ctx, "ada", self)
	if err != nil || got.ID != "world-1" {
		t.Fatalf("pick after Remember = %q, %v, want world-1", got.ID, err)
	}

	cl, _ := v.conn()
	ttl, err := cl.Do(ctx, cl.B().Ttl().Key(playerKey("ada")).Build()).AsInt64()
	if err != nil || ttl <= int64(TownTTL/time.Second) || ttl > int64(PlayerTTL/time.Second) {
		t.Fatalf("player TTL = %d, %v", ttl, err)
	}
}

func TestValkeyDownFailsOpen(t *testing.T) {
	ctx := context.Background()
	// Nothing listens on port 1.
	v := NewValkey("127.0.0.1:1", NewCaps(2, 75))
	defer v.Close()

	got, err := v.Pick(ctx, "ada", Town{ID: "world-0", Players: 1})
	if err != nil || got.ID != "world-0" {
		t.Fatalf("Pick = %q, %v, want self", got.ID, err)
	}
	_, err = v.Pick(ctx, "ada", Town{ID: "world-0", Players: 2})
	if !errors.Is(err, ErrFull) {
		t.Fatalf("full self: err = %v, want ErrFull", err)
	}
	if err := v.Publish(ctx, Town{ID: "world-0"}); err == nil {
		t.Fatal("Publish with no Valkey: want an error")
	}
	if n := testutil.ToFloat64(v.Errors.WithLabelValues("pick")); n != 2 {
		t.Fatalf("pick errors = %v, want 2", n)
	}
	if n := testutil.ToFloat64(v.Errors.WithLabelValues("publish")); n != 1 {
		t.Fatalf("publish errors = %v, want 1", n)
	}
}

func discard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
