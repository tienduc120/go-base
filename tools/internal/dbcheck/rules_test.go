package dbcheck_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/dbcheck"
)

// star is spliced into test SQL so the repository's own SELECT-star lint does not flag the data these tests need.
const star = "*"

// withStar turns the STAR placeholder into a real star, for the same reason.
func withStar(sql string) string { return strings.ReplaceAll(sql, "STAR", star) }

// query builds a Query whose compiled form equals its source, which holds for any statement without a star.
func query(name, cmd, sql string) dbcheck.Query {
	return dbcheck.Query{File: "q.sql", Line: 1, Name: name, Cmd: cmd, Raw: sql, SQL: sql}
}

func rulesOf(findings []dbcheck.Finding) []string {
	var out []string
	for _, f := range findings {
		out = append(out, f.Rule)
	}
	slices.Sort(out)
	return out
}

func TestEachRuleFiresOnItsViolationAndNotOnTheCleanTwin(t *testing.T) {
	t.Parallel()

	// One violation and one compliant twin per rule: a rule that cannot fail is not a rule.
	for _, tc := range []struct {
		rule      string
		violation dbcheck.Query
		clean     dbcheck.Query
	}{
		{
			rule:      "select-star",
			violation: query("GetNote", "one", withStar("SELECT STAR FROM notes WHERE id = $1")),
			clean:     query("GetNote", "one", "SELECT id, name FROM notes WHERE id = $1"),
		},
		{
			rule:      "select-star",
			violation: query("GetNote", "one", withStar("SELECT n.STAR FROM notes n WHERE n.id = $1")),
			clean:     query("CountNotes", "one", "SELECT count(*) FROM notes WHERE owner = $1"),
		},
		{
			rule:      "select-star",
			violation: query("CreateNote", "one", "INSERT INTO notes (id) VALUES ($1) RETURNING *"),
			clean:     query("CreateNote", "one", "INSERT INTO notes (id) VALUES ($1) RETURNING id"),
		},
		{
			rule:      "write-needs-where",
			violation: query("DeleteNotes", "exec", "DELETE FROM notes"),
			clean:     query("DeleteNote", "exec", "DELETE FROM notes WHERE id = $1"),
		},
		{
			rule:      "write-needs-where",
			violation: query("UpdateNotes", "exec", "UPDATE notes SET name = $1"),
			clean:     query("UpdateNote", "exec", "UPDATE notes SET name = $1 WHERE id = $2"),
		},
		{
			rule:      "list-needs-limit",
			violation: query("ListNotes", "many", "SELECT id FROM notes ORDER BY id"),
			clean:     query("ListNotes", "many", "SELECT id FROM notes ORDER BY id LIMIT $1"),
		},
		{
			rule:      "list-needs-order",
			violation: query("ListNotes", "many", "SELECT id FROM notes WHERE owner = $1 LIMIT $2"),
			clean:     query("ListNotes", "many", "SELECT id FROM notes WHERE owner = $1 ORDER BY id LIMIT $2"),
		},
		{
			rule:      "no-offset",
			violation: query("ListNotes", "many", "SELECT id FROM notes ORDER BY id LIMIT 10 OFFSET $1"),
			clean:     query("ListNotes", "many", "SELECT id FROM notes WHERE id > $1 ORDER BY id LIMIT 10"),
		},
		{
			rule:      "no-random-order",
			violation: query("ListNotes", "many", "SELECT id FROM notes ORDER BY random() LIMIT 5"),
			clean:     query("ListNotes", "many", "SELECT id FROM notes ORDER BY id LIMIT 5"),
		},
		{
			rule: "join-limit",
			violation: query("ListNotes", "many", "SELECT a.id FROM a JOIN b ON b.a=a.id JOIN c ON c.b=b.id "+
				"JOIN d ON d.c=c.id LEFT JOIN e ON e.d=d.id ORDER BY a.id LIMIT 5"),
			clean: query("ListNotes", "many", "SELECT a.id FROM a JOIN b ON b.a=a.id JOIN c ON c.b=b.id "+
				"JOIN d ON d.c=c.id ORDER BY a.id LIMIT 5"),
		},
		{
			rule:      "query-name",
			violation: query("InsertNote", "exec", "INSERT INTO notes (id) VALUES ($1)"),
			clean:     query("CreateNote", "exec", "INSERT INTO notes (id) VALUES ($1)"),
		},
	} {
		if !slices.Contains(rulesOf(dbcheck.Check(tc.violation)), tc.rule) {
			t.Errorf("%s did not fire on %q", tc.rule, tc.violation.Raw)
		}
		if got := rulesOf(dbcheck.Check(tc.clean)); slices.Contains(got, tc.rule) {
			t.Errorf("%s fired on the compliant twin %q", tc.rule, tc.clean.Raw)
		}
	}
}

