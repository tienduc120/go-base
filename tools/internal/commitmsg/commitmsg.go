// Package commitmsg checks a commit message against Conventional Commits as this repository uses them.
package commitmsg

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxHeader is the longest first line git tooling and terminals show without wrapping.
const maxHeader = 72

var (
	header = regexp.MustCompile(`^([a-z]+)(?:\(([^)]*)\))?(!)?: (.+)$`)
	scope  = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

func types() []string {
	return []string{"feat", "fix", "perf", "refactor", "test", "docs", "build", "ci", "chore", "revert", "style"}
}

// Validate returns every problem with msg, so one pass tells the author everything to fix. An empty result is valid.
func Validate(msg string) []string {
	lines := contentLines(msg)
	if len(lines) == 0 {
		return []string{"the message is empty"}
	}

	first := lines[0]
	if strings.HasPrefix(first, "Merge ") || strings.HasPrefix(first, `Revert "`) {
		return nil
	}

	var problems []string
	if n := utf8.RuneCountInString(first); n > maxHeader {
		problems = append(problems, fmt.Sprintf("the first line is %d characters; the limit is %d", n, maxHeader))
	}

	m := header.FindStringSubmatch(first)
	if m == nil {
		return append(problems, "the first line must read `type(scope): subject` with a space after the colon")
	}
	typ, sc, subject := m[1], m[2], m[4]

	if !slices.Contains(types(), typ) {
		problems = append(problems, fmt.Sprintf("type %q is not one of: %s", typ, strings.Join(types(), ", ")))
	}
	if strings.Contains(first, "(") && !scope.MatchString(sc) {
		problems = append(
			problems,
			"the scope must be lowercase kebab-case and not empty, like `tools` or `db-migrations`",
		)
	}
	problems = append(problems, subjectProblems(subject)...)

	if len(lines) > 1 && lines[1] != "" {
		problems = append(problems, "leave a blank line between the first line and the body")
	}
	return problems
}

func subjectProblems(subject string) []string {
	var problems []string
	if r, _ := utf8.DecodeRuneInString(subject); unicode.IsUpper(r) {
		problems = append(problems, "the subject must start lowercase")
	}
	if strings.HasSuffix(subject, ".") {
		problems = append(problems, "the subject must not end with a period")
	}
	return problems
}

// contentLines drops git's comment lines and trailing blanks.
func contentLines(msg string) []string {
	var lines []string
	for raw := range strings.SplitSeq(msg, "\n") {
		if strings.HasPrefix(raw, "#") {
			continue
		}
		lines = append(lines, strings.TrimRight(raw, " \t\r"))
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		return nil
	}
	return lines
}
