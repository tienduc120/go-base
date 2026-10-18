package debt

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var (
	sqlQueryName = regexp.MustCompile(`^\s*--\s*name:\s*(\w+)`)
	sqlAllow     = regexp.MustCompile(`^\s*--\s*agent:allow\s+([\w-]+)\s*(?::\s*(.*?))?\s*$`)
)

// scanSQL finds `-- agent:allow` lines in a query file. They accept a finding of the query gate, which makes them
// suppressions exactly as //nolint is, and are tracked the same way.
func scanSQL(root, path string) ([]Item, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	rel := relPath(root, path)
	query := ""
	seen := map[string]int{}
	var items []Item
	for i, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if m := sqlQueryName.FindStringSubmatch(line); m != nil {
			query = m[1]
			continue
		}
		m := sqlAllow.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		rule, reason := m[1], m[2]
		key := strings.Join([]string{query, rule, reason}, "|")
		n := seen[key]
		seen[key]++

		score := suppressionScore
		if reason == "" {
			score += unexplainedExtra
		}
		items = append(items, Item{
			ID:       fingerprint(KindSuppression, rel, query, rule, reason, strconv.Itoa(n)),
			Kind:     KindSuppression,
			Where:    fmt.Sprintf("%s:%d", rel, i+1),
			Subject:  rule,
			Evidence: clip(line),
			Cost:     "S",
			Score:    score,
		})
	}
	return items, nil
}