func TestSelectStarIsReadFromTheSourceBecauseSqlcExpandsItInTheCompiledText(t *testing.T) {
	t.Parallel()

	q := dbcheck.Query{
		File: "q.sql", Line: 3, Name: "GetNote", Cmd: "one",
		Raw: "SELECT * FROM notes WHERE id = $1",
		SQL: "SELECT id, name, created_at FROM notes WHERE id = $1",
	}

	findings := dbcheck.Check(q)
	if !slices.Contains(rulesOf(findings), "select-star") {
		t.Errorf("the source says SELECT * even though sqlc compiled it away: %v", findings)
	}
}

func TestAFindingSaysWhereAndHowToFixIt(t *testing.T) {
	t.Parallel()

	q := query("ListNotes", "many", "SELECT id FROM notes ORDER BY id")
	q.File, q.Line = "services/x/queries/notes.sql", 12

	findings := dbcheck.Check(q)
	if len(findings) != 1 {
		t.Fatalf("findings = %v", findings)
	}
	f := findings[0]
	if f.File != "services/x/queries/notes.sql" || f.Line != 12 || f.Query != "ListNotes" ||
		f.Rule != "list-needs-limit" {
		t.Errorf("finding = %+v", f)
	}
	if !strings.Contains(f.Message, "LIMIT") {
		t.Errorf("the message should tell the author what to do: %q", f.Message)
	}
}

func TestAnAllowWithAReasonSilencesOnlyItsRule(t *testing.T) {
	t.Parallel()

	q := query("ListKinds", "many", "SELECT id FROM kinds")
	q.Allow = map[string]string{"list-needs-limit": "a lookup table of at most ten rows"}

	got := rulesOf(dbcheck.Check(q))
	if slices.Contains(got, "list-needs-limit") {
		t.Errorf("an explained allow should silence its rule: %v", got)
	}
}

func TestAnAllowWithoutAReasonIsRefusedAndSilencesNothing(t *testing.T) {
	t.Parallel()

	q := query("ListKinds", "many", "SELECT id FROM kinds")
	q.Allow = map[string]string{"list-needs-limit": ""}

	got := rulesOf(dbcheck.Check(q))
	if !slices.Contains(got, "list-needs-limit") || !slices.Contains(got, "allow-needs-reason") {
		t.Errorf("a bare allow is a silent suppression; want both findings, got %v", got)
	}
}

func TestAnAllowForAnUnknownRuleIsAFinding(t *testing.T) {
	t.Parallel()

	q := query("GetKind", "one", "SELECT id FROM kinds WHERE id = $1")
	q.Allow = map[string]string{"no-such-rule": "typo"}

	if got := rulesOf(dbcheck.Check(q)); !slices.Contains(got, "allow-unknown-rule") {
		t.Errorf("a misspelt rule name would otherwise suppress nothing and look fine: %v", got)
	}
}

func TestACompliantQueryHasNoFindings(t *testing.T) {
	t.Parallel()

	q := query(
		"ListNotes",
		"many",
		"SELECT id, name FROM notes WHERE (created_at, id) > ($1, $2) ORDER BY created_at, id LIMIT $3",
	)
	if findings := dbcheck.Check(q); len(findings) != 0 {
		t.Errorf("findings = %v", findings)
	}
}
