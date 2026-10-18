// Package pprofan turns a pprof profile into a short, evidence-backed report an agent can act on: which of our
// functions the time, memory, lock wait, or goroutines belong to, and what to look at next.
//
// A raw profile ranks the leaf functions, which are usually the runtime (mallocgc, gopark). The useful question is
// who in our code called them, so every sample is also charged to the nearest frame in our own code.
package pprofan

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/pprof/profile"
)

// OutsideOurs is the owner of samples with no frame in our code, such as pure runtime or garbage collection work.
const OutsideOurs = "(outside our code)"

// The kinds of profile, which are also the hints a caller can give.
const (
	KindCPU           = "cpu"
	KindHeap          = "heap"
	KindMutex         = "mutex"
	KindBlock         = "block"
	KindContention    = "contention"
	KindGoroutine     = "goroutine"
	KindGoroutineLeak = "goroutineleak"
)

const (
	defaultTop            = 10
	defaultShareThreshold = 30.0
	defaultGroupThreshold = 50
	maxStackDepth         = 8
	stackPreview          = 3
	maxGroupFindings      = 3
	percent               = 100.0
)

// ErrUnknownProfile means the sample types are not ones this package knows how to read.
var ErrUnknownProfile = errors.New("unknown profile type")

type Options struct {
	// Prefixes are import path prefixes that mark our code. Package main always counts.
	Prefixes []string
	// Top bounds every list in the report.
	Top int
	// Hint names what a delay profile is (KindMutex or KindBlock) or marks a goroutine profile as the runtime's leak
	// profile (KindGoroutineLeak); the file contents do not always say.
	Hint string
	// ShareThreshold is the percent of a profile one owner must hold to be reported as a hotspot.
	ShareThreshold float64
	// GroupThreshold is how many goroutines must be parked at one stack to be reported as a pile-up.
	GroupThreshold int64
	// Growth marks a diff of two snapshots, so findings say growth rather than a state.
	Growth bool
}

type ProfileInfo struct {
	Kind          string `json:"kind"`
	Measure       string `json:"measure"`
	Unit          string `json:"unit"`
	Total         int64  `json:"total"`
	Samples       int    `json:"samples"`
	DurationNanos int64  `json:"duration_ns"`
}

type Hotspot struct {
	Function string  `json:"function"`
	File     string  `json:"file"`
	Line     int64   `json:"line"`
	Flat     int64   `json:"flat"`
	FlatPct  float64 `json:"flat_pct"`
	Cum      int64   `json:"cum"`
	CumPct   float64 `json:"cum_pct"`
	Ours     bool    `json:"ours"`
}

type Owner struct {
	Function string  `json:"function"`
	File     string  `json:"file"`
	Line     int64   `json:"line"`
	Value    int64   `json:"value"`
	Pct      float64 `json:"pct"`
}

type Group struct {
	Count int64    `json:"count"`
	Owner string   `json:"owner"`
	Stack []string `json:"stack"`
}

type Finding struct {
	Kind    string  `json:"kind"`
	Where   string  `json:"where"`
	Share   float64 `json:"share_pct"`
	Count   int64   `json:"count"`
	Message string  `json:"message"`
	Next    string  `json:"next"`
}

type Report struct {
	Profile    ProfileInfo `json:"profile"`
	Hotspots   []Hotspot   `json:"hotspots"`
	Owners     []Owner     `json:"owners"`
	Goroutines []Group     `json:"goroutines"`
	Findings   []Finding   `json:"findings"`
}

func (o Options) withDefaults() Options {
	if o.Top <= 0 {
		o.Top = defaultTop
	}
	if o.ShareThreshold <= 0 {
		o.ShareThreshold = defaultShareThreshold
	}
	if o.GroupThreshold <= 0 {
		o.GroupThreshold = defaultGroupThreshold
	}
	return o
}

// Parse reads a profile in pprof's protobuf format, gzipped or not.
func Parse(data []byte) (*profile.Profile, error) {
	return profile.ParseData(data)
}

// Diff returns after minus before, so only what grew between two snapshots remains positive.
func Diff(before, after *profile.Profile) (*profile.Profile, error) {
	negated := before.Copy()
	negated.Scale(-1)
	return profile.Merge([]*profile.Profile{after.Copy(), negated})
}

// AnalyzeDiff reports what grew between two snapshots of the same kind of profile.
func AnalyzeDiff(before, after *profile.Profile, opts Options) (Report, error) {
	diff, err := Diff(before, after)
	if err != nil {
		return Report{}, fmt.Errorf("diff profiles: %w", err)
	}
	opts.Growth = true
	return Analyze(diff, opts)
}

func hasType(p *profile.Profile, name string) bool {
	return slices.ContainsFunc(p.SampleType, func(v *profile.ValueType) bool { return v.Type == name })
}

func kindOf(p *profile.Profile, hint string) (string, error) {
	switch {
	case hasType(p, "cpu"):
		return KindCPU, nil
	case hasType(p, "alloc_space") || hasType(p, "inuse_space"):
		return KindHeap, nil
	case hasType(p, "delay"):
		if hint == KindMutex || hint == KindBlock {
			return hint, nil
		}
		return KindContention, nil
	case hasType(p, "goroutineleak"):
		return KindGoroutineLeak, nil
	case hasType(p, "goroutine"):
		if hint == KindGoroutineLeak {
			return hint, nil
		}
		return KindGoroutine, nil
	}
	return "", ErrUnknownProfile
}

// primaryIndex picks the measure pprof itself would show: the profile's default, else the last one.
func primaryIndex(p *profile.Profile) int {
	if p.DefaultSampleType != "" {
		for i, st := range p.SampleType {
			if st.Type == p.DefaultSampleType {
				return i
			}
		}
	}
	return len(p.SampleType) - 1
}

