// Package verify runs the repository's gates in one pass and reports them compactly: every step runs even after a
// failure, full output goes to a log file, and only the tail of a failure comes back.
package verify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/tienduc120/go-base/tools/internal/proc"
	"github.com/tienduc120/go-base/tools/internal/rules"
)

type Level int

const (
	// Fast is what runs on every commit; Full adds the slow gates run before a push.
	Fast Level = iota
	Full
)

const (
	vetTimeout   = 3 * time.Minute
	buildTimeout = 3 * time.Minute
	lintTimeout  = 5 * time.Minute
	testTimeout  = 5 * time.Minute
	archTimeout  = 2 * time.Minute
	vulnTimeout  = 5 * time.Minute
	rulesTimeout = 10 * time.Minute

	// goTestLimit is Go's own per-package limit; it prints a goroutine dump before the step timeout kills the run.
	goTestLimit = "120s"
	// tailLines bounds how much of a failing step's output comes back.
	tailLines = 30

	// roundTo keeps durations readable in the summary.
	roundTo = 100 * time.Millisecond

	allPackages = "./..."
	archConfig  = "arch-go.yml"
	logMode     = 0o600
	dirMode     = 0o750
)

type Module struct {
	Dir           string
	Rel           string
	HasGo         bool
	HasArchConfig bool
}

type Outcome struct {
	Pass    bool
	Skipped bool
	// Detail is the skip reason or the tail of a failure.
	Detail string
	// Output is the complete output, kept for the log.
	Output string
}

type Step struct {
	Name string
	Run  func(ctx context.Context) Outcome
}

type Result struct {
	Outcome

	Name     string
	Duration time.Duration
	LogPath  string
}

type Env struct {
	Root      string
	Modules   []Module
	Runner    proc.Runner
	LogDir    string
	GoVersion string
}

// Modules lists the modules named by go.work, in order.
func Modules(root string) ([]Module, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.work"))
	if err != nil {
		return nil, fmt.Errorf("read go.work: %w", err)
	}

	var mods []Module
	for _, entry := range workUses(string(data)) {
		dir := filepath.Join(root, filepath.FromSlash(entry))
		rel, relErr := filepath.Rel(root, dir)
		if relErr != nil {
			return nil, relErr
		}
		m := Module{Dir: dir, Rel: filepath.ToSlash(rel), HasGo: hasGoFiles(dir)}
		if _, statErr := os.Stat(filepath.Join(dir, archConfig)); statErr == nil {
			m.HasArchConfig = true
		}
		mods = append(mods, m)
	}
	return mods, nil
}

var useLine = regexp.MustCompile(`^use\s+(\S+)\s*$`)

func workUses(src string) []string {
	var out []string
	inBlock := false
	for raw := range strings.SplitSeq(src, "\n") {
		code, _, _ := strings.Cut(raw, "//")
		line := strings.TrimSpace(code)
		switch {
		case line == "use (":
			inBlock = true
		case inBlock && line == ")":
			inBlock = false
		case inBlock && line != "":
			out = append(out, line)
		default:
			if m := useLine.FindStringSubmatch(line); m != nil {
				out = append(out, m[1])
			}
		}
	}
	return out
}

// hasGoFiles reports whether dir holds Go code of its own: nested modules and directories the go tool ignores do not count.
func hasGoFiles(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable directory is simply not counted
		}
		if d.IsDir() {
			return skipDir(dir, p, d.Name())
		}
		if strings.HasSuffix(d.Name(), ".go") {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
}

func skipDir(root, p, name string) error {
	if p == root {
		return nil
	}
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata" || name == "vendor" {
		return fs.SkipDir
	}
	if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
		return fs.SkipDir
	}
	return nil
}

// agentctlPkg is built into .agentctl/bin so the agent hooks start in milliseconds instead of compiling on every call.
const agentctlPkg = "github.com/tienduc120/go-base/tools/agentctl"

func agentlintBin(root string) string { return builtBin(root, "agentlint") }

func builtBin(root, name string) string {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(root, ".agentctl", "bin", name)
}

