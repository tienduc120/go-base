package postgres_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tienduc120/go-base/platform/postgres"
)

const testURL = "postgres://app:s3cret@localhost:5432/appdb?sslmode=disable"

func TestPoolConfigSetsEveryServerSideTimeoutAsASessionParameter(t *testing.T) {
	t.Parallel()

	cfg := postgres.DefaultConfig(testURL)
	cfg.StatementTimeout = 5 * time.Second
	cfg.LockTimeout = 2 * time.Second
	cfg.IdleInTransactionTimeout = 10 * time.Second
	cfg.AppName = "billing-api"

	pool, err := postgres.PoolConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	params := pool.ConnConfig.RuntimeParams
	for key, want := range map[string]string{
		"statement_timeout":                   "5000",
		"lock_timeout":                        "2000",
		"idle_in_transaction_session_timeout": "10000",
		"application_name":                    "billing-api",
	} {
		if params[key] != want {
			t.Errorf("%s = %q, want %q: the server must enforce it, whatever the client does", key, params[key], want)
		}
	}
}

func TestPoolConfigBoundsThePoolAndInstallsTheTracer(t *testing.T) {
	t.Parallel()

	cfg := postgres.DefaultConfig(testURL)
	cfg.MaxConns = 7
	pool, err := postgres.PoolConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if pool.MaxConns != 7 || pool.MaxConnLifetime <= 0 || pool.MaxConnIdleTime <= 0 || pool.HealthCheckPeriod <= 0 {
		t.Errorf("pool = %+v: size, lifetime, idle time and health checks must all be bounded", pool)
	}
	if pool.ConnConfig.ConnectTimeout <= 0 {
		t.Error("connecting must have a deadline")
	}
	if pool.ConnConfig.Tracer == nil {
		t.Error("without the tracer there is no query budget and no slow-query log")
	}
}

func TestConfigRefusesAZeroOrNegativeBound(t *testing.T) {
	t.Parallel()

	for name, edit := range map[string]func(*postgres.Config){
		"StatementTimeout":         func(c *postgres.Config) { c.StatementTimeout = 0 },
		"LockTimeout":              func(c *postgres.Config) { c.LockTimeout = 0 },
		"IdleInTransactionTimeout": func(c *postgres.Config) { c.IdleInTransactionTimeout = -time.Second },
		"ConnectTimeout":           func(c *postgres.Config) { c.ConnectTimeout = 0 },
		"MaxConnLifetime":          func(c *postgres.Config) { c.MaxConnLifetime = 0 },
		"MaxConnIdleTime":          func(c *postgres.Config) { c.MaxConnIdleTime = 0 },
		"HealthCheckPeriod":        func(c *postgres.Config) { c.HealthCheckPeriod = 0 },
		"MaxConns":                 func(c *postgres.Config) { c.MaxConns = 0 },
	} {
		cfg := postgres.DefaultConfig(testURL)
		edit(&cfg)

		_, err := postgres.PoolConfig(cfg)
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf(
				"%s: err = %v, want a refusal that names the field: zero means unlimited, which is a bug",
				name,
				err,
			)
		}
	}
}

func TestConfigRefusesAMissingURL(t *testing.T) {
	t.Parallel()

	if _, err := postgres.PoolConfig(postgres.DefaultConfig("")); err == nil {
		t.Error("an empty URL must be an error, not a connection to a default local server")
	}
}

func TestAParseErrorNeverEchoesThePassword(t *testing.T) {
	t.Parallel()

	_, err := postgres.PoolConfig(postgres.DefaultConfig("postgres://app:s3cret@local host:5432/db"))

	if err == nil {
		t.Fatal("want a parse error")
	}
	if strings.Contains(err.Error(), "s3cret") {
		t.Errorf("a configuration error ends up in logs and must not carry the password: %v", err)
	}
}
