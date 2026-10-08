package health_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Reference-Systems-Lab/commerce-backend/internal/health"
	"github.com/Reference-Systems-Lab/commerce-backend/internal/platform/db"
	"github.com/Reference-Systems-Lab/commerce-backend/internal/platform/dbtest"
	"github.com/Reference-Systems-Lab/commerce-backend/internal/platform/httpserver"
)

func get(t *testing.T, h http.Handler) (int, health.Status) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	var body health.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	return rec.Code, body
}

// Against a real Postgres: healthy while it runs, 503 once it stops.
func TestHealthFollowsPostgres(t *testing.T) {
	pg := dbtest.Start(t)
	pool, err := db.Open(context.Background(), pg.Config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	api, h := httpserver.NewAPI()
	health.Register(api, pool)

	if code, body := get(t, h); code != http.StatusOK || body.Status != "ok" {
		t.Fatalf("running: %d %+v", code, body)
	}
	if err := pg.Container.Stop(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if code, body := get(t, h); code != http.StatusServiceUnavailable || body.Status != "unavailable" {
		t.Fatalf("stopped: %d %+v", code, body)
	}
}
