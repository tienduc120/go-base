package perf_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/pprof/profile"

	"github.com/tienduc120/go-base/tools/internal/perf"
)

const ours = "github.com/acme/shop"

func encode(t *testing.T, sampleTypes [][2]string, frames []string, values []int64) []byte {
	t.Helper()
	p := &profile.Profile{}
	for _, st := range sampleTypes {
		p.SampleType = append(p.SampleType, &profile.ValueType{Type: st[0], Unit: st[1]})
	}
	var locs []*profile.Location
	for i, name := range frames {
		fn := &profile.Function{ID: uint64(i + 1), Name: name, Filename: "f.go"}
		loc := &profile.Location{ID: uint64(i + 1), Line: []profile.Line{{Function: fn, Line: 1}}}
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

type fakeService struct {
	srv        *httptest.Server
	cpuSeconds atomic.Value
	goroutines atomic.Int32
	status     map[string]int
}

func newService(t *testing.T) *fakeService {
	t.Helper()
	f := &fakeService{status: map[string]int{}}
	delay := [][2]string{{"contentions", "count"}, {"delay", "nanoseconds"}}
	heap := [][2]string{
		{"alloc_objects", "count"},
		{"alloc_space", "bytes"},
		{"inuse_objects", "count"},
		{"inuse_space", "bytes"},
	}

	mux := http.NewServeMux()
	serve := func(path string, body func() []byte) {
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			if code := f.status[path]; code != 0 {
				http.Error(w, "no", code)
				return
			}
			_, _ = w.Write(body())
		})
	}
	mux.HandleFunc("/debug/pprof/profile", func(w http.ResponseWriter, r *http.Request) {
		f.cpuSeconds.Store(r.URL.Query().Get("seconds"))
		if code := f.status["/debug/pprof/profile"]; code != 0 {
			http.Error(w, "no", code)
			return
		}
		_, _ = w.Write(
			encode(
				t,
				[][2]string{{"samples", "count"}, {"cpu", "nanoseconds"}},
				[]string{ours + ".hot"},
				[]int64{8, 80_000_000},
			),
		)
	})
	serve("/debug/pprof/goroutine", func() []byte {
		count := int64(10)
		if f.goroutines.Add(1) > 1 {
			count = 300
		}
		return encode(
			t,
			[][2]string{{"goroutine", "count"}},
			[]string{"runtime.gopark", ours + ".worker"},
			[]int64{count},
		)
	})
	for _, name := range []string{"mutex", "block"} {
		serve("/debug/pprof/"+name, func() []byte {
			return encode(t, delay, []string{ours + ".steady"}, []int64{1, 100})
		})
	}
	for _, name := range []string{"heap", "allocs"} {
		serve("/debug/pprof/"+name, func() []byte {
			return encode(t, heap, []string{ours + ".steady"}, []int64{1, 100, 1, 100})
		})
	}
	mux.HandleFunc("/debug/pprof/goroutineleak", func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	})

	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func config(f *fakeService, out string) perf.Config {
	return perf.Config{BaseURL: f.srv.URL, Seconds: 2, OutDir: out, Prefixes: []string{ours}}
}

func TestCaptureTakesSnapshotsAroundTheCPUWindowAndDiffsThem(t *testing.T) {
	t.Parallel()

	f := newService(t)
	out := filepath.Join(t.TempDir(), "run")

	res, err := perf.Capture(t.Context(), config(f, out))
	if err != nil {
		t.Fatal(err)
	}

	if f.cpuSeconds.Load() != "2" {
		t.Errorf("cpu seconds = %v, want the configured 2", f.cpuSeconds.Load())
	}
	for _, name := range []string{"cpu.pprof", "goroutine-before.pprof", "goroutine-after.pprof", "findings.json"} {
		if _, statErr := os.Stat(filepath.Join(out, name)); statErr != nil {
			t.Errorf("missing %s: %v", name, statErr)
		}
	}

	cpu := res.Reports["cpu"]
	if len(cpu.Findings) != 1 || cpu.Findings[0].Where != ours+".hot" {
		t.Errorf("cpu findings = %+v", cpu.Findings)
	}
	gr := res.Reports["goroutine"]
	if len(gr.Findings) != 1 || gr.Findings[0].Kind != "goroutine-growth" || gr.Findings[0].Count != 290 {
		t.Errorf("goroutine findings = %+v, want 290 more goroutines at worker", gr.Findings)
	}
	if got := res.Reports["mutex"]; len(got.Findings) != 0 {
		t.Errorf("a steady profile has nothing to report: %+v", got.Findings)
	}
}

