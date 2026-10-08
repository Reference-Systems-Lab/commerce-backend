-- name: ListProducts :many
-- One page in id order, after the cursor's id. The caller asks for one row more than the page size to
-- learn whether another page follows.
SELECT id, slug, name, price_amount, price_currency
FROM products
WHERE id > sqlc.arg(after_id)
ORDER BY id
LIMIT sqlc.arg(row_limit);

-- name: UpsertProduct :exec
-- Inserts a product, or updates it by slug only when something changed, so reseeding is a no-op.
INSERT INTO products (slug, name, price_amount, price_currency)
VALUES (sqlc.arg(slug), sqlc.arg(name), sqlc.arg(price_amount), sqlc.arg(price_currency))
ON CONFLICT (slug) DO UPDATE
SET name = EXCLUDED.name, price_amount = EXCLUDED.price_amount, price_currency = EXCLUDED.price_currency
WHERE (products.name, products.price_amount, products.price_currency)
    IS DISTINCT FROM (EXCLUDED.name, EXCLUDED.price_amount, EXCLUDED.price_currency);
