package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

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
	err := s.pool.QueryRow(ctx, `
		SELECT a.stash, c.xp, c.equipment, c.bag
		FROM accounts a
		LEFT JOIN characters c ON c.account_id = a.id AND c.class_id = $2
		WHERE a.name = $1`, name, class).Scan(&stash, &xp, &equipment, &bag)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, false, nil
	}
	if err != nil {
		return c, false, err
	}
	if c.Stash, err = decodeItems(stash); err != nil {
		return c, false, fmt.Errorf("stash of %s: %w", name, err)
	}
	if xp == nil {
		return c, false, nil
	}
	c.XP = *xp
	if c.Loadout.Equipment, err = decodeItems(equipment); err != nil {
		return c, false, fmt.Errorf("equipment of %s %s: %w", name, class, err)
	}
	if c.Loadout.Items, err = decodeItems(bag); err != nil {
		return c, false, fmt.Errorf("bag of %s %s: %w", name, class, err)
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
		var account int64
		err := tx.QueryRow(ctx, `
			UPDATE characters c SET equipment = $3, bag = $4, updated_at = now()
			FROM accounts a
			WHERE a.id = c.account_id AND a.name = $1 AND c.class_id = $2
			RETURNING a.id`, name, class, equipment, bag).Scan(&account)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %s %s", inventory.ErrNoCharacter, name, class)
		}
		if err != nil {
			return err
		}
		return saveStash(ctx, tx, account, stashJSON)
	})
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
		var account int64
		var lastRun *string
		err := tx.QueryRow(ctx, `
			SELECT a.id, c.last_run_id::text
			FROM characters c JOIN accounts a ON a.id = c.account_id
			WHERE a.name = $1 AND c.class_id = $2
			FOR UPDATE OF c`, c.Name, c.Class).Scan(&account, &lastRun)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %s %s", inventory.ErrNoCharacter, c.Name, c.Class)
		}
		if err != nil {
			return err
		}
		if lastRun != nil && *lastRun == runID {
			return nil
		}
		if _, err := tx.Exec(ctx, `
			UPDATE characters SET xp = $3, equipment = $4, bag = $5, last_run_id = $6, updated_at = now()
			WHERE account_id = $1 AND class_id = $2`, account, c.Class, c.XP, equipment, bag, runID); err != nil {
			return err
		}
		return saveStash(ctx, tx, account, stash)
	})
}

func saveStash(ctx context.Context, tx pgx.Tx, account int64, stash []byte) error {
	_, err := tx.Exec(ctx, `UPDATE accounts SET stash = $2, updated_at = now() WHERE id = $1`, account, stash)
	return err
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
	if stash, err = encodeItems(c.Stash); err != nil {
		return
	}
	if equipment, err = encodeItems(c.Loadout.Equipment); err != nil {
		return
	}
	bag, err = encodeItems(c.Loadout.Items)
	return
}
