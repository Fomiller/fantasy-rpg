package manager

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	agonesv1 "agones.dev/agones/pkg/apis/agones/v1"
	allocationv1 "agones.dev/agones/pkg/apis/allocation/v1"
	agonesfake "agones.dev/agones/pkg/client/clientset/versioned/fake"
	"agones.dev/agones/pkg/client/informers/externalversions"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const ns = "fantasy-rpg"

type harness struct {
	m       *Manager
	agones  *agonesfake.Clientset
	kube    *kubefake.Clientset
	dynamic *dynamicfake.FakeDynamicClient
	metrics *Metrics
	// allocations has every GameServerAllocation the manager sent.
	allocations []*allocationv1.GameServerAllocation
}

// newHarness answers each allocation with status, as Agones would.
func newHarness(t *testing.T, status func(*allocationv1.GameServerAllocation) (allocationv1.GameServerAllocationStatus, error), objects ...runtime.Object) *harness {
	t.Helper()
	h := &harness{
		agones:  agonesfake.NewSimpleClientset(objects...),
		kube:    kubefake.NewClientset(),
		dynamic: dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{IngressRoutes: "IngressRouteList"}),
		metrics: NewMetrics(prometheus.NewRegistry()),
	}
	h.agones.PrependReactor("create", "gameserverallocations", func(action k8stesting.Action) (bool, runtime.Object, error) {
		gsa := action.(k8stesting.CreateAction).GetObject().(*allocationv1.GameServerAllocation).DeepCopy()
		h.allocations = append(h.allocations, gsa)
		st, err := status(gsa)
		gsa.Status = st
		return true, gsa, err
	})
	cfg := Config{Namespace: ns, Fleet: "fantasy-rpg-instance", PublicURL: "wss://game.example", Host: "game.example", EntryPoint: "web"}
	h.m = New(cfg, h.agones, h.kube, h.dynamic, h.metrics, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return h
}

func gameServer(name string, state agonesv1.GameServerState, runs ...string) *agonesv1.GameServer {
	return &agonesv1.GameServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         ns,
			UID:               types.UID("uid-" + name),
			Annotations:       map[string]string{BootAnnotation: "boot-" + name},
			CreationTimestamp: metav1.NewTime(time.Now().Add(-3 * time.Second)),
		},
		Spec: agonesv1.GameServerSpec{Ports: []agonesv1.GameServerPort{{Name: "ws", ContainerPort: 8080}}},
		Status: agonesv1.GameServerStatus{
			State: state,
			Lists: map[string]agonesv1.ListStatus{RunsList: {Capacity: 8, Values: runs}},
		},
	}
}

// allocated answers as Agones does when gs took the request's runs.
func allocated(gs *agonesv1.GameServer) func(*allocationv1.GameServerAllocation) (allocationv1.GameServerAllocationStatus, error) {
	return func(gsa *allocationv1.GameServerAllocation) (allocationv1.GameServerAllocationStatus, error) {
		values := append(slices.Clone(gs.Status.Lists[RunsList].Values), gsa.Spec.Lists[RunsList].AddValues...)
		return allocationv1.GameServerAllocationStatus{
			State:          allocationv1.GameServerAllocationAllocated,
			GameServerName: gs.Name,
			Metadata:       &allocationv1.GameServerMetadata{Annotations: gs.Annotations},
			Lists:          map[string]agonesv1.ListStatus{RunsList: {Capacity: 8, Values: values}},
		}, nil
	}
}

