-- name: ListProducts :many
-- One page in id order, after the cursor's id. The caller asks for one row more than the page size to
-- learn whether another page follows.
SELECT id, slug, name, price_amount, price_currency
FROM products
WHERE id > sqlc.arg(after_id)
ORDER BY id
LIMIT sqlc.arg(row_limit);

-- name: UpdateProduct :execrows
-- Updates a product by slug only when something changed, so reseeding writes nothing.
UPDATE products
SET name = sqlc.arg(name), price_amount = sqlc.arg(price_amount), price_currency = sqlc.arg(price_currency)
WHERE slug = sqlc.arg(slug)
  AND (name, price_amount, price_currency)
      IS DISTINCT FROM (sqlc.arg(name)::text, sqlc.arg(price_amount)::bigint, sqlc.arg(price_currency)::char(3));

-- name: InsertProductIfMissing :exec
-- Inserts a product only when its slug is new. INSERT … SELECT draws an id only for a row it actually
-- inserts, unlike ON CONFLICT, which uses one up on every attempt.
INSERT INTO products (slug, name, price_amount, price_currency)
SELECT sqlc.arg(slug)::text, sqlc.arg(name)::text, sqlc.arg(price_amount)::bigint, sqlc.arg(price_currency)::char(3)
WHERE NOT EXISTS (SELECT 1 FROM products WHERE slug = sqlc.arg(slug)::text)
ON CONFLICT (slug) DO NOTHING;
