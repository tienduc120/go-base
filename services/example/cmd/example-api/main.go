// Command example-api serves the example service over HTTP, with a loopback-only debug listener beside it.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/tienduc120/go-base/platform/async"
	"github.com/tienduc120/go-base/platform/clock"
	"github.com/tienduc120/go-base/platform/debugserver"
	"github.com/tienduc120/go-base/platform/httpserver"
	"github.com/tienduc120/go-base/platform/postgres"
	"github.com/tienduc120/go-base/services/example/internal/application"
	"github.com/tienduc120/go-base/services/example/internal/infrastructure/memory"
	pgstore "github.com/tienduc120/go-base/services/example/internal/infrastructure/postgres"
	"github.com/tienduc120/go-base/services/example/internal/interfaces/rest"
)

const (
	defaultAddr      = ":8080"
	defaultDebugAddr = "127.0.0.1:6060"
)

type config struct {
	addr      string
	debugAddr string
	// databaseURL selects Postgres; empty keeps everything in memory, which suits a first run and most tests.
	databaseURL string
}

func configFrom(getenv func(string) string) config {
	return config{
		addr:        valueOr(getenv("ADDR"), defaultAddr),
		debugAddr:   valueOr(getenv("DEBUG_ADDR"), defaultDebugAddr),
		databaseURL: getenv("DATABASE_URL"),
	}
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func main() { os.Exit(runMain()) }

func runMain() int {
	// Kubernetes sends SIGTERM, not an interrupt, before it kills a pod.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	svc, err := newService(ctx, logger, configFrom(os.Getenv))
	if err != nil {
		logger.ErrorContext(ctx, "start", "error", err)
		return 1
	}
	logger.InfoContext(ctx, "listening", "addr", svc.api.Addr(), "debug_addr", svc.debug.Addr())
	if err = svc.run(ctx); err != nil {
		logger.ErrorContext(ctx, "stopped", "error", err)
		return 1
	}
	return 0
}

// service owns both listeners and the storage. The listeners are bound before anything runs, so a bad or taken
// address fails at startup.
type service struct {
	api   *httpserver.Server
	debug *debugserver.Server
	// closeStorage releases the database pool, if there is one.
	closeStorage func()
}

func newService(ctx context.Context, logger *slog.Logger, cfg config) (*service, error) {
	repo, closeStorage, err := newRepository(ctx, logger, cfg)
	if err != nil {
		return nil, err
	}
	creator := application.NewCreateNote(repo, clock.System{})
	handler := postgres.BudgetMiddleware(postgres.DefaultLimits(), logger)(rest.NewHandler(creator))

	api, err := httpserver.New(ctx, cfg.addr, handler, httpserver.DefaultTimeouts())
	if err != nil {
		closeStorage()
		return nil, fmt.Errorf("api listener: %w", err)
	}
	debug, err := debugserver.New(ctx, cfg.debugAddr)
	if err != nil {
		_ = api.Close()
		closeStorage()
		return nil, fmt.Errorf("debug listener: %w", err)
	}
	debugserver.EnableContentionProfiles()
	return &service{api: api, debug: debug, closeStorage: closeStorage}, nil
}

// newRepository is the one place that knows which storage backs the service; nothing else changes between them.
func newRepository(
	ctx context.Context,
	logger *slog.Logger,
	cfg config,
) (application.NoteRepository, func(), error) {
	if cfg.databaseURL == "" {
		return memory.NewNoteRepository(), func() {}, nil
	}

	dbCfg := postgres.DefaultConfig(cfg.databaseURL)
	dbCfg.AppName = "example-api"
	dbCfg.Logger = logger
	pool, err := postgres.Connect(ctx, dbCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("database: %w", err)
	}
	return pgstore.NewNoteRepository(pool), pool.Close, nil
}

// run serves until ctx ends or either server fails, and a failure of one stops the other.
func (s *service) run(ctx context.Context) error {
	defer s.closeStorage()
	return async.Run(ctx, s.api.Run, s.debug.Run)
}