func TestAllocateRequest(t *testing.T) {
	gs := gameServer("gs-a", agonesv1.GameServerStateReady)
	h := newHarness(t, allocated(gs), gs)

	inst, err := h.m.Allocate(t.Context(), []string{"r1", "r2"})
	if err != nil {
		t.Fatal(err)
	}
	if inst.ID != "gs-a" || inst.Boot != "boot-gs-a" || inst.URL != "wss://game.example/instances/gs-a/ws" {
		t.Fatalf("instance = %+v", inst)
	}

	if len(h.allocations) != 1 {
		t.Fatalf("sent %d allocations, want 1", len(h.allocations))
	}
	spec := h.allocations[0].Spec
	if h.allocations[0].Namespace != ns {
		t.Errorf("allocation namespace = %q", h.allocations[0].Namespace)
	}
	if len(spec.Selectors) != 2 {
		t.Fatalf("selectors = %+v, want 2", spec.Selectors)
	}
	first, second := spec.Selectors[0], spec.Selectors[1]
	if *first.GameServerState != agonesv1.GameServerStateAllocated || first.Lists[RunsList].MinAvailable != 2 {
		t.Errorf("first selector = %+v, want Allocated with room for 2 runs", first)
	}
	if *second.GameServerState != agonesv1.GameServerStateReady || len(second.Lists) != 0 {
		t.Errorf("second selector = %+v, want any Ready", second)
	}
	for _, sel := range spec.Selectors {
		if sel.MatchLabels[agonesv1.FleetNameLabel] != "fantasy-rpg-instance" {
			t.Errorf("selector labels = %v, want the fleet", sel.MatchLabels)
		}
	}
	want := []agonesv1.Priority{{Type: agonesv1.GameServerPriorityList, Key: RunsList, Order: agonesv1.GameServerPriorityAscending}}
	if !slices.Equal(spec.Priorities, want) {
		t.Errorf("priorities = %+v, want %+v", spec.Priorities, want)
	}
	if !slices.Equal(spec.Lists[RunsList].AddValues, []string{"r1", "r2"}) {
		t.Errorf("addValues = %v, want the party's runs", spec.Lists[RunsList].AddValues)
	}
	if n := testutil.CollectAndCount(h.metrics.allocation, "instance_allocation_seconds"); n != 1 {
		t.Errorf("allocation series = %d, want 1", n)
	}
	assertRouted(t, h, gs)
}

// assertRouted checks gs has a Service and an IngressRoute, both owned by it.
func assertRouted(t *testing.T, h *harness, gs *agonesv1.GameServer) {
	t.Helper()
	svc, err := h.kube.CoreV1().Services(ns).Get(t.Context(), gs.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	if svc.Spec.Selector[agonesv1.GameServerPodLabel] != gs.Name || svc.Spec.Ports[0].Port != 8080 {
		t.Errorf("service spec = %+v", svc.Spec)
	}
	assertOwned(t, svc.OwnerReferences, gs)
	if _, ok := svc.Annotations["argocd.argoproj.io/tracking-id"]; ok {
		t.Error("service carries an Argo CD tracking annotation")
	}

	route, err := h.dynamic.Resource(IngressRoutes).Namespace(ns).Get(t.Context(), gs.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("ingressroute: %v", err)
	}
	assertOwned(t, route.GetOwnerReferences(), gs)
	if len(route.GetAnnotations()) != 0 {
		t.Errorf("ingressroute annotations = %v, want none", route.GetAnnotations())
	}
	routes := route.Object["spec"].(map[string]any)["routes"].([]any)
	r := routes[0].(map[string]any)
	if want := "Host(`game.example`) && PathPrefix(`/instances/" + gs.Name + "/`)"; r["match"] != want {
		t.Errorf("match = %v, want %s", r["match"], want)
	}
	svcRef := r["services"].([]any)[0].(map[string]any)
	if svcRef["name"] != gs.Name || svcRef["port"] != int64(8080) {
		t.Errorf("route service = %v", svcRef)
	}
}

func assertOwned(t *testing.T, refs []metav1.OwnerReference, gs *agonesv1.GameServer) {
	t.Helper()
	if len(refs) != 1 || refs[0].Kind != "GameServer" || refs[0].APIVersion != "agones.dev/v1" || refs[0].Name != gs.Name || refs[0].UID != gs.UID {
		t.Errorf("owner refs = %+v, want the GameServer %s", refs, gs.Name)
	}
}

func TestAllocateReusesARunningCave(t *testing.T) {
	gs := gameServer("gs-a", agonesv1.GameServerStateAllocated, "old")
	h := newHarness(t, allocated(gs), gs)
	inst, err := h.m.Allocate(t.Context(), []string{"r1"})
	if err != nil || !slices.Equal(inst.Runs, []string{"old", "r1"}) {
		t.Fatalf("allocate = %+v, %v", inst, err)
	}
	if c := histogramCount(t, h.metrics.allocation, "reused"); c != 1 {
		t.Errorf("reused allocations = %d, want 1", c)
	}
}

func histogramCount(t *testing.T, vec *prometheus.HistogramVec, result string) uint64 {
	t.Helper()
	ch := make(chan prometheus.Metric, 1)
	vec.WithLabelValues(result).(prometheus.Histogram).Collect(ch)
	var m dto.Metric
	if err := (<-ch).Write(&m); err != nil {
		t.Fatal(err)
	}
	return m.GetHistogram().GetSampleCount()
}

func TestAllocateFull(t *testing.T) {
	h := newHarness(t, func(*allocationv1.GameServerAllocation) (allocationv1.GameServerAllocationStatus, error) {
		return allocationv1.GameServerAllocationStatus{State: allocationv1.GameServerAllocationUnAllocated}, nil
	})
	rec := post(t, h, `{"runs":["r1"]}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "full") {
		t.Fatalf("allocate = %d %s, want 409 full", rec.Code, rec.Body)
	}
	if n := testutil.ToFloat64(h.metrics.routingErrors.WithLabelValues("full")); n != 1 {
		t.Errorf("full errors = %v, want 1", n)
	}
	if c := histogramCount(t, h.metrics.allocation, "full"); c != 1 {
		t.Errorf("full allocations = %d, want 1", c)
	}
}

func TestAllocateAPIDown(t *testing.T) {
	h := newHarness(t, func(*allocationv1.GameServerAllocation) (allocationv1.GameServerAllocationStatus, error) {
		return allocationv1.GameServerAllocationStatus{}, errors.New("connection refused")
	})
	if rec := post(t, h, `{"runs":["r1"]}`); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("allocate = %d, want 503", rec.Code)
	}
	if n := testutil.ToFloat64(h.metrics.routingErrors.WithLabelValues("allocate")); n != 1 {
		t.Errorf("allocate errors = %v, want 1", n)
	}
	if rec := post(t, h, `{"runs":[]}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("no runs = %d, want 400", rec.Code)
	}
}

func post(t *testing.T, h *harness, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.m.Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/allocate", strings.NewReader(body)))
	return rec
}

func getInstance(t *testing.T, h *harness, id string) (int, Instance) {
	t.Helper()
	mux := http.NewServeMux()
	h.m.Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/instances/"+id, nil))
	var inst Instance
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &inst); err != nil {
			t.Fatal(err)
		}
	}
	return rec.Code, inst
}

