-- +goose Up
-- The lease is on the account, not the character, since the stash is per
-- account and a run end writes it. All three are set together or not at all.
ALTER TABLE accounts
    ADD COLUMN active_run_id   uuid,
    ADD COLUMN active_instance text,
    ADD COLUMN active_since    timestamptz,
    ADD CONSTRAINT run_lease_whole CHECK (
        (active_run_id IS NULL) = (active_instance IS NULL)
        AND (active_run_id IS NULL) = (active_since IS NULL)
    );
