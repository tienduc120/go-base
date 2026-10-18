package dbcheck

import (
	"context"
	"fmt"
	"time"

	"github.com/tienduc120/go-base/tools/internal/proc"
)

const (
	goTestLimit   = "180s"
	goTestTimeout = 8 * time.Minute
	goTestIdle    = 3 * time.Minute

	adminDatabase = "postgres"
)

// TestOptions say which tests to run against a database.
type TestOptions struct {
	// Root is the directory go test runs in: the module holding the services and platform.
	Root string
	// Packages are the package patterns to test; empty means ./...
	Packages []string
	// Image is the Postgres image; empty means DefaultImage.
	Image string
}

// RunTests starts a throwaway Postgres, runs go test with TEST_DATABASE_URL pointing at it, and removes the server.
// The tests get AGENTCTL_REQUIRE_DB too, so one that needs the database fails instead of skipping. A failing test run
// comes back as a Result; an error means the server or the run could not be started.
func RunTests(ctx context.Context, runner proc.Runner, opts TestOptions) (proc.Result, error) {
	image := opts.Image
	if image == "" {
		image = DefaultImage
	}
	packages := opts.Packages
	if len(packages) == 0 {
		packages = []string{"./..."}
	}

	pg, err := StartPostgres(ctx, runner, image, WithPublishedPort())
	if err != nil {
		return proc.Result{}, err
	}
	defer pg.Stop(ctx)

	url, err := pg.URL(ctx, adminDatabase)
	if err != nil {
		return proc.Result{}, err
	}

	res, err := runner.Run(ctx, proc.Spec{
		Dir:         opts.Root,
		Name:        "go",
		Args:        append([]string{"test", "-race", "-count=1", "-timeout", goTestLimit}, packages...),
		Env:         []string{"TEST_DATABASE_URL=" + url, "AGENTCTL_REQUIRE_DB=1"},
		Timeout:     goTestTimeout,
		IdleTimeout: goTestIdle,
	})
	if err != nil {
		return proc.Result{}, fmt.Errorf("run go test: %w", err)
	}
	return res, nil
}
