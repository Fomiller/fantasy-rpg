package director

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type fakeSource struct {
	mu  sync.Mutex
	p   Players
	err error
}

func (f *fakeSource) Players(context.Context) (Players, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.p, f.err
}

func (f *fakeSource) set(p Players, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.p, f.err = p, err
}

type fakeScaler struct {
	mu       sync.Mutex
	replicas int
}

func (f *fakeScaler) Scale(_ context.Context, n int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replicas = n
	return nil
}

func (f *fakeScaler) get() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.replicas
}

func testConfig() Config {
	return Config{
		Default:    Target{Mode: Off},
		PerPod:     5,
		MaxWorkers: 3,
		Fill:       FillRule{Max: 12, Slack: 1, Step: 100},
		Every:      time.Hour,
		PodTTL:     15 * time.Second,
	}
}

func newDirector(cfg Config, store Store, src Source, sc Scaler) (*Director, *Metrics) {
	m := NewMetrics(prometheus.NewRegistry())
	return New(cfg, store, sc, src, m, quiet()), m
}

// Replicas are ceil(desired / PerPod), and no mode passes MaxWorkers.
func TestFixedSetsReplicas(t *testing.T) {
	sc := &fakeScaler{}
	d, m := newDirector(testConfig(), &Memory{}, &fakeSource{}, sc)
	for _, tc := range []struct{ count, desired, replicas int }{{0, 0, 0}, {1, 1, 1}, {5, 5, 1}, {6, 6, 2}, {40, 15, 3}} {
		if err := d.SetTarget(t.Context(), Target{Mode: Fixed, Count: tc.count}); err != nil {
			t.Fatal(err)
		}
		d.Step(t.Context(), time.Now())
		if s := d.Status(); s.Desired != tc.desired || sc.get() != tc.replicas || s.Replicas != tc.replicas {
			t.Fatalf("fixed %d = %+v, scaled to %d", tc.count, s, sc.get())
		}
	}
	if got := testutil.ToFloat64(m.desired); got != 15 {
		t.Fatalf("bot_director_desired = %v", got)
	}
	if got := testutil.ToFloat64(m.mode.WithLabelValues("fixed")); got != 1 {
		t.Fatalf("bot_director_mode{fixed} = %v", got)
	}
}

func TestFillCountsHumansOnly(t *testing.T) {
	src := &fakeSource{p: Players{Humans: 2, Bots: 0}}
	d, m := newDirector(testConfig(), &Memory{}, src, &fakeScaler{})
	if err := d.SetTarget(t.Context(), Target{Mode: Fill, Count: 6}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	d.Step(t.Context(), now)
	if s := d.Status(); s.Desired != 4 || s.Humans != 2 {
		t.Fatalf("status = %+v", s)
	}
	// The bots playing do not take a human's place.
	src.set(Players{Humans: 2, Bots: 4}, nil)
	d.Step(t.Context(), now.Add(time.Second))
	if s := d.Status(); s.Desired != 4 {
		t.Fatalf("with bots = %+v", s)
	}
	// With no count the director holds what runs.
	src.set(Players{}, errors.New("world down"))
	d.Step(t.Context(), now.Add(2*time.Second))
	if s := d.Status(); s.Desired != 4 || s.Humans != 2 {
		t.Fatalf("without a count = %+v", s)
	}
	if got := testutil.ToFloat64(m.errors.WithLabelValues("players")); got != 1 {
		t.Fatalf("errors = %v", got)
	}
}

// After a restart the director starts fill mode from the bots playing, not
// from zero.
func TestRestartKeepsTargetAndBots(t *testing.T) {
	store := &Memory{}
	d, _ := newDirector(testConfig(), store, &fakeSource{}, &fakeScaler{})
	if err := d.SetTarget(t.Context(), Target{Mode: Fill, Count: 6}); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig()
	cfg.Fill.DownDelay = time.Hour
	src := &fakeSource{p: Players{Humans: 3, Bots: 5}}
	again, _ := newDirector(cfg, store, src, &fakeScaler{})
	if err := again.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	again.Step(t.Context(), time.Now())
	if s := again.Status(); s.Target != (Target{Mode: Fill, Count: 6}) || s.Desired != 4 {
		t.Fatalf("after restart = %+v, want the saved target and 5 bots cut to 3 wanted plus 1 slack", s)
	}
}

func TestTargetAPI(t *testing.T) {
	d, _ := newDirector(testConfig(), &Memory{}, &fakeSource{}, &fakeScaler{})
	mux := http.NewServeMux()
	d.Register(mux)
	do := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rec
	}
	if rec := do("PUT", "/v1/target", `{"mode":"sometimes","count":3}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad mode = %d", rec.Code)
	}
	if rec := do("PUT", "/v1/target", `{"mode":"fixed","count":-1}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("negative = %d", rec.Code)
	}
	if rec := do("PUT", "/v1/target", `{"mode":"fixed","count":7}`); rec.Code != http.StatusOK {
		t.Fatalf("set = %d %s", rec.Code, rec.Body)
	}
	if rec := do("GET", "/v1/target", ""); strings.TrimSpace(rec.Body.String()) != `{"mode":"fixed","count":7}` {
		t.Fatalf("get = %s", rec.Body)
	}
	d.Step(t.Context(), time.Now())
	if rec := do("GET", "/v1/assignment?pod=w-a&have=", ""); strings.TrimSpace(rec.Body.String()) != `{"slots":[0,1,2,3,4]}` {
		t.Fatalf("assignment = %s", rec.Body)
	}
	if rec := do("GET", "/v1/assignment?have=1", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("no pod = %d", rec.Code)
	}
}

func TestConfigMapStore(t *testing.T) {
	kube := kubefake.NewClientset()
	s := ConfigMap{Client: kube, Namespace: "fantasy-rpg", Name: "bots-target"}
	if _, ok, err := s.Load(t.Context()); ok || err != nil {
		t.Fatalf("empty load = %v %v", ok, err)
	}
	for _, want := range []Target{{Mode: Fill, Count: 6}, {Mode: Fixed, Count: 50}} {
		if err := s.Save(t.Context(), want); err != nil {
			t.Fatal(err)
		}
		got, ok, err := s.Load(t.Context())
		if !ok || err != nil || got != want {
			t.Fatalf("load = %+v %v %v, want %+v", got, ok, err, want)
		}
	}
}

func TestDeploymentScale(t *testing.T) {
	kube := kubefake.NewClientset()
	var replicas int32 = 1
	updates := 0
	kube.PrependReactor("get", "deployments", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() != "scale" {
			return false, nil, nil
		}
		return true, &autoscalingv1.Scale{Spec: autoscalingv1.ScaleSpec{Replicas: replicas}}, nil
	})
	kube.PrependReactor("update", "deployments", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() != "scale" {
			return false, nil, nil
		}
		updates++
		replicas = a.(k8stesting.UpdateAction).GetObject().(*autoscalingv1.Scale).Spec.Replicas
		return true, a.(k8stesting.UpdateAction).GetObject(), nil
	})
	d := Deployment{Client: kube, Namespace: "fantasy-rpg", Name: "workers"}
	for _, n := range []int{3, 3, 0} {
		if err := d.Scale(t.Context(), n); err != nil {
			t.Fatal(err)
		}
	}
	if replicas != 0 || updates != 2 {
		t.Fatalf("replicas %d after %d updates, want 0 after 2", replicas, updates)
	}
}
