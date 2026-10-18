// Package runcmd holds the pieces of running a command safely that are not the process itself: choosing a go test
// limit, keeping a short tail of long output, and writing the full output to a log.
package runcmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	// margin is how far below the wrapper's limit go test's own limit sits, so Go gets time to print every
	// goroutine's stack before the wrapper kills the run.
	margin = 10 * time.Second
	// shortLimit is the wrapper limit at or below which subtracting the margin would leave nothing.
	shortLimit = 20 * time.Second

	// halve is how a short wrapper limit is split: half for go test, half for the wrapper to react.
	halve = 2

	maxSlug = 48
	logMode = 0o600
	dirMode = 0o750
)

// InjectGoTestTimeout gives `go test` its own -timeout just under limit when the caller set none. A hung test then
// dies with a goroutine dump showing where it is stuck, instead of being killed from outside with nothing to read.
// The input is never modified.
func InjectGoTestTimeout(cmd []string, limit time.Duration) []string {
	out := slices.Clone(cmd)
	if limit <= 0 || len(cmd) < 2 || cmd[0] != "go" || cmd[1] != "test" {
		return out
	}
	for _, arg := range cmd[2:] {
		if arg == "-timeout" || arg == "--timeout" || strings.HasPrefix(arg, "-timeout=") ||
			strings.HasPrefix(arg, "--timeout=") {
			return out
		}
	}

	inner := limit - margin
	if limit <= shortLimit {
		inner = limit / halve
	}
	value := fmt.Sprintf("%ds", int(inner.Seconds()))
	return slices.Concat(out[:2], []string{"-timeout", value}, out[2:])
}

// Tail keeps the last lines of output and says how many it dropped.
func Tail(output string, lines int) string {
	all := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(all) <= lines {
		return strings.Join(all, "\n")
	}
	dropped := len(all) - lines
	return fmt.Sprintf("... %d earlier lines in the log\n%s", dropped, strings.Join(all[dropped:], "\n"))
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slug turns words into a short file-name-safe string.
func Slug(words []string) string {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(strings.Join(words, " ")), "-"), "-")
	if len(s) > maxSlug {
		s = strings.Trim(s[:maxSlug], "-")
	}
	return s
}

// WriteLog stores the full output and returns its path, or an empty string when it could not be written.
func WriteLog(dir, name, output string) string {
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return ""
	}
	p := filepath.Join(dir, name+".log")
	if err := os.WriteFile(p, []byte(output), logMode); err != nil {
		return ""
	}
	return p
}
