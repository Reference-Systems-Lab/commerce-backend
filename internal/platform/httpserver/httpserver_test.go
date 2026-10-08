package httpserver

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHeaders(t *testing.T) {
	for _, status := range []int{200, 400, 503} {
		rec := httptest.NewRecorder()
		Headers(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		})).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
		h := rec.Header()
		if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Cache-Control") != "no-store" {
			t.Fatalf("%d: headers = %v", status, h)
		}
		if h.Get("Strict-Transport-Security") != "" {
			t.Fatalf("%d: the app must never send HSTS", status)
		}
	}
}

func TestNewSetsTimeouts(t *testing.T) {
	s := New(http.NotFoundHandler(), 8080)
	if s.Addr != ":8080" {
		t.Fatalf("Addr = %q", s.Addr)
	}
	for name, d := range map[string]time.Duration{
		"ReadHeaderTimeout": s.ReadHeaderTimeout, "ReadTimeout": s.ReadTimeout,
		"WriteTimeout": s.WriteTimeout, "IdleTimeout": s.IdleTimeout,
	} {
		if d <= 0 {
			t.Fatalf("%s is not set", name)
		}
	}
}

// A request in flight when shutdown starts finishes, and Run returns nil within the timeout.
func TestRunDrainsOnCancel(t *testing.T) {
	started := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		time.Sleep(time.Second)
		_, _ = io.WriteString(w, "done")
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() {
		runErr <- Run(ctx, New(handler, 0), ln, slog.New(slog.DiscardHandler))
	}()

	type result struct {
		body string
		err  error
	}
	resc := make(chan result, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String() + "/")
		if err != nil {
			resc <- result{err: err}
			return
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		resc <- result{string(b), err}
	}()

	<-started
	begin := time.Now()
	cancel()
	res := <-resc
	if res.err != nil || res.body != "done" {
		t.Fatalf("in-flight request: %q, %v", res.body, res.err)
	}
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(ShutdownTimeout):
		t.Fatal("Run did not return within the shutdown timeout")
	}
	if time.Since(begin) > ShutdownTimeout {
		t.Fatal("draining took longer than the shutdown timeout")
	}
	if resp, err := http.Get("http://" + ln.Addr().String() + "/"); err == nil {
		resp.Body.Close()
		t.Fatal("the server still accepts connections after shutdown")
	}
}
