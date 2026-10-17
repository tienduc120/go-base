// Package doctor checks that the toolchain and the pinned developer tools are present and recent enough.
// It reports; it never installs anything.
package doctor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

type Status string

const (
	StatusOK       Status = "ok"
	StatusOutdated Status = "outdated"
	StatusMissing  Status = "missing"
	StatusError    Status = "error"
)

// Column layout of the report table.
const (
	tabMinWidth = 0
	tabWidth    = 4
	tabPadding  = 2
)

// killGrace bounds how long a child holding our pipes open can delay Run after its context ends.
const killGrace = 2 * time.Second

var defaultPattern = regexp.MustCompile(`(\d+\.\d+(?:\.\d+)?)`)

type Tool struct {
	Name    string
	Command []string
	// Pattern's first group is the version; nil uses the first x.y[.z] in the output.
	Pattern *regexp.Regexp
	// Min is the lowest accepted version; empty means presence only.
	Min      string
	Required bool
	Install  string
	// Timeout overrides the caller's default for a tool that may legitimately be slow.
	Timeout time.Duration
}

type Result struct {
	Name     string `json:"name"`
	Status   Status `json:"status"`
	Found    string `json:"found,omitempty"`
	Want     string `json:"want,omitempty"`
	Required bool   `json:"required"`
	Detail   string `json:"detail,omitempty"`
}

type Runner interface {
	// Run returns exec.ErrNotFound (wrapped is fine) when the executable does not exist.
	Run(ctx context.Context, name string, args ...string) (string, error)
}

// ExecRunner runs real processes. Every call is bound by its context so a hung tool cannot hang the doctor.
type ExecRunner struct{ Dir string }

func (e ExecRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = e.Dir
	cmd.WaitDelay = killGrace
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Check runs every tool in order. Sequential on purpose: the list is short and ordered output reads better.
func Check(ctx context.Context, r Runner, tools []Tool, timeout time.Duration) []Result {
	results := make([]Result, 0, len(tools))
	for _, tool := range tools {
		results = append(results, checkOne(ctx, r, tool, timeout))
	}
	return results
}

func checkOne(ctx context.Context, r Runner, tool Tool, timeout time.Duration) Result {
	res := Result{Name: tool.Name, Want: tool.Min, Required: tool.Required}

	limit := timeout
	if tool.Timeout > 0 {
		limit = tool.Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()

	out, err := r.Run(ctx, tool.Command[0], tool.Command[1:]...)
	switch {
	case errors.Is(err, exec.ErrNotFound):
		res.Status = StatusMissing
		res.Detail = "install: " + tool.Install
		return res
	case errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil:
		res.Status = StatusError
		res.Detail = fmt.Sprintf("timed out after %s", limit)
		return res
	case err != nil:
		res.Status = StatusError
		res.Detail = err.Error()
		return res
	}

	if tool.Min == "" {
		res.Status = StatusOK
		return res
	}

	pattern := tool.Pattern
	if pattern == nil {
		pattern = defaultPattern
	}
	m := pattern.FindStringSubmatch(out)
	if m == nil {
		res.Status = StatusError
		res.Detail = "no version found in output"
		return res
	}
	res.Found = m[len(m)-1]

	cmp, err := CompareVersions(res.Found, tool.Min)
	if err != nil {
		res.Status = StatusError
		res.Detail = err.Error()
		return res
	}
	if cmp < 0 {
		res.Status = StatusOutdated
		res.Detail = "upgrade: " + tool.Install
		return res
	}
	res.Status = StatusOK
	return res
}

// Failed reports whether any required tool is not ok.
func Failed(results []Result) bool {
	for _, r := range results {
		if r.Required && r.Status != StatusOK {
			return true
		}
	}
	return false
}

// CompareVersions orders two x.y.z versions; a missing part counts as zero and a leading v is ignored.
func CompareVersions(a, b string) (int, error) {
	pa, err := parseVersion(a)
	if err != nil {
		return 0, err
	}
	pb, err := parseVersion(b)
	if err != nil {
		return 0, err
	}
	for i := range pa {
		switch {
		case pa[i] < pb[i]:
			return -1, nil
		case pa[i] > pb[i]:
			return 1, nil
		}
	}
	return 0, nil
}

func parseVersion(v string) ([3]int, error) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) > len(out) {
		return out, fmt.Errorf("version %q has more than three parts", v)
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, fmt.Errorf("version %q: %w", v, err)
		}
		out[i] = n
	}
	return out, nil
}

var goDirective = regexp.MustCompile(`(?m)^go\s+(\d+\.\d+(?:\.\d+)?)\s*$`)

// GoDirective returns the language version go.mod in dir declares, the one the toolchain must match.
func GoDirective(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return "", err
	}
	m := goDirective.FindSubmatch(data)
	if m == nil {
		return "", errors.New("go.mod has no go directive")
	}
	return string(m[1]), nil
}

// FindRoot walks up from start to the directory holding go.work.
func FindRoot(start string) (string, error) {
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.work found above %s", start)
		}
		dir = parent
	}
}

func Render(w io.Writer, results []Result) error {
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, tabMinWidth, tabWidth, tabPadding, ' ', 0)
	for _, r := range results {
		name := r.Name
		if !r.Required {
			name += " (optional)"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.Status, name, r.Found, wantCol(r.Want), r.Detail)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	_, err := w.Write(buf.Bytes())
	return err
}

func wantCol(want string) string {
	if want == "" {
		return ""
	}
	return ">= " + want
}
