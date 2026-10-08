// Package httpserver builds the HTTP API and runs it with timeouts and a graceful shutdown.
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

// Title and Version describe the API in its OpenAPI document. Version is the contract's version,
// not the release's.
const (
	Title   = "Commerce API"
	Version = "1.0.0"
)

// ShutdownTimeout bounds how long in-flight requests may run after SIGTERM.
const ShutdownTimeout = 10 * time.Second

func init() {
	// Huma answers 422 when a parameter fails validation. The API contract says 400 for any request
	// the client got wrong, so map it once here, before any API exists.
	base := huma.NewError
	huma.NewError = func(status int, msg string, errs ...error) huma.StatusError {
		if status == http.StatusUnprocessableEntity {
			status = http.StatusBadRequest
		}
		return base(status, msg, errs...)
	}
}

// NewAPI returns a Huma API on a standard mux, plus the handler that serves it with the headers every
// response carries. It serves its OpenAPI document at /openapi.json and no docs page, which would
// load scripts from a CDN.
func NewAPI() (huma.API, http.Handler) {
	cfg := huma.DefaultConfig(Title, Version)
	cfg.Info.Description = "The commerce platform's backend API."
	// No `$schema` links or schema endpoints: responses are exactly the documented bodies.
	cfg.CreateHooks = nil
	cfg.SchemasPath = ""
	cfg.DocsPath = ""
	mux := http.NewServeMux()
	api := humago.New(mux, cfg)
	return api, Headers(mux)
}

// Headers sets the headers every response carries. Caching is off because responses may show
// prices the backend can change at any time. HSTS is the proxy's job, never the app's.
func Headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// New returns a server with every timeout set.
func New(handler http.Handler, port int) *http.Server {
	return &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
}

// Run serves on ln until ctx is cancelled, then stops accepting connections and waits up to
// ShutdownTimeout for in-flight requests.
func Run(ctx context.Context, srv *http.Server, ln net.Listener, log *slog.Logger) error {
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	log.Info("listening", "addr", ln.Addr().String())

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
