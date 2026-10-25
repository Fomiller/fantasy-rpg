-- +goose Up
-- Goes up by one on every stash write. inventory.applied carries it.
ALTER TABLE accounts ADD COLUMN stash_version bigint NOT NULL DEFAULT 0;
