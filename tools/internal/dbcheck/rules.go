package dbcheck

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// The rule names are part of the interface: findings, allow annotations, and fixtures all use them.
const (
	RuleSelectStar      = "select-star"
	RuleWriteNeedsWhere = "write-needs-where"
	RuleListNeedsLimit  = "list-needs-limit"
	RuleListNeedsOrder  = "list-needs-order"
	RuleNoOffset        = "no-offset"
	RuleNoRandomOrder   = "no-random-order"
	RuleJoinLimit       = "join-limit"
	RuleQueryName       = "query-name"
	RuleSeqScan         = "seq-scan"
	RulePlanFailed      = "plan-failed"
	// RuleGeneratedStale is about a service, not a query, so an agent:allow cannot accept it.
	RuleGeneratedStale = "generated-stale"

	ruleAllowNeedsReason = "allow-needs-reason"
	ruleAllowUnknown     = "allow-unknown-rule"

	maxJoins = 3
)

// Query is one query as the rules see it.
type Query struct {
	File string
	Line int
	Name string
	Cmd  string
	// Raw is the source as written, comments and string contents blanked. SQL is what sqlc compiled from it.
	Raw   string
	SQL   string
	Allow map[string]string
}

// Finding is one broken rule, with enough to find it and fix it.
type Finding struct {
	File    string
	Line    int
	Query   string
	Rule    string
	Message string
	// Reason is set only on an accepted finding: the author's explanation from `agent:allow`.
	Reason string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: %s: %s: %s", f.File, f.Line, f.Query, f.Rule, f.Message)
}

var (
	limitClause  = regexp.MustCompile(`(?i)\blimit\b`)
	orderByClaus = regexp.MustCompile(`(?i)\border\s+by\b`)
	whereClause  = regexp.MustCompile(`(?i)\bwhere\b`)
	offsetClause = regexp.MustCompile(`(?i)\boffset\b`)
	randomOrder  = regexp.MustCompile(`(?i)\border\s+by\b[^;]*\brandom\s*\(`)
	joinKeyword  = regexp.MustCompile(`(?i)\bjoin\b`)
	writeStart   = regexp.MustCompile(`(?i)^\s*(update|delete)\b`)
	allowedName  = regexp.MustCompile(`^(Create|Get|List|Update|Delete|Count|Exists)[A-Z]`)
)

type rule struct {
	id      string
	message string
	broken  func(Query) bool
}

func staticRules() []rule {
	return []rule{
		{
			RuleSelectStar, "SELECT * ships columns nobody reads and hides schema changes; list the columns you need.",
			func(q Query) bool { return hasStar(q.Raw) },
		},
		{
			RuleWriteNeedsWhere, "UPDATE or DELETE without WHERE rewrites the whole table; add a WHERE.",
			func(q Query) bool { return writeStart.MatchString(q.SQL) && !whereClause.MatchString(q.SQL) },
		},
		{
			RuleListNeedsLimit, "a :many query without LIMIT grows with the table; add LIMIT and paginate by key.",
			func(q Query) bool { return q.Cmd == "many" && !limitClause.MatchString(q.SQL) },
		},
		{
			RuleListNeedsOrder, "LIMIT without ORDER BY returns arbitrary rows; order by an indexed key.",
			func(q Query) bool {
				return q.Cmd == "many" && limitClause.MatchString(q.SQL) && !orderByClaus.MatchString(q.SQL)
			},
		},
		{
			RuleNoOffset, "OFFSET rereads every skipped row; paginate by key: WHERE key > $1 ORDER BY key LIMIT n.",
			func(q Query) bool { return offsetClause.MatchString(q.SQL) },
		},
		{
			RuleNoRandomOrder, "ORDER BY random() sorts the whole table; pick by a random key within an indexed range.",
			func(q Query) bool { return randomOrder.MatchString(q.SQL) },
		},
		{
			RuleJoinLimit, fmt.Sprintf("more than %d joins; split the query or add a read model.", maxJoins),
			func(q Query) bool { return len(joinKeyword.FindAllString(q.SQL, -1)) > maxJoins },
		},
		{
			RuleQueryName, "name it Create, Get, List, Update, Delete, Count or Exists followed by the subject.",
			func(q Query) bool { return !allowedName.MatchString(q.Name) },
		},
	}
}

// hasStar reports a select-list star: after SELECT, DISTINCT, ALL, RETURNING, a comma, or a dot. A star after
// an opening parenthesis (count(*)) or an operand (a * b) is not one.
func hasStar(sql string) bool {
	runes := []rune(sql)
	for i, r := range runes {
		if r != '*' {
			continue
		}
		j := i - 1
		for j >= 0 && unicode.IsSpace(runes[j]) {
			j--
		}
		if j < 0 {
			continue
		}
		switch prev := runes[j]; {
		case prev == ',' || prev == '.':
			return true
		case unicode.IsLetter(prev):
			end := j
			for j >= 0 && (unicode.IsLetter(runes[j]) || runes[j] == '_') {
				j--
			}
			if slices.Contains(
				[]string{"select", "distinct", "all", "returning"},
				strings.ToLower(string(runes[j+1:end+1])),
			) {
				return true
			}
		}
	}
	return false
}

// Check applies every static rule to q and returns the violations; see Evaluate for the accepted ones too.
func Check(q Query) []Finding {
	violations, _ := Evaluate(q)
	return violations
}

// Evaluate splits q's findings into violations and those accepted with an explained agent:allow. Accepted ones are
// returned so a report can keep showing them: an allow is a decision, not an erasure.
func Evaluate(q Query) ([]Finding, []Finding) {
	var violations, accepted []Finding
	for _, r := range staticRules() {
		if !r.broken(q) {
			continue
		}
		f := q.finding(r.id, r.message)
		if v, ok := q.Resolve(f); ok {
			accepted = append(accepted, v)
		} else {
			violations = append(violations, f)
		}
	}

	for _, id := range slices.Sorted(maps.Keys(q.Allow)) {
		switch {
		case !slices.Contains(knownRules(), id):
			violations = append(violations, q.finding(
				ruleAllowUnknown,
				fmt.Sprintf(
					"agent:allow names %q, which is not a rule; the finding it meant to accept is still live.",
					id,
				),
			))
		case q.Allow[id] == "":
			violations = append(violations, q.finding(ruleAllowNeedsReason,
				fmt.Sprintf("agent:allow %s needs a reason after a colon; a bare allow is a silent suppression.", id)))
		}
	}
	return violations, accepted
}

// Resolve reports whether f is accepted by an explained allow, returning it with the reason attached.
func (q Query) Resolve(f Finding) (Finding, bool) {
	reason, ok := q.Allow[f.Rule]
	if !ok || reason == "" {
		return f, false
	}
	f.Reason = reason
	return f, true
}

func (q Query) finding(rule, message string) Finding {
	return Finding{File: q.File, Line: q.Line, Query: q.Name, Rule: rule, Message: message}
}

func knownRules() []string {
	ids := []string{RuleSeqScan, RulePlanFailed}
	for _, r := range staticRules() {
		ids = append(ids, r.id)
	}
	return ids
}
