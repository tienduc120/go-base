// Package mdstyle keeps Markdown to one paragraph or list item per line. Hard-wrapped prose shows up as arbitrary
// mid-sentence breaks in any editor that wraps for itself, and a line of wrapped text diffs badly.
package mdstyle

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const (
	fileMode = 0o600
	frontEnd = "---"
)

type kind int

const (
	blank kind = iota
	prose
	list
	other
)

var (
	headingRe = regexp.MustCompile(`^\s{0,3}#{1,6}(\s|$)`)
	listRe    = regexp.MustCompile(`^\s*(?:[-*+]|\d+[.)])\s+`)
	tableRe   = regexp.MustCompile(`^\s*\|`)
	quoteRe   = regexp.MustCompile(`^\s*>`)
	htmlRe    = regexp.MustCompile(`^\s*<`)
	ruleRe    = regexp.MustCompile(`^\s*(?:(?:-\s*){3,}|(?:\*\s*){3,}|(?:_\s*){3,})$`)
	fenceRe   = regexp.MustCompile("^\\s*(?:```|~~~)")
)

func classify(line string) kind {
	switch {
	case strings.TrimSpace(line) == "":
		return blank
	case headingRe.MatchString(line), tableRe.MatchString(line), quoteRe.MatchString(line),
		htmlRe.MatchString(line), ruleRe.MatchString(line):
		return other
	case listRe.MatchString(line):
		return list
	}
	return prose
}

// hardBreak reports a deliberate line break: two trailing spaces or a trailing backslash.
func hardBreak(line string) bool {
	return strings.HasSuffix(line, "  ") || strings.HasSuffix(line, `\`)
}

// process joins every line that merely continues the one before it and reports the 1-based numbers of those lines.
func process(src string) (string, []int) {
	if src == "" {
		return "", nil
	}
	lines := strings.Split(src, "\n")

	var out []string
	var joined []int
	prev, prevHard, inFence := blank, false, false

	start := 0
	if end := frontMatterEnd(lines); end > 0 {
		out = append(out, lines[:end+1]...)
		start, prev = end+1, other
	}

	for i := start; i < len(lines); i++ {
		line := lines[i]
		if fenceRe.MatchString(line) {
			inFence = !inFence
			out, prev, prevHard = append(out, line), other, false
			continue
		}
		if inFence {
			out = append(out, line)
			continue
		}

		k := classify(line)
		if k == prose && (prev == prose || prev == list) && !prevHard {
			out[len(out)-1] = strings.TrimRight(out[len(out)-1], " \t") + " " + strings.TrimLeft(line, " \t")
			joined = append(joined, i+1)
			prevHard = hardBreak(line)
			continue
		}
		out, prev = append(out, line), k
		prevHard = (k == prose || k == list) && hardBreak(line)
	}
	return strings.Join(out, "\n"), joined
}

// frontMatterEnd returns the index of the closing delimiter of a leading front-matter block, or -1.
func frontMatterEnd(lines []string) int {
	if len(lines) == 0 || lines[0] != frontEnd {
		return -1
	}
	for i := 1; i < len(lines); i++ {
		if lines[i] == frontEnd {
			return i
		}
	}
	return -1
}

// Unwrap joins hard-wrapped prose so each paragraph or list item is one line. Code blocks, tables, headings, quotes,
// HTML, front matter, and deliberate hard breaks are left alone.
func Unwrap(src string) string {
	out, _ := process(src)
	return out
}

// Check returns the 1-based numbers of the lines that only continue the line before them.
func Check(src string) []int {
	_, joined := process(src)
	return joined
}

type Violation struct {
	Path  string
	Lines []int
}

// skipped are directories whose Markdown is not ours to format: other people's text, scratch, and generated output.
func skipped(name string) bool {
	return slices.Contains(
		[]string{".git", ".local", ".agentctl", "node_modules", "testdata", "LICENSES", "vendor"},
		name,
	)
}

// Files lists the repository's Markdown files.
func Files(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && p != root && skipped(d.Name()):
			return fs.SkipDir
		case !d.IsDir() && strings.HasSuffix(d.Name(), ".md"):
			files = append(files, p)
		}
		return nil
	})
	return files, err
}

// CheckFiles reports every Markdown file with wrapped lines.
func CheckFiles(root string) ([]Violation, error) {
	files, err := Files(root)
	if err != nil {
		return nil, err
	}
	var out []Violation
	for _, f := range files {
		data, readErr := os.ReadFile(f)
		if readErr != nil {
			return nil, readErr
		}
		if lines := Check(string(data)); len(lines) > 0 {
			out = append(out, Violation{Path: f, Lines: lines})
		}
	}
	return out, nil
}

// Fix unwraps every Markdown file in the repository in place and returns the ones it changed.
func Fix(root string) ([]string, error) {
	files, err := Files(root)
	if err != nil {
		return nil, err
	}
	return FixFiles(files)
}

// FixFiles unwraps the named files in place and returns the ones it changed.
func FixFiles(files []string) ([]string, error) {
	var changed []string
	for _, f := range files {
		data, readErr := os.ReadFile(f)
		if readErr != nil {
			return nil, readErr
		}
		fixed := Unwrap(string(data))
		if fixed == string(data) {
			continue
		}
		//nolint:gosec // the path comes from the repository walk or a file named to a local developer command
		if writeErr := os.WriteFile(f, []byte(fixed), fileMode); writeErr != nil {
			return nil, writeErr
		}
		changed = append(changed, f)
	}
	return changed, nil
}
