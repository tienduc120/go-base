package debt

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tienduc120/go-base/tools/internal/proc"
)

const (
	coverageTimeout = 10 * time.Minute
	// untestedScore is above the worst weak score (weakBase + weakWeight), so no tests always outrank weak tests.
	untestedScore = 9.5
	weakBase      = 3.0
	weakWeight    = 6.0
	percentFull   = 100.0
	// minFields is the least a go test line needs to name a package: a status and a path.
	minFields = 2
)

var coveragePct = regexp.MustCompile(`coverage: ([\d.]+)% of statements`)

// Package is one package's test coverage as `go test -cover` reports it.
type Package struct {
	Path     string
	Coverage float64
	HasTests bool
}

// ParseCoverage reads `go test -cover` output. Failing packages are not listed: the test gate reports those.
func ParseCoverage(output string) []Package {
	var out []Package
	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < minFields {
			continue
		}
		switch {
		case fields[0] == "ok":
			p := Package{Path: fields[1], HasTests: true}
			if m := coveragePct.FindStringSubmatch(line); m != nil {
				p.Coverage, _ = strconv.ParseFloat(m[1], 64)
			}
			out = append(out, p)
		case fields[0] == "?" && strings.Contains(line, "[no test files]"):
			out = append(out, Package{Path: fields[1]})
		case strings.HasPrefix(line, "\t") && coveragePct.MatchString(line):
			out = append(out, Package{Path: fields[0]})
		}
	}
	return out
}

// CoverageItems turns packages with weak or no tests into debt.
func CoverageItems(pkgs []Package, threshold float64) []Item {
	var items []Item
	for _, p := range pkgs {
		var evidence string
		score := untestedScore
		switch {
		case !p.HasTests:
			evidence = "no test files"
		case p.Coverage < threshold:
			evidence = fmt.Sprintf("coverage %.1f%% of statements (threshold %.0f%%)", p.Coverage, threshold)
			score = weakBase + weakWeight*(1-p.Coverage/percentFull)
		default:
			continue
		}
		items = append(items, Item{
			ID: fingerprint(KindCoverage, p.Path), Kind: KindCoverage, Where: p.Path, Subject: p.Path,
			Evidence: evidence, Cost: "M", Score: score, Status: StatusUnconfirmed,
		})
	}
	Rank(items)
	return items
}

// Coverage runs the module's tests with coverage on and returns the weak spots. It is opt-in because it runs the suite.
func Coverage(ctx context.Context, r proc.Runner, dir string, threshold float64) ([]Item, error) {
	res, err := r.Run(ctx, proc.Spec{
		Dir: dir, Name: "go", Args: []string{"test", "-cover", "-count=1", "./..."}, Timeout: coverageTimeout,
	})
	switch {
	case err != nil:
		return nil, err
	case res.TimedOut:
		return nil, errors.New("the coverage run was cut short, so it says nothing about coverage")
	}
	return CoverageItems(ParseCoverage(res.Output), threshold), nil
}
