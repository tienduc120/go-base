package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"time"

	"github.com/tienduc120/go-base/tools/internal/commitmsg"
	"github.com/tienduc120/go-base/tools/internal/debt"
	"github.com/tienduc120/go-base/tools/internal/proc"
)

const gitDiffLimit = 30 * time.Second

var refactorSubject = regexp.MustCompile(`^refactor(\(|!|:)`)

// runCommitmsg is the entry point for git's commit-msg hook, which passes the message file as the only argument.
// A refactor commit is also held to its promise: it may add tests but never edit one.
func runCommitmsg(ctx context.Context, args []string, stderr io.Writer, env environment) int {
	if len(args) != 1 {
		_, _ = fmt.Fprintln(stderr, "usage: agentctl commitmsg <message-file>")
		return exitUsage
	}
	data, err := os.ReadFile(args[0]) //nolint:gosec // the path is the message file git passes to the hook
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "agentctl commitmsg:", err)
		return exitUsage
	}

	problems := commitmsg.Validate(string(data))
	if len(problems) > 0 {
		_, _ = fmt.Fprintln(stderr, "commit message rejected:")
		for _, p := range problems {
			_, _ = fmt.Fprintln(stderr, "  -", p)
		}
		_, _ = fmt.Fprintln(
			stderr,
			"expected: type(scope): subject  (imperative, lowercase, no period, at most 72 characters)",
		)
		return exitFail
	}

	if refactorSubject.Match(data) {
		return guardRefactor(ctx, stderr, env)
	}
	return exitOK
}

// guardRefactor refuses a refactor commit whose staged changes edit or delete an existing test line. If git cannot be
// asked it warns and lets the commit through: a broken tool must not block the work.
func guardRefactor(ctx context.Context, stderr io.Writer, env environment) int {
	runner := env.procRunner
	if runner == nil {
		runner = proc.Exec{}
	}
	res, err := runner.Run(ctx, proc.Spec{
		Dir: env.cwd, Name: "git", Args: []string{"diff", "--cached", "--unified=0"}, Timeout: gitDiffLimit,
	})
	if err != nil || res.TimedOut {
		_, _ = fmt.Fprintln(
			stderr,
			"agentctl commitmsg: could not check the staged changes of this refactor, allowing it:",
			err,
		)
		return exitOK
	}

	violations := debt.RefactorViolations(res.Output)
	if len(violations) == 0 {
		return exitOK
	}
	_, _ = fmt.Fprintln(stderr, "refactor rejected: it edits or removes existing tests, so it changes behaviour:")
	for _, v := range violations {
		_, _ = fmt.Fprintln(stderr, "  -", v)
	}
	_, _ = fmt.Fprintln(
		stderr,
		"A refactor may only add tests. If behaviour really changes, make this a feat: or fix: commit.",
	)
	return exitFail
}
