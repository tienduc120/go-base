package dbcheck

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// planNode is the part of a Postgres EXPLAIN (FORMAT JSON) node the rules read.
type planNode struct {
	NodeType  string     `json:"Node Type"`
	Relation  string     `json:"Relation Name"`
	IndexCond string     `json:"Index Cond"`
	Filter    string     `json:"Filter"`
	Plans     []planNode `json:"Plans"`
}

// scansEverything reports a node that reads every row of its table: a sequential scan, or an index walked end to end
// only to discard rows by a column the index does not cover.
func (n planNode) scansEverything() bool {
	switch n.NodeType {
	case "Seq Scan":
		return true
	case "Index Scan", "Index Only Scan":
		return n.Filter != "" && n.IndexCond == ""
	}
	return false
}

func (n planNode) describe() string {
	if n.NodeType == "Seq Scan" {
		return fmt.Sprintf("sequential scan of %q: add an index that serves the filter, "+
			"or allow it with a reason if the table stays tiny.", n.Relation)
	}
	return fmt.Sprintf("%q is read end to end through an index, then filtered by %s, which no index serves: "+
		"add an index on the filtered columns, or allow it with a reason if the table stays tiny.", n.Relation, n.Filter)
}

// AnalyzePlan reads EXPLAIN (FORMAT JSON) output and reports every table scanned sequentially, wherever it sits in
// the tree. sqlc vet sees only the root node, so a scan under a Limit or a join would pass there.
func AnalyzePlan(data []byte) ([]Finding, error) {
	var doc []struct {
		Plan *planNode `json:"Plan"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("not an EXPLAIN plan: %w", err)
	}
	if len(doc) == 0 || doc[0].Plan == nil {
		return nil, errors.New("not an EXPLAIN plan: no Plan in the output")
	}

	var findings []Finding
	reported := map[string]bool{}
	var walk func(planNode)
	walk = func(n planNode) {
		if n.scansEverything() && n.Relation != "" && !reported[n.Relation] {
			reported[n.Relation] = true
			findings = append(findings, Finding{Rule: RuleSeqScan, Message: n.describe()})
		}
		for _, child := range n.Plans {
			walk(child)
		}
	}
	walk(*doc[0].Plan)
	return findings, nil
}

var jsonArray = regexp.MustCompile(`(?s)\[\s*\{.*\}\s*\]`)

// ExtractJSON pulls the EXPLAIN JSON out of psql output, which may carry notices around it.
func ExtractJSON(out string) (string, error) {
	found := jsonArray.FindString(out)
	if found == "" {
		return "", fmt.Errorf("no plan in the output: %s", strings.TrimSpace(out))
	}
	return found, nil
}
