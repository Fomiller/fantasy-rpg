// Package director is the bot director. It decides how many bots run, sets
// the bot worker Deployment's replicas, and splits the bots across the
// worker pods by slot.
package director

import "fmt"

type Mode string

const (
	// Off runs no bots.
	Off Mode = "off"
	// Fill keeps humans plus bots at Count, adding bots when few humans play.
	Fill Mode = "fill"
	// Fixed runs exactly Count bots, for load tests.
	Fixed Mode = "fixed"
)

// Target is what the director is asked to do. It is what GET and PUT
// /v1/target carry, and what the store keeps.
type Target struct {
	Mode  Mode `json:"mode"`
	Count int  `json:"count"`
}

func (t Target) Validate() error {
	switch t.Mode {
	case Off, Fill, Fixed:
	default:
		return fmt.Errorf("mode %q is not off, fill or fixed", t.Mode)
	}
	if t.Count < 0 {
		return fmt.Errorf("count %d is negative", t.Count)
	}
	return nil
}
