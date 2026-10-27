-- +goose Up
-- One live session per name across every world shard. Keyed by name, not
-- account id, since a first join claims the name before its account exists.
CREATE TABLE sessions (
    name       text PRIMARY KEY,
    session    text NOT NULL,
    shard      text NOT NULL,
    boot       text NOT NULL,
    claimed_at timestamptz NOT NULL DEFAULT now()
);

-- A claim is live while its shard's row has the same boot and has not
-- expired. One row per world shard, written on its heartbeat.
CREATE TABLE shards (
    id         text PRIMARY KEY,
    boot       text NOT NULL,
    seen_at    timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);
