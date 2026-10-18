// Command agentctl is the single command surface for developers and AI agents in this repository.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/tienduc120/go-base/tools/internal/doctor"
	"github.com/tienduc120/go-base/tools/internal/proc"
)

// Exit codes: callers (hooks, CI) branch on these, so they are part of the interface.
const (
	exitOK    = 0
	exitFail  = 1
	exitUsage = 2
)

const toolTimeout = 15 * time.Second

const usage = `usage: agentctl <command> [flags]

commands:
  doctor [--json]   check the Go toolchain and the pinned developer tools
  rules test        prove every rule fires on its violation fixture
  verify [--full]   run the gates; --full adds govulncheck and rules test
  commitmsg <file>  check a commit message (called by the commit-msg hook)
  run -- <command>  run a command under a deadline and idle limit, killing its whole process tree
  fmt md            put every Markdown paragraph and list item on one line
  hook <event>      guard for agent tool calls: pre-edit or pre-bash (reads the tool call as JSON on stdin)
  help              show this text
`

type environment struct {
	// The runners, stdin, and getenv are nil in production; tests inject fakes.
	runner     doctor.Runner
	procRunner proc.Runner
	stdin      io.Reader
	getenv     func(string) string
	cwd        string
}

func (e environment) env(key string) string {
	if e.getenv == nil {
		return ""
	}
	return e.getenv(key)
}

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "agentctl:", err)
		os.Exit(exitUsage)
	}

	// Ctrl-C cancels every child process the command started.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr, environment{cwd: cwd, stdin: os.Stdin, getenv: os.Getenv})
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, env environment) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return exitUsage
	}
	switch args[0] {
	case "doctor":
		return runDoctor(ctx, args[1:], stdout, stderr, env)
	case "rules":
		return runRules(ctx, args[1:], stdout, stderr, env)
	case "verify":
		return runVerify(ctx, args[1:], stdout, stderr, env)
	case "commitmsg":
		return runCommitmsg(args[1:], stderr)
	case "run":
		return runRun(ctx, args[1:], stdout, stderr, env)
	case "fmt":
		return runFmt(args[1:], stdout, stderr, env)
	case "hook":
		return runHook(ctx, args[1:], stderr, env)
	case "help", "-h", "--help":
		_, _ = fmt.Fprint(stdout, usage)
		return exitOK
	default:
		_, _ = fmt.Fprintf(stderr, "agentctl: unknown command %q\n\n%s", args[0], usage)
		return exitUsage
	}
}

func runDoctor(ctx context.Context, args []string, stdout, stderr io.Writer, env environment) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print machine-readable results")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	root, err := doctor.FindRoot(env.cwd)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "agentctl doctor:", err, "(run it inside the repository)")
		return exitUsage
	}
	goVersion, err := doctor.GoDirective(root)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "agentctl doctor:", err)
		return exitUsage
	}

	runner := env.runner
	if runner == nil {
		runner = doctor.ExecRunner{Dir: root}
	}
	results := doctor.Check(ctx, runner, doctor.DefaultTools(goVersion), toolTimeout)

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		err = enc.Encode(results)
	} else {
		err = doctor.Render(stdout, results)
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "agentctl doctor:", err)
		return exitUsage
	}

	if doctor.Failed(results) {
		return exitFail
	}
	return exitOK
}
