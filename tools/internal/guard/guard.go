// Package guard decides whether an agent's edit or shell command may go ahead. It is a guard rail against mistakes
// and shortcuts, not a security boundary: a determined agent can route around string matching, so the git hooks and
// CI repeat the real checks.
package guard

import (
	"fmt"
	"strings"
)

// ApprovalEnv is set by the person, in the environment that starts the session, to approve a change to a protected
// file. It cannot be set from inside the session, which is the point.
const ApprovalEnv = "AGENTCTL_ALLOW_PROTECTED"

type Decision struct {
	Block  bool
	Reason string
}

const (
	judge   = "a rule that judges your work"
	fixture = "a fixture that proves a rule fires"
	legal   = "legal text"
)

type protectedRule struct {
	pattern string
	what    string
}

// protectedRules are the files an agent must not weaken to make a change pass. They are protected only once they
// exist: adding a new fixture or analyzer adds to the rules, it does not loosen them.
func protectedRules() []protectedRule {
	return []protectedRule{
		{".golangci.yml", judge},
		{"arch-go.yml", judge},
		{"lefthook.yml", judge},
		{".github/workflows/**", judge},
		{".github/CODEOWNERS", judge},
		{".claude/settings.json", judge},
		{"tools/analyzers/**", judge},
		{"tools/agentlint/**", judge},
		{"tools/internal/rules/**", judge},
		{"tools/internal/verify/**", judge},
		{"tools/internal/commitmsg/**", judge},
		{"tools/internal/guard/**", judge},
		{"testdata/**", fixture},
		{"LICENSE", legal},
		{"LICENSES/**", legal},
	}
}

func (r protectedRule) matches(rel string) bool {
	if prefix, ok := strings.CutSuffix(r.pattern, "/**"); ok {
		return rel == prefix || strings.HasPrefix(rel, prefix+"/")
	}
	return rel == r.pattern
}

// token is how the rule appears inside a shell command.
func (r protectedRule) token() string {
	if prefix, ok := strings.CutSuffix(r.pattern, "/**"); ok {
		return prefix + "/"
	}
	return r.pattern
}

func block(format string, args ...any) Decision {
	return Decision{Block: true, Reason: fmt.Sprintf("BLOCKED: "+format, args...)}
}

func approvalHint() string {
	return "If the user wants it changed, they approve by starting the session with " + ApprovalEnv + "=1."
}
