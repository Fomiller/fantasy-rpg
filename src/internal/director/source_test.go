package director

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func playersServer(body string, status int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
}

func TestEndpointsOptionalShardsMayBeDown(t *testing.T) {
	up := playersServer(`{"humans":2,"bots":1}`, http.StatusOK)
	defer up.Close()
	extra := playersServer(`{"humans":3,"bots":0}`, http.StatusOK)
	defer extra.Close()
	down := playersServer("", http.StatusServiceUnavailable)
	defer down.Close()

	e := NewEndpoints([]string{up.URL})
	e.Optional = []string{extra.URL, down.URL, "http://127.0.0.1:1"}
	got, err := e.Players(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := (Players{Humans: 5, Bots: 1}); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestEndpointsRequiredShardFailsTheCount(t *testing.T) {
	down := playersServer("", http.StatusServiceUnavailable)
	defer down.Close()
	if _, err := NewEndpoints([]string{down.URL}).Players(context.Background()); err == nil {
		t.Fatal("want an error when a required URL is down")
	}
}
