package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/types/known/timestamppb"

	eventsv1 "github.com/Fomiller/fantasy-rpg/gen/go/events/v1"
	"github.com/Fomiller/fantasy-rpg/src/internal/events"
	"github.com/Fomiller/fantasy-rpg/src/internal/inventory"
	"github.com/Fomiller/fantasy-rpg/src/internal/items"
)

// Store is the Postgres inventory.Store.
type Store struct {
	pool *pgxpool.Pool
}

var _ inventory.Store = (*Store)(nil)

// NewStore does not connect until first use.
func NewStore(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

func (s *Store) Load(ctx context.Context, name, class string) (inventory.Character, bool, error) {
	c := inventory.Character{Name: name, Class: class}
	var stash, equipment, bag []byte
	var xp *int
	var lease nullLease
	err := s.pool.QueryRow(ctx, `
		SELECT a.stash, a.active_run_id::text, a.active_instance, a.active_since, c.xp, c.equipment, c.bag
		FROM accounts a
		LEFT JOIN characters c ON c.account_id = a.id AND c.class_id = $2
		WHERE a.name = $1`, name, class).Scan(&stash, &lease.runID, &lease.instance, &lease.since, &xp, &equipment, &bag)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, false, nil
	}
	if err != nil {
		return c, false, err
	}
	c.Run = lease.lease()
	if c.Stash, err = items.DecodeJSON(stash); err != nil {
		return c, false, fmt.Errorf("stash of %s: %w", name, err)
	}
	if xp == nil {
		return c, false, nil
	}
	c.XP = *xp
	if err := decodeLoadout(&c, equipment, bag); err != nil {
		return c, false, err
	}
	return c, true, nil
}

func (s *Store) Create(ctx context.Context, c inventory.Character) error {
	if err := checkStash(c.Stash); err != nil {
		return err
	}
	stash, equipment, bag, err := encodeAll(c)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO accounts (name, stash) VALUES ($1, $2)
			ON CONFLICT (name) DO NOTHING`, c.Name, stash); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO characters (account_id, class_id, xp, equipment, bag)
			SELECT id, $2, $3, $4, $5 FROM accounts WHERE name = $1
			ON CONFLICT (account_id, class_id) DO NOTHING`, c.Name, c.Class, c.XP, equipment, bag)
		return err
	})
}

func (s *Store) SaveLobby(ctx context.Context, name, class string, l inventory.Loadout, stash []items.Instance) error {
	if err := checkStash(stash); err != nil {
		return err
	}
	stashJSON, equipment, bag, err := encodeAll(inventory.Character{Loadout: l, Stash: stash})
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		account, lease, err := lockAccount(ctx, tx, name, class)
		if err != nil {
			return err
		}
		if lease.runID != nil {
			return inventory.ErrInRun
		}
		tag, err := tx.Exec(ctx, `
			UPDATE characters SET equipment = $3, bag = $4, updated_at = now()
			WHERE account_id = $1 AND class_id = $2`, account, class, equipment, bag)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("%w: %s %s", inventory.ErrNoCharacter, name, class)
		}
		_, err = saveStash(ctx, tx, account, stashJSON, false)
		return err
	})
}

func (s *Store) StartRun(ctx context.Context, name, class, runID, instance string) (inventory.Character, error) {
	c := inventory.Character{Name: name, Class: class}
	if runID == "" {
		return c, inventory.ErrNoRunID
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		account, lease, err := lockAccount(ctx, tx, name, class)
		if err != nil {
			return err
		}
		if lease.runID != nil {
			return inventory.ErrInRun
		}
		var stash, equipment, bag []byte
		var lastRun *string
		err = tx.QueryRow(ctx, `
			SELECT a.stash, c.xp, c.equipment, c.bag, c.last_run_id::text
			FROM characters c JOIN accounts a ON a.id = c.account_id
			WHERE a.id = $1 AND c.class_id = $2`, account, class).Scan(&stash, &c.XP, &equipment, &bag, &lastRun)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %s %s", inventory.ErrNoCharacter, name, class)
		}
		if err != nil {
			return err
		}
		if lastRun != nil && *lastRun == runID {
			return inventory.ErrRunUsed
		}
		if c.Stash, err = items.DecodeJSON(stash); err != nil {
			return fmt.Errorf("stash of %s: %w", name, err)
		}
		if err := decodeLoadout(&c, equipment, bag); err != nil {
			return err
		}
		c.Run = inventory.Lease{RunID: runID, Instance: instance}
		return tx.QueryRow(ctx, `
			UPDATE accounts SET active_run_id = $2, active_instance = $3, active_since = now()
			WHERE id = $1
			RETURNING active_since`, account, runID, instance).Scan(&c.Run.Since)
	})
	if err != nil {
		return inventory.Character{Name: name, Class: class}, err
	}
	return c, nil
}

