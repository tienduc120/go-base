package httpserver_test

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/tienduc120/go-base/platform/httpserver"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func ok() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") })
}

func TestNewRefusesAZeroTimeout(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		edit func(*httpserver.Timeouts)
	}{
		{"ReadHeader", func(to *httpserver.Timeouts) { to.ReadHeader = 0 }},
		{"Read", func(to *httpserver.Timeouts) { to.Read = 0 }},
		{"Write", func(to *httpserver.Timeouts) { to.Write = 0 }},
		{"Idle", func(to *httpserver.Timeouts) { to.Idle = 0 }},
		{"Shutdown", func(to *httpserver.Timeouts) { to.Shutdown = -time.Second }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			timeouts := httpserver.DefaultTimeouts()
			tc.edit(&timeouts)
			srv, err := httpserver.New(t.Context(), "127.0.0.1:0", ok(), timeouts)
			if err == nil {
				_ = srv.Close()
				t.Errorf(
					"a %s timeout of zero means no limit, which lets one slow client hold a connection forever",
					tc.name,
				)
			}
		})
	}
}

func TestNewRefusesANilHandler(t *testing.T) {
	t.Parallel()

	srv, err := httpserver.New(t.Context(), "127.0.0.1:0", nil, httpserver.DefaultTimeouts())
	if err == nil {
		_ = srv.Close()
		t.Error("a nil handler would serve the default mux, which anything may register on")
	}
}

func TestNewReportsATakenPort(t *testing.T) {
	t.Parallel()

	first, err := httpserver.New(t.Context(), "127.0.0.1:0", ok(), httpserver.DefaultTimeouts())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })

	second, err := httpserver.New(t.Context(), first.Addr(), ok(), httpserver.DefaultTimeouts())
	if err == nil {
		_ = second.Close()
		t.Error("binding a taken port must fail at startup, not later inside Run")
	}
}

func get(t *testing.T, url string) string {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestRunServesUntilTheContextEnds(t *testing.T) {
	t.Parallel()

	srv, err := httpserver.New(t.Context(), "127.0.0.1:0", ok(), httpserver.DefaultTimeouts())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	if body := get(t, "http://"+srv.Addr()+"/"); body != "ok" {
		t.Errorf("body = %q, want ok", body)
	}

	cancel()
	if err = <-done; err != nil {
		t.Errorf("a context that ends is a clean shutdown, got %v", err)
	}
}

func TestShutdownWaitsForAnInFlightRequest(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	slow := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = io.WriteString(w, "finished")
	})
	srv, err := httpserver.New(t.Context(), "127.0.0.1:0", slow, httpserver.DefaultTimeouts())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	body := make(chan string, 1)
	go func() { body <- get(t, "http://"+srv.Addr()+"/") }()

	<-started
	cancel()
	close(release)

	if got := <-body; got != "finished" {
		t.Errorf("an in-flight request must complete during shutdown, got %q", got)
	}
	if err = <-done; err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
}

func TestCloseReleasesThePortWithoutRunning(t *testing.T) {
	t.Parallel()

	srv, err := httpserver.New(t.Context(), "127.0.0.1:0", ok(), httpserver.DefaultTimeouts())
	if err != nil {
		t.Fatal(err)
	}
	if err = srv.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := httpserver.New(t.Context(), srv.Addr(), ok(), httpserver.DefaultTimeouts())
	if err != nil {
		t.Fatalf("the port should be free after Close: %v", err)
	}
	_ = again.Close()
}
