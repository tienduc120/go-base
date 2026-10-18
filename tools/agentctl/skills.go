package main

import (
	"context"
	"fmt"
	"io"

	"github.com/tienduc120/go-base/tools/internal/doctor"
	"github.com/tienduc120/go-base/tools/internal/skills"
)

const skillsUsage = `usage: agentctl skills link|check

  link    make .claude/skills lead to .agents/skills, where skills are written. It is a symlink (a junction on Windows,
          which needs no administrator rights), is not committed, and has to be made once per clone.
  check   fail unless that link is in place, because without it Claude Code silently sees no skills.
`

func runSkills(ctx context.Context, args []string, stdout, stderr io.Writer, env environment) int {
	if len(args) != 1 || (args[0] != "link" && args[0] != cmdCheck) {
		_, _ = fmt.Fprint(stderr, skillsUsage)
		return exitUsage
	}
	root, err := doctor.FindRoot(env.cwd)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "agentctl skills:", err, "(run it inside the repository)")
		return exitUsage
	}

	if args[0] == "link" {
		if err = skills.Link(ctx, root); err != nil {
			_, _ = fmt.Fprintln(stderr, "agentctl skills:", err)
			return exitFail
		}
	}
	if err = skills.Check(root); err != nil {
		_, _ = fmt.Fprintln(stderr, "agentctl skills:", err)
		return exitFail
	}
	_, _ = fmt.Fprintf(stdout, "skills: ok (%s is linked to %s)\n", skills.LinkDir, skills.SourceDir)
	return exitOK
}
