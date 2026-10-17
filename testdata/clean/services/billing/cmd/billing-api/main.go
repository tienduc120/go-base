// Command billing-api serves the billing service over HTTP.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"fixture.test/clean/platform/clock"
	"fixture.test/clean/services/billing/internal/application"
	"fixture.test/clean/services/billing/internal/infrastructure/memory"
	"fixture.test/clean/services/billing/internal/interfaces/httpapi"
)

const (
	addr              = ":8080"
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = time.Minute
	shutdownTimeout   = 10 * time.Second
)

func main() { os.Exit(run()) }

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	issuer := application.NewIssueInvoice(memory.NewInvoiceRepository(), clock.System{})
	srv := &http.Server{
		Addr:              addr,
		Handler:           httpapi.NewHandler(issuer),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	// AfterFunc starts the shutdown without a bare go statement; done lets run wait for in-flight requests.
	done := make(chan struct{})
	context.AfterFunc(ctx, func() {
		defer close(done)
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.ErrorContext(shutdownCtx, "shutdown", "error", err)
		}
	})

	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		logger.ErrorContext(ctx, "server stopped", "error", err)
		return 1
	}
	<-done
	return 0
}
