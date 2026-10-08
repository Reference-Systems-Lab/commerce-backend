// Package health reports whether the backend can serve: the process is up and PostgreSQL answers.
package health

import (
	"context"
	"net/http"
	"reflect"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// PingTimeout bounds the database check, so a hung database makes the check fail rather than hang.
const PingTimeout = 2 * time.Second

// Pinger checks a dependency; *pgxpool.Pool satisfies it.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Status is the health response body.
type Status struct {
	Status string `json:"status" enum:"ok,unavailable" doc:"ok when the backend can serve requests"`
}

type output struct {
	Status int
	Body   Status
}

// Register adds GET /health. db may be nil when only the OpenAPI document is wanted.
func Register(api huma.API, db Pinger) {
	huma.Register(api, huma.Operation{
		OperationID: "getHealth",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Report whether the backend can serve",
		Description: "200 while PostgreSQL answers within 2 seconds, 503 otherwise.",
		Tags:        []string{"Health"},
		Responses: map[string]*huma.Response{
			"503": {
				Description: "The backend can't reach PostgreSQL.",
				Content: map[string]*huma.MediaType{
					"application/json": {Schema: api.OpenAPI().Components.Schemas.Schema(
						reflect.TypeFor[Status](), true, "Status")},
				},
			},
		},
	}, func(ctx context.Context, _ *struct{}) (*output, error) {
		ctx, cancel := context.WithTimeout(ctx, PingTimeout)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			return &output{Status: http.StatusServiceUnavailable, Body: Status{"unavailable"}}, nil
		}
		return &output{Status: http.StatusOK, Body: Status{"ok"}}, nil
	})
}
