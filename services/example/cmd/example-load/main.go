// Command example-load applies bounded load to a running example-api, so a profile taken beside it shows what the
// service does under traffic. The requests are yours to write: see scenario.go.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tienduc120/go-base/platform/loadgen"
)

// The exit codes: scripts and agents branch on them.
const (
	exitOK    = 0
	exitFail  = 1
	exitUsage = 2
)

const (
	defaultURL      = "http://127.0.0.1:8080"
	defaultDuration = 15 * time.Second
	defaultWorkers  = 4
)

type options struct {
	baseURL string
	load    loadgen.Config
}

// parseFlags reads the command line. It refuses a target that is not on this machine unless told otherwise, because
// pointing a load program at a shared or production address by accident is the expensive mistake.
func parseFlags(args []string, errOut io.Writer) (options, error) {
	fs := flag.NewFlagSet("example-load", flag.ContinueOnError)
	fs.SetOutput(errOut)
	target := fs.String("url", defaultURL, "base URL of the running example-api")
	duration := fs.Duration("duration", defaultDuration, "how long to apply load")
	workers := fs.Int("workers", defaultWorkers, "requests in flight at once")
	maxRequests := fs.Int("max-requests", 0, "stop after this many requests (0: the engine's default cap)")
	remote := fs.Bool("allow-remote", false, "allow a target that is not on this machine")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}

	u, err := url.Parse(*target)
	if err != nil || u.Host == "" {
		return options{}, fmt.Errorf("-url %q is not a URL such as %s", *target, defaultURL)
	}
	if !*remote && !onThisMachine(u.Hostname()) {
		return options{}, fmt.Errorf(
			"-url %q is not on this machine; pass -allow-remote if that is really intended",
			*target,
		)
	}
	return options{
		baseURL: *target,
		load:    loadgen.Config{Workers: *workers, Duration: *duration, MaxRequests: *maxRequests},
	}, nil
}

func onThisMachine(host string) bool {
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}

// run applies the load, prints what happened, and returns an error when any request failed.
func run(ctx context.Context, opts options, step loadgen.Step, out io.Writer) error {
	stats, err := loadgen.Run(ctx, opts.load, step)
	_, _ = fmt.Fprint(out, stats.Summary())
	if err != nil {
		return err
	}
	if stats.Errors > 0 {
		return fmt.Errorf("%d of %d requests failed", stats.Errors, stats.Requests)
	}
	return nil
}

func main() { os.Exit(runMain()) }

func runMain() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	opts, err := parseFlags(os.Args[1:], os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return exitOK
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "example-load:", err)
		return exitUsage
	}
	if err = run(ctx, opts, scenario(opts.baseURL), os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "example-load:", err)
		return exitFail
	}
	return exitOK
}
