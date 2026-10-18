package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/tienduc120/go-base/tools/internal/doctor"
	"github.com/tienduc120/go-base/tools/internal/guard"
)

const (
	// hookBlock is the exit code Claude Code treats as "refuse this tool call"; the reason on stderr goes to the agent.
	// Every other failure here exits 1, which lets the call through, so a broken guard never bricks a session.
	hookBlock = 2

	maxHookInput = 1 << 20

	// gitCheckLimit bounds the one git call made for a protected path, so a stuck git cannot stall an edit.
	gitCheckLimit = 5 * time.Second
)

const hookUsage = `usage: agentctl hook pre-edit|pre-bash   (the tool call arrives as JSON on stdin)
`

type hookInput struct {
	Cwd       string `json:"cwd"`
	ToolInput struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
		Command      string `json:"command"`
	} `json:"tool_input"`
}

func runHook(ctx context.Context, args []string, stderr io.Writer, env environment) int {
	if len(args) != 1 || env.stdin == nil {
		_, _ = fmt.Fprint(stderr, hookUsage)
		return exitFail
	}

	data, err := io.ReadAll(io.LimitReader(env.stdin, maxHookInput))
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "agentctl hook: cannot read the tool call, allowing it:", err)
		return exitFail
	}
	var in hookInput
	if err = json.Unmarshal(data, &in); err != nil {
		_, _ = fmt.Fprintln(stderr, "agentctl hook: cannot parse the tool call, allowing it:", err)
		return exitFail
	}

	cwd := in.Cwd
	if cwd == "" {
		cwd = env.cwd
	}
	approved := env.env(guard.ApprovalEnv) == "1"

	var decision guard.Decision
	switch args[0] {
	case "pre-edit":
		decision = checkEdit(ctx, in, cwd, approved)
	case "pre-bash":
		decision = guard.CheckCommand(in.ToolInput.Command, approved)
	default:
		_, _ = fmt.Fprint(stderr, hookUsage)
		return exitFail
	}

	if decision.Block {
		_, _ = fmt.Fprintln(stderr, decision.Reason)
		return hookBlock
	}
	return exitOK
}

func checkEdit(ctx context.Context, in hookInput, cwd string, approved bool) guard.Decision {
	target := in.ToolInput.FilePath
	if target == "" {
		target = in.ToolInput.NotebookPath
	}
	if target == "" || approved {
		return guard.Decision{}
	}
	root, err := doctor.FindRoot(cwd)
	if err != nil {
		return guard.Decision{}
	}
	// "Exists" means committed: a rule that is in HEAD has been accepted and may not be weakened, while a gate still
	// being written in this session has not been accepted by anyone yet. With no git history nothing is protected.
	return guard.CheckEdit(root, cwd, target, func(p string) bool { return committed(ctx, root, p) })
}

func committed(ctx context.Context, root, abs string) bool {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, gitCheckLimit)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", root, "cat-file", "-e", "HEAD:"+filepath.ToSlash(rel))
	return cmd.Run() == nil
}
