package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Config is everything a pool needs, with no field allowed to mean "unlimited".
type Config struct {
	URL     string
	AppName string
	// MaxConns bounds the pool, which bounds the connections one service can hold on the server.
	MaxConns int32

	// ConnectTimeout bounds opening a connection.
	ConnectTimeout time.Duration
	// StatementTimeout makes the server cancel any statement that runs longer.
	StatementTimeout time.Duration
	// LockTimeout makes the server give up waiting for a lock.
	LockTimeout time.Duration
	// IdleInTransactionTimeout makes the server end a session that sits inside an open transaction.
	IdleInTransactionTimeout time.Duration

	MaxConnLifetime   time.Duration
	MaxConnIdleTime   time.Duration
	HealthCheckPeriod time.Duration

	// SlowQuery is the duration past which a statement is logged; non-positive means the tracer's default.
	SlowQuery time.Duration
	Logger    *slog.Logger
}

const (
	defaultMaxConns          = 10
	defaultConnectTimeout    = 5 * time.Second
	defaultStatementTimeout  = 5 * time.Second
	defaultLockTimeout       = 2 * time.Second
	defaultIdleInTxTimeout   = 10 * time.Second
	defaultMaxConnLifetime   = 30 * time.Minute
	defaultMaxConnIdleTime   = 5 * time.Minute
	defaultHealthCheckPeriod = 30 * time.Second
)

// DefaultConfig fills every bound with a value that suits a request/response service.
func DefaultConfig(url string) Config {
	return Config{
		URL:                      url,
		MaxConns:                 defaultMaxConns,
		ConnectTimeout:           defaultConnectTimeout,
		StatementTimeout:         defaultStatementTimeout,
		LockTimeout:              defaultLockTimeout,
		IdleInTransactionTimeout: defaultIdleInTxTimeout,
		MaxConnLifetime:          defaultMaxConnLifetime,
		MaxConnIdleTime:          defaultMaxConnIdleTime,
		HealthCheckPeriod:        defaultHealthCheckPeriod,
	}
}

func (c Config) validate() error {
	if c.URL == "" {
		return errors.New("postgres config: URL is empty")
	}
	if c.MaxConns <= 0 {
		return fmt.Errorf("postgres config: MaxConns must be positive, got %d", c.MaxConns)
	}
	// A server-side timeout is sent in milliseconds, and Postgres reads 0 as "no limit", so a bound under one
	// millisecond would silently switch the protection off.
	for name, d := range map[string]time.Duration{
		"ConnectTimeout": c.ConnectTimeout, "StatementTimeout": c.StatementTimeout, "LockTimeout": c.LockTimeout,
		"IdleInTransactionTimeout": c.IdleInTransactionTimeout, "MaxConnLifetime": c.MaxConnLifetime,
		"MaxConnIdleTime": c.MaxConnIdleTime, "HealthCheckPeriod": c.HealthCheckPeriod,
	} {
		if d < time.Millisecond {
			return fmt.Errorf(
				"postgres config: %s must be at least 1ms, got %v: zero means unlimited, which hangs",
				name,
				d,
			)
		}
	}
	return nil
}

// PoolConfig turns c into a pgxpool configuration without connecting.
func PoolConfig(c Config) (*pgxpool.Config, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}
	pool, err := pgxpool.ParseConfig(c.URL)
	if err != nil {
		return nil, redact(err, c.URL)
	}

	pool.MaxConns = c.MaxConns
	pool.MaxConnLifetime = c.MaxConnLifetime
	pool.MaxConnIdleTime = c.MaxConnIdleTime
	pool.HealthCheckPeriod = c.HealthCheckPeriod
	pool.ConnConfig.ConnectTimeout = c.ConnectTimeout
	pool.ConnConfig.Tracer = NewTracer(TracerOptions{Logger: c.Logger, Slow: c.SlowQuery})

	params := pool.ConnConfig.RuntimeParams
	params["statement_timeout"] = milliseconds(c.StatementTimeout)
	params["lock_timeout"] = milliseconds(c.LockTimeout)
	params["idle_in_transaction_session_timeout"] = milliseconds(c.IdleInTransactionTimeout)
	if c.AppName != "" {
		params["application_name"] = c.AppName
	}
	return pool, nil
}

func milliseconds(d time.Duration) string { return strconv.FormatInt(d.Milliseconds(), 10) }

var urlPassword = regexp.MustCompile(`://[^:/@\s]*:([^@\s]*)@`)

// redact removes the URL's password from err, because the parser's message repeats the URL it was given and
// configuration errors end up in logs.
func redact(err error, url string) error {
	msg := err.Error()
	if m := urlPassword.FindStringSubmatch(url); m != nil && m[1] != "" {
		msg = strings.ReplaceAll(msg, m[1], "xxxxx")
	}
	return errors.New("postgres config: " + msg)
}

// Connect opens a pool and checks that the server answers within ConnectTimeout.
func Connect(ctx context.Context, c Config) (*pgxpool.Pool, error) {
	cfg, err := PoolConfig(c)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.ConnectTimeout)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres connect: %w", err)
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres ping: %w", err)
	}
	return pool, nil
}
