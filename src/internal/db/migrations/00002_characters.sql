-- +goose Up
CREATE TABLE characters (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id   bigint NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    class_id     text NOT NULL,
    xp           integer NOT NULL DEFAULT 0,
    equipment    jsonb NOT NULL DEFAULT '[]',
    bag          jsonb NOT NULL DEFAULT '[]',
    last_run_id  uuid,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (account_id, class_id),
    CONSTRAINT xp_not_negative CHECK (xp >= 0),
    CONSTRAINT equipment_is_array CHECK (jsonb_typeof(equipment) = 'array'),
    CONSTRAINT bag_is_array CHECK (jsonb_typeof(bag) = 'array'),
    -- sim.Config.InventorySize
    CONSTRAINT bag_size CHECK (jsonb_array_length(bag) <= 20)
);
