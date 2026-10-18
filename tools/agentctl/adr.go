package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/tienduc120/go-base/tools/internal/adr"
	"github.com/tienduc120/go-base/tools/internal/doctor"
)

const adrUsage = `usage: agentctl adr check
       agentctl adr new <title...>

  check   fail unless every record in docs/adr is numbered without gaps, headed and dated, has its four sections, has no
          template text once accepted, and is listed in the index with the same title and status.
  new     write the next record as a proposal and add its row to the index.
`

func runADR(_ context.Context, args []string, stdout, stderr io.Writer, env environment) int {
	if !validADRArgs(args) {
		_, _ = fmt.Fprint(stderr, adrUsage)
		return exitUsage
	}
	root, err := doctor.FindRoot(env.cwd)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "agentctl adr:", err, "(run it inside the repository)")
		return exitUsage
	}
	dir := filepath.Join(root, "docs", "adr")

	if args[0] == cmdNew {
		path, newErr := adr.New(dir, strings.Join(args[1:], " "), env.today())
		if newErr != nil {
			_, _ = fmt.Fprintln(stderr, "agentctl adr:", newErr)
			return exitUsage
		}
		_, _ = fmt.Fprintln(stdout, "created", filepath.ToSlash(mustRel(root, path)), "and listed it in the index")
		return exitOK
	}

	problems, err := adr.Check(dir)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "agentctl adr:", err)
		return exitUsage
	}
	for _, p := range problems {
		_, _ = fmt.Fprintln(stdout, p)
	}
	if len(problems) > 0 {
		_, _ = fmt.Fprintf(stdout, "adr: %d %s\n", len(problems), plural(len(problems), "problem", "problems"))
		return exitFail
	}
	_, _ = fmt.Fprintln(stdout, "adr: ok")
	return exitOK
}

// validADRArgs accepts `check` alone, or `new` followed by at least one word of title.
func validADRArgs(args []string) bool {
	switch {
	case len(args) == 1:
		return args[0] == cmdCheck
	case len(args) > 1:
		return args[0] == cmdNew
	}
	return false
}

// mustRel is the path relative to root, or the path itself when it cannot be made relative.
func mustRel(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil {
		return rel
	}
	return path
}
