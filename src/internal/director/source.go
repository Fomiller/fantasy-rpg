package director

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Players is a head count split by kind, as GET /v1/players answers it on
// the world server, the instance manager and an instance server.
type Players struct {
	Humans int `json:"humans"`
	Bots   int `json:"bots"`
}

// Source counts the players in the game.
type Source interface {
	Players(ctx context.Context) (Players, error)
}

// Endpoints adds up GET /v1/players from each base URL: every world server
// and the instance manager in the cluster. Any one failing fails the count,
// since a short count would add bots for humans who are there.
//
// Optional URLs are shards that may not be running. One that fails counts as
// empty.
type Endpoints struct {
	URLs     []string
	Optional []string
	Client   *http.Client
}

func NewEndpoints(urls []string) *Endpoints {
	return &Endpoints{URLs: urls, Client: &http.Client{Timeout: 3 * time.Second}}
}

func (e *Endpoints) Players(ctx context.Context) (Players, error) {
	var sum Players
	for _, u := range e.URLs {
		p, err := e.get(ctx, u+"/v1/players")
		if err != nil {
			return Players{}, err
		}
		sum.Humans += p.Humans
		sum.Bots += p.Bots
	}
	for _, u := range e.Optional {
		if p, err := e.get(ctx, u+"/v1/players"); err == nil {
			sum.Humans += p.Humans
			sum.Bots += p.Bots
		}
	}
	return sum, nil
}

func (e *Endpoints) get(ctx context.Context, url string) (Players, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Players{}, err
	}
	resp, err := e.Client.Do(req)
	if err != nil {
		return Players{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Players{}, fmt.Errorf("%s: %s", url, resp.Status)
	}
	var p Players
	return p, json.NewDecoder(resp.Body).Decode(&p)
}
