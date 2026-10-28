package towns

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestChoose(t *testing.T) {
	t0 := time.Unix(1000, 0)
	old, cur := t0, t0.Add(time.Hour)
	town := func(id string, players int) Town {
		return Town{ID: id, Players: players, Revision: "new", Started: cur}
	}
	oldTown := func(id string, players int) Town {
		return Town{ID: id, Players: players, Revision: "old", Started: old}
	}
	draining := func(t Town) Town { t.Draining = true; return t }
	caps := NewCaps(8, 75) // soft 6

	tests := []struct {
		name  string
		towns []Town
		self  Town
		last  string
		want  string
		err   error
	}{
		{
			name: "only self, as in just dev",
			self: town("world-0", 0),
			want: "world-0",
		},
		{
			name:  "last shard under the hard cap, even past the soft cap",
			towns: []Town{town("world-0", 2), town("world-1", 7)},
			self:  town("world-0", 2),
			last:  "world-1",
			want:  "world-1",
		},
		{
			name:  "last shard on an old revision still wins",
			towns: []Town{town("world-0", 2), oldTown("world-1", 1)},
			self:  town("world-0", 2),
			last:  "world-1",
			want:  "world-1",
		},
		{
			name:  "last shard at the hard cap falls through to fill-first",
			towns: []Town{town("world-0", 2), town("world-1", 8)},
			self:  town("world-0", 2),
			last:  "world-1",
			want:  "world-0",
		},
		{
			name:  "last shard draining falls through",
			towns: []Town{town("world-0", 2), draining(town("world-1", 1))},
			self:  town("world-0", 2),
			last:  "world-1",
			want:  "world-0",
		},
		{
			name:  "last shard gone from the directory falls through",
			towns: []Town{town("world-0", 2), town("world-1", 4)},
			self:  town("world-0", 2),
			last:  "world-9",
			want:  "world-1",
		},
		{
			name:  "fullest under the soft cap",
			towns: []Town{town("world-0", 1), town("world-1", 5), town("world-2", 6), town("world-3", 3)},
			self:  town("world-0", 1),
			want:  "world-1",
		},
		{
			name:  "fill-first tie goes to the lowest ordinal, not string order",
			towns: []Town{town("world-10", 4), town("world-2", 4), town("world-0", 1)},
			self:  town("world-0", 1),
			want:  "world-2",
		},
		{
			name:  "fill-first skips the old revision",
			towns: []Town{oldTown("world-0", 5), town("world-1", 1)},
			self:  oldTown("world-0", 5),
			want:  "world-1",
		},
		{
			name:  "fill-first skips a draining shard",
			towns: []Town{town("world-0", 1), draining(town("world-1", 5))},
			self:  town("world-0", 1),
			want:  "world-0",
		},
		{
			name:  "newest revision ignores a draining shard started later",
			towns: []Town{oldTown("world-0", 5), oldTown("world-1", 1), draining(Town{ID: "world-2", Revision: "new", Started: cur})},
			self:  oldTown("world-0", 5),
			want:  "world-0",
		},
		{
			name:  "all past the soft cap: the emptiest under the hard cap",
			towns: []Town{town("world-0", 7), town("world-1", 6), oldTown("world-2", 7)},
			self:  town("world-0", 7),
			want:  "world-1",
		},
		{
			name:  "the emptiest can be on the old revision",
			towns: []Town{town("world-0", 7), oldTown("world-1", 6)},
			self:  town("world-0", 7),
			want:  "world-1",
		},
		{
			name:  "emptiest tie goes to the lowest ordinal",
			towns: []Town{town("world-3", 6), town("world-1", 6), town("world-0", 8)},
			self:  town("world-0", 8),
			want:  "world-1",
		},
		{
			name:  "ids with no ordinal fall back to string order",
			towns: []Town{town("beta", 3), town("alpha", 3)},
			self:  town("beta", 3),
			want:  "alpha",
		},
		{
			name:  "self replaces its own stale entry",
			towns: []Town{town("world-0", 8), town("world-1", 7)},
			self:  town("world-0", 1),
			want:  "world-0",
		},
		{
			name:  "every shard full",
			towns: []Town{town("world-0", 8), town("world-1", 8)},
			self:  town("world-0", 8),
			err:   ErrFull,
		},
		{
			name:  "every shard with room is draining",
			towns: []Town{draining(town("world-0", 1)), draining(town("world-1", 0))},
			self:  draining(town("world-0", 1)),
			last:  "world-1",
			err:   ErrFull,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Choose(tc.towns, tc.self, tc.last, caps)
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("err = %v, want %v", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.ID != tc.want {
				t.Fatalf("picked %q, want %q", got.ID, tc.want)
			}
		})
	}
}

func TestNewCaps(t *testing.T) {
	for _, tc := range []struct{ hard, percent, soft int }{
		{32, 75, 24},
		{8, 75, 6},
		{1, 75, 1},
		{10, 100, 10},
	} {
		if got := NewCaps(tc.hard, tc.percent); got != (Caps{Hard: tc.hard, Soft: tc.soft}) {
			t.Errorf("NewCaps(%d, %d) = %+v, want soft %d", tc.hard, tc.percent, got, tc.soft)
		}
	}
}

func TestBefore(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"world-2", "world-10", true},
		{"world-10", "world-2", false},
		{"world-1", "world-1", false},
		{"alpha", "world-0", true},
		{"world-0", "world0", true},
	} {
		if got := before(tc.a, tc.b); got != tc.want {
			t.Errorf("before(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestOffPicksSelf(t *testing.T) {
	off := Off{Caps: NewCaps(2, 75)}
	got, err := off.Pick(context.Background(), "ada", Town{ID: "world-0", Players: 1})
	if err != nil || got.ID != "world-0" {
		t.Fatalf("Pick = %v, %v", got.ID, err)
	}
	if _, err := off.Pick(context.Background(), "ada", Town{ID: "world-0", Players: 2}); !errors.Is(err, ErrFull) {
		t.Fatalf("full self: err = %v, want ErrFull", err)
	}
}

// fakeDir keeps what was published, and fails when err is set.
type fakeDir struct {
	Off
	mu        sync.Mutex
	published []Town
	err       error
}

func (f *fakeDir) Publish(_ context.Context, t Town) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.published = append(f.published, t)
	return f.err
}

func (f *fakeDir) last() (Town, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.published) == 0 {
		return Town{}, 0
	}
	return f.published[len(f.published)-1], len(f.published)
}

func TestShardRunAndDrain(t *testing.T) {
	dir := &fakeDir{}
	players := 3
	s := &Shard{Dir: dir, ID: "world-0", URL: "ws://x/ws", Revision: "abc", Players: func() int { return players }, Log: discard()}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	deadline := time.Now().Add(time.Second)
	for {
		if _, n := dir.last(); n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Run never published")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done

	got, _ := dir.last()
	if got.ID != "world-0" || got.URL != "ws://x/ws" || got.Players != 3 || got.Revision != "abc" || got.Draining {
		t.Fatalf("published %+v", got)
	}
	s.Drain(context.Background())
	if got, _ := dir.last(); !got.Draining {
		t.Fatalf("after Drain published %+v, want draining", got)
	}
}