type frame struct {
	name string
	file string
	line int64
}

// framesOf lists a sample's functions leaf first, expanding inlined calls.
func framesOf(s *profile.Sample) []frame {
	var out []frame
	for _, loc := range s.Location {
		if len(loc.Line) == 0 {
			out = append(out, frame{name: fmt.Sprintf("0x%x", loc.Address)})
			continue
		}
		for _, ln := range loc.Line {
			if ln.Function != nil {
				out = append(out, frame{name: ln.Function.Name, file: ln.Function.Filename, line: ln.Line})
			}
		}
	}
	return out
}

type tally struct {
	flat, cum int64
	file      string
	line      int64
}

type ownerTally struct {
	value int64
	file  string
	line  int64
}

// collector accumulates the per-function and per-owner totals while the samples are walked.
type collector struct {
	isOurs    func(string) bool
	goroutine bool
	functions map[string]*tally
	owners    map[string]*ownerTally
	groups    []Group
	total     int64
	samples   int
}

func (c *collector) add(frames []frame, v int64) {
	c.total += v
	c.samples++

	seen := map[string]bool{}
	for i, f := range frames {
		t := c.functions[f.name]
		if t == nil {
			t = &tally{file: f.file, line: f.line}
			c.functions[f.name] = t
		}
		if i == 0 {
			t.flat += v
		}
		if !seen[f.name] {
			seen[f.name] = true
			t.cum += v
		}
	}

	owner, ownerFrame := c.ownerOf(frames)
	ot := c.owners[owner]
	if ot == nil {
		ot = &ownerTally{file: ownerFrame.file, line: ownerFrame.line}
		c.owners[owner] = ot
	}
	ot.value += v

	if c.goroutine {
		c.groups = append(c.groups, groupOf(frames, owner, v))
	}
}

// ownerOf walks from the leaf up and returns the first frame in our code.
func (c *collector) ownerOf(frames []frame) (string, frame) {
	for _, f := range frames {
		if c.isOurs(f.name) {
			return f.name, f
		}
	}
	return OutsideOurs, frame{}
}

// Analyze summarises one profile.
func Analyze(p *profile.Profile, opts Options) (Report, error) {
	opts = opts.withDefaults()
	kind, err := kindOf(p, opts.Hint)
	if err != nil {
		return Report{}, err
	}
	idx := primaryIndex(p)

	c := &collector{
		isOurs: func(name string) bool {
			return strings.HasPrefix(name, "main.") || slices.ContainsFunc(opts.Prefixes, func(pre string) bool {
				return strings.HasPrefix(name, pre)
			})
		},
		goroutine: kind == KindGoroutine || kind == KindGoroutineLeak,
		functions: map[string]*tally{},
		owners:    map[string]*ownerTally{},
	}
	for _, s := range p.Sample {
		if idx >= len(s.Value) || s.Value[idx] <= 0 {
			continue
		}
		if frames := framesOf(s); len(frames) > 0 {
			c.add(frames, s.Value[idx])
		}
	}

	r := Report{
		Profile: ProfileInfo{
			Kind: kind, Measure: p.SampleType[idx].Type, Unit: p.SampleType[idx].Unit,
			Total: c.total, Samples: c.samples, DurationNanos: p.DurationNanos,
		},
		Hotspots: []Hotspot{}, Owners: []Owner{}, Goroutines: []Group{}, Findings: []Finding{},
	}
	fill(&r, c, opts)
	r.Findings = findings(r, opts)
	return r, nil
}

func groupOf(frames []frame, owner string, count int64) Group {
	stack := make([]string, 0, maxStackDepth)
	for _, f := range frames {
		if len(stack) == maxStackDepth {
			break
		}
		stack = append(stack, f.name)
	}
	return Group{Count: count, Owner: owner, Stack: stack}
}

func pct(v, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(v) / float64(total) * percent
}

func fill(r *Report, c *collector, opts Options) {
	total := r.Profile.Total

	for name, t := range c.functions {
		r.Hotspots = append(r.Hotspots, Hotspot{
			Function: name, File: t.file, Line: t.line,
			Flat: t.flat, FlatPct: pct(t.flat, total), Cum: t.cum, CumPct: pct(t.cum, total), Ours: c.isOurs(name),
		})
	}
	slices.SortFunc(r.Hotspots, func(a, b Hotspot) int {
		return cmpDesc(a.Flat, b.Flat, a.Cum, b.Cum, a.Function, b.Function)
	})

	for name, o := range c.owners {
		r.Owners = append(
			r.Owners,
			Owner{Function: name, File: o.file, Line: o.line, Value: o.value, Pct: pct(o.value, total)},
		)
	}
	slices.SortFunc(r.Owners, func(a, b Owner) int { return cmpDesc(a.Value, b.Value, 0, 0, a.Function, b.Function) })

	slices.SortFunc(c.groups, func(a, b Group) int { return cmpDesc(a.Count, b.Count, 0, 0, a.Owner, b.Owner) })
	r.Goroutines = append(r.Goroutines, c.groups...)

	r.Hotspots = truncate(r.Hotspots, opts.Top)
	r.Owners = truncate(r.Owners, opts.Top)
	r.Goroutines = truncate(r.Goroutines, opts.Top)
}

// cmpDesc orders by two numbers descending, then by name so reports are stable.
func cmpDesc(a1, b1, a2, b2 int64, aName, bName string) int {
	switch {
	case a1 != b1:
		if a1 > b1 {
			return -1
		}
		return 1
	case a2 != b2:
		if a2 > b2 {
			return -1
		}
		return 1
	}
	return strings.Compare(aName, bName)
}

func truncate[T any](list []T, n int) []T {
	if len(list) > n {
		return list[:n]
	}
	return list
}
