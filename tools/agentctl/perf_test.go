package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/pprof/profile"
)

const shop = "example.com/root"

func writeProfile(t *testing.T, name string, sampleTypes [][2]string, frames []string, values []int64) string {
	t.Helper()
	data := profileBytes(t, sampleTypes, frames, values)
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func profileBytes(t *testing.T, sampleTypes [][2]string, frames []string, values []int64) []byte {
	t.Helper()
	p := &profile.Profile{}
	for _, st := range sampleTypes {
		p.SampleType = append(p.SampleType, &profile.ValueType{Type: st[0], Unit: st[1]})
	}
	var locs []*profile.Location
	for i, name := range frames {
		fn := &profile.Function{ID: uint64(i + 1), Name: name, Filename: "f.go"}
		loc := &profile.Location{ID: uint64(i + 1), Line: []profile.Line{{Function: fn, Line: 7}}}
		p.Function = append(p.Function, fn)
		p.Location = append(p.Location, loc)
		locs = append(locs, loc)
	}
	p.Sample = []*profile.Sample{{Location: locs, Value: values}}
	var buf bytes.Buffer
	if err := p.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func cpuTypes() [][2]string { return [][2]string{{"samples", "count"}, {"cpu", "nanoseconds"}} }

func TestPerfAnalyzeNamesTheHotFunction(t *testing.T) {
	t.Parallel()

	file := writeProfile(
		t,
		"cpu.pprof",
		cpuTypes(),
		[]string{"runtime.mallocgc", shop + ".parse"},
		[]int64{9, 90_000_000},
	)
	got := invoke(t, environment{cwd: verifyRepo(t)}, "perf", "analyze", file)

	if got.code != 0 {
		t.Fatalf("exit = %d\n%s\n%s", got.code, got.stdout, got.stderr)
	}
	for _, want := range []string{"cpu-hotspot", shop + ".parse", "next:"} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, got.stdout)
		}
	}
}

func TestPerfAnalyzeJSON(t *testing.T) {
	t.Parallel()

	file := writeProfile(t, "cpu.pprof", cpuTypes(), []string{shop + ".parse"}, []int64{9, 90_000_000})
	got := invoke(t, environment{cwd: verifyRepo(t)}, "perf", "analyze", "--json", file)

	var report struct {
		Findings []struct {
			Kind  string `json:"kind"`
			Where string `json:"where"`
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(got.stdout), &report); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, got.stdout)
	}
	if len(report.Findings) != 1 || report.Findings[0].Where != shop+".parse" {
		t.Errorf("findings = %+v", report.Findings)
	}
}

func TestPerfAnalyzeFailOnFindings(t *testing.T) {
	t.Parallel()

	hot := writeProfile(t, "cpu.pprof", cpuTypes(), []string{shop + ".parse"}, []int64{9, 90_000_000})
	calm := writeProfile(t, "calm.pprof", cpuTypes(), []string{"runtime.gcBgMarkWorker"}, []int64{0, 0})
	repo := verifyRepo(t)

	if got := invoke(t, environment{cwd: repo}, "perf", "analyze", "--fail-on-findings", hot); got.code != 1 {
		t.Errorf("a hotspot with --fail-on-findings: exit = %d, want 1", got.code)
	}
	if got := invoke(t, environment{cwd: repo}, "perf", "analyze", "--fail-on-findings", calm); got.code != 0 {
		t.Errorf("nothing to report: exit = %d, want 0", got.code)
	}
	if got := invoke(t, environment{cwd: repo}, "perf", "analyze", hot); got.code != 0 {
		t.Errorf("without the flag a finding is information, not failure: exit = %d", got.code)
	}
}

func TestPerfAnalyzeDiffsAgainstABase(t *testing.T) {
	t.Parallel()

	types := [][2]string{{"goroutine", "count"}}
	before := writeProfile(
		t,
		"goroutine-before.pprof",
		types,
		[]string{"runtime.gopark", shop + ".worker"},
		[]int64{10},
	)
	after := writeProfile(t, "goroutine-after.pprof", types, []string{"runtime.gopark", shop + ".worker"}, []int64{310})

	got := invoke(t, environment{cwd: verifyRepo(t)}, "perf", "analyze", "--base", before, after)

	if got.code != 0 || !strings.Contains(got.stdout, "goroutine-growth") || !strings.Contains(got.stdout, "300") {
		t.Errorf("exit = %d, want growth of 300 reported:\n%s\n%s", got.code, got.stdout, got.stderr)
	}
}

