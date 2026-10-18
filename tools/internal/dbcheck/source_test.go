package dbcheck_test

import (
	"strings"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/dbcheck"
)

func TestParseSourceSplitsQueriesAndKeepsTheirLines(t *testing.T) {
	t.Parallel()

	src := "-- name: GetNote :one\nSELECT id FROM notes WHERE id = $1;\n\n-- name: ListNotes :many\nSELECT id FROM notes\nLIMIT $1;\n"
	blocks, err := dbcheck.ParseSource("queries/notes.sql", src)
	if err != nil {
		t.Fatal(err)
	}

	if len(blocks) != 2 {
		t.Fatalf("got %d blocks, want 2: %+v", len(blocks), blocks)
	}
	first, second := blocks[0], blocks[1]
	if first.Name != "GetNote" || first.Cmd != "one" || first.Line != 1 {
		t.Errorf("first = %+v", first)
	}
	if second.Name != "ListNotes" || second.Cmd != "many" || second.Line != 4 {
		t.Errorf("second = %+v", second)
	}
	if !strings.Contains(second.Raw, "LIMIT $1") || strings.Contains(second.Raw, "GetNote") {
		t.Errorf("a block holds its own statement and nothing of the neighbour: %q", second.Raw)
	}
}

func TestParseSourceBlanksCommentsAndStringContents(t *testing.T) {
	t.Parallel()

	src := "-- name: Find :one\n-- select " + star + " is only a comment\n" +
		"SELECT id /* offset */ FROM t WHERE label = 'select " + star + " from it''s offset' AND id = $1;\n"
	blocks, err := dbcheck.ParseSource("q.sql", src)
	if err != nil {
		t.Fatal(err)
	}

	raw := strings.ToLower(blocks[0].Raw)
	for _, leaked := range []string{"select " + star, "offset", "it's"} {
		if strings.Contains(raw, leaked) {
			t.Errorf("%q must not survive blanking, or a rule would fire on prose: %q", leaked, blocks[0].Raw)
		}
	}
	if !strings.Contains(raw, "where label =") || !strings.Contains(raw, "id = $1") {
		t.Errorf("real SQL must survive: %q", blocks[0].Raw)
	}
}

func TestParseSourceReadsAllowAnnotations(t *testing.T) {
	t.Parallel()

	src := "-- name: ListKinds :many\n-- agent:allow list-needs-limit: a lookup table that never passes ten rows\n-- agent:allow select-star\nSELECT id FROM kinds;\n"
	blocks, err := dbcheck.ParseSource("q.sql", src)
	if err != nil {
		t.Fatal(err)
	}

	allow := blocks[0].Allow
	if got := allow["list-needs-limit"]; got != "a lookup table that never passes ten rows" {
		t.Errorf("reason = %q", got)
	}
	if reason, ok := allow["select-star"]; !ok || reason != "" {
		t.Errorf("an allow with no reason must still be recorded, as empty: %q, %v", reason, ok)
	}
}

func TestParseSourceRefusesAStatementWithNoName(t *testing.T) {
	t.Parallel()

	if _, err := dbcheck.ParseSource("q.sql", "SELECT 1;\n"); err == nil {
		t.Error("SQL before any `-- name:` header belongs to no query and must be an error")
	}
}

func TestParseSourceRefusesDuplicateNames(t *testing.T) {
	t.Parallel()

	src := "-- name: Same :one\nSELECT 1;\n-- name: Same :one\nSELECT 2;\n"
	if _, err := dbcheck.ParseSource("q.sql", src); err == nil {
		t.Error("two queries with one name cannot both be generated")
	}
}
