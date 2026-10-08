package catalog_test

import (
	"context"
	"encoding/json"
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
			"/v1/products?cursor=" + "cDox", // base64url of "p:1" is cDox; truncated forms must fail
			"/v1/products?cursor=eDox",      // "x:1": wrong prefix
			"/v1/products?cursor=cDowMQ",    // "p:01": not canonical
			"/v1/products?cursor=cDotMQ",    // "p:-1"
		} {
			rec := get(t, h, target)
			if target == "/v1/products?cursor=cDox" {
				// "p:1" is a valid cursor: everything after id 1.
				if p := page(t, rec); len(p.Items) != 5 {
					t.Fatalf("valid cursor: %d items", len(p.Items))
				}
				continue
			}
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
}
