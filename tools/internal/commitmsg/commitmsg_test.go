package commitmsg_test

import (
	"strings"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/commitmsg"
)

func TestValidAccepts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		msg  string
	}{
		{"plain", "feat: add the thing"},
		{"scope", "fix(tools): handle an empty module list"},
		{"breaking marker", "feat(api)!: drop the v1 route"},
		{"scope with dash", "build(lint): add strict config"},
		{
			"with body and trailer",
			"docs(repo): explain the gates\n\nWhy this matters.\n\nCo-Authored-By: A <a@example.com>\n",
		},
		{"trailing newline", "chore: tidy\n"},
		{
			"git comment lines are ignored",
			"feat: add it\n\n# Please enter the commit message\n# Changes to be committed:\n",
		},
		{"merge commit", "Merge branch 'feature' into main"},
		{"revert commit", "Revert \"feat: add the thing\""},
		{"subject of exactly 72", "fix: " + strings.Repeat("a", 67)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if problems := commitmsg.Validate(tc.msg); len(problems) != 0 {
				t.Errorf("Validate(%q) = %v, want none", tc.msg, problems)
			}
		})
	}
}

func TestValidateRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		msg  string
		want string
	}{
		{"empty", "", "empty"},
		{"only comments", "# nothing here\n", "empty"},
		{"no type", "add the thing", "type(scope): subject"},
		{"unknown type", "feature: add the thing", "type"},
		{"missing space after colon", "feat:add the thing", "type(scope): subject"},
		{"uppercase scope", "feat(Tools): add it", "scope"},
		{"empty scope", "feat(): add it", "scope"},
		{"uppercase subject", "feat: Add the thing", "lowercase"},
		{"trailing period", "feat: add the thing.", "period"},
		{"too long", "fix: " + strings.Repeat("a", 68), "72"},
		{"body without blank line", "feat: add it\nbody right away", "blank line"},
		{"empty subject", "feat: ", "subject"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			problems := commitmsg.Validate(tc.msg)
			if len(problems) == 0 {
				t.Fatalf("Validate(%q) accepted a bad message", tc.msg)
			}
			if !strings.Contains(strings.Join(problems, " | "), tc.want) {
				t.Errorf("problems %v do not mention %q", problems, tc.want)
			}
		})
	}
}

func TestValidateReportsEveryProblem(t *testing.T) {
	t.Parallel()

	problems := commitmsg.Validate("feat(Bad): Add the thing.")
	if len(problems) < 3 {
		t.Errorf("got %v: an agent wants all the fixes in one pass, not one per attempt", problems)
	}
}
