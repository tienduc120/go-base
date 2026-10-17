package rules

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/tienduc120/go-base/tools/internal/proc"
)

const allPackages = "./..."

const (
	preflightTimeout = 3 * time.Minute
	buildTimeout     = 3 * time.Minute
	golangciTimeout  = 5 * time.Minute
	archGoTimeout    = 2 * time.Minute

	// exitFindings is the exit code golangci-lint and arch-go use for "ran fine, found problems".
	exitFindings = 1
	// outputTail bounds how much of a crashed enforcer's output goes into the error.
	outputTail = 800
)

type Deps struct {
	Runner    proc.Runner
	FS        fs.FS
	GoVersion string
	// TempDir is where the staging module is created; empty uses the system default.
	TempDir string
	// RepoRoot is where the vettool is built from; only needed when a fixture uses agentlint.
	RepoRoot string
}

// AgentlintPkg is the package built into a vet tool and pointed at the staged module.
const AgentlintPkg = "github.com/tienduc120/go-base/tools/agentlint"

// Run stages every fixture as one module, runs each enforcer a fixture names exactly once, and evaluates.
// It returns an error, never an outcome, when an enforcer could not run: that must not read as "the rule fired".
func Run(ctx context.Context, deps Deps) ([]Outcome, error) {
	fixtures, err := Discover(deps.FS)
	if err != nil {
		return nil, err
	}

	stage, err := os.MkdirTemp(deps.TempDir, "agentctl-rules-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(stage) }()

	if err = Stage(deps.FS, fixtures, stage, deps.GoVersion); err != nil {
		return nil, err
	}

	if err = preflight(ctx, deps.Runner, stage); err != nil {
		return nil, err
	}

	findings := make(map[Enforcer][]Finding)
	for _, enforcer := range enforcersUsed(fixtures) {
		found, runErr := runEnforcer(ctx, deps, enforcer, stage)
		if runErr != nil {
			return nil, runErr
		}
		findings[enforcer] = found
	}
	return Evaluate(fixtures, findings), nil
}

// preflight proves the staged module compiles, tests included. A linter that cannot load one package silently
// drops its findings for every other package, which would turn a typo in one fixture into a wall of false
// failures. Vet is off on purpose: a violation fixture may legitimately trip a vet check.
func preflight(ctx context.Context, r proc.Runner, stage string) error {
	steps := [][]string{
		{"build", allPackages},
		{"test", "-vet=off", "-run=^$", "-count=1", allPackages},
	}
	for _, args := range steps {
		res, err := r.Run(ctx, proc.Spec{Dir: stage, Name: "go", Args: args, Timeout: preflightTimeout})
		switch {
		case err != nil:
			return fmt.Errorf("%w: go %s: %w", ErrEnforcerFailed, args[0], err)
		case res.TimedOut:
			return fmt.Errorf("%w: go %s timed out", ErrEnforcerFailed, args[0])
		case res.ExitCode != 0:
			return fmt.Errorf("%w: %s", ErrFixtureBroken, tail(res.Output))
		}
	}
	return nil
}

func enforcersUsed(fixtures []Fixture) []Enforcer {
	set := make(map[Enforcer]bool)
	for _, fx := range fixtures {
		set[fx.Spec.Enforcer] = true
	}
	out := make([]Enforcer, 0, len(set))
	for e := range set {
		out = append(out, e)
	}
	slices.Sort(out)
	return out
}

func runEnforcer(ctx context.Context, deps Deps, enforcer Enforcer, stage string) ([]Finding, error) {
	switch enforcer {
	case GolangciLint:
		return runGolangci(ctx, deps.Runner, stage)
	case ArchGo:
		return runArchGo(ctx, deps.Runner, stage)
	case Agentlint:
		return runAgentlint(ctx, deps, stage)
	}
	return nil, fmt.Errorf("%w: unknown enforcer %q", ErrEnforcerFailed, enforcer)
}

func runGolangci(ctx context.Context, r proc.Runner, stage string) ([]Finding, error) {
	const report = "issues.json"
	res, err := r.Run(ctx, proc.Spec{
		Dir:     stage,
		Name:    "golangci-lint",
		Args:    []string{"run", "--output.json.path=" + report, "--show-stats=false", allPackages},
		Timeout: golangciTimeout,
	})
	if err = checkRun(GolangciLint, res, err); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(filepath.Join(stage, report))
	if err != nil {
		return nil, fmt.Errorf("%w: golangci-lint wrote no report: %w", ErrEnforcerFailed, err)
	}
	return ParseGolangci(data)
}

func runArchGo(ctx context.Context, r proc.Runner, stage string) ([]Finding, error) {
	res, err := r.Run(ctx, proc.Spec{
		Dir:     stage,
		Name:    "arch-go",
		Args:    []string{"--json"},
		Timeout: archGoTimeout,
	})
	if err = checkRun(ArchGo, res, err); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(filepath.Join(stage, ".arch-go", "report.json"))
	if err != nil {
		return nil, fmt.Errorf("%w: arch-go wrote no report: %w", ErrEnforcerFailed, err)
	}
	return ParseArchGo(data, stagedModule)
}

func runAgentlint(ctx context.Context, deps Deps, stage string) ([]Finding, error) {
	bin := filepath.Join(stage, ".bin", "agentlint"+exeSuffix())

	res, err := deps.Runner.Run(ctx, proc.Spec{
		Dir:     deps.RepoRoot,
		Name:    "go",
		Args:    []string{"build", "-o", bin, AgentlintPkg},
		Timeout: buildTimeout,
	})
	if err = checkBuild(res, err); err != nil {
		return nil, err
	}

	res, err = deps.Runner.Run(ctx, proc.Spec{
		Dir:     stage,
		Name:    "go",
		Args:    []string{"vet", "-vettool=" + bin, allPackages},
		Timeout: archGoTimeout,
	})
	if err = checkRun(Agentlint, res, err); err != nil {
		return nil, err
	}
	return ParseVet(res.Output), nil
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func checkBuild(res proc.Result, err error) error {
	switch {
	case err != nil:
		return fmt.Errorf("%w: build agentlint: %w", ErrEnforcerFailed, err)
	case res.TimedOut:
		return fmt.Errorf("%w: build agentlint timed out", ErrEnforcerFailed)
	case res.ExitCode != 0:
		return fmt.Errorf("%w: build agentlint: %s", ErrEnforcerFailed, tail(res.Output))
	}
	return nil
}

// checkRun accepts exit 0 (clean) and 1 (findings); anything else, or a cut-short run, is a failure to run.
func checkRun(enforcer Enforcer, res proc.Result, err error) error {
	switch {
	case err != nil:
		return fmt.Errorf("%w: %s: %w", ErrEnforcerFailed, enforcer, err)
	case res.TimedOut:
		return fmt.Errorf("%w: %s timed out", ErrEnforcerFailed, enforcer)
	case res.ExitCode != 0 && res.ExitCode != exitFindings:
		return fmt.Errorf("%w: %s exited %d: %s", ErrEnforcerFailed, enforcer, res.ExitCode, tail(res.Output))
	}
	return nil
}

func tail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= outputTail {
		return s
	}
	return "..." + s[len(s)-outputTail:]
}
