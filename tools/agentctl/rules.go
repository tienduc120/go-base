package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/tienduc120/go-base/tools/internal/doctor"
	"github.com/tienduc120/go-base/tools/internal/proc"
	"github.com/tienduc120/go-base/tools/internal/rules"
)

const rulesUsage = `usage: agentctl rules test

  test   prove every rule fires on its violation fixture and the compliant twin stays clean
`

func runRules(ctx context.Context, args []string, stdout, stderr io.Writer, env environment) int {
	if len(args) == 0 || args[0] != "test" {
		_, _ = fmt.Fprint(stderr, rulesUsage)
		return exitUsage
	}

	root, err := doctor.FindRoot(env.cwd)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "agentctl rules:", err, "(run it inside the repository)")
		return exitUsage
	}
	goVersion, err := doctor.GoDirective(root)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "agentctl rules:", err)
		return exitUsage
	}

	runner := env.procRunner
	if runner == nil {
		runner = proc.Exec{}
	}
	outcomes, err := rules.Run(
		ctx,
		rules.Deps{Runner: runner, FS: os.DirFS(root), GoVersion: goVersion, RepoRoot: root},
	)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "agentctl rules:", err)
		switch {
		case errors.Is(err, rules.ErrFixtureBroken):
			_, _ = fmt.Fprintln(stderr, "every fixture must compile, or the linters silently drop all findings")
		case errors.Is(err, rules.ErrEnforcerFailed):
			_, _ = fmt.Fprintln(stderr, "an enforcer that cannot run proves nothing; fix the tool first")
		}
		return exitUsage
	}

	return report(stdout, outcomes)
}

func report(w io.Writer, outcomes []rules.Outcome) int {
	passed, failed := 0, 0
	for _, o := range outcomes {
		if o.Pass {
			passed++
			_, _ = fmt.Fprintf(w, "PASS  %s\n", label(o))
			continue
		}
		failed++
		_, _ = fmt.Fprintf(w, "FAIL  %s\n      %s\n", label(o), o.Detail)
	}
	_, _ = fmt.Fprintf(w, "\n%d passed, %d failed\n", passed, failed)
	if failed > 0 {
		return exitFail
	}
	return exitOK
}

func label(o rules.Outcome) string {
	if o.Enforcer == "" {
		return o.ID
	}
	return fmt.Sprintf("%s (%s)", o.ID, o.Enforcer)
}
