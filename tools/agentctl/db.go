package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/tienduc120/go-base/tools/internal/dbcheck"
	"github.com/tienduc120/go-base/tools/internal/doctor"
	"github.com/tienduc120/go-base/tools/internal/proc"
)

const dbUsage = `usage: agentctl db check [--no-plan] [--image IMAGE] [service...]
       agentctl db test [--image IMAGE] [package...]

  test    start a throwaway Postgres, run go test (default ./...) with TEST_DATABASE_URL pointing at it, and remove the
          server. Tests that need a database fail instead of skipping. Use it for platform/postgres and for service adapters.
  check   judge the SQL of every service that has queries, or the named ones. It compiles the queries with sqlc, applies the
          query rules (no select-star, LIMIT on lists, no OFFSET, WHERE on writes, and more), and runs each query's plan on a
          throwaway Postgres in Docker, failing on a sequential scan. --no-plan skips Docker and the plan checks.
          Accept a finding with a reason in the query file: -- agent:allow <rule>: <why>
`

func runDB(ctx context.Context, args []string, stdout, stderr io.Writer, env environment) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, dbUsage)
		return exitUsage
	}
	switch args[0] {
	case cmdCheck:
		return dbCheck(ctx, args[1:], stdout, stderr, env)
	case "test":
		return dbTest(ctx, args[1:], stdout, stderr, env)
	}
	_, _ = fmt.Fprint(stderr, dbUsage)
	return exitUsage
}

func dbCheck(ctx context.Context, args []string, stdout, stderr io.Writer, env environment) int {
	fs := flag.NewFlagSet("db check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	noPlan := fs.Bool("no-plan", false, "skip the Postgres plan checks (no Docker needed)")
	image := fs.String("image", dbcheck.DefaultImage, "Postgres image for plans; match production's major version")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	root, runner, ok := dbEnv(stderr, env)
	if !ok {
		return exitUsage
	}

	report, err := dbcheck.Run(ctx, runner, dbcheck.Options{
		Root: root, Services: fs.Args(), Plan: !*noPlan, Image: *image,
	})
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "agentctl db check:", err)
		return exitUsage
	}
	return renderDB(stdout, report)
}

func renderDB(w io.Writer, report dbcheck.Report) int {
	if report.Queries == 0 {
		_, _ = fmt.Fprintln(w, "db check: no queries to check")
		return exitOK
	}
	for _, f := range report.Findings {
		_, _ = fmt.Fprintln(w, f)
	}
	for _, f := range report.Allowed {
		_, _ = fmt.Fprintf(w, "accepted  %s:%d: %s: %s: %s\n", f.File, f.Line, f.Query, f.Rule, f.Reason)
	}

	summary := fmt.Sprintf("%d %s in %d %s", report.Queries, plural(report.Queries, "query", "queries"),
		len(report.Services), plural(len(report.Services), "service", "services"))
	if n := len(report.Findings); n > 0 {
		_, _ = fmt.Fprintf(w, "db check: %s: %d %s\n", summary, n, plural(n, "finding", "findings"))
		return exitFail
	}
	_, _ = fmt.Fprintf(w, "db check: %s: ok\n", summary)
	return exitOK
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func dbTest(ctx context.Context, args []string, stdout, stderr io.Writer, env environment) int {
	fs := flag.NewFlagSet("db test", flag.ContinueOnError)
	fs.SetOutput(stderr)
	image := fs.String("image", dbcheck.DefaultImage, "Postgres image; match production's major version")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	root, runner, ok := dbEnv(stderr, env)
	if !ok {
		return exitUsage
	}
	res, err := dbcheck.RunTests(ctx, runner, dbcheck.TestOptions{Root: root, Packages: fs.Args(), Image: *image})
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "agentctl db test:", err)
		return exitUsage
	}

	if res.TimedOut || res.ExitCode != 0 {
		_, _ = fmt.Fprint(stdout, tailLines(res.Output, failureTail))
		reason := fmt.Sprintf("exit %d", res.ExitCode)
		if res.TimedOut {
			reason = "killed: " + res.Reason
		}
		_, _ = fmt.Fprintf(stdout, "db test: failed (%s)\n", reason)
		return exitFail
	}
	_, _ = fmt.Fprintln(stdout, "db test: ok")
	return exitOK
}

// dbEnv finds the repository root and the process runner, printing why when there is no repository.
func dbEnv(stderr io.Writer, env environment) (string, proc.Runner, bool) {
	root, err := doctor.FindRoot(env.cwd)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "agentctl db:", err, "(run it inside the repository)")
		return "", nil, false
	}
	var runner proc.Runner = proc.Exec{}
	if env.procRunner != nil {
		runner = env.procRunner
	}
	return root, runner, true
}

const failureTail = 40

func tailLines(out string, n int) string {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n") + "\n"
}
