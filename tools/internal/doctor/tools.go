package doctor

import (
	"regexp"
	"time"
)

const (
	flagVersion = "--version"
	cmdVersion  = "version"
)

// goSlowStart covers the one-time toolchain download GOTOOLCHAIN=auto does when go.mod asks for a newer Go.
const goSlowStart = 3 * time.Minute

// DefaultTools lists what a contributor needs. Minimums are the versions the gates were written against.
func DefaultTools(goVersion string) []Tool {
	return []Tool{
		{
			Name:     "go",
			Command:  []string{"go", cmdVersion},
			Min:      goVersion,
			Required: true,
			Timeout:  goSlowStart,
			Install:  "install Go from https://go.dev/dl/ (go.mod's version is fetched automatically when GOTOOLCHAIN=auto)",
		},
		{
			Name:     "git",
			Command:  []string{"git", flagVersion},
			Required: true,
			Install:  "install Git for Windows or your package manager",
		},
		{
			Name: "docker", Command: []string{"docker", flagVersion}, Required: true,
			Install: "install Docker Desktop or Docker Engine",
		},
		{
			Name: "golangci-lint", Command: []string{"golangci-lint", flagVersion}, Min: "2.15.0", Required: true,
			// Prebuilt binary: the project advises against go install / go tool for it.
			Install: "scoop install golangci-lint",
		},
		{
			Name: "gofumpt", Command: []string{"gofumpt", flagVersion}, Min: "0.12.0", Required: true,
			Install: "go install mvdan.cc/gofumpt@latest",
		},
		{
			Name: "govulncheck", Command: []string{"govulncheck", "-version"}, Min: "1.9.0", Required: true,
			// The output also prints the Go version first, so anchor on the scanner.
			Pattern: regexp.MustCompile(`govulncheck@v(\d+\.\d+\.\d+)`),
			Install: "go install golang.org/x/vuln/cmd/govulncheck@latest",
		},
		{
			Name:     "lefthook",
			Command:  []string{"lefthook", cmdVersion},
			Min:      "2.2.2",
			Required: true,
			Install:  "scoop install lefthook",
		},
		{
			Name:     "sqlc",
			Command:  []string{"sqlc", cmdVersion},
			Min:      "1.31.1",
			Required: true,
			Install:  "scoop install sqlc",
		},
		{
			Name:     "buf",
			Command:  []string{"buf", flagVersion},
			Min:      "1.74.0",
			Required: true,
			Install:  "scoop install buf",
		},
		{
			// arch-go reports "master" as its version, so only presence is checked.
			Name: "arch-go", Command: []string{"arch-go", "--help"}, Required: true,
			Install: "go install github.com/arch-go/arch-go/v2@latest",
		},
	}
}
