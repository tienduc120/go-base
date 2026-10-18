package guard_test

import (
	"strings"
	"testing"

	"github.com/tienduc120/go-base/tools/internal/guard"
)

func TestCheckCommandBlocks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cmd  string
		want string
	}{
		// Skipping the gates.
		{"commit no-verify", `git commit --no-verify -m "x"`, "hooks"},
		{"commit short flag", `git commit -n -m "x"`, "hooks"},
		{"commit combined short flags", `git commit -nm "x"`, "hooks"},
		{"push no-verify", `git push --no-verify origin main`, "hooks"},
		{"husky off", `HUSKY=0 git commit -m "x"`, "hooks"},
		{"lefthook off", `LEFTHOOK=0 git commit -m "x"`, "hooks"},
		{"lefthook exclude", `LEFTHOOK_EXCLUDE=verify git commit -m "x"`, "hooks"},
		{"hooks path", `git -c core.hooksPath=/dev/null commit -m "x"`, "hooks"},
		{"uninstall", `lefthook uninstall`, "hooks"},
		{"in a chain", `cd tools && git commit --no-verify -m x`, "hooks"},

		// Changing the rules through the shell.
		{"redirect over a rule", `echo "x" > .golangci.yml`, "protected"},
		{"append to a rule", `echo "x" >> arch-go.yml`, "protected"},
		{"sed in place", `sed -i 's/a/b/' .golangci.yml`, "protected"},
		{"remove a rule file", `rm lefthook.yml`, "protected"},
		{"remove a fixture", `rm -r testdata/violations/forbidigo-time-sleep`, "protected"},
		{"move a fixture", `mv testdata/clean testdata/clean.old`, "protected"},
		{"powershell set-content", `Set-Content -Path .golangci.yml -Value x`, "protected"},
		{"powershell remove-item", `Remove-Item .github/workflows/ci.yml`, "protected"},
		{"edit the guard", `sed -i 's/Block: true/Block: false/' tools/internal/guard/edit.go`, "protected"},

		// Commands that never come back.
		{"go test without a limit", `go test ./...`, "timeout"},
		{"go test race without a limit", `go test -race ./tools/...`, "timeout"},
		{"go test in a chain", `cd tools && go test ./...`, "timeout"},
		{"compose up in the foreground", `docker compose up`, "detach"},
		{"compose v1 up", `docker-compose up --build`, "detach"},
		{"a server in the foreground", `go run ./services/billing/cmd/billing-api`, "foreground"},
		{"tail follow", `tail -f app.log`, "follow"},
		{"docker logs follow", `docker logs -f billing`, "follow"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := guard.CheckCommand(tc.cmd, false)
			if !got.Block {
				t.Fatalf("%q must be blocked", tc.cmd)
			}
			if !strings.Contains(strings.ToLower(got.Reason), tc.want) {
				t.Errorf("reason %q should mention %q", got.Reason, tc.want)
			}
		})
	}
}

func TestCheckCommandAllows(t *testing.T) {
	t.Parallel()

	for _, cmd := range []string{
		`go test -timeout 120s ./...`,
		`go test -timeout=2m -race ./tools/...`,
		`go run ./tools/agentctl run -- go test ./...`,
		`go run ./tools/agentctl verify`,
		`go run ./tools/agentctl doctor`,
		`git commit -m "fix: handle --no-verify in the docs"`,
		"git commit -F - <<'EOF'\nfeat: x\n\nmentions --no-verify and .golangci.yml > arch-go.yml\nEOF",
		`git push origin main`,
		`git push -n origin main`,
		`git log --oneline`,
		`cat .golangci.yml`,
		`grep -n forbidigo .golangci.yml`,
		`golangci-lint run --config .golangci.yml ./...`,
		`git diff -- arch-go.yml`,
		`ls testdata/violations`,
		`grep -r "time.Sleep" testdata/`,
		`docker compose up -d`,
		`docker compose up --detach`,
		`docker logs billing`,
		`tail -n 50 app.log`,
		`go build ./...`,
		`go vet ./...`,
		`go run ./tools/agentctl fmt md`,
		`echo "x" > notes.txt`,
		`rm -f .agentctl/logs/old.log`,
		`mkdir -p docs/adr`,
		`GOFLAGS=-count=1 go test -timeout 30s ./pkg`,
	} {
		if got := guard.CheckCommand(cmd, false); got.Block {
			t.Errorf("%q must be allowed, blocked with: %q", cmd, got.Reason)
		}
	}
}

func TestApprovalLiftsOnlyTheProtectedFileRule(t *testing.T) {
	t.Parallel()

	if got := guard.CheckCommand(`sed -i 's/a/b/' .golangci.yml`, true); got.Block {
		t.Errorf("an approved change to a rule file may go through: %q", got.Reason)
	}
	for _, cmd := range []string{`git commit --no-verify -m x`, `go test ./...`, `tail -f x.log`} {
		if got := guard.CheckCommand(cmd, true); !got.Block {
			t.Errorf("approval to change a rule is not approval to skip the gates or hang: %q", cmd)
		}
	}
}

func TestCheckCommandReasonsSayWhatToDoInstead(t *testing.T) {
	t.Parallel()

	for cmd, want := range map[string]string{
		`go test ./...`:               "agentctl run",
		`git commit --no-verify -m x`: "fix",
		`docker compose up`:           "-d",
		`tail -f x.log`:               "tail -n",
	} {
		got := guard.CheckCommand(cmd, false)
		if !strings.Contains(strings.ToLower(got.Reason), strings.ToLower(want)) {
			t.Errorf("%q: reason %q should suggest %q", cmd, got.Reason, want)
		}
	}
}
