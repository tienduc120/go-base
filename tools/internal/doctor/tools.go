package doctor

import (
	"regexp"
	"time"
)

// goSlowStart covers the one-time toolchain download GOTOOLCHAIN=auto does when go.mod asks for a newer Go.
const goSlowStart = 3 * time.Minute

// DefaultTools lists what a contributor needs. Minimums are the versions the gates were written against.
func DefaultTools(goVersion string) []Tool {
	return []Tool{
		{
			Name: "go", Command: []string{"go", "version"}, Min: goVersion, Required: true, Timeout: goSlowStart,
			Install: "install Go from https://go.dev/dl/ (go.mod's version is fetched automatically when GOTOOLCHAIN=auto)",
		},
		{Name: "git", Command: []string{"git", "--version"}, Required: true, Install: "install Git for Windows or your package manager"},
		{
			Name: "docker", Command: []string{"docker", "--version"}, Required: true,
			Install: "install Docker Desktop or Docker Engine",
		},
		{
			Name: "golangci-lint", Command: []string{"golangci-lint", "--version"}, Min: "2.15.0", Required: true,
			// Prebuilt binary: the project advises against go install / go tool for it.
			Install: "scoop install golangci-lint",
		},
		{
			Name: "gofumpt", Command: []string{"gofumpt", "--version"}, Min: "0.12.0", Required: true,
			Install: "go install mvdan.cc/gofumpt@latest",
		},
		{
			Name: "govulncheck", Command: []string{"govulncheck", "-version"}, Min: "1.9.0", Required: true,
			// The output also prints the Go version first, so anchor on the scanner.
			Pattern: regexp.MustCompile(`govulncheck@v(\d+\.\d+\.\d+)`),
			Install: "go install golang.org/x/vuln/cmd/govulncheck@latest",
		},
		{Name: "lefthook", Command: []string{"lefthook", "version"}, Min: "2.2.2", Required: true, Install: "scoop install lefthook"},
		{Name: "sqlc", Command: []string{"sqlc", "version"}, Min: "1.31.1", Required: true, Install: "scoop install sqlc"},
		{Name: "buf", Command: []string{"buf", "--version"}, Min: "1.74.0", Required: true, Install: "scoop install buf"},
		// Architecture linters are a spike: one will be dropped, so neither blocks the doctor.
		{
			Name: "go-arch-lint", Command: []string{"go-arch-lint", "version"}, Min: "1.19.0",
			Install: "go install github.com/fe3dback/go-arch-lint@latest",
		},
		{
			// arch-go has no version subcommand, so only presence is checked.
			Name: "arch-go", Command: []string{"arch-go", "--help"},
			Install: "go install github.com/arch-go/arch-go/v2@latest",
		},
	}
}
