package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/pprof/profile"

	"github.com/tienduc120/go-base/tools/internal/doctor"
	"github.com/tienduc120/go-base/tools/internal/perf"
	"github.com/tienduc120/go-base/tools/internal/pprofan"
	"github.com/tienduc120/go-base/tools/internal/verify"
)

const (
	defaultCaptureSeconds = 10
	defaultPerfTop        = 10
	summaryRows           = 5
)

const perfUsage = `usage: agentctl perf <command>

  analyze [--base before.pprof] [--kind mutex|block|goroutineleak] [--prefix a,b] [--top N] [--json] [--fail-on-findings] <profile>
      say which function in our code owns the time, memory, lock wait, or goroutines in a pprof profile.
      With --base the result is what grew between the two snapshots. Never profile CPU on a -race build.
  capture --url http://127.0.0.1:6060 [--seconds 10] [--out dir] [--allow-remote] [--prefix a,b]
      snapshot a running service's pprof endpoint before and after a CPU window, then analyse everything.
      Run your load while it captures. Loopback only unless --allow-remote; the window is capped at 30 seconds.
`

func runPerf(ctx context.Context, args []string, stdout, stderr io.Writer, env environment) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, perfUsage)
		return exitUsage
	}
	switch args[0] {
	case "analyze":
		return perfAnalyze(args[1:], stdout, stderr, env)
	case "capture":
		return perfCapture(ctx, args[1:], stdout, stderr, env)
	}
	_, _ = fmt.Fprint(stderr, perfUsage)
	return exitUsage
}

var goModule = regexp.MustCompile(`(?m)^module\s+(\S+)`)

// modulePrefixes are the module paths of the workspace, which is what "our code" means by default.
func modulePrefixes(cwd string) []string {
	root, err := doctor.FindRoot(cwd)
	if err != nil {
		return nil
	}
	mods, err := verify.Modules(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, m := range mods {
		data, readErr := os.ReadFile(filepath.Join(m.Dir, "go.mod"))
		if readErr != nil {
			continue
		}
		if match := goModule.FindSubmatch(data); match != nil {
			out = append(out, string(match[1]))
		}
	}
	return out
}

func splitList(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// hintFromName guesses what a delay or goroutine profile is from its file name, because the contents do not say.
func hintFromName(name string) string {
	lower := strings.ToLower(filepath.Base(name))
	for _, hint := range []string{"goroutineleak", "mutex", "block"} {
		if strings.Contains(lower, hint) {
			return hint
		}
	}
	return ""
}

func perfAnalyze(args []string, stdout, stderr io.Writer, env environment) int {
	fs := flag.NewFlagSet("perf analyze", flag.ContinueOnError)
	fs.SetOutput(stderr)
	kind := fs.String("kind", "", "what a delay or goroutine profile is: mutex, block, or goroutineleak")
	prefix := fs.String(
		"prefix",
		"",
		"comma-separated import path prefixes that count as our code (default: the workspace modules)",
	)
	top := fs.Int("top", defaultPerfTop, "rows per table")
	asJSON := fs.Bool("json", false, "print the full report as JSON")
	base := fs.String("base", "", "an earlier snapshot of the same profile; report only what grew since")
	failOn := fs.Bool("fail-on-findings", false, "exit 1 when the report has findings")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 1 {
		_, _ = fmt.Fprint(stderr, perfUsage)
		return exitUsage
	}
	file := fs.Arg(0)

	opts := pprofan.Options{Prefixes: splitList(*prefix), Top: *top, Hint: *kind}
	if len(opts.Prefixes) == 0 {
		opts.Prefixes = modulePrefixes(env.cwd)
	}
	if opts.Hint == "" {
		opts.Hint = hintFromName(file)
	}

	report, err := analyzeFiles(*base, file, opts)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "agentctl perf analyze:", err)
		return exitUsage
	}

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err = enc.Encode(report); err != nil {
			_, _ = fmt.Fprintln(stderr, "agentctl perf analyze:", err)
			return exitUsage
		}
	} else {
		_, _ = fmt.Fprint(stdout, report.Summary(summaryRows))
	}

	if *failOn && len(report.Findings) > 0 {
		return exitFail
	}
	return exitOK
}

func analyzeFiles(baseFile, file string, opts pprofan.Options) (pprofan.Report, error) {
	cur, err := readProfile(file)
	if err != nil {
		return pprofan.Report{}, err
	}
	if baseFile == "" {
		return pprofan.Analyze(cur, opts)
	}
	prev, err := readProfile(baseFile)
	if err != nil {
		return pprofan.Report{}, err
	}
	return pprofan.AnalyzeDiff(prev, cur, opts)
}

func readProfile(path string) (*profile.Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	p, err := pprofan.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return p, nil
}

func perfCapture(ctx context.Context, args []string, stdout, stderr io.Writer, env environment) int {
	fs := flag.NewFlagSet("perf capture", flag.ContinueOnError)
	fs.SetOutput(stderr)
	url := fs.String("url", "", "the service's pprof address, such as http://127.0.0.1:6060")
	seconds := fs.Int(
		"seconds",
		defaultCaptureSeconds,
		"CPU window; the other profiles are snapshotted before and after it",
	)
	out := fs.String("out", "", "directory for the profiles and findings.json (default: .agentctl/perf/<time>)")
	remote := fs.Bool("allow-remote", false, "allow a non-loopback address")
	prefix := fs.String(
		"prefix",
		"",
		"comma-separated import path prefixes that count as our code (default: the workspace modules)",
	)
	top := fs.Int("top", defaultPerfTop, "rows per table")
	failOn := fs.Bool("fail-on-findings", false, "exit 1 when anything is found")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *url == "" {
		_, _ = fmt.Fprint(stderr, perfUsage)
		return exitUsage
	}

	prefixes := splitList(*prefix)
	if len(prefixes) == 0 {
		prefixes = modulePrefixes(env.cwd)
	}
	dir := *out
	if dir == "" {
		dir = filepath.Join(logDirFor(env.cwd), "..", "perf", time.Now().Format("20060102T150405"))
	}

	res, err := perf.Capture(ctx, perf.Config{
		BaseURL: *url, Seconds: *seconds, OutDir: dir, AllowRemote: *remote, Prefixes: prefixes, Top: *top,
	})
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "agentctl perf capture:", err)
		if errors.Is(err, context.Canceled) {
			return exitFail
		}
		return exitUsage
	}

	printCapture(stdout, res)
	if *failOn && len(res.Findings()) > 0 {
		return exitFail
	}
	return exitOK
}

func printCapture(w io.Writer, res perf.Result) {
	_, _ = fmt.Fprintf(w, "captured %d files in %s\n", len(res.Files), res.Dir)
	for _, name := range slices.Sorted(maps.Keys(res.Skipped)) {
		_, _ = fmt.Fprintf(w, "skipped %s: %s\n", name, res.Skipped[name])
	}

	found := res.Findings()
	if len(found) == 0 {
		_, _ = fmt.Fprintln(w, "no findings: nothing in our code stands out")
	}
	for i, nf := range found {
		_, _ = fmt.Fprintf(
			w,
			"%d. %s: %s: %s\n   next: %s\n",
			i+1,
			nf.Profile,
			nf.Finding.Kind,
			nf.Finding.Message,
			nf.Finding.Next,
		)
	}
	_, _ = fmt.Fprintf(
		w,
		"details: %s (agentctl perf analyze <file> for any one profile)\n",
		filepath.Join(res.Dir, "findings.json"),
	)
}
