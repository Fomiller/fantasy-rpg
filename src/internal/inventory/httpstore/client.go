package httpstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
)

// clientTimeout backs up the caller's context, which is shorter.
const clientTimeout = 10 * time.Second

// Client is the inventory.Store at the inventory service.
type Client struct {
	base   string
	client *http.Client
}

var _ inventory.Store = (*Client)(nil)

// NewClient uses hc, or a client with a timeout when hc is nil.
func NewClient(base string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: clientTimeout}
	}
	return &Client{base: strings.TrimSuffix(base, "/"), client: hc}
}

func (c *Client) Load(ctx context.Context, name, class string) (inventory.Character, bool, error) {
	var out loadReply
	path := "/v1/characters/" + url.PathEscape(name) + "/" + url.PathEscape(class)
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return inventory.Character{Name: name, Class: class}, false, err
	}
	ch, err := decodeCharacter(out.Character)
	if err != nil {
		return inventory.Character{Name: name, Class: class}, false, err
	}
	return ch, out.Found, nil
}

func (c *Client) Create(ctx context.Context, ch inventory.Character) error {
	in, err := encodeCharacter(ch)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, "/v1/characters", in, nil)
}

func (c *Client) SaveLobby(ctx context.Context, name, class string, l inventory.Loadout, stash []items.Instance) error {
	enc, err := encodeCharacter(inventory.Character{Loadout: l, Stash: stash})
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, "/v1/lobby", lobbyRequest{Name: name, Class: class, Equipment: enc.Equipment, Bag: enc.Bag, Stash: enc.Stash}, nil)
}

func (c *Client) StartRun(ctx context.Context, name, class, runID, instance string) (inventory.Character, error) {
	var out character
	if err := c.do(ctx, http.MethodPost, "/v1/runs/start", startRequest{Name: name, Class: class, RunID: runID, Instance: instance}, &out); err != nil {
		return inventory.Character{Name: name, Class: class}, err
	}
	return decodeCharacter(out)
}

func (c *Client) EndRun(ctx context.Context, runID string, ch inventory.Character) error {
	in, err := encodeCharacter(ch)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, "/v1/runs/end", endRequest{RunID: runID, Character: in}, nil)
}

func (c *Client) ClearRun(ctx context.Context, name, runID string) error {
	return c.do(ctx, http.MethodPost, "/v1/runs/clear", clearRequest{Name: name, RunID: runID}, nil)
}

// do sends in as JSON, if it is set, and decodes a 200 body into out. Any
// other status is the store's error.
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK && out != nil:
		return json.NewDecoder(resp.Body).Decode(out)
	case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent:
		return nil
	}
	var e errorBody
	if err := json.NewDecoder(resp.Body).Decode(&e); err != nil {
		return fmt.Errorf("inventory: %s %s: status %d", method, path, resp.StatusCode)
	}
	return errorFor(e, resp.StatusCode)
}
