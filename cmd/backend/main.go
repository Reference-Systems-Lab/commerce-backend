// Command backend is the commerce backend: one static binary whose subcommands are its processes.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Reference-Systems-Lab/commerce-backend/internal/app"
	"github.com/Reference-Systems-Lab/commerce-backend/internal/platform/config"
	"github.com/Reference-Systems-Lab/commerce-backend/internal/platform/db"
	"github.com/Reference-Systems-Lab/commerce-backend/internal/platform/httpserver"
)

const usage = `usage: backend <command>

commands:
  serve        run the HTTP API
  healthcheck  exit 0 if the API on 127.0.0.1:$PORT reports healthy, 1 otherwise
`

// Exit codes.
const (
	exitOK    = 0
	exitFail  = 1
	exitUsage = 2
)

// env is the process's view of the outside world, injected so the commands can be tested.
type env struct {
	stdout, stderr io.Writer
	lookup         config.Lookup
	readFile       config.ReadFile
}

type command func(ctx context.Context, e env) error

var commands = map[string]command{
	"serve":       serve,
	"healthcheck": healthcheck,
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	code := run(ctx, os.Args[1:], env{os.Stdout, os.Stderr, os.LookupEnv, os.ReadFile})
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, e env) int {
	if len(args) != 1 {
		fmt.Fprint(e.stderr, usage)
		return exitUsage
	}
	cmd, ok := commands[args[0]]
	if !ok {
		fmt.Fprintf(e.stderr, "backend: unknown command %q\n\n%s", args[0], usage)
		return exitUsage
	}
	if err := cmd(ctx, e); err != nil {
		fmt.Fprintf(e.stderr, "backend %s: %v\n", args[0], err)
		return exitFail
	}
	return exitOK
}

func logger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, nil))
}

func serve(ctx context.Context, e env) error {
	srvCfg, err := config.LoadServer(e.lookup)
	if err != nil {
		return err
	}
	dbCfg, err := config.LoadDatabase(e.lookup, e.readFile)
	if err != nil {
		return err
	}
	pool, err := db.Open(ctx, dbCfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	api, handler := httpserver.NewAPI()
	app.Register(api, app.Deps{DB: pool})
	srv := httpserver.New(handler, srvCfg.Port)
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return err
	}
	return httpserver.Run(ctx, srv, ln, logger(e.stdout))
}

// HealthcheckTimeout bounds the container health check.
const HealthcheckTimeout = 3 * time.Second

// healthcheck is the image's HEALTHCHECK: the image has no shell or curl.
func healthcheck(ctx context.Context, e env) error {
	srvCfg, err := config.LoadServer(e.lookup)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, HealthcheckTimeout)
	defer cancel()
	url := fmt.Sprintf("http://127.0.0.1:%d/health", srvCfg.Port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.New("the API did not answer")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the API reported %d", resp.StatusCode)
	}
	return nil
}
