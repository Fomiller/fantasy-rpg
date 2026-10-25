package httpstore

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
)

// maxBody fits a full stash and loadout several times over.
const maxBody = 1 << 20

// Register adds the inventory API for store to mux.
func Register(mux *http.ServeMux, store inventory.Store, log *slog.Logger) {
	h := &handler{store: store, log: log}
	mux.HandleFunc("GET /v1/characters/{name}/{class}", h.load)
	mux.HandleFunc("POST /v1/characters", h.create)
	mux.HandleFunc("POST /v1/lobby", h.saveLobby)
	mux.HandleFunc("POST /v1/runs/start", h.startRun)
	mux.HandleFunc("POST /v1/runs/end", h.endRun)
	mux.HandleFunc("POST /v1/runs/clear", h.clearRun)
}

type handler struct {
	store inventory.Store
	log   *slog.Logger
}

func (h *handler) load(w http.ResponseWriter, r *http.Request) {
	c, ok, err := h.store.Load(r.Context(), r.PathValue("name"), r.PathValue("class"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out, err := encodeCharacter(c)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	reply(w, http.StatusOK, loadReply{Found: ok, Character: out})
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	var in character
	if !decode(w, r, &in) {
		return
	}
	c, err := decodeCharacter(in)
	if err != nil {
		badRequest(w, err)
		return
	}
	h.done(w, r, h.store.Create(r.Context(), c))
}

func (h *handler) saveLobby(w http.ResponseWriter, r *http.Request) {
	var in lobbyRequest
	if !decode(w, r, &in) {
		return
	}
	c, err := decodeCharacter(character{Equipment: in.Equipment, Bag: in.Bag, Stash: in.Stash})
	if err != nil {
		badRequest(w, err)
		return
	}
	h.done(w, r, h.store.SaveLobby(r.Context(), in.Name, in.Class, c.Loadout, c.Stash))
}

func (h *handler) startRun(w http.ResponseWriter, r *http.Request) {
	var in startRequest
	if !decode(w, r, &in) {
		return
	}
	c, err := h.store.StartRun(r.Context(), in.Name, in.Class, in.RunID, in.Instance)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out, err := encodeCharacter(c)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	reply(w, http.StatusOK, out)
}

func (h *handler) endRun(w http.ResponseWriter, r *http.Request) {
	var in endRequest
	if !decode(w, r, &in) {
		return
	}
	c, err := decodeCharacter(in.Character)
	if err != nil {
		badRequest(w, err)
		return
	}
	h.done(w, r, h.store.EndRun(r.Context(), in.RunID, c))
}

func (h *handler) clearRun(w http.ResponseWriter, r *http.Request) {
	var in clearRequest
	if !decode(w, r, &in) {
		return
	}
	h.done(w, r, h.store.ClearRun(r.Context(), in.Name, in.RunID))
}

func (h *handler) done(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	status, body := bodyFor(err)
	if status == http.StatusInternalServerError {
		h.log.Error("store call", "path", r.URL.Path, "err", err)
	}
	reply(w, status, body)
}

func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(out); err != nil {
		badRequest(w, err)
		return false
	}
	return true
}

func badRequest(w http.ResponseWriter, err error) {
	reply(w, http.StatusBadRequest, errorBody{Error: "bad_request", Message: err.Error()})
}

func reply(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}
