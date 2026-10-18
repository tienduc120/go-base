package main

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/tienduc120/go-base/tools/internal/doctor"
	"github.com/tienduc120/go-base/tools/internal/mdstyle"
)

const fmtUsage = `usage: agentctl fmt md [file...]

  md   put every Markdown paragraph and list item on one line (code blocks, tables, and headings are left alone).
       With no files it covers the repository; named files are always rewritten, even in skipped directories.
`

func runFmt(args []string, stdout, stderr io.Writer, env environment) int {
	if len(args) == 0 || args[0] != "md" {
		_, _ = fmt.Fprint(stderr, fmtUsage)
		return exitUsage
	}

	root, err := doctor.FindRoot(env.cwd)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "agentctl fmt:", err, "(run it inside the repository)")
		return exitUsage
	}

	var changed []string
	if named := args[1:]; len(named) > 0 {
		changed, err = mdstyle.FixFiles(named)
	} else {
		changed, err = mdstyle.Fix(root)
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "agentctl fmt:", err)
		return exitUsage
	}

	if len(changed) == 0 {
		_, _ = fmt.Fprintln(stdout, "markdown: nothing to change")
		return exitOK
	}
	for _, p := range changed {
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			rel = p
		}
		_, _ = fmt.Fprintf(stdout, "rewrote %s\n", filepath.ToSlash(rel))
	}
	return exitOK
}
