package httpstore

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/Fomiller/fantasy-rpg/src/internal/db"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
)

// Open is the store world and instance servers use: the inventory service
// when inventoryURL is set, else db.OpenStore on databaseURL, so local dev
// runs without the service.
func Open(ctx context.Context, inventoryURL, databaseURL string, reg prometheus.Registerer) (store inventory.Store, name string, closeFn func(), err error) {
	if inventoryURL == "" {
		return db.OpenStore(ctx, databaseURL, reg)
	}
	return inventory.Metered(NewClient(inventoryURL, nil), reg), "inventory", func() {}, nil
}
