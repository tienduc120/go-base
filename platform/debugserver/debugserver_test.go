package debugserver_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/tienduc120/go-base/platform/debugserver"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestNewRefusesAnythingButLoopback(t *testing.T) {
	t.Parallel()

	for _, addr := range []string{"0.0.0.0:0", ":0", "10.1.2.3:6060", "example.com:80", "[::]:0", "no-port"} {
		srv, err := debugserver.New(t.Context(), addr)
		if err == nil {
			_ = srv.Close()
			t.Errorf("%q must be refused: a profiling endpoint is internal and never reachable from outside", addr)
		}
	}
}

func TestNewBindsALoopbackAddress(t *testing.T) {
	t.Parallel()

	srv, err := debugserver.New(t.Context(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	if !strings.HasPrefix(srv.Addr(), "127.0.0.1:") || strings.HasSuffix(srv.Addr(), ":0") {
		t.Errorf("Addr() = %q, want the real bound port so a caller can find it", srv.Addr())
	}
}

func get(t *testing.T, url string) (int, string) {
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
	return resp.StatusCode, string(body)
}

func TestRunServesPprofUntilTheContextEnds(t *testing.T) {
	t.Parallel()

	srv, err := debugserver.New(t.Context(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	base := "http://" + srv.Addr()
	if code, body := get(t, base+"/debug/pprof/"); code != http.StatusOK || !strings.Contains(body, "goroutine") {
		t.Errorf("index: %d\n%s", code, body)
	}
	if code, body := get(
		t,
		base+"/debug/pprof/goroutine?debug=1",
	); code != http.StatusOK ||
		!strings.Contains(body, "goroutine profile:") {
		t.Errorf("goroutine profile: %d\n%s", code, body)
	}

	cancel()
	if err = <-done; err != nil {
		t.Errorf("a context that ends is a clean shutdown, got %v", err)
	}
}

func TestOnlyPprofIsServed(t *testing.T) {
	t.Parallel()

	srv, err := debugserver.New(t.Context(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	for _, path := range []string{"/", "/debug/vars", "/metrics", "/admin"} {
		if code, _ := get(t, "http://"+srv.Addr()+path); code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404: the debug listener serves profiles and nothing else", path, code)
		}
	}
}

func TestCloseReleasesThePortWithoutRunning(t *testing.T) {
	t.Parallel()

	srv, err := debugserver.New(t.Context(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err = srv.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := debugserver.New(t.Context(), srv.Addr())
	if err != nil {
		t.Fatalf("the port should be free after Close: %v", err)
	}
	_ = again.Close()
}
