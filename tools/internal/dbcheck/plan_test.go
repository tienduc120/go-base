package dbcheck_test

import (
	"strings"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/dbcheck"
)

const indexedPlan = `[{"Plan":{"Node Type":"Index Scan","Relation Name":"notes","Index Name":"notes_pkey"}}]`

// The sequential scan sits two levels down, under a Limit and a Sort, where a root-only check would never see it.
const nestedSeqScan = `[{"Plan":{"Node Type":"Limit","Plans":[{"Node Type":"Sort","Plans":[
  {"Node Type":"Seq Scan","Relation Name":"notes","Alias":"n"}]}]}}]`

func TestAnalyzePlanPassesAnIndexedPlan(t *testing.T) {
	t.Parallel()

	findings, err := dbcheck.AnalyzePlan([]byte(indexedPlan))
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Errorf("findings = %v", findings)
	}
}

func TestAnalyzePlanFindsASequentialScanAnywhereInTheTree(t *testing.T) {
	t.Parallel()

	findings, err := dbcheck.AnalyzePlan([]byte(nestedSeqScan))
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Rule != "seq-scan" {
		t.Fatalf("findings = %v", findings)
	}
	if !strings.Contains(findings[0].Message, `"notes"`) || !strings.Contains(findings[0].Message, "index") {
		t.Errorf("the message should name the table and the fix: %q", findings[0].Message)
	}
}

func TestAnalyzePlanReportsEachScannedTableOnce(t *testing.T) {
	t.Parallel()

	plan := `[{"Plan":{"Node Type":"Nested Loop","Plans":[
	  {"Node Type":"Seq Scan","Relation Name":"a"},{"Node Type":"Seq Scan","Relation Name":"b"}]}}]`
	findings, err := dbcheck.AnalyzePlan([]byte(plan))
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Errorf("want one finding per table, got %v", findings)
	}
}

func TestAnalyzePlanRejectsOutputThatIsNotAPlan(t *testing.T) {
	t.Parallel()

	for _, bad := range []string{"", "ERROR: relation does not exist", `{"Plan":{}}`, `[]`} {
		if _, err := dbcheck.AnalyzePlan([]byte(bad)); err == nil {
			t.Errorf(
				"%q should be an error, not a clean pass: a query that cannot be planned is not a query that is fine",
				bad,
			)
		}
	}
}

func TestExtractJSONFindsThePlanAmongNoise(t *testing.T) {
	t.Parallel()

	out := "NOTICE: something\n" + indexedPlan + "\n"
	got, err := dbcheck.ExtractJSON(out)
	if err != nil {
		t.Fatal(err)
	}
	if got != indexedPlan {
		t.Errorf("got %q", got)
	}
	if _, err = dbcheck.ExtractJSON("ERROR: syntax error"); err == nil {
		t.Error("output with no JSON should be an error")
	}
}

func TestAnalyzePlanFindsAWholeIndexWalkThatOnlyFilters(t *testing.T) {
	t.Parallel()

	// With sequential scans off, Postgres can read the primary key in order and discard rows by an unindexed column.
	// That touches every row exactly like a Seq Scan does, with an Index Scan label on it.
	plan := `[{"Plan":{"Node Type":"Limit","Plans":[{"Node Type":"Index Scan","Relation Name":"notes",
	  "Index Name":"notes_pkey","Filter":"(name = $1)"}]}}]`

	findings, err := dbcheck.AnalyzePlan([]byte(plan))
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Rule != "seq-scan" {
		t.Fatalf("findings = %v", findings)
	}
	if !strings.Contains(findings[0].Message, "(name = $1)") {
		t.Errorf("the message should show the filter that no index serves: %q", findings[0].Message)
	}
}

func TestAnalyzePlanAcceptsIndexScansThatUseTheirIndex(t *testing.T) {
	t.Parallel()

	for name, plan := range map[string]string{
		"condition and filter": `[{"Plan":{"Node Type":"Index Scan","Relation Name":"notes","Index Cond":"(id > $1)","Filter":"(name = $2)"}}]`,
		"ordered first page":   `[{"Plan":{"Node Type":"Limit","Plans":[{"Node Type":"Index Scan","Relation Name":"notes","Index Name":"notes_pkey"}]}}]`,
		"index only":           `[{"Plan":{"Node Type":"Index Only Scan","Relation Name":"notes","Index Cond":"(created_at > $1)"}}]`,
	} {
		findings, err := dbcheck.AnalyzePlan([]byte(plan))
		if err != nil || len(findings) != 0 {
			t.Errorf("%s: findings = %v, err = %v", name, findings, err)
		}
	}
}
