package verify

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/tienduc120/go-base/tools/internal/mdstyle"
)

// maxReported keeps a repository-wide first run from printing hundreds of lines.
const maxReported = 20

// markdownStep fails on hard-wrapped prose: one paragraph or list item belongs on one line.
func markdownStep(env Env) Step {
	return Step{Name: "markdown style", Run: func(context.Context) Outcome {
		violations, err := mdstyle.CheckFiles(env.Root)
		if err != nil {
			return Outcome{Detail: err.Error()}
		}
		if len(violations) == 0 {
			return Outcome{Pass: true}
		}

		var lines []string
		total := 0
		for _, v := range violations {
			rel, relErr := filepath.Rel(env.Root, v.Path)
			if relErr != nil {
				rel = v.Path
			}
			for _, n := range v.Lines {
				total++
				if len(lines) < maxReported {
					lines = append(lines, fmt.Sprintf(
						"%s:%d: continues the previous line; keep one paragraph or list item per line",
						filepath.ToSlash(rel), n))
				}
			}
		}
		if total > maxReported {
			lines = append(lines, fmt.Sprintf("... and %d more", total-maxReported))
		}
		lines = append(lines, "fix all of them with: go run ./tools/agentctl fmt md")
		return Outcome{Detail: strings.Join(lines, "\n")}
	}}
}