// Routes that already exist are kept, as after a manager restart.
func TestEnsureRoutesKeepsWhatExists(t *testing.T) {
	gs := gameServer("gs-a", agonesv1.GameServerStateReady)
	h := newHarness(t, allocated(gs), gs)
	if err := h.m.EnsureRoutes(t.Context(), gs); err != nil {
		t.Fatal(err)
	}
	h.kube.ClearActions()
	h.dynamic.ClearActions()
	if err := h.m.EnsureRoutes(t.Context(), gs); err != nil {
		t.Fatal(err)
	}
	for _, a := range append(h.kube.Actions(), h.dynamic.Actions()...) {
		if a.GetVerb() != "get" {
			t.Errorf("second ensure did %s %s, want only gets", a.GetVerb(), a.GetResource().Resource)
		}
	}
}

// Two replicas route one GameServer at once: one from a walk-in, the other
// from its informer. Both read before either creates, so both create. The
// loser's AlreadyExists is a success.
func TestTwoManagersRouteOneGameServer(t *testing.T) {
	gs := gameServer("gs-a", agonesv1.GameServerStateReady)
	h := newHarness(t, allocated(gs), gs)
	other := New(h.m.cfg, h.agones, h.kube, h.dynamic, NewMetrics(prometheus.NewRegistry()), h.m.log)

	var racing atomic.Bool
	racing.Store(true)
	stale := func(action k8stesting.Action) (bool, runtime.Object, error) {
		if !racing.Load() {
			return false, nil, nil
		}
		gr := action.GetResource().GroupResource()
		return true, nil, apierrors.NewNotFound(gr, action.(k8stesting.GetAction).GetName())
	}
	h.kube.PrependReactor("get", "services", stale)
	h.dynamic.PrependReactor("get", "ingressroutes", stale)

	var wg sync.WaitGroup
	var allocErr, routeErr error
	var inst Instance
	wg.Go(func() { inst, allocErr = h.m.Allocate(t.Context(), []string{"r1"}) })
	wg.Go(func() { routeErr = other.EnsureRoutes(t.Context(), gs) })
	wg.Wait()
	racing.Store(false)
	if allocErr != nil || inst.ID != "gs-a" {
		t.Fatalf("allocate = %+v, %v", inst, allocErr)
	}
	if routeErr != nil {
		t.Fatalf("informer route = %v", routeErr)
	}

	creates := map[string]int{}
	for _, a := range append(h.kube.Actions(), h.dynamic.Actions()...) {
		if a.GetVerb() == "create" {
			creates[a.GetResource().Resource]++
		}
	}
	if creates["services"] != 2 || creates["ingressroutes"] != 2 {
		t.Fatalf("creates = %v, want both managers to try each", creates)
	}
	for _, m := range []*Manager{h.m, other} {
		if n := testutil.ToFloat64(m.metrics.routingErrors.WithLabelValues("route")); n != 0 {
			t.Errorf("route errors = %v, want 0", n)
		}
	}
	assertRouted(t, h, gs)
}

