-- +goose Up
CREATE TABLE products (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    slug           text        NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    name           text        NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    -- Money is an integer count of the currency's minor units (cents for USD), never a float.
    price_amount   bigint      NOT NULL CHECK (price_amount >= 0),
    price_currency char(3)     NOT NULL CHECK (price_currency ~ '^[A-Z]{3}$'),
    created_at     timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE products;
