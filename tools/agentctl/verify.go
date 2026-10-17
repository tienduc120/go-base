package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/tienduc120/go-base/tools/internal/doctor"
	"github.com/tienduc120/go-base/tools/internal/proc"
	"github.com/tienduc120/go-base/tools/internal/verify"
)

func runVerify(ctx context.Context, args []string, stdout, stderr io.Writer, env environment) int {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Bool("fast", true, "run the gates every commit needs (the default)")
	full := fs.Bool("full", false, "also run govulncheck and prove every rule with its fixture")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	root, err := doctor.FindRoot(env.cwd)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "agentctl verify:", err, "(run it inside the repository)")
		return exitUsage
	}
	goVersion, err := doctor.GoDirective(root)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "agentctl verify:", err)
		return exitUsage
	}
	modules, err := verify.Modules(root)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "agentctl verify:", err)
		return exitUsage
	}

	runner := env.procRunner
	if runner == nil {
		runner = proc.Exec{}
	}
	vEnv := verify.Env{
		Root:      root,
		Modules:   modules,
		Runner:    runner,
		LogDir:    filepath.Join(root, ".agentctl", "logs"),
		GoVersion: goVersion,
	}

	level := verify.Fast
	if *full {
		level = verify.Full
	}
	results := verify.Execute(ctx, vEnv, verify.Plan(vEnv, level))
	if !verify.Summarize(stdout, results) {
		return exitFail
	}
	return exitOK
}
