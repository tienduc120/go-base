package main

import (
	"fmt"
	"io"
	"os"

	"github.com/tienduc120/go-base/tools/internal/commitmsg"
)

// runCommitmsg is the entry point for git's commit-msg hook, which passes the message file as the only argument.
func runCommitmsg(args []string, stderr io.Writer) int {
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
	if len(problems) == 0 {
		return exitOK
	}
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
