// Package perf captures profiles from a running service's pprof endpoint and runs them through the analyzer.
//
// Capture is built so profiling can never hurt the service it inspects: only loopback addresses unless explicitly
// allowed, a hard cap on the CPU window, a deadline on every request, and bounded response sizes.
package perf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/google/pprof/profile"

	"github.com/tienduc120/go-base/tools/internal/pprofan"
)

const (
	// MaxSeconds caps the CPU window: a profile longer than this is a load test, not a diagnosis.
	MaxSeconds = 30

	// snapshotLimit is how long one non-CPU request may take; cpuSlack is the time beyond the CPU window before giving up.
	snapshotLimit = 10 * time.Second
	cpuSlack      = 15 * time.Second
	maxBody       = 64 << 20

	fileMode = 0o600
	dirMode  = 0o750
)

type Config struct {
	// BaseURL is the service's pprof server, such as http://127.0.0.1:6060.
	BaseURL string
	// Seconds is the CPU window; the other profiles are snapshotted before and after it.
	Seconds int
	OutDir  string
	// AllowRemote permits a non-loopback address. Profiling endpoints belong on an internal port.
	AllowRemote bool
	// Prefixes mark our code in the analysis.
	Prefixes []string
	Top      int
	// Client is optional; the default has no overall timeout because every request carries its own deadline.
	Client *http.Client
}

type Result struct {
	Dir     string                    `json:"dir"`
	Files   []string                  `json:"files"`
	Reports map[string]pprofan.Report `json:"reports"`
	Skipped map[string]string         `json:"skipped"`
}

type NamedFinding struct {
	Profile string
	Finding pprofan.Finding
}

// profileOrder is the order reports are listed in; a function rather than a variable keeps it read-only.
func profileOrder() []string {
	return []string{"cpu", "heap", "allocs", "goroutine", "goroutineleak", "mutex", "block"}
}

// Findings flattens every report's findings in a stable order, each tagged with its profile.
func (r Result) Findings() []NamedFinding {
	var out []NamedFinding
	for _, name := range profileOrder() {
		for _, f := range r.Reports[name].Findings {
			out = append(out, NamedFinding{Profile: name, Finding: f})
		}
	}
	return out
}

type statusError struct{ code int }

func (e statusError) Error() string { return "HTTP " + strconv.Itoa(e.code) }

type snapshot struct {
	name string
	hint string
}

func snapshots() []snapshot {
	return []snapshot{
		{"heap", ""},
		{"allocs", ""},
		{"goroutine", ""},
		{"mutex", pprofan.KindMutex},
		{"block", pprofan.KindBlock},
	}
}

func validate(cfg Config) (string, error) {
	if cfg.Seconds < 1 || cfg.Seconds > MaxSeconds {
		return "", fmt.Errorf("seconds must be between 1 and %d, got %d", MaxSeconds, cfg.Seconds)
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", fmt.Errorf("%q is not an http(s) address like http://127.0.0.1:6060", cfg.BaseURL)
	}
	if !cfg.AllowRemote && !isLoopback(u.Hostname()) {
		return "", fmt.Errorf(
			"%s is not a loopback address; a profiling endpoint stays internal (pass --allow-remote to override)",
			u.Hostname(),
		)
	}
	return u.Scheme + "://" + u.Host, nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Capture takes before snapshots, runs the CPU window, takes after snapshots, and analyses everything.
func Capture(ctx context.Context, cfg Config) (Result, error) {
	base, err := validate(cfg)
	if err != nil {
		return Result{}, err
	}
	if err = os.MkdirAll(cfg.OutDir, dirMode); err != nil {
		return Result{}, err
	}

	c := &capturer{
		cfg:    cfg,
		base:   base,
		client: cfg.Client,
		res: Result{
			Dir:     cfg.OutDir,
			Files:   []string{},
			Reports: map[string]pprofan.Report{},
			Skipped: map[string]string{},
		},
	}
	if c.client == nil {
		c.client = &http.Client{}
	}

	before := c.takeSnapshots(ctx, "before", nil)
	if err = ctx.Err(); err != nil {
		return Result{}, err
	}

	window := time.Duration(cfg.Seconds)*time.Second + cpuSlack
	cpu, err := c.fetch(ctx, "/debug/pprof/profile?seconds="+strconv.Itoa(cfg.Seconds), window)
	if err != nil {
		return Result{}, fmt.Errorf("cpu profile: %w", err)
	}
	c.save("cpu.pprof", cpu)
	c.analyse("cpu", pprofan.Options{}, nil, cpu)

	c.takeSnapshots(ctx, "after", before)
	c.leakProfile(ctx)

	if err = c.writeFindings(); err != nil {
		return c.res, err
	}
	return c.res, nil
}

type capturer struct {
	cfg    Config
	base   string
	client *http.Client
	res    Result
}

// takeSnapshots fetches every snapshot profile. Before the window it returns what it got; after, it diffs against that.
func (c *capturer) takeSnapshots(ctx context.Context, phase string, before map[string][]byte) map[string][]byte {
	got := map[string][]byte{}
	for _, s := range snapshots() {
		if phase == "after" && before[s.name] == nil {
			continue
		}

		data, err := c.fetch(ctx, "/debug/pprof/"+s.name, snapshotLimit)
		if err != nil {
			c.res.Skipped[s.name] = reason(err)
			continue
		}
		c.save(s.name+"-"+phase+".pprof", data)
		got[s.name] = data
		if phase == "after" {
			c.analyse(s.name, pprofan.Options{Hint: s.hint}, before[s.name], data)
		}
	}
	return got
}

// leakProfile asks for the runtime's own leak profile, which only exists from Go 1.27.
func (c *capturer) leakProfile(ctx context.Context) {
	data, err := c.fetch(ctx, "/debug/pprof/goroutineleak", snapshotLimit)
	if err != nil {
		c.res.Skipped["goroutineleak"] = reason(err)
		return
	}
	c.save("goroutineleak.pprof", data)
	c.analyse("goroutineleak", pprofan.Options{Hint: pprofan.KindGoroutineLeak}, nil, data)
}

func reason(err error) string {
	if se, ok := errors.AsType[statusError](err); ok {
		return se.Error()
	}
	return err.Error()
}

func (c *capturer) analyse(name string, opts pprofan.Options, before, after []byte) {
	opts.Prefixes, opts.Top = c.cfg.Prefixes, c.cfg.Top

	cur, err := pprofan.Parse(after)
	if err != nil {
		c.res.Skipped[name] = "unreadable profile: " + err.Error()
		return
	}
	var report pprofan.Report
	if before == nil {
		report, err = pprofan.Analyze(cur, opts)
	} else {
		report, err = diffReport(before, cur, opts)
	}
	if err != nil {
		c.res.Skipped[name] = "cannot analyse: " + err.Error()
		return
	}
	c.res.Reports[name] = report
}

func diffReport(before []byte, after *profile.Profile, opts pprofan.Options) (pprofan.Report, error) {
	prev, err := pprofan.Parse(before)
	if err != nil {
		return pprofan.Report{}, err
	}
	return pprofan.AnalyzeDiff(prev, after, opts)
}

func (c *capturer) save(name string, data []byte) {
	if err := os.WriteFile(filepath.Join(c.cfg.OutDir, name), data, fileMode); err == nil {
		c.res.Files = append(c.res.Files, name)
	}
}

func (c *capturer) writeFindings() error {
	data, err := json.MarshalIndent(c.res, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(c.cfg.OutDir, "findings.json"), data, fileMode)
}

func (c *capturer) fetch(ctx context.Context, path string, limit time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, statusError{code: resp.StatusCode}
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBody))
}
