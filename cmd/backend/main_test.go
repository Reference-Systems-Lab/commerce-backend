package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
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

func TestServeNamesMissingVariable(t *testing.T) {
	e, stderr := testEnv(nil)
	if code := run(context.Background(), []string{"serve"}, e); code != exitFail {
		t.Fatalf("exit %d, want %d", code, exitFail)
	}
	if !strings.Contains(stderr.String(), "DATABASE_URL is required") {
		t.Fatalf("stderr = %q", stderr.String())
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
}