func (s *Store) EndRun(ctx context.Context, runID string, c inventory.Character) error {
	if runID == "" {
		return inventory.ErrNoRunID
	}
	if err := checkStash(c.Stash); err != nil {
		return err
	}
	stash, equipment, bag, err := encodeAll(c)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		account, lease, err := lockAccount(ctx, tx, c.Name, c.Class)
		if err != nil {
			return err
		}
		var lastRun *string
		err = tx.QueryRow(ctx, `
			SELECT last_run_id::text FROM characters
			WHERE account_id = $1 AND class_id = $2
			FOR UPDATE`, account, c.Class).Scan(&lastRun)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %s %s", inventory.ErrNoCharacter, c.Name, c.Class)
		}
		if err != nil {
			return err
		}
		if lastRun != nil && *lastRun == runID {
			return nil
		}
		if lease.runID == nil || *lease.runID != runID {
			return inventory.ErrLeaseLost
		}
		if _, err := tx.Exec(ctx, `
			UPDATE characters SET xp = $3, equipment = $4, bag = $5, last_run_id = $6, updated_at = now()
			WHERE account_id = $1 AND class_id = $2`, account, c.Class, c.XP, equipment, bag, runID); err != nil {
			return err
		}
		version, err := saveStash(ctx, tx, account, stash, true)
		if err != nil {
			return err
		}
		return writeOutbox(ctx, tx, events.TopicInventoryApplied, c.Name, &eventsv1.InventoryApplied{
			Version:      events.Version,
			Account:      c.Name,
			RunId:        runID,
			StashVersion: version,
			Time:         timestamppb.Now(),
		})
	})
}

func (s *Store) ClearRun(ctx context.Context, name, runID string) error {
	if runID == "" {
		return nil
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE accounts SET active_run_id = NULL, active_instance = NULL, active_since = NULL, updated_at = now()
		WHERE name = $1 AND active_run_id = $2`, name, runID)
	return err
}

// lockAccount locks the account row before anything else, so every write
// takes its locks in the same order and sees the lease it checks.
func lockAccount(ctx context.Context, tx pgx.Tx, name, class string) (int64, nullLease, error) {
	var account int64
	var lease nullLease
	err := tx.QueryRow(ctx, `
		SELECT id, active_run_id::text FROM accounts WHERE name = $1
		FOR UPDATE`, name).Scan(&account, &lease.runID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, lease, fmt.Errorf("%w: %s %s", inventory.ErrNoCharacter, name, class)
	}
	return account, lease, err
}

// saveStash also drops the lease when the write is a run end. It returns the
// new stash version.
func saveStash(ctx context.Context, tx pgx.Tx, account int64, stash []byte, endRun bool) (int64, error) {
	var version int64
	err := tx.QueryRow(ctx, `
		UPDATE accounts SET stash = $2, stash_version = stash_version + 1, updated_at = now(),
			active_run_id = CASE WHEN $3 THEN NULL ELSE active_run_id END,
			active_instance = CASE WHEN $3 THEN NULL ELSE active_instance END,
			active_since = CASE WHEN $3 THEN NULL ELSE active_since END
		WHERE id = $1
		RETURNING stash_version`, account, stash, endRun).Scan(&version)
	return version, err
}

type nullLease struct {
	runID, instance *string
	since           *time.Time
}

func (n nullLease) lease() inventory.Lease {
	if n.runID == nil {
		return inventory.Lease{}
	}
	l := inventory.Lease{RunID: *n.runID}
	if n.instance != nil {
		l.Instance = *n.instance
	}
	if n.since != nil {
		l.Since = *n.since
	}
	return l
}

func decodeLoadout(c *inventory.Character, equipment, bag []byte) error {
	var err error
	if c.Loadout.Equipment, err = items.DecodeJSON(equipment); err != nil {
		return fmt.Errorf("equipment of %s %s: %w", c.Name, c.Class, err)
	}
	if c.Loadout.Items, err = items.DecodeJSON(bag); err != nil {
		return fmt.Errorf("bag of %s %s: %w", c.Name, c.Class, err)
	}
	return nil
}

// checkStash fails before the database does, with a clearer error than the
// stash_size constraint.
func checkStash(stash []items.Instance) error {
	if len(stash) > inventory.StashSize {
		return fmt.Errorf("stash has %d entries, the limit is %d", len(stash), inventory.StashSize)
	}
	return nil
}

func encodeAll(c inventory.Character) (stash, equipment, bag []byte, err error) {
	if stash, err = items.EncodeJSON(c.Stash); err != nil {
		return
	}
	if equipment, err = items.EncodeJSON(c.Loadout.Equipment); err != nil {
		return
	}
	bag, err = items.EncodeJSON(c.Loadout.Items)
	return
}
