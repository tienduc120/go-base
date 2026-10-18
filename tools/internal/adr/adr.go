// Package adr creates and checks the architecture decision records in docs/adr.
//
// A record that is hard to start, or whose index entry drifts from the file, is a record people skip. Check holds the
// set to one shape: numbered without gaps, headed and dated, with the four sections, and listed in the index.
package adr

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	indexFile  = "README.md"
	dateLayout = "2006-01-02"
	fileMode   = 0o600
	maxSlug    = 60
	numberLen  = 4

	// The template's guidance lines. A record that is accepted with any of them left in was never written.
	contextHint      = "What forces the decision: the problem, the constraints, and what is already decided or tried."
	decisionHint     = "1. What we will do, in numbered sentences a tool or a reviewer can check."
	consequencesHint = "- Positive: what gets easier. Negative: what gets harder. Neutral: what changes elsewhere."
	alternativesHint = "- **Option**: why it lost, and what would make it win."
)

var (
	fileName    = regexp.MustCompile(`^(\d{4})-([a-z0-9]+(?:-[a-z0-9]+)*)\.md$`)
	headingLine = regexp.MustCompile(`^# (\d{4}) - (.+)$`)
	statusLine  = regexp.MustCompile(`^- \*\*Status\*\*: (.+)$`)
	dateLine    = regexp.MustCompile(`^- \*\*Date\*\*: (.+)$`)
	statusValue = regexp.MustCompile(`^(proposed|accepted|rejected|deprecated|superseded by (\d{4}))$`)
	indexRow    = regexp.MustCompile(`^\|\s*\[(\d{4})\]\(([^)]+)\)\s*\|\s*(.*?)\s*\|\s*(.*?)\s*\|\s*$`)
	notSlug     = regexp.MustCompile(`[^a-z0-9]+`)
)

// requiredSections are the headings every record has, in this order.
func requiredSections() []string {
	return []string{"Context", "Decision", "Consequences", "Alternatives considered"}
}

type record struct {
	number int
	file   string
	title  string
	status string
	dated  bool
}

// Check returns every way the records in dir break the shape, or nil when none do. The error is for a directory it
// could not read.
func Check(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}

	var problems []string
	byNumber := map[int][]record{}
	for _, e := range entries {
		m := fileName.FindStringSubmatch(e.Name())
		switch {
		case e.IsDir() || e.Name() == indexFile:
		case m == nil:
			problems = append(problems, e.Name()+": a record is named NNNN-slug.md, in lowercase")
		default:
			n, _ := strconv.Atoi(m[1])
			rec, found, readErr := readRecord(dir, e.Name(), n)
			if readErr != nil {
				return nil, readErr
			}
			problems = append(problems, found...)
			byNumber[n] = append(byNumber[n], rec)
		}
	}

	problems = append(problems, numbering(byNumber)...)
	records := firstOfEach(byNumber)
	problems = append(problems, supersession(records)...)

	indexProblems, err := checkIndex(dir, records)
	if err != nil {
		return nil, err
	}
	return append(problems, indexProblems...), nil
}

// numbering reports duplicates and gaps: the order of decisions is part of what they say.
func numbering(byNumber map[int][]record) []string {
	var problems []string
	top := 0
	for n, recs := range byNumber {
		top = max(top, n)
		if len(recs) > 1 {
			problems = append(problems, fmt.Sprintf("%04d is used by %d records: %s",
				n, len(recs), strings.Join(files(recs), ", ")))
		}
	}
	for n := 1; n <= top; n++ {
		if _, ok := byNumber[n]; !ok {
			problems = append(problems, fmt.Sprintf("%04d is missing: records are numbered without gaps", n))
		}
	}
	return problems
}

func files(recs []record) []string {
	var out []string
	for _, r := range recs {
		out = append(out, r.file)
	}
	slices.Sort(out)
	return out
}

func firstOfEach(byNumber map[int][]record) map[int]record {
	out := map[int]record{}
	for n, recs := range byNumber {
		slices.SortFunc(recs, func(a, b record) int { return strings.Compare(a.file, b.file) })
		out[n] = recs[0]
	}
	return out
}

// supersession reports a record that says another replaced it when that other does not exist.
func supersession(records map[int]record) []string {
	var problems []string
	for _, r := range records {
		if m := statusValue.FindStringSubmatch(r.status); m != nil && m[2] != "" {
			if target, _ := strconv.Atoi(m[2]); records[target].file == "" {
				problems = append(problems, fmt.Sprintf("%s: superseded by %s, which does not exist", r.file, m[2]))
			}
		}
	}
	slices.Sort(problems)
	return problems
}

func readRecord(dir, name string, number int) (record, []string, error) {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return record{}, nil, fmt.Errorf("read %s: %w", name, err)
	}
	text := string(data)
	rec := record{number: number, file: name}

	var problems []string
	for line := range strings.SplitSeq(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if m := headingLine.FindStringSubmatch(line); m != nil && rec.title == "" {
			rec.title = m[2]
			if got, _ := strconv.Atoi(m[1]); got != number {
				problems = append(
					problems,
					fmt.Sprintf("%s: the heading says %s, the file says %04d", name, m[1], number),
				)
			}
		}
		if m := statusLine.FindStringSubmatch(line); m != nil && rec.status == "" {
			rec.status = m[1]
		}
		if m := dateLine.FindStringSubmatch(line); m != nil {
			rec.dated = true
			if _, parseErr := time.Parse(dateLayout, m[1]); parseErr != nil {
				problems = append(problems, fmt.Sprintf("%s: Date %q is not a YYYY-MM-DD date", name, m[1]))
			}
		}
	}

	problems = append(problems, shapeProblems(name, text, rec)...)
	return rec, problems, nil
}

