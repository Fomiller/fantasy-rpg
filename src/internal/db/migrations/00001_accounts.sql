-- +goose Up
CREATE TABLE accounts (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name        text NOT NULL UNIQUE,
    stash       jsonb NOT NULL DEFAULT '[]',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT stash_is_array CHECK (jsonb_typeof(stash) = 'array'),
    -- inventory.StashSize
    CONSTRAINT stash_size CHECK (jsonb_array_length(stash) <= 100)
);
