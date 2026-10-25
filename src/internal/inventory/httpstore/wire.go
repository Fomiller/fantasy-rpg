// Package httpstore puts an inventory.Store behind the inventory service's
// HTTP API, and is the client world and instance servers use for it.
package httpstore

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
)

// character is inventory.Character on the wire. Items use the saved shape
// from items.EncodeJSON.
type character struct {
	Name      string          `json:"name"`
	Class     string          `json:"class"`
	XP        int             `json:"xp"`
	Equipment json.RawMessage `json:"equipment"`
	Bag       json.RawMessage `json:"bag"`
	Stash     json.RawMessage `json:"stash"`
	Run       *lease          `json:"run,omitempty"`
}

type lease struct {
	RunID    string    `json:"run_id"`
	Instance string    `json:"instance"`
	Since    time.Time `json:"since"`
}

type loadReply struct {
	Found     bool      `json:"found"`
	Character character `json:"character"`
}

type lobbyRequest struct {
	Name      string          `json:"name"`
	Class     string          `json:"class"`
	Equipment json.RawMessage `json:"equipment"`
	Bag       json.RawMessage `json:"bag"`
	Stash     json.RawMessage `json:"stash"`
}

type startRequest struct {
	Name     string `json:"name"`
	Class    string `json:"class"`
	RunID    string `json:"run_id"`
	Instance string `json:"instance"`
}

type endRequest struct {
	RunID     string    `json:"run_id"`
	Character character `json:"character"`
}

type clearRequest struct {
	Name  string `json:"name"`
	RunID string `json:"run_id"`
}

type errorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func encodeCharacter(c inventory.Character) (character, error) {
	out := character{Name: c.Name, Class: c.Class, XP: c.XP}
	var err error
	if out.Equipment, err = items.EncodeJSON(c.Loadout.Equipment); err != nil {
		return out, err
	}
	if out.Bag, err = items.EncodeJSON(c.Loadout.Items); err != nil {
		return out, err
	}
	if out.Stash, err = items.EncodeJSON(c.Stash); err != nil {
		return out, err
	}
	if c.Run.Held() {
		out.Run = &lease{RunID: c.Run.RunID, Instance: c.Run.Instance, Since: c.Run.Since}
	}
	return out, nil
}

func decodeCharacter(in character) (inventory.Character, error) {
	c := inventory.Character{Name: in.Name, Class: in.Class, XP: in.XP}
	var err error
	if c.Loadout.Equipment, err = decodeList(in.Equipment); err != nil {
		return c, err
	}
	if c.Loadout.Items, err = decodeList(in.Bag); err != nil {
		return c, err
	}
	if c.Stash, err = decodeList(in.Stash); err != nil {
		return c, err
	}
	if in.Run != nil {
		c.Run = inventory.Lease{RunID: in.Run.RunID, Instance: in.Run.Instance, Since: in.Run.Since}
	}
	return c, nil
}

// decodeList takes a missing list as empty.
func decodeList(b json.RawMessage) ([]items.Instance, error) {
	if len(b) == 0 {
		return nil, nil
	}
	return items.DecodeJSON(b)
}

// codes is how the store's errors cross the wire, so errors.Is still works
// on the client.
var codes = []struct {
	err    error
	code   string
	status int
}{
	{inventory.ErrNoCharacter, "no_character", http.StatusNotFound},
	{inventory.ErrNoRunID, "no_run_id", http.StatusBadRequest},
	{inventory.ErrInRun, "in_run", http.StatusConflict},
	{inventory.ErrRunUsed, "run_used", http.StatusConflict},
	{inventory.ErrLeaseLost, "lease_lost", http.StatusConflict},
}

// remoteError is a store error from the service. It keeps the message and
// unwraps to the store's sentinel, if it had one.
type remoteError struct {
	sentinel error
	msg      string
}

func (e *remoteError) Error() string { return e.msg }
func (e *remoteError) Unwrap() error { return e.sentinel }

func errorFor(body errorBody, status int) error {
	for _, c := range codes {
		if c.code == body.Error {
			return &remoteError{sentinel: c.err, msg: body.Message}
		}
	}
	if body.Message == "" {
		body.Message = http.StatusText(status)
	}
	return &remoteError{msg: "inventory: " + body.Message}
}

func bodyFor(err error) (int, errorBody) {
	for _, c := range codes {
		if errors.Is(err, c.err) {
			return c.status, errorBody{Error: c.code, Message: err.Error()}
		}
	}
	return http.StatusInternalServerError, errorBody{Error: "store", Message: err.Error()}
}
