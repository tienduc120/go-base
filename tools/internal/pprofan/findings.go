package pprofan

import (
	"fmt"
	"strings"
)

// The kinds of finding.
const (
	FindingOutsideOurCode  = "outside-our-code"
	FindingCPUHotspot      = "cpu-hotspot"
	FindingAllocHotspot    = "alloc-hotspot"
	FindingMemoryRetention = "memory-retention"
	FindingMemoryGrowth    = "memory-growth"
	FindingLockContention  = "lock-contention"
	FindingBlocking        = "blocking"
	FindingContention      = "contention"
	FindingGoroutinePileup = "goroutine-pileup"
	FindingGoroutineGrowth = "goroutine-growth"
	FindingGoroutineLeak   = "goroutine-leak"
	leakThreshold          = 1
	gcPressureNext         = "Capture an allocs profile and check garbage collection pressure, then look at the dependency calls that dominate."
	goroutineNext          = "Give every goroutine here an owner that cancels it (a context) and bound whatever starts them; a goroutine nobody can stop is a leak."
	inuseMeasurePrefix     = "inuse"
	allocMeasurePrefix     = "alloc"
	stackSeparator         = " <- "
)

// findings turns the numbers into at most a few claims, each naming where and what to do next.
func findings(r Report, opts Options) []Finding {
	out := []Finding{}
	switch r.Profile.Kind {
	case KindGoroutine, KindGoroutineLeak:
		return append(out, goroutineFindings(r, opts)...)
	}
	if len(r.Owners) == 0 {
		return out
	}

	top := r.Owners[0]
	if top.Pct < opts.ShareThreshold {
		return out
	}
	if top.Function == OutsideOurs {
		// Only CPU and allocation churn say something when the runtime owns the cost (garbage collection pressure).
		// For retained memory, lock delay, and blocking it is just the runtime's own bookkeeping.
		if !meansGCPressure(r.Profile) {
			return out
		}
		return append(out, Finding{
			Kind:  FindingOutsideOurCode,
			Where: OutsideOurs,
			Share: top.Pct,
			Message: fmt.Sprintf(
				"%.0f%% of the %s is spent outside our code (runtime, garbage collection, or a dependency)",
				top.Pct,
				r.Profile.Measure,
			),
			Next: gcPressureNext,
		})
	}

	kind := hotspotKind(r.Profile, opts)
	return append(out, Finding{
		Kind:  kind,
		Where: top.Function,
		Share: top.Pct,
		Message: fmt.Sprintf(
			"%s accounts for %.0f%% of the %s (%s)",
			top.Function,
			top.Pct,
			r.Profile.Measure,
			describe(kind),
		),
		Next: advice(kind, top.Function),
	})
}

func meansGCPressure(info ProfileInfo) bool {
	return info.Kind == KindCPU || (info.Kind == KindHeap && strings.HasPrefix(info.Measure, allocMeasurePrefix))
}

func hotspotKind(info ProfileInfo, opts Options) string {
	switch info.Kind {
	case KindCPU:
		return FindingCPUHotspot
	case KindHeap:
		inuse := strings.HasPrefix(info.Measure, inuseMeasurePrefix)
		switch {
		case inuse && opts.Growth:
			return FindingMemoryGrowth
		case inuse:
			return FindingMemoryRetention
		}
		return FindingAllocHotspot
	case KindMutex:
		return FindingLockContention
	case KindBlock:
		return FindingBlocking
	}
	return FindingContention
}

func describe(kind string) string {
	switch kind {
	case FindingCPUHotspot:
		return "CPU time is concentrated here"
	case FindingAllocHotspot:
		return "this is where the allocations come from"
	case FindingMemoryRetention, FindingMemoryGrowth:
		return "this is what keeps memory alive"
	case FindingLockContention:
		return "goroutines wait here for a lock"
	case FindingBlocking:
		return "goroutines wait here on a channel, pool, or call"
	}
	return "goroutines wait here"
}

func advice(kind, where string) string {
	switch kind {
	case FindingCPUHotspot:
		return "Read " + where + " for work repeated per call (regexp compile, string building, copies, a linear search) " +
			"and fix the biggest one, then capture again and compare."
	case FindingAllocHotspot:
		return "Look in " + where + " for allocations inside a loop: preallocate slices and maps, reuse buffers, " +
			"avoid converting between string and []byte."
	case FindingMemoryRetention, FindingMemoryGrowth:
		return "Find what keeps the objects reachable from " + where +
			": a cache with no bound, a goroutine holding a reference, a slice of a large array."
	case FindingLockContention:
		return "Shorten the critical section in " + where + ", shard the lock, or replace it with an atomic or a channel hand-off."
	case FindingBlocking:
		return "Find what " + where + " waits for (an unbuffered channel, a slow dependency, a pool at capacity) " +
			"and give it a deadline and a bound."
	}
	return "Read " + where + " to see what it waits for, then bound it with a deadline."
}

func goroutineFindings(r Report, opts Options) []Finding {
	threshold := opts.GroupThreshold
	kind := FindingGoroutinePileup
	switch {
	case r.Profile.Kind == KindGoroutineLeak:
		// The runtime already decided these can never run again, so even one is a finding.
		kind, threshold = FindingGoroutineLeak, leakThreshold
	case opts.Growth:
		kind = FindingGoroutineGrowth
	}

	out := []Finding{}
	for _, g := range r.Goroutines {
		if g.Count < threshold || len(out) == maxGroupFindings {
			continue
		}
		where := g.Owner
		if where == OutsideOurs && len(g.Stack) > 0 {
			where = g.Stack[len(g.Stack)-1]
		}
		out = append(out, Finding{
			Kind:  kind,
			Where: where,
			Count: g.Count,
			Message: fmt.Sprintf(
				"%d goroutines are parked at %s (%s)",
				g.Count,
				where,
				strings.Join(g.Stack[:min(stackPreview, len(g.Stack))], stackSeparator),
			),
			Next: goroutineNext,
		})
	}
	return out
}
