package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/tienduc120/go-base/tools/internal/doctor"
	"github.com/tienduc120/go-base/tools/internal/proc"
	"github.com/tienduc120/go-base/tools/internal/scaffold"
)

const sqlcTimeout = time.Minute

const newUsage = `usage: agentctl new service <name> [--aggregate Name]

  service   create services/<name> with every layer, a test beside each piece of logic, graceful shutdown, and the
            debug listener, already passing every gate. <name> is lowercase words joined by hyphens (order-intake).
            --aggregate names the first aggregate in PascalCase (Invoice); it defaults to the service name.
            Refuses to touch a service that already exists.
`

func runNew(ctx context.Context, args []string, stdout, stderr io.Writer, env environment) int {
	if len(args) == 0 || args[0] != "service" {
		_, _ = fmt.Fprint(stderr, newUsage)
		return exitUsage
	}
	req, ok := parseNewService(args[1:], stderr)
	if !ok {
		return exitUsage
	}

	root, err := doctor.FindRoot(env.cwd)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "agentctl new:", err, "(run it inside the repository)")
		return exitUsage
	}
	module, err := rootModule(root)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "agentctl new:", err)
		return exitUsage
	}

	files, err := scaffold.Generate(scaffold.Spec{Module: module, Service: req.name, Aggregate: req.aggregate})
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "agentctl new:", err)
		return exitUsage
	}
	if err = scaffold.Write(root, files); err != nil {
		_, _ = fmt.Fprintln(stderr, "agentctl new:", err)
		return exitFail
	}
	for _, f := range files {
		_, _ = fmt.Fprintln(stdout, "created", f.Path)
	}

	// The queries' Go code is sqlc's output, so the service does not compile until it has run.
	pgDir := filepath.Join(root, "services", req.name, filepath.FromSlash(scaffold.PostgresDir))
	if err = generateQueries(ctx, env, pgDir); err != nil {
		_, _ = fmt.Fprintln(stderr, "agentctl new: the files are written, but", err)
		return exitFail
	}
	_, _ = fmt.Fprintln(
		stdout,
		"generated the query code in",
		path.Join("services", req.name, scaffold.PostgresDir, "db"),
	)
	_, _ = fmt.Fprintf(
		stdout,
		"\nservices/%s is ready. Next: agentctl verify. It runs in memory until DATABASE_URL is set; "+
			"agentctl db test ./services/%s/... runs the Postgres adapter against a real server.\n",
		req.name,
		req.name,
	)
	return exitOK
}

type newRequest struct{ name, aggregate string }

// parseNewService takes the service name and --aggregate in either order; it prints usage and reports false when
// the arguments do not name exactly one service.
func parseNewService(args []string, stderr io.Writer) (newRequest, bool) {
	var name string
	// The name comes first in the documented form, and flag stops parsing at the first non-flag.
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("new service", flag.ContinueOnError)
	fs.SetOutput(stderr)
	aggregateFlag := fs.String("aggregate", "", "PascalCase name of the first aggregate (default: the service name)")
	if err := fs.Parse(args); err != nil {
		return newRequest{}, false
	}
	extra := fs.Args()
	if name == "" && len(extra) > 0 {
		name, extra = extra[0], extra[1:]
	}
	if name == "" || len(extra) > 0 {
		_, _ = fmt.Fprint(stderr, newUsage)
		return newRequest{}, false
	}
	return newRequest{name: name, aggregate: *aggregateFlag}, true
}

// generateQueries runs sqlc in the service's postgres directory.
func generateQueries(ctx context.Context, env environment, dir string) error {
	var runner proc.Runner = proc.Exec{}
	if env.procRunner != nil {
		runner = env.procRunner
	}
	res, err := runner.Run(ctx, proc.Spec{Dir: dir, Name: "sqlc", Args: []string{"generate"}, Timeout: sqlcTimeout})
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return errors.New(
			"sqlc is not installed: run `agentctl doctor` for the install command, then `sqlc generate` in " + dir,
		)
	case err != nil:
		return err
	case res.TimedOut || res.ExitCode != 0:
		return fmt.Errorf("sqlc generate failed:\n%s", strings.TrimSpace(res.Output))
	}
	return nil
}

// rootModule reads the module path of the repository's own (root) module, which is what a service's imports start with.
func rootModule(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", errors.New("the repository root has no go.mod to take the module path from")
	}
	match := goModule.FindSubmatch(data)
	if match == nil {
		return "", errors.New("no module line in the root go.mod")
	}
	return string(match[1]), nil
}
