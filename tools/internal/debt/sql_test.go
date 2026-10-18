package debt_test

import (
	"strings"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/debt"
)

const queriesPath = "services/billing/internal/infrastructure/postgres/queries/notes.sql"

func TestAnAllowInAQueryFileIsASuppression(t *testing.T) {
	t.Parallel()

	root := tree(t, map[string]string{queriesPath: "-- name: GetNote :one\nSELECT id FROM notes WHERE id = $1;\n\n" +
		"-- name: ListKinds :many\n-- agent:allow list-needs-limit: a lookup table of at most ten rows\nSELECT id FROM kinds ORDER BY id;\n"})

	items := ofKind(scan(t, root, debt.Options{}), debt.KindSuppression)

	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
	it := items[0]
	if it.Subject != "list-needs-limit" || it.Where != queriesPath+":5" {
		t.Errorf("subject = %q, where = %q", it.Subject, it.Where)
	}
	if !strings.Contains(it.Evidence, "a lookup table of at most ten rows") {
		t.Errorf("evidence should carry the author's reason: %q", it.Evidence)
	}
}

func TestAnAllowKeepsItsIDWhenTheFileGrowsAbove(t *testing.T) {
	t.Parallel()

	allow := "-- name: ListKinds :many\n-- agent:allow list-needs-limit: ten rows\nSELECT id FROM kinds ORDER BY id;\n"
	before := ofKind(scan(t, tree(t, map[string]string{queriesPath: allow}), debt.Options{}), debt.KindSuppression)
	after := ofKind(scan(t, tree(t, map[string]string{
		queriesPath: "-- name: GetNote :one\nSELECT id FROM notes WHERE id = $1;\n\n" + allow,
	}), debt.Options{}), debt.KindSuppression)

	if len(before) != 1 || len(after) != 1 || before[0].ID != after[0].ID {
		t.Errorf(
			"a decision recorded against the allow must survive edits elsewhere in the file: %v vs %v",
			before,
			after,
		)
	}
}

func TestABareAllowScoresHigherThanAnExplainedOne(t *testing.T) {
	t.Parallel()

	root := tree(
		t,
		map[string]string{queriesPath: "-- name: A :many\n-- agent:allow list-needs-limit: why\nSELECT 1;\n\n" +
			"-- name: B :many\n-- agent:allow list-needs-limit\nSELECT 1;\n"},
	)

	items := ofKind(scan(t, root, debt.Options{}), debt.KindSuppression)

	if len(items) != 2 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Score <= items[1].Score || !strings.Contains(items[0].Where, ":6") {
		t.Errorf("the unexplained allow should rank first: %+v", items)
	}
}

func TestSQLFixturesUnderTestdataAreNotDebt(t *testing.T) {
	t.Parallel()

	root := tree(t, map[string]string{
		"tools/internal/dbcheck/testdata/allowed/queries.sql": "-- name: A :many\n-- agent:allow seq-scan: fixture\nSELECT 1;\n",
	})
	if items := scan(t, root, debt.Options{}); len(items) != 0 {
		t.Errorf("items = %+v", items)
	}
}
