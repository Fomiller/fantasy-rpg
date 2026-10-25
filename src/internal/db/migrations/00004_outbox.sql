-- +goose Up
-- Events that describe a write go in the same transaction as the write. The
-- inventory relay produces rows in id order and deletes each once acked.
CREATE TABLE outbox (
    id         bigserial PRIMARY KEY,
    topic      text NOT NULL,
    key        text NOT NULL,
    payload    bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