func shapeProblems(name, text string, rec record) []string {
	var problems []string
	if rec.title == "" {
		problems = append(problems, name+": the first line should be the heading `# NNNN - Title`")
	}
	switch {
	case rec.status == "":
		problems = append(problems, name+": there is no `- **Status**: ...` line")
	case !statusValue.MatchString(rec.status):
		problems = append(
			problems,
			fmt.Sprintf("%s: Status %q is not proposed, accepted, rejected, deprecated, or superseded by NNNN",
				name, rec.status),
		)
	}
	if !rec.dated {
		problems = append(problems, name+": there is no `- **Date**: YYYY-MM-DD` line")
	}
	for _, section := range requiredSections() {
		if !strings.Contains(text, "\n## "+section+"\n") {
			problems = append(problems, fmt.Sprintf("%s: the section %q is missing", name, "## "+section))
		}
	}
	if rec.status != "" && rec.status != "proposed" {
		for _, hint := range []string{contextHint, decisionHint, consequencesHint, alternativesHint} {
			if strings.Contains(text, hint) {
				problems = append(
					problems,
					fmt.Sprintf("%s: still holds template text (%q): write it before accepting", name, hint),
				)
			}
		}
	}
	return problems
}

func checkIndex(dir string, records map[int]record) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(dir, indexFile))
	if err != nil {
		return nil, fmt.Errorf("read the index: %w", err)
	}

	rows := map[int][]string{}
	for line := range strings.SplitSeq(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if m := indexRow.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[1])
			rows[n] = m[2:]
		}
	}

	var problems []string
	for n, r := range records {
		row, ok := rows[n]
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("index: %04d (%s) is not listed in %s", n, r.file, indexFile))
		case row[0] != r.file:
			problems = append(problems, fmt.Sprintf("index: %04d links to %s, not %s", n, row[0], r.file))
		case row[1] != r.title:
			problems = append(problems, fmt.Sprintf("index: %04d has title %q, the record says %q", n, row[1], r.title))
		case row[2] != r.status:
			problems = append(
				problems,
				fmt.Sprintf("index: %04d has status %q, the record says %q", n, row[2], r.status),
			)
		}
	}
	for n := range rows {
		if _, ok := records[n]; !ok {
			problems = append(problems, fmt.Sprintf("index: lists %04d, but there is no such record", n))
		}
	}
	slices.Sort(problems)
	return problems, nil
}

func template(number int, title string, now time.Time) string {
	return fmt.Sprintf(`# %04d - %s

- **Status**: proposed
- **Date**: %s

## Context

%s

## Decision

%s

## Consequences

%s

## Alternatives considered

%s
`, number, title, now.Format(dateLayout), contextHint, decisionHint, consequencesHint, alternativesHint)
}

// New writes the next record as a proposal and lists it in the index. It returns the new file's path.
func New(dir, title string, now time.Time) (string, error) {
	title = strings.TrimSpace(title)
	slug := strings.Trim(notSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if title == "" || slug == "" {
		return "", errors.New("a record needs a title with some letters or digits in it")
	}
	slug = strings.TrimRight(slug[:min(len(slug), maxSlug)], "-")

	next, err := nextNumber(dir)
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("%0*d-%s.md", numberLen, next, slug)

	// A Root confines every write to dir, so no title can name a file elsewhere.
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", dir, err)
	}
	defer func() { _ = root.Close() }()

	if err = addIndexRow(root, next, name, title); err != nil {
		return "", err
	}
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		return "", fmt.Errorf("create %s: %w", name, err)
	}
	_, writeErr := file.WriteString(template(next, title, now))
	if closeErr := file.Close(); writeErr == nil {
		writeErr = closeErr
	}
	return filepath.Join(dir, name), writeErr
}

func nextNumber(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", dir, err)
	}
	top := 0
	for _, e := range entries {
		if m := fileName.FindStringSubmatch(e.Name()); m != nil {
			n, _ := strconv.Atoi(m[1])
			top = max(top, n)
		}
	}
	return top + 1, nil
}

// addIndexRow puts the new record's row after the last one already in the table.
func addIndexRow(root *os.Root, number int, file, title string) error {
	data, err := root.ReadFile(indexFile)
	if err != nil {
		return fmt.Errorf("read the index: %w", err)
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	last := -1
	for i, line := range lines {
		if indexRow.MatchString(line) {
			last = i
		}
	}
	if last < 0 {
		return fmt.Errorf("%s has no table of records to add a row to", indexFile)
	}
	row := fmt.Sprintf("| [%04d](%s) | %s | proposed |", number, file, title)
	lines = slices.Insert(lines, last+1, row)
	return root.WriteFile(indexFile, []byte(strings.Join(lines, "\n")), fileMode)
}
