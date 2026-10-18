// Package postgrestest gives a test its own empty database on a real server, with a service's migrations applied.
//
// The server comes from TEST_DATABASE_URL, which `agentctl db test` sets after starting a throwaway Postgres. With
// the variable unset a test skips, loudly, unless AGENTCTL_REQUIRE_DB is set, which CI sets so that a test which
// cannot run fails instead of passing by absence.
package postgrestest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tienduc120/go-base/platform/postgres"
)

const (
	envURL     = "TEST_DATABASE_URL"
	envRequire = "AGENTCTL_REQUIRE_DB"

	setupTimeout = 30 * time.Second
	randomBytes  = 6
)

// AdminURL returns the server URL, skipping or failing the test when there is none.
func AdminURL(t testing.TB) string {
	t.Helper()
	url := os.Getenv(envURL)
	switch {
	case url != "":
		return url
	case os.Getenv(envRequire) != "":
		t.Fatalf(
			"%s is unset and %s is set: the test cannot be proven here; run `agentctl db test`",
			envURL,
			envRequire,
		)
	default:
		t.Skipf("%s is unset: this test was NOT run against a database (run `agentctl db test`)", envURL)
	}
	return ""
}

// New creates a database for t, applies the .sql files of migrations in name order, and returns its URL. The
// database is dropped when the test ends.
func New(t testing.TB, migrations string) string {
	t.Helper()
	admin := AdminURL(t)

	ctx, cancel := context.WithTimeout(t.Context(), setupTimeout)
	defer cancel()
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatalf("connect to the test server: %v", err)
	}

	suffix := make([]byte, randomBytes)
	if _, err = rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	name := "t_" + hex.EncodeToString(suffix)
	if _, err = conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(t.Context()), setupTimeout)
		defer stop()
		_, _ = conn.Exec(cleanup, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		_ = conn.Close(cleanup)
	})

	dbURL := withDatabase(t, admin, name)
	if migrations != "" {
		apply(ctx, t, dbURL, migrations)
	}
	return dbURL
}

// Pool is New plus a pool opened the way a service opens one, closed when the test ends.
func Pool(t testing.TB, migrations string) *pgxpool.Pool {
	t.Helper()
	pool, err := postgres.Connect(t.Context(), postgres.DefaultConfig(New(t, migrations)))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func withDatabase(t testing.TB, raw, name string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("%s is not a URL", envURL)
	}
	u.Path = "/" + name
	return u.String()
}

func apply(ctx context.Context, t testing.TB, dbURL, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	slices.Sort(files)

	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to the new database: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	for _, f := range files {
		sql, readErr := os.ReadFile(filepath.Join(dir, f))
		if readErr != nil {
			t.Fatalf("read %s: %v", f, readErr)
		}
		// With no arguments pgx uses the simple protocol, which accepts a file of several statements.
		if _, err = conn.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("migration %s: %v", f, err)
		}
	}
}
