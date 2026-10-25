package httpstore

import (
	"context"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/Fomiller/fantasy-rpg/src/internal/db"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
)

// Open is the store world and instance servers use: the inventory service
// when inventoryURL is set, else db.OpenStore, so local dev runs without the
// service.
func Open(ctx context.Context, inventoryURL, databaseURL, store string, reg prometheus.Registerer) (inventory.Store, string, func(), error) {
	if inventoryURL == "" {
		if databaseURL == "" && store != db.Memory {
			return nil, "", nil, fmt.Errorf("%w: set INVENTORY_URL or DATABASE_URL, or STORE=memory for local dev", db.ErrNoStore)
		}
		return db.OpenStore(ctx, databaseURL, store, reg)
	}
	return inventory.Metered(NewClient(inventoryURL, nil), reg), "inventory", func() {}, nil
}