func TestRouteFailureIsCounted(t *testing.T) {
	gs := gameServer("gs-a", agonesv1.GameServerStateReady)
	h := newHarness(t, allocated(gs), gs)
	h.kube.PrependReactor("create", "services", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("forbidden")
	})
	if rec := post(t, h, `{"runs":["r1"]}`); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("allocate = %d, want 503", rec.Code)
	}
	if n := testutil.ToFloat64(h.metrics.routingErrors.WithLabelValues("route")); n != 1 {
		t.Errorf("route errors = %v, want 1", n)
	}
}

// A GameServer that turns Ready is routed before anyone asks for it, and
// its time to Ready is counted.
func TestWatchRoutesOnReady(t *testing.T) {
	gs := gameServer("gs-a", agonesv1.GameServerStateScheduled)
	h := newHarness(t, allocated(gs), gs)
	informers := externalversions.NewSharedInformerFactoryWithOptions(h.agones, 0, externalversions.WithNamespace(ns))
	informer := informers.Agones().V1().GameServers().Informer()
	if err := h.m.Watch(informer); err != nil {
		t.Fatal(err)
	}
	informers.Start(t.Context().Done())
	t.Cleanup(informers.Shutdown)
	informers.WaitForCacheSync(t.Context().Done())

	if _, err := h.kube.CoreV1().Services(ns).Get(t.Context(), gs.Name, metav1.GetOptions{}); err == nil {
		t.Fatal("a Scheduled GameServer got a Service")
	}
	ready := gs.DeepCopy()
	ready.Status.State = agonesv1.GameServerStateReady
	if _, err := h.agones.AgonesV1().GameServers(ns).Update(t.Context(), ready, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := h.dynamic.Resource(IngressRoutes).Namespace(ns).Get(t.Context(), gs.Name, metav1.GetOptions{}); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no route for the Ready GameServer")
		}
		time.Sleep(10 * time.Millisecond)
	}
	assertRouted(t, h, gs)
	if n := testutil.CollectAndCount(h.metrics.startup); n != 1 {
		t.Fatalf("startup series = %d", n)
	}
	ch := make(chan prometheus.Metric, 1)
	h.metrics.startup.Collect(ch)
	var m dto.Metric
	if err := (<-ch).Write(&m); err != nil {
		t.Fatal(err)
	}
	if c, sum := m.GetHistogram().GetSampleCount(), m.GetHistogram().GetSampleSum(); c != 1 || sum < 3 {
		t.Errorf("startup = %d samples, %.1fs, want 1 of at least 3s", c, sum)
	}
}

// Once a GameServer is gone the manager says so. It deletes nothing itself:
// the owner references leave that to Kubernetes.
func TestInstanceGone(t *testing.T) {
	gs := gameServer("gs-a", agonesv1.GameServerStateAllocated, "r1")
	h := newHarness(t, allocated(gs), gs)
	if err := h.m.EnsureRoutes(t.Context(), gs); err != nil {
		t.Fatal(err)
	}
	if code, inst := getInstance(t, h, "gs-a"); code != http.StatusOK || inst.Boot != "boot-gs-a" || inst.State != "Allocated" || !slices.Equal(inst.Runs, []string{"r1"}) {
		t.Fatalf("instance = %d %+v", code, inst)
	}

	if err := h.agones.AgonesV1().GameServers(ns).Delete(t.Context(), gs.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if code, _ := getInstance(t, h, "gs-a"); code != http.StatusNotFound {
		t.Fatalf("gone instance = %d, want 404", code)
	}
	for _, a := range append(h.kube.Actions(), h.dynamic.Actions()...) {
		if a.GetVerb() == "delete" {
			t.Errorf("manager deleted %s", a.GetResource().Resource)
		}
	}
	if _, err := h.kube.CoreV1().Services(ns).Get(t.Context(), gs.Name, metav1.GetOptions{}); err != nil {
		t.Errorf("service = %v, want it kept for the garbage collector", err)
	}
}
