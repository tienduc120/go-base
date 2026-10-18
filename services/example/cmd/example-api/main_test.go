package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/tienduc120/go-base/platform/postgres/postgrestest"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func quiet() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestConfigFromUsesDefaultsAndOverrides(t *testing.T) {
	t.Parallel()

	got := configFrom(func(string) string { return "" })
	if got.addr != defaultAddr || got.debugAddr != defaultDebugAddr || got.databaseURL != "" {
		t.Errorf("empty environment gave %+v, want the defaults and no database", got)
	}

	env := map[string]string{"ADDR": ":9000", "DEBUG_ADDR": "127.0.0.1:9001", "DATABASE_URL": "postgres://x"}
	got = configFrom(func(key string) string { return env[key] })
	if got.addr != ":9000" || got.debugAddr != "127.0.0.1:9001" || got.databaseURL != "postgres://x" {
		t.Errorf("environment gave %+v, want the overrides", got)
	}
}

func TestNewServiceRefusesAPublicDebugAddress(t *testing.T) {
	t.Parallel()

	svc, err := newService(t.Context(), quiet(), config{addr: "127.0.0.1:0", debugAddr: "0.0.0.0:0"})
	if err == nil {
		_ = svc.api.Close()
		_ = svc.debug.Close()
		t.Error("profiling endpoints must never listen beyond loopback")
	}
}

func TestNewServiceFailsFastOnAnUnreachableDatabase(t *testing.T) {
	t.Parallel()

	cfg := config{
		addr: "127.0.0.1:0", debugAddr: "127.0.0.1:0",
		databaseURL: "postgres://nobody@127.0.0.1:1/none?sslmode=disable&connect_timeout=1",
	}
	svc, err := newService(t.Context(), quiet(), cfg)
	if err == nil {
		_ = svc.api.Close()
		_ = svc.debug.Close()
		t.Error("a service that cannot reach its database must not start and fail on the first request")
	}
}

func request(t *testing.T, method, url, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(out)
}

// start runs a service built from cfg until the test ends and returns it.
func start(t *testing.T, cfg config) *service {
	t.Helper()
	svc, err := newService(t.Context(), quiet(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- svc.run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if runErr := <-done; runErr != nil {
			t.Errorf("a context that ends is a clean shutdown, got %v", runErr)
		}
	})
	return svc
}

func TestServiceServesBothListenersAndStopsWhenItsContextEnds(t *testing.T) {
	t.Parallel()

	svc := start(t, config{addr: "127.0.0.1:0", debugAddr: "127.0.0.1:0"})

	code, body := request(t, http.MethodPost, "http://"+svc.api.Addr()+"/notes", `{"id":"id-1","name":"first"}`)
	if code != http.StatusCreated {
		t.Errorf("create: %d %s, want 201", code, body)
	}
	if code, body = request(t, http.MethodGet, "http://"+svc.debug.Addr()+"/debug/pprof/", ""); code != http.StatusOK {
		t.Errorf("debug listener: %d %s, want 200", code, body)
	}
	if code, _ = request(t, http.MethodGet, "http://"+svc.api.Addr()+"/debug/pprof/", ""); code != http.StatusNotFound {
		t.Errorf("profiles on the API port: %d, want 404: they belong on the debug listener only", code)
	}
}

func TestServiceKeepsItsDataInPostgresWhenADatabaseURLIsSet(t *testing.T) {
	t.Parallel()

	svc := start(t, config{
		addr: "127.0.0.1:0", debugAddr: "127.0.0.1:0",
		databaseURL: postgrestest.New(t, "../../internal/infrastructure/postgres/migrations"),
	})
	url := "http://" + svc.api.Addr() + "/notes"

	if code, body := request(t, http.MethodPost, url, `{"id":"id-1","name":"first"}`); code != http.StatusCreated {
		t.Fatalf("first create: %d %s, want 201", code, body)
	}
	if code, body := request(t, http.MethodPost, url, `{"id":"id-1","name":"again"}`); code != http.StatusConflict {
		t.Errorf("second create of the same id: %d %s, want 409 from the database's unique key", code, body)
	}
}
