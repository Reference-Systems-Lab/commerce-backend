package catalog_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Reference-Systems-Lab/commerce-backend/internal/catalog"
	"github.com/Reference-Systems-Lab/commerce-backend/internal/catalog/store"
	"github.com/Reference-Systems-Lab/commerce-backend/internal/platform/db"
	"github.com/Reference-Systems-Lab/commerce-backend/internal/platform/dbtest"
	"github.com/Reference-Systems-Lab/commerce-backend/internal/platform/httpserver"
	"github.com/Reference-Systems-Lab/commerce-backend/internal/platform/migrate"
)

// One database for the package: migrated and seeded once, then shared by read-only tests.
func setup(t *testing.T) (*pgxpool.Pool, http.Handler) {
	t.Helper()
	ctx := context.Background()
	pg := dbtest.Start(t)
	pool, err := db.Open(ctx, pg.Config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := migrate.Up(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Seed(ctx, store.New(pool)); err != nil {
		t.Fatal(err)
	}
	api, h := httpserver.NewAPI()
	catalog.Register(api, store.New(pool))
	return pool, h
}

func rowsDigest(t *testing.T, pool *pgxpool.Pool) (count int, digest string) {
	t.Helper()
	err := pool.QueryRow(context.Background(), `
		SELECT count(*), coalesce(md5(string_agg(
			id || '|' || slug || '|' || name || '|' || price_amount || '|' || price_currency || '|' || created_at,
			',' ORDER BY id)), '')
		FROM products`).Scan(&count, &digest)
	if err != nil {
		t.Fatal(err)
	}
	return count, digest
}

func get(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func page(t *testing.T, rec *httptest.ResponseRecorder) catalog.ProductPage {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var p catalog.ProductPage
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCatalog(t *testing.T) {
	pool, h := setup(t)
	ctx := context.Background()

	t.Run("a second migrate applies nothing", func(t *testing.T) {
		n, err := migrate.Up(ctx, pool)
		if err != nil || n != 0 {
			t.Fatalf("applied %d, err %v; want 0, nil", n, err)
		}
	})

	t.Run("reseeding changes nothing", func(t *testing.T) {
		count, before := rowsDigest(t, pool)
		if count != 6 {
			t.Fatalf("seeded %d products, want 6", count)
		}
		if err := catalog.Seed(ctx, store.New(pool)); err != nil {
			t.Fatal(err)
		}
		if c, after := rowsDigest(t, pool); c != count || after != before {
			t.Fatalf("reseed changed the rows: %d %s -> %d %s", count, before, c, after)
		}
	})

	t.Run("reseeding uses up no ids, and restores a changed product", func(t *testing.T) {
		lastValue := func() (v int64) {
			if err := pool.QueryRow(ctx, `SELECT last_value FROM products_id_seq`).Scan(&v); err != nil {
				t.Fatal(err)
			}
			return v
		}
		before := lastValue()
		if _, err := pool.Exec(ctx, `UPDATE products SET price_amount = 1 WHERE slug = 'ceramic-mug'`); err != nil {
			t.Fatal(err)
		}
		if err := catalog.Seed(ctx, store.New(pool)); err != nil {
			t.Fatal(err)
		}
		if after := lastValue(); after != before {
			t.Fatalf("the id sequence moved from %d to %d", before, after)
		}
		var amount int64
		if err := pool.QueryRow(ctx, `SELECT price_amount FROM products WHERE slug = 'ceramic-mug'`).Scan(&amount); err != nil || amount != 1800 {
			t.Fatalf("ceramic-mug price = %d, %v; want the seeded 1800", amount, err)
		}
	})

	t.Run("constraints reject bad rows", func(t *testing.T) {
		for name, q := range map[string]string{
			"negative price":     `INSERT INTO products (slug, name, price_amount, price_currency) VALUES ('x-neg', 'X', -1, 'USD')`,
			"lowercase currency": `INSERT INTO products (slug, name, price_amount, price_currency) VALUES ('x-cur', 'X', 1, 'usd')`,
			"duplicate slug":     `INSERT INTO products (slug, name, price_amount, price_currency) VALUES ('ceramic-mug', 'X', 1, 'USD')`,
			"unsafe slug":        `INSERT INTO products (slug, name, price_amount, price_currency) VALUES ('Bad Slug', 'X', 1, 'USD')`,
		} {
			if _, err := pool.Exec(ctx, q); err == nil {
				t.Errorf("%s: insert succeeded", name)
			}
		}
	})

	t.Run("paging returns every product once", func(t *testing.T) {
		first := page(t, get(t, h, "/v1/products?limit=4"))
		if len(first.Items) != 4 || first.NextCursor == nil {
			t.Fatalf("first page: %d items, cursor %v", len(first.Items), first.NextCursor)
		}
		second := page(t, get(t, h, "/v1/products?limit=4&cursor="+*first.NextCursor))
		if len(second.Items) != 2 || second.NextCursor != nil {
			t.Fatalf("second page: %d items, cursor %v", len(second.Items), second.NextCursor)
		}
		seen := map[int64]bool{}
		var last int64
		for _, p := range append(first.Items, second.Items...) {
			if seen[p.ID] || p.ID <= last {
				t.Fatalf("id %d repeated or out of order", p.ID)
			}
			seen[p.ID], last = true, p.ID
			if p.Price.Currency != "USD" || p.Price.Amount <= 0 || p.Slug == "" || p.Name == "" {
				t.Fatalf("product %+v", p)
			}
		}
	})

	t.Run("a limit that divides the total ends with a null cursor, not an empty page", func(t *testing.T) {
		first := page(t, get(t, h, "/v1/products?limit=3"))
		if len(first.Items) != 3 || first.NextCursor == nil {
			t.Fatalf("first page: %d items, cursor %v", len(first.Items), first.NextCursor)
		}
		second := page(t, get(t, h, "/v1/products?limit=3&cursor="+*first.NextCursor))
		if len(second.Items) != 3 || second.NextCursor != nil {
			t.Fatalf("second page: %d items, cursor %v", len(second.Items), second.NextCursor)
		}
		all := page(t, get(t, h, "/v1/products?limit=6"))
		if len(all.Items) != 6 || all.NextCursor != nil {
			t.Fatalf("limit=6: %d items, cursor %v", len(all.Items), all.NextCursor)
		}
	})

	t.Run("a cursor past the last product gives an empty page", func(t *testing.T) {
		rec := get(t, h, "/v1/products?cursor=cDo5OTk5OTk") // "p:9999999"
		p := page(t, rec)
		if len(p.Items) != 0 || p.NextCursor != nil {
			t.Fatalf("%d items, cursor %v", len(p.Items), p.NextCursor)
		}
		if !strings.Contains(rec.Body.String(), `"items":[]`) {
			t.Fatalf("items must be an empty array, not null: %s", rec.Body)
		}
	})

	t.Run("a valid cursor resumes after its id", func(t *testing.T) {
		if p := page(t, get(t, h, "/v1/products?cursor=cDox")); len(p.Items) != 5 || p.Items[0].ID <= 1 { // "p:1"
			t.Fatalf("%d items", len(p.Items))
		}
	})

	t.Run("the default page holds everything, and the last page says so in JSON", func(t *testing.T) {
		rec := get(t, h, "/v1/products")
		if p := page(t, rec); len(p.Items) != 6 {
			t.Fatalf("default limit: %d items", len(p.Items))
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
			t.Fatal(err)
		}
		if string(raw["next_cursor"]) != "null" {
			t.Fatalf("next_cursor = %s, want null", raw["next_cursor"])
		}
		for _, k := range []string{"$schema", "links"} {
			if _, ok := raw[k]; ok {
				t.Fatalf("unexpected field %q", k)
			}
		}
	})

	t.Run("bad requests are 400 problem details with no internals", func(t *testing.T) {
		for _, target := range []string{
			"/v1/products?limit=0",
			"/v1/products?limit=101",
			"/v1/products?limit=x",
			"/v1/products?cursor=%25%25%25",
			"/v1/products?cursor=eDox",   // "x:1": wrong prefix
			"/v1/products?cursor=cDowMQ", // "p:01": not canonical
			"/v1/products?cursor=cDotMQ", // "p:-1"
			"/v1/products?cursor=cDoxMh", // "p:12" (cDoxMg) with non-zero padding bits
			"/v1/products?cursor=cDo",    // truncated
		} {
			rec := get(t, h, target)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s: status %d, want 400: %s", target, rec.Code, rec.Body)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Fatalf("%s: content type %q", target, ct)
			}
			body := strings.ToLower(rec.Body.String())
			for _, leak := range []string{"sql", "pgx", "select", "products"} {
				if strings.Contains(body, leak) {
					t.Fatalf("%s: body mentions %q: %s", target, leak, rec.Body)
				}
			}
		}
	})

	// Last, because it adds rows: with more than 20 products, the default page holds exactly 20.
	t.Run("limit defaults to 20", func(t *testing.T) {
		for i := range 15 {
			if _, err := pool.Exec(ctx, `INSERT INTO products (slug, name, price_amount, price_currency)
				VALUES ($1, 'Extra', 100, 'USD')`, fmt.Sprintf("extra-%d", i)); err != nil {
				t.Fatal(err)
			}
		}
		p := page(t, get(t, h, "/v1/products"))
		if len(p.Items) != 20 || p.NextCursor == nil {
			t.Fatalf("default page: %d items, cursor %v; want 20 and a cursor", len(p.Items), p.NextCursor)
		}
	})
}