func TestCaptureWritesAMachineReadableFindingsFile(t *testing.T) {
	t.Parallel()

	f := newService(t)
	out := filepath.Join(t.TempDir(), "run")
	if _, err := perf.Capture(t.Context(), config(f, out)); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(out, "findings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]json.RawMessage
	if err = json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("findings.json is not JSON: %v", err)
	}
	for _, key := range []string{"reports", "skipped"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("findings.json lacks %q: %s", key, data)
		}
	}
}

func TestCaptureSkipsWhatTheServiceDoesNotOfferAndSaysWhy(t *testing.T) {
	t.Parallel()

	f := newService(t)
	f.status["/debug/pprof/mutex"] = http.StatusInternalServerError

	res, err := perf.Capture(t.Context(), config(f, filepath.Join(t.TempDir(), "run")))
	if err != nil {
		t.Fatal(err)
	}

	if reason := res.Skipped["goroutineleak"]; !strings.Contains(reason, "404") {
		t.Errorf("goroutineleak is missing before Go 1.27 and must be named as skipped: %q", reason)
	}
	if reason := res.Skipped["mutex"]; !strings.Contains(reason, "500") {
		t.Errorf("a failing optional profile is skipped with its status: %q", reason)
	}
	if _, ok := res.Reports["cpu"]; !ok {
		t.Error("the rest of the capture must still happen")
	}
}

func TestCaptureFailsWhenTheCPUProfileFails(t *testing.T) {
	t.Parallel()

	f := newService(t)
	f.status["/debug/pprof/profile"] = http.StatusInternalServerError

	_, err := perf.Capture(t.Context(), config(f, filepath.Join(t.TempDir(), "run")))
	if err == nil || !strings.Contains(err.Error(), "cpu") {
		t.Errorf("err = %v, want one naming the cpu profile", err)
	}
}

func TestCaptureRefusesAnythingButLoopbackUnlessAllowed(t *testing.T) {
	t.Parallel()

	cfg := perf.Config{BaseURL: "http://10.1.2.3:6060", Seconds: 5, OutDir: t.TempDir()}
	_, err := perf.Capture(t.Context(), cfg)
	if err == nil || !strings.Contains(err.Error(), "allow-remote") {
		t.Errorf("err = %v, want a refusal that names --allow-remote: profiling endpoints stay internal", err)
	}
}

func TestCaptureBoundsTheDuration(t *testing.T) {
	t.Parallel()

	f := newService(t)
	for _, seconds := range []int{0, -1, perf.MaxSeconds + 1} {
		cfg := config(f, t.TempDir())
		cfg.Seconds = seconds
		if _, err := perf.Capture(t.Context(), cfg); err == nil {
			t.Errorf("%d seconds must be refused: an unbounded capture is how profiling hangs a service", seconds)
		}
	}
}

func TestCaptureRejectsANonHTTPAddress(t *testing.T) {
	t.Parallel()

	for _, base := range []string{"", "ftp://127.0.0.1:6060", "127.0.0.1:6060", "http://"} {
		cfg := perf.Config{BaseURL: base, Seconds: 2, OutDir: t.TempDir()}
		if _, err := perf.Capture(t.Context(), cfg); err == nil {
			t.Errorf("%q must be rejected", base)
		}
	}
}

func TestCaptureStopsWhenCancelled(t *testing.T) {
	t.Parallel()

	f := newService(t)
	ctx, cancel := contextWithCancel(t)
	cancel()

	if _, err := perf.Capture(ctx, config(f, filepath.Join(t.TempDir(), "run"))); err == nil {
		t.Error("a cancelled capture must stop, not keep profiling")
	}
}

func TestResultFindingsAreFlattenedWithTheirProfile(t *testing.T) {
	t.Parallel()

	f := newService(t)
	res, err := perf.Capture(t.Context(), config(f, filepath.Join(t.TempDir(), "run")))
	if err != nil {
		t.Fatal(err)
	}

	got := res.Findings()
	if len(got) != 2 {
		t.Fatalf("findings = %+v, want the cpu hotspot and the goroutine growth", got)
	}
	if got[0].Profile != "cpu" || got[1].Profile != "goroutine" {
		t.Errorf("profiles = %q, %q, want cpu then goroutine in a stable order", got[0].Profile, got[1].Profile)
	}
}
