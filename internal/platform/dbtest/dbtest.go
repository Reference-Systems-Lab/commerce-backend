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
const Image = "postgres:18.7-trixie@sha256:9f4fd4aa918f52ae3d6f3b687f1de90d8f867cd4d520021545a9a50c120eae3b"

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
