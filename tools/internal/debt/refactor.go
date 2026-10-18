package debt

import (
	"fmt"
	"strings"
)

// maxRefactorLines keeps a commit that rewrote a whole test file from printing every line of it.
const maxRefactorLines = 10

// RefactorViolations reads a unified diff and lists every line of an existing test that a refactor changed or removed.
// A refactor must leave behaviour alone, so its tests may only grow: an edited expectation means behaviour moved.
// Blank lines and comments carry no behaviour and are ignored.
func RefactorViolations(diff string) []string {
	var out []string
	file := ""
	for line := range strings.SplitSeq(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			file = fileOf(line)
		case strings.HasPrefix(line, "--- a/"), strings.HasPrefix(line, "--- /dev/null"),
			strings.HasPrefix(line, "+++ b/"), strings.HasPrefix(line, "+++ /dev/null"):
		case strings.HasPrefix(line, "-") && strings.HasSuffix(file, "_test.go"):
			content := strings.TrimSpace(line[1:])
			if content == "" || strings.HasPrefix(content, "//") {
				continue
			}
			out = append(out, fmt.Sprintf("%s: a refactor changed or removed an existing test line: %s", file, content))
		}
	}

	if len(out) > maxRefactorLines {
		rest := len(out) - maxRefactorLines
		out = append(out[:maxRefactorLines], fmt.Sprintf("... and %d more lines", rest))
	}
	return out
}

// fileOf takes the path from a "diff --git a/x b/y" header, preferring the new name.
func fileOf(header string) string {
	_, after, ok := strings.Cut(header, " b/")
	if !ok {
		return ""
	}
	return after
}
