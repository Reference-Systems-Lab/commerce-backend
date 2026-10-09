package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Reference-Systems-Lab/commerce-backend/internal/platform/httpserver"
)

func testEnv(vars map[string]string) (env, *bytes.Buffer) {
	var stderr bytes.Buffer
	return env{
		stdout:   &bytes.Buffer{},
		stderr:   &stderr,
		lookup:   func(k string) (string, bool) { v, ok := vars[k]; return v, ok },
		readFile: func(string) ([]byte, error) { return nil, errors.New("no files in tests") },
	}, &stderr
}

func TestUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"nope"}, {"serve", "extra"}} {
		e, stderr := testEnv(nil)
		if code := run(context.Background(), args, e); code != exitUsage {
			t.Fatalf("%v: exit %d, want %d", args, code, exitUsage)
		}
		if !strings.Contains(stderr.String(), "usage: backend <command>") {
			t.Fatalf("%v: no usage printed: %q", args, stderr.String())
		}
	}
}

func TestCommandsNameMissingVariable(t *testing.T) {
	for _, cmd := range []string{"serve", "migrate", "seed"} {
		e, stderr := testEnv(nil)
		if code := run(context.Background(), []string{cmd}, e); code != exitFail {
			t.Fatalf("%s: exit %d, want %d", cmd, code, exitFail)
		}
		if !strings.Contains(stderr.String(), "DATABASE_URL is required") {
			t.Fatalf("%s: stderr = %q", cmd, stderr.String())
		}
	}
}

func portOf(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Port()
}

func TestHealthcheck(t *testing.T) {
	for _, c := range []struct {
		status int
		want   int
	}{{200, exitOK}, {503, exitFail}} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/health" {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(c.status)
		}))
		e, stderr := testEnv(map[string]string{"PORT": portOf(t, srv.URL)})
		if code := run(context.Background(), []string{"healthcheck"}, e); code != c.want {
			t.Fatalf("API answers %d: exit %d, want %d (%s)", c.status, code, c.want, stderr)
		}
		srv.Close()
	}

	// Nothing listening.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	ln.Close()
	e, _ := testEnv(map[string]string{"PORT": port})
	if code := run(context.Background(), []string{"healthcheck"}, e); code != exitFail {
		t.Fatalf("closed port: exit %d, want %d", code, exitFail)
	}

	// Accepts the connection but never answers: the check gives up after its timeout.
	hung, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer hung.Close()
	go func() {
		for {
			c, err := hung.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	_, port, _ = net.SplitHostPort(hung.Addr().String())
	e, _ = testEnv(map[string]string{"PORT": port})
	start := time.Now()
	if code := run(context.Background(), []string{"healthcheck"}, e); code != exitFail {
		t.Fatalf("hung API: exit %d, want %d", code, exitFail)
	}
	if took := time.Since(start); took < HealthcheckTimeout || took > HealthcheckTimeout+time.Second {
		t.Fatalf("hung API: gave up after %v, want about %v", took, HealthcheckTimeout)
	}
}

// The real binary, sent a real SIGTERM, drains and exits 0. The database is unreachable on purpose:
// serve starts anyway and reports 503 until it can reach it.
func TestServeExitsZeroOnSIGTERM(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "backend")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	pwFile := filepath.Join(dir, "pw")
	if err := os.WriteFile(pwFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	ln.Close()

	cmd := exec.Command(bin, "serve")
	cmd.Env = append(os.Environ(),
		"PORT="+port,
		"DATABASE_URL=postgres://commerce@127.0.0.1:1/commerce?sslmode=disable&connect_timeout=1",
		"DATABASE_PASSWORD_FILE="+pwFile,
	)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	url := "http://127.0.0.1:" + port + "/health"
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode != http.StatusServiceUnavailable {
				t.Fatalf("health with no database: %d, want 503", resp.StatusCode)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("serve never started listening")
		}
		time.Sleep(50 * time.Millisecond)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve after SIGTERM: %v, want exit 0", err)
		}
	case <-time.After(httpserver.ShutdownTimeout + 2*time.Second):
		t.Fatal("serve did not exit after SIGTERM")
	}
}

func TestOpenAPI(t *testing.T) {
	e, stderr := testEnv(nil) // no database configuration at all
	out := e.stdout.(*bytes.Buffer)
	if code := run(context.Background(), []string{"openapi"}, e); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var doc struct {
		OpenAPI string                    `json:"openapi"`
		Paths   map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.OpenAPI != "3.1.0" {
		t.Fatalf("openapi = %q", doc.OpenAPI)
	}
	for _, p := range []string{"/health", "/v1/products"} {
		if _, ok := doc.Paths[p]["get"]; !ok {
			t.Fatalf("no GET %s in the document", p)
		}
	}
	if bytes.Contains(out.Bytes(), []byte(`"422"`)) {
		t.Fatal("the document lists 422, which the API never sends")
	}
}

// The committed api/openapi.json must be exactly what the code generates (REQ-008). Run `make spec`
// after changing a handler's types.
func TestCommittedSpecIsCurrent(t *testing.T) {
	want, err := openAPIDocument()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../../api/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("api/openapi.json is out of date: run `make spec` and commit the result")
	}
}
