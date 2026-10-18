package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/tienduc120/go-base/tools/internal/dbcheck"
	"github.com/tienduc120/go-base/tools/internal/doctor"
	"github.com/tienduc120/go-base/tools/internal/proc"
)

const dbUsage = `usage: agentctl db check [--no-plan] [--image IMAGE] [service...]

  check   judge the SQL of every service that has queries, or the named ones. It compiles the queries with sqlc, applies the
          query rules (no select-star, LIMIT on lists, no OFFSET, WHERE on writes, and more), and runs each query's plan on a
          throwaway Postgres in Docker, failing on a sequential scan. --no-plan skips Docker and the plan checks.
          Accept a finding with a reason in the query file: -- agent:allow <rule>: <why>
`

func runDB(ctx context.Context, args []string, stdout, stderr io.Writer, env environment) int {
	if len(args) == 0 || args[0] != "check" {
		_, _ = fmt.Fprint(stderr, dbUsage)
		return exitUsage
	}

	fs := flag.NewFlagSet("db check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	noPlan := fs.Bool("no-plan", false, "skip the Postgres plan checks (no Docker needed)")
	image := fs.String("image", dbcheck.DefaultImage, "Postgres image for plans; match production's major version")
	if err := fs.Parse(args[1:]); err != nil {
		return exitUsage
	}

	root, err := doctor.FindRoot(env.cwd)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "agentctl db:", err, "(run it inside the repository)")
		return exitUsage
	}
	var runner proc.Runner = proc.Exec{}
	if env.procRunner != nil {
		runner = env.procRunner
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
