package pprofan

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	nsPerMicro = 1e3
	nsPerMilli = 1e6
	nsPerSec   = 1e9
	bytesPerKB = 1 << 10
	bytesPerMB = 1 << 20
	decimals   = 10.0
)

// FormatValue renders a sample value in its own unit, trimmed to what a person reads at a glance.
func FormatValue(v int64, unit string) string {
	f := float64(v)
	switch unit {
	case "nanoseconds":
		switch {
		case f >= nsPerSec:
			return trim(f/nsPerSec) + "s"
		case f >= nsPerMilli:
			return trim(f/nsPerMilli) + "ms"
		case f >= nsPerMicro:
			return trim(f/nsPerMicro) + "µs"
		}
		return strconv.FormatInt(v, 10) + "ns"
	case "bytes":
		switch {
		case f >= bytesPerMB:
			return trim(f/bytesPerMB) + "MB"
		case f >= bytesPerKB:
			return trim(f/bytesPerKB) + "KB"
		}
		return strconv.FormatInt(v, 10) + "B"
	}
	return strconv.FormatInt(v, 10)
}

func trim(x float64) string {
	return strconv.FormatFloat(math.Round(x*decimals)/decimals, 'f', -1, 64)
}

// Summary is the report as a person or an agent reads it first: findings, then who owns the cost, then the leaves.
// top bounds the rows of each table.
func (r Report) Summary(top int) string {
	var b strings.Builder
	info := r.Profile
	fmt.Fprintf(
		&b,
		"%s profile: %s %s across %d samples\n",
		info.Kind,
		FormatValue(info.Total, info.Unit),
		info.Measure,
		info.Samples,
	)

	if len(r.Findings) == 0 {
		b.WriteString("no findings: nothing in our code holds enough of the profile to single out\n")
	}
	for i, f := range r.Findings {
		fmt.Fprintf(&b, "%d. %s: %s\n   next: %s\n", i+1, f.Kind, f.Message, f.Next)
	}

	b.WriteString("\nowners (the cost charged to the nearest function in our code):\n")
	for _, o := range r.Owners[:min(top, len(r.Owners))] {
		fmt.Fprintf(
			&b,
			"  %5.1f%%  %s  %s\n",
			o.Pct,
			FormatValue(o.Value, info.Unit),
			location(o.Function, o.File, o.Line),
		)
	}

	if len(r.Goroutines) > 0 {
		b.WriteString("\ngoroutines (largest groups first):\n")
		for _, g := range r.Goroutines[:min(top, len(r.Goroutines))] {
			fmt.Fprintf(
				&b,
				"  %5d  %s  <-  %s\n",
				g.Count,
				g.Owner,
				strings.Join(g.Stack[:min(stackPreview, len(g.Stack))], " <- "),
			)
		}
	}

	b.WriteString("\nhotspots (the leaf functions, which are often the runtime):\n")
	for _, h := range r.Hotspots[:min(top, len(r.Hotspots))] {
		fmt.Fprintf(
			&b,
			"  %5.1f%%  %s  %s\n",
			h.FlatPct,
			FormatValue(h.Flat, info.Unit),
			location(h.Function, h.File, h.Line),
		)
	}
	return b.String()
}

func location(function, file string, line int64) string {
	if file == "" {
		return function
	}
	return fmt.Sprintf("%s (%s:%d)", function, file, line)
}