// Plan lists the steps for a level. A module without Go code is named as skipped, never silently dropped.
func Plan(env Env, level Level) []Step {
	var steps []Step
	var withGo []Module
	for _, m := range env.Modules {
		if m.HasGo {
			withGo = append(withGo, m)
			continue
		}
		steps = append(
			steps,
			skipped("checks ("+m.Rel+")", "no Go packages yet: vet, lint, tests, and layer rules skipped"),
		)
	}
	steps = append(steps, markdownStep(env))
	steps = append(steps, DebtStep(env))
	steps = append(steps, ADRStep(env))

	for _, m := range withGo {
		steps = append(steps, command(env, "go vet ("+m.Rel+")", m.Dir, vetTimeout, "go", "vet", allPackages))
	}
	if len(withGo) > 0 {
		bin := agentlintBin(env.Root)
		steps = append(steps, command(env, "build agentlint", env.Root, buildTimeout,
			"go", "build", "-o", bin, rules.AgentlintPkg))
		steps = append(steps, command(env, "build agentctl", env.Root, buildTimeout,
			"go", "build", "-o", builtBin(env.Root, "agentctl"), agentctlPkg))
		for _, m := range withGo {
			steps = append(steps, command(env, "agentlint ("+m.Rel+")", m.Dir, vetTimeout,
				"go", "vet", "-vettool="+bin, allPackages))
		}
	}
	for _, m := range withGo {
		steps = append(
			steps,
			command(env, "golangci-lint ("+m.Rel+")", m.Dir, lintTimeout, "golangci-lint", "run", allPackages),
		)
	}
	for _, m := range withGo {
		steps = append(steps, command(env, "go test ("+m.Rel+")", m.Dir, testTimeout,
			"go", "test", "-race", "-timeout", goTestLimit, "-count=1", allPackages))
	}
	for _, m := range withGo {
		if m.HasArchConfig {
			steps = append(steps, command(env, "arch-go ("+m.Rel+")", m.Dir, archTimeout, "arch-go"))
		}
	}
	if len(withGo) > 0 {
		// After "build agentctl": the step runs that binary.
		steps = append(steps, DBStep(env, level))
	}

	if level == Full {
		for _, m := range withGo {
			steps = append(
				steps,
				command(env, "govulncheck ("+m.Rel+")", m.Dir, vulnTimeout, "govulncheck", allPackages),
			)
		}
		steps = append(steps, rulesStep(env))
	}
	return steps
}

func skipped(name, reason string) Step {
	return Step{Name: name, Run: func(context.Context) Outcome { return Outcome{Skipped: true, Detail: reason} }}
}

func command(env Env, name, dir string, timeout time.Duration, tool string, args ...string) Step {
	return Step{Name: name, Run: func(ctx context.Context) Outcome {
		res, err := env.Runner.Run(ctx, proc.Spec{Dir: dir, Name: tool, Args: args, Timeout: timeout})
		switch {
		case errors.Is(err, exec.ErrNotFound):
			return Outcome{Detail: tool + " is not installed: run `agentctl doctor` for the install command"}
		case err != nil:
			return Outcome{Detail: err.Error()}
		case res.TimedOut:
			return Outcome{Detail: fmt.Sprintf("timed out after %s\n%s", timeout, tail(res.Output)), Output: res.Output}
		case res.ExitCode != 0:
			return Outcome{Detail: tail(res.Output), Output: res.Output}
		}
		return Outcome{Pass: true, Output: res.Output}
	}}
}

func rulesStep(env Env) Step {
	return Step{Name: "rules test", Run: func(ctx context.Context) Outcome {
		ctx, cancel := context.WithTimeout(ctx, rulesTimeout)
		defer cancel()

		outcomes, err := rules.Run(ctx, rules.Deps{
			Runner: env.Runner, FS: os.DirFS(env.Root), GoVersion: env.GoVersion, RepoRoot: env.Root,
		})
		if err != nil {
			return Outcome{Detail: err.Error()}
		}
		var failed []string
		for _, o := range outcomes {
			if !o.Pass {
				failed = append(failed, o.ID+": "+o.Detail)
			}
		}
		if len(failed) > 0 {
			return Outcome{Detail: tail(strings.Join(failed, "\n"))}
		}
		return Outcome{Pass: true}
	}}
}

// Execute runs every step in order; a failure never stops the ones after it.
func Execute(ctx context.Context, env Env, steps []Step) []Result {
	results := make([]Result, 0, len(steps))
	for _, step := range steps {
		start := time.Now()
		out := step.Run(ctx)
		res := Result{Name: step.Name, Outcome: out, Duration: time.Since(start)}
		if !out.Skipped && out.Output != "" {
			res.LogPath = writeLog(env.LogDir, step.Name, out.Output)
		}
		results = append(results, res)
	}
	return results
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func writeLog(dir, name, output string) string {
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return ""
	}
	p := filepath.Join(dir, strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")+".log")
	if err := os.WriteFile(p, []byte(output), logMode); err != nil {
		return ""
	}
	return p
}

func tail(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= tailLines {
		return strings.Join(lines, "\n")
	}
	return fmt.Sprintf(
		"... %d earlier lines in the log\n%s",
		len(lines)-tailLines,
		strings.Join(lines[len(lines)-tailLines:], "\n"),
	)
}

// Summarize prints one line per step and the tail of each failure; it reports whether everything passed.
func Summarize(w io.Writer, results []Result) bool {
	passed, failed, skippedCount := 0, 0, 0
	for _, r := range results {
		switch {
		case r.Skipped:
			skippedCount++
			_, _ = fmt.Fprintf(w, "SKIP  %s: %s\n", r.Name, r.Detail)
		case r.Pass:
			passed++
			_, _ = fmt.Fprintf(w, "ok    %s  %s\n", r.Name, r.Duration.Round(roundTo))
		default:
			failed++
			_, _ = fmt.Fprintf(w, "FAIL  %s  %s\n", r.Name, r.Duration.Round(roundTo))
			_, _ = fmt.Fprintln(w, indent(r.Detail))
			if r.LogPath != "" {
				_, _ = fmt.Fprintf(w, "      full log: %s\n", r.LogPath)
			}
		}
	}
	_, _ = fmt.Fprintf(w, "\n%d passed, %d failed, %d skipped\n", passed, failed, skippedCount)
	return failed == 0
}

func indent(s string) string {
	return "      " + strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n      ")
}
