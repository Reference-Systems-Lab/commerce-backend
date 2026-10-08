// Package db opens the PostgreSQL pool.
package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Reference-Systems-Lab/commerce-backend/internal/platform/config"
)

// Open creates a pool from the configuration. It doesn't connect: the first query or Ping does,
// so the API can start, and report itself unhealthy, before the database is up.
func Open(ctx context.Context, cfg config.Database) (*pgxpool.Pool, error) {
	pc, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		// pgx masks passwords in its errors, and the URL has none (config.LoadDatabase).
		return nil, fmt.Errorf("DATABASE_URL: %w", err)
	}
	pc.ConnConfig.Password = cfg.Password.Reveal()
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, errors.New("cannot create the database pool")
	}
	return pool, nil
}
