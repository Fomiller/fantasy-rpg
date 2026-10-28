package manager

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	agonesv1 "agones.dev/agones/pkg/apis/agones/v1"
)

func inFleet(gs *agonesv1.GameServer, fleet string) *agonesv1.GameServer {
	gs.Labels = map[string]string{agonesv1.FleetNameLabel: fleet}
	return gs
}

func answering(t *testing.T, body string) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/players" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// Only Allocated servers in the Fleet are asked. One that does not answer is
// counted as unreachable, not as an error for the whole call.
func TestPlayersSumsAllocatedCaves(t *testing.T) {
	down := httptest.NewServer(http.NotFoundHandler())
	down.Close()
	urls := map[string]string{
		"gs-a": answering(t, `{"humans":2,"bots":1}`),
		"gs-b": answering(t, `{"humans":0,"bots":3}`),
		"gs-c": answering(t, `{"humans":9,"bots":9}`),
		"gs-d": down.URL,
		"gs-e": answering(t, `{"humans":9,"bots":9}`),
	}
	fleet := "fantasy-rpg-instance"
	h := newHarness(t, nil,
		inFleet(gameServer("gs-a", agonesv1.GameServerStateAllocated), fleet),
		inFleet(gameServer("gs-b", agonesv1.GameServerStateAllocated), fleet),
		inFleet(gameServer("gs-c", agonesv1.GameServerStateReady), fleet),
		inFleet(gameServer("gs-d", agonesv1.GameServerStateAllocated), fleet),
		inFleet(gameServer("gs-e", agonesv1.GameServerStateAllocated), "other"),
	)
	h.m.instanceURL = func(gs *agonesv1.GameServer) string { return urls[gs.Name] }

	mux := http.NewServeMux()
	h.m.Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/players", nil))
	var got Players
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || got != (Players{Humans: 2, Bots: 4, Unreachable: 1}) {
		t.Fatalf("players = %d %+v", rec.Code, got)
	}
}

func TestInternalURL(t *testing.T) {
	h := newHarness(t, nil)
	if got := h.m.instanceURL(gameServer("gs-a", agonesv1.GameServerStateAllocated)); got != "http://gs-a.fantasy-rpg.svc:8080" {
		t.Fatalf("url = %q", got)
	}
}