func TestPerfAnalyzeReadsTheKindFromTheFileName(t *testing.T) {
	t.Parallel()

	delay := [][2]string{{"contentions", "count"}, {"delay", "nanoseconds"}}
	file := writeProfile(t, "mutex-after.pprof", delay, []string{shop + ".guarded"}, []int64{5, 5_000_000})

	got := invoke(t, environment{cwd: verifyRepo(t)}, "perf", "analyze", file)
	if !strings.Contains(got.stdout, "lock-contention") {
		t.Errorf("a file named mutex-* is a mutex profile:\n%s", got.stdout)
	}
	if got = invoke(
		t,
		environment{cwd: verifyRepo(t)},
		"perf",
		"analyze",
		"--kind",
		"block",
		file,
	); !strings.Contains(
		got.stdout,
		"blocking",
	) {
		t.Errorf("--kind overrides the name:\n%s", got.stdout)
	}
}

func TestPerfAnalyzeRejectsBadInput(t *testing.T) {
	t.Parallel()

	garbage := filepath.Join(t.TempDir(), "x.pprof")
	if err := os.WriteFile(garbage, []byte("not a profile"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := verifyRepo(t)
	for name, args := range map[string][]string{
		"no file":      {"perf", "analyze"},
		"missing file": {"perf", "analyze", filepath.Join(t.TempDir(), "nope.pprof")},
		"garbage":      {"perf", "analyze", garbage},
		"no subcmd":    {"perf"},
		"unknown":      {"perf", "bogus"},
	} {
		if got := invoke(t, environment{cwd: repo}, args...); got.code != 2 {
			t.Errorf("%s: exit = %d, want 2", name, got.code)
		}
	}
}

func cpuOnlyService(t *testing.T) *httptest.Server {
	t.Helper()
	cpu := profileBytes(t, cpuTypes(), []string{shop + ".hot"}, []int64{8, 80_000_000})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/debug/pprof/profile" {
			_, _ = w.Write(cpu)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPerfCaptureReportsFindingsAndWhatItSkipped(t *testing.T) {
	t.Parallel()

	srv := cpuOnlyService(t)
	out := filepath.Join(t.TempDir(), "run")
	got := invoke(
		t,
		environment{cwd: verifyRepo(t)},
		"perf",
		"capture",
		"--url",
		srv.URL,
		"--seconds",
		"1",
		"--out",
		out,
	)

	if got.code != 0 {
		t.Fatalf("exit = %d\n%s\n%s", got.code, got.stdout, got.stderr)
	}
	for _, want := range []string{"cpu-hotspot", shop + ".hot", "skipped", "goroutineleak", out} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, got.stdout)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "findings.json")); err != nil {
		t.Errorf("findings.json: %v", err)
	}
}

func TestPerfCaptureRefusesARemoteAddressAndAnUnboundedWindow(t *testing.T) {
	t.Parallel()

	repo := verifyRepo(t)
	remote := invoke(t, environment{cwd: repo}, "perf", "capture", "--url", "http://10.9.8.7:6060", "--seconds", "1")
	if remote.code != 2 || !strings.Contains(remote.stderr, "allow-remote") {
		t.Errorf("remote: exit = %d, stderr = %q", remote.code, remote.stderr)
	}
	long := invoke(t, environment{cwd: repo}, "perf", "capture", "--url", "http://127.0.0.1:1", "--seconds", "999")
	if long.code != 2 {
		t.Errorf("a 999-second window: exit = %d, want 2", long.code)
	}
}

func TestPerfCaptureNeedsAURL(t *testing.T) {
	t.Parallel()

	if got := invoke(t, environment{cwd: verifyRepo(t)}, "perf", "capture"); got.code != 2 {
		t.Errorf("exit = %d, want 2", got.code)
	}
}
