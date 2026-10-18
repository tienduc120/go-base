package dbcheck

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/tienduc120/go-base/tools/internal/proc"
)

const (
	// DefaultImage is the Postgres the plans are made on; production should match its major version.
	DefaultImage = "postgres:18-alpine"

	label = "gobase.dbcheck=1"
	// lifetimeSeconds ends the container by itself if agentctl is killed before it can clean up.
	lifetimeSeconds = "600"

	startTimeout = 3 * time.Minute
	stepTimeout  = 30 * time.Second
	stopTimeout  = 15 * time.Second
	readyTimeout = 40 * time.Second
	readyPoll    = 300 * time.Millisecond

	randomBytes = 6
	superuser   = "postgres"
)

// Postgres is a throwaway server in a container, reached only through `docker exec`: nothing is published on the
// network, so there is no port to collide with or expose.
type Postgres struct {
	runner proc.Runner
	name   string
}

func (p *Postgres) Name() string { return p.name }

// StartOption changes how StartPostgres runs the container.
type StartOption func(*startConfig)

type startConfig struct{ publish bool }

// WithPublishedPort makes the server reachable from the host, on a random port of the loopback interface only. Plan
// checks do not need it; a test run that connects with a Go driver does.
func WithPublishedPort() StartOption { return func(c *startConfig) { c.publish = true } }

// StartPostgres runs a disposable container and returns once the server answers.
func StartPostgres(ctx context.Context, runner proc.Runner, image string, opts ...StartOption) (*Postgres, error) {
	var cfg startConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	suffix := make([]byte, randomBytes)
	if _, err := rand.Read(suffix); err != nil {
		return nil, fmt.Errorf("name the container: %w", err)
	}
	pg := &Postgres{runner: runner, name: "gobase-dbcheck-" + hex.EncodeToString(suffix)}

	args := []string{
		"run", "-d", "--rm", "--name", pg.name, "--label", label,
		"-e", "POSTGRES_HOST_AUTH_METHOD=trust", "--tmpfs", "/var/lib/postgresql",
	}
	if cfg.publish {
		args = append(args, "-p", "127.0.0.1::5432")
	}
	args = append(args, "--entrypoint", "sh", image,
		"-c", "exec timeout "+lifetimeSeconds+" docker-entrypoint.sh postgres "+
			"-c fsync=off -c full_page_writes=off -c synchronous_commit=off")

	if _, err := pg.docker(ctx, startTimeout, args...); err != nil {
		return nil, fmt.Errorf("start postgres (%s): %w", image, err)
	}

	if err := pg.waitReady(ctx); err != nil {
		pg.Stop(ctx)
		return nil, err
	}
	return pg, nil
}

// Stop removes the container. It ignores a cancelled ctx because cleanup is exactly what must still happen then.
func (p *Postgres) Stop(ctx context.Context) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
	defer cancel()
	_, _ = p.docker(cleanup, stopTimeout, "rm", "-f", p.name)
}

func (p *Postgres) waitReady(ctx context.Context) error {
	deadline := time.NewTimer(readyTimeout)
	defer deadline.Stop()
	for {
		res, err := p.runner.Run(ctx, proc.Spec{
			Name: "docker", Timeout: stepTimeout,
			Args: []string{"exec", p.name, "pg_isready", "-h", "127.0.0.1", "-U", superuser},
		})
		if err == nil && !res.TimedOut && res.ExitCode == 0 {
			return nil
		}

		poll := time.NewTimer(readyPoll)
		select {
		case <-ctx.Done():
			poll.Stop()
			return fmt.Errorf("waiting for postgres: %w", ctx.Err())
		case <-deadline.C:
			poll.Stop()
			return fmt.Errorf("postgres did not become ready within %s", readyTimeout)
		case <-poll.C:
		}
	}
}

// URL is the connection URL of db on the host, for a server started WithPublishedPort.
func (p *Postgres) URL(ctx context.Context, db string) (string, error) {
	res, err := p.docker(ctx, stepTimeout, "port", p.name, "5432/tcp")
	if err != nil {
		return "", err
	}
	first, _, _ := strings.Cut(strings.TrimSpace(res.Output), "\n")
	if _, _, splitErr := net.SplitHostPort(strings.TrimSpace(first)); splitErr != nil {
		return "", fmt.Errorf(
			"docker did not report a published port (was the server started WithPublishedPort?): %q",
			res.Output,
		)
	}
	return fmt.Sprintf("postgres://%s@%s/%s?sslmode=disable", superuser, strings.TrimSpace(first), db), nil
}

// CreateDatabase makes an empty database, so each service is judged against only its own schema.
func (p *Postgres) CreateDatabase(ctx context.Context, name string) error {
	_, err := p.psql(ctx, superuser, nil, "-c", "CREATE DATABASE "+name)
	return err
}

// ApplyMigrations runs the directory's .sql files in name order into db. Migrations are forward-only plain SQL.
func (p *Postgres) ApplyMigrations(ctx context.Context, db, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	slices.Sort(files)

	if _, err = p.docker(ctx, stepTimeout, "cp", dir+string(os.PathSeparator)+".", p.name+":/migrations"); err != nil {
		return fmt.Errorf("copy migrations: %w", err)
	}
	for _, f := range files {
		if _, err = p.psql(ctx, db, nil, "-f", path.Join("/migrations", f)); err != nil {
			return fmt.Errorf("migration %s: %w", f, err)
		}
	}
	return nil
}

// Explain returns the EXPLAIN (FORMAT JSON) plan of sql in db. Sequential scans are disabled for the session, so one
// appears in the plan only when no index can serve the query; on an empty table the planner would otherwise pick it
// for everything. GENERIC_PLAN lets the $1 placeholders stand without values.
func (p *Postgres) Explain(ctx context.Context, db, sql string) (string, error) {
	flat := strings.Join(strings.Fields(sql), " ")
	res, err := p.psql(ctx, db, []string{"PGOPTIONS=-c enable_seqscan=off"},
		"-At", "-c", "EXPLAIN (GENERIC_PLAN, FORMAT JSON) "+flat)
	if err != nil {
		return "", err
	}
	return ExtractJSON(res.Output)
}

func (p *Postgres) psql(ctx context.Context, db string, env []string, args ...string) (proc.Result, error) {
	full := []string{"exec"}
	for _, e := range env {
		full = append(full, "-e", e)
	}
	full = append(full, p.name, "psql", "-U", superuser, "-d", db, "-q", "-v", "ON_ERROR_STOP=1")
	res, err := p.docker(ctx, stepTimeout, append(full, args...)...)
	if err != nil && res.ExitCode != 0 && strings.Contains(res.Output, "ERROR:") {
		return res, &SQLError{Output: strings.TrimSpace(res.Output)}
	}
	return res, err
}

// SQLError is Postgres rejecting a statement, as opposed to Docker or the network failing.
type SQLError struct{ Output string }

func (e *SQLError) Error() string { return e.Output }

func (p *Postgres) docker(ctx context.Context, timeout time.Duration, args ...string) (proc.Result, error) {
	res, err := p.runner.Run(ctx, proc.Spec{Name: "docker", Args: args, Timeout: timeout})
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return res, errors.New(
			"docker is not installed or not on PATH: install Docker, or run with --no-plan to skip plans",
		)
	case err != nil:
		return res, err
	case res.TimedOut:
		return res, fmt.Errorf("docker %s timed out (%s)", args[0], res.Reason)
	case res.ExitCode != 0:
		return res, fmt.Errorf("docker %s failed: %s", args[0], strings.TrimSpace(res.Output))
	}
	return res, nil
}
