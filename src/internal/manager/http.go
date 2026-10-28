package manager

import (
	"encoding/json"
	"errors"
	"net/http"
)

// AllocateRequest is one party: a run id per member.
type AllocateRequest struct {
	Runs []string `json:"runs"`
}

type errorBody struct {
	Error string `json:"error"`
}

// Register adds the manager's API to mux.
func (m *Manager) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/allocate", m.handleAllocate)
	mux.HandleFunc("GET /v1/instances/{id}", m.handleInstance)
	mux.HandleFunc("GET /v1/players", m.handlePlayers)
}

func (m *Manager) handlePlayers(w http.ResponseWriter, r *http.Request) {
	p, err := m.Players(r.Context())
	if err != nil {
		m.log.Error("players", "err", err)
		reply(w, http.StatusServiceUnavailable, errorBody{"unavailable"})
		return
	}
	reply(w, http.StatusOK, p)
}

func (m *Manager) handleAllocate(w http.ResponseWriter, r *http.Request) {
	var req AllocateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil || len(req.Runs) == 0 {
		reply(w, http.StatusBadRequest, errorBody{"want runs"})
		return
	}
	inst, err := m.Allocate(r.Context(), req.Runs)
	switch {
	case errors.Is(err, ErrFull):
		reply(w, http.StatusConflict, errorBody{"full"})
	case err != nil:
		m.log.Error("allocate", "runs", req.Runs, "err", err)
		reply(w, http.StatusServiceUnavailable, errorBody{"unavailable"})
	default:
		m.log.Info("allocated", "instance", inst.ID, "runs", req.Runs)
		reply(w, http.StatusOK, inst)
	}
}

func (m *Manager) handleInstance(w http.ResponseWriter, r *http.Request) {
	inst, ok, err := m.Instance(r.Context(), r.PathValue("id"))
	switch {
	case err != nil:
		m.log.Error("get instance", "instance", r.PathValue("id"), "err", err)
		reply(w, http.StatusServiceUnavailable, errorBody{"unavailable"})
	case !ok:
		reply(w, http.StatusNotFound, errorBody{"gone"})
	default:
		reply(w, http.StatusOK, inst)
	}
}

func reply(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}
