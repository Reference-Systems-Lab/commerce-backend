// Package app wires the domains into the HTTP API. It is the one package allowed to know them all.
package app

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Reference-Systems-Lab/commerce-backend/internal/health"
)

// Deps are what the handlers need at run time. They are nil when only the OpenAPI document is built.
type Deps struct {
	DB *pgxpool.Pool
}

// Register adds every operation to the API.
func Register(api huma.API, d Deps) {
	var pinger health.Pinger
	if d.DB != nil {
		pinger = d.DB
	}
	health.Register(api, pinger)
}
