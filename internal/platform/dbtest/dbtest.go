// Package dbtest starts a throwaway PostgreSQL for integration tests, on the same image the platform
// runs, so the tests exercise the real server.
package dbtest

import (
	"context"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/Reference-Systems-Lab/commerce-backend/internal/platform/config"
)

// Image is the platform's Postgres pin (commerce-platform compose.yaml). Dependabot doesn't watch Go
// string constants, so bump it when the platform bumps its pin.
const Image = "postgres:18.6-trixie@sha256:74935e72241653ca55e0414067e6d8763aceb8a810eb51b452253ec3dcfc4336"

// Password is the test database's throwaway password.
const Password = "test-only-password"

// Postgres is a running test database.
type Postgres struct {
	Container *postgres.PostgresContainer
	// Config has a URL without the password and the password separately, as the backend expects.
	Config config.Database
}

// Start runs a fresh database for the test and removes it when the test ends.
func Start(t *testing.T) *Postgres {
	t.Helper()
	ctx := context.Background()
	c, err := postgres.Run(ctx, Image,
		postgres.WithDatabase("commerce"),
		postgres.WithUsername("commerce"),
		postgres.WithPassword(Password),
		postgres.BasicWaitStrategies(),
	)
	testcontainers.CleanupContainer(t, c)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	host, err := c.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := c.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatal(err)
	}
	return &Postgres{
		Container: c,
		Config: config.Database{
			URL:      "postgres://commerce@" + host + ":" + port.Port() + "/commerce?sslmode=disable",
			Password: config.Secret(Password),
		},
	}
}
