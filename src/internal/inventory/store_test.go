package inventory_test

import (
	"testing"

	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory/storetest"
)

func TestMemoryStore(t *testing.T) {
	storetest.Run(t, func() inventory.Store { return inventory.NewMemory() })
}
