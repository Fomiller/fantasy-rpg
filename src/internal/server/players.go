package server

import (
	"encoding/json"
	"net/http"
)

// Players is a head count split by kind, as GET /v1/players answers it. A
// bot is a player whose JoinRequest set bot.
type Players struct {
	Humans int `json:"humans"`
	Bots   int `json:"bots"`
}

func count[C comparable](clients map[C]*client, in func(*client) bool) Players {
	var p Players
	for _, c := range clients {
		switch {
		case !in(c):
		case c.bot:
			p.Bots++
		default:
			p.Humans++
		}
	}
	return p
}

// townPlayers is everyone with a body in the town.
func (s *Lobby) townPlayers() Players {
	return count(s.inTown, func(*client) bool { return true })
}

// HandlePlayers answers the bot director. Like /status it is for the
// internal port only.
func (s *Lobby) HandlePlayers(w http.ResponseWriter, r *http.Request) {
	answer(s, w, r, s.townPlayers)
}

// HandlePlayers counts the same players as /status does.
func (s *Server) HandlePlayers(w http.ResponseWriter, r *http.Request) {
	answer(s.Lobby, w, r, s.cavePlayers)
}

func (s *Server) cavePlayers() Players {
	return count(s.conns, func(c *client) bool { return c.entering || c.id != 0 })
}

// answer runs read on the loop and writes what it returns as JSON.
func answer[T any](s *Lobby, w http.ResponseWriter, r *http.Request, read func() T) {
	got := make(chan T, 1)
	if !s.enqueue(func() { got <- read() }) {
		http.Error(w, "shutting down", http.StatusServiceUnavailable)
		return
	}
	select {
	case v := <-got:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
	case <-r.Context().Done():
	case <-s.done:
		http.Error(w, "shutting down", http.StatusServiceUnavailable)
	}
}
