package verify

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/tienduc120/go-base/tools/internal/adr"
)

// ADRStep fails when the decision records in docs/adr break their shape: a gap in the numbering, a missing section,
// template text in an accepted record, or an index that disagrees with the files. It reads files and runs nothing.
func ADRStep(env Env) Step {
	return Step{Name: "adr check", Run: func(context.Context) Outcome {
		dir := filepath.Join(env.Root, "docs", "adr")
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			return Outcome{Skipped: true, Detail: "no docs/adr directory: there are no decision records to check"}
		}

		problems, err := adr.Check(dir)
		switch {
		case err != nil:
			return Outcome{Detail: err.Error()}
		case len(problems) > 0:
			return Outcome{Detail: strings.Join(problems, "\n")}
		}
		return Outcome{Pass: true}
	}}
}
