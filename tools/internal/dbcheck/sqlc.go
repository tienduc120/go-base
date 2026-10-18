package dbcheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tienduc120/go-base/tools/internal/proc"
)

const (
	sqlcTimeout = time.Minute
	scratchDir  = ".dbcheck"
	dirMode     = 0o750
	fileMode    = 0o600
)

// compiledQuery is a query as sqlc understood it.
type compiledQuery struct {
	Name string
	// Cmd is the sqlc command without its colon: one, many, exec.
	Cmd string
	// Text is the SQL with comments gone, star expanded, and named parameters turned into $n.
	Text string
}

// compile has sqlc check the queries against the migrations and emit them as JSON, which is the one place its
// compiled SQL can be read without a Go parser. The scratch config lives beside the real files because sqlc
// resolves paths relative to its configuration.
func compile(ctx context.Context, runner proc.Runner, pgDir string) (map[string]compiledQuery, error) {
	work := filepath.Join(pgDir, scratchDir)
	if err := os.MkdirAll(work, dirMode); err != nil {
		return nil, fmt.Errorf("scratch directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(work) }()

	config := "version: \"2\"\nsql:\n  - engine: postgresql\n    schema: ../migrations\n    queries: ../queries\n" +
		"    gen:\n      json:\n        out: out\n"
	if err := os.WriteFile(filepath.Join(work, "sqlc.yaml"), []byte(config), fileMode); err != nil {
		return nil, fmt.Errorf("write sqlc config: %w", err)
	}

	res, err := runner.Run(ctx, proc.Spec{
		Dir: pgDir, Name: "sqlc", Timeout: sqlcTimeout,
		Args: []string{"generate", "-f", filepath.Join(scratchDir, "sqlc.yaml")},
	})
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return nil, errors.New("sqlc is not installed: run `agentctl doctor` for the install command")
	case err != nil:
		return nil, err
	case res.TimedOut:
		return nil, fmt.Errorf("sqlc timed out (%s)", res.Reason)
	case res.ExitCode != 0:
		return nil, fmt.Errorf("sqlc rejected the queries:\n%s", strings.TrimSpace(res.Output))
	}

	data, err := os.ReadFile(filepath.Join(work, "out", "codegen_request.json"))
	if err != nil {
		return nil, fmt.Errorf("read sqlc output: %w", err)
	}
	var doc struct {
		Queries []struct {
			Name string `json:"name"`
			Cmd  string `json:"cmd"`
			Text string `json:"text"`
		} `json:"queries"`
	}
	if err = json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse sqlc output: %w", err)
	}

	out := make(map[string]compiledQuery, len(doc.Queries))
	for _, q := range doc.Queries {
		out[q.Name] = compiledQuery{Name: q.Name, Cmd: strings.TrimPrefix(q.Cmd, ":"), Text: q.Text}
	}
	return out, nil
}
