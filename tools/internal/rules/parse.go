package rules

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

func slash(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	return strings.TrimPrefix(p, "./")
}

// ParseGolangci reads golangci-lint's JSON report.
func ParseGolangci(data []byte) ([]Finding, error) {
	var report struct {
		Issues []struct {
			FromLinter string `json:"FromLinter"`
			Text       string `json:"Text"`
			Pos        struct {
				Filename string `json:"Filename"`
			} `json:"Pos"`
		} `json:"Issues"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("golangci-lint report: %w", err)
	}

	findings := make([]Finding, 0, len(report.Issues))
	for _, is := range report.Issues {
		findings = append(findings, Finding{
			Path:    slash(is.Pos.Filename),
			Message: is.FromLinter + ": " + is.Text,
		})
	}
	return findings, nil
}

var vetLine = regexp.MustCompile(`^(.+?\.go):\d+:\d+: (.+)$`)

// ParseVet reads `go vet` output, ignoring the package headers and the trailing exit status.
func ParseVet(output string) []Finding {
	var findings []Finding
	for line := range strings.SplitSeq(output, "\n") {
		m := vetLine.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil {
			continue
		}
		findings = append(findings, Finding{Path: slash(m[1]), Message: m[2]})
	}
	return findings
}

// ParseArchGo reads arch-go's JSON report and returns one finding per failing package.
func ParseArchGo(data []byte, module string) ([]Finding, error) {
	var report struct {
		Compliance struct {
			Details struct {
				DependenciesRules struct {
					Details []struct {
						PackageDetails []struct {
							Package string   `json:"package"`
							Pass    bool     `json:"pass"`
							Details []string `json:"details"`
						} `json:"packageDetails"`
					} `json:"details"`
				} `json:"dependenciesRules"`
			} `json:"details"`
		} `json:"compliance"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("arch-go report: %w", err)
	}

	var findings []Finding
	for _, rule := range report.Compliance.Details.DependenciesRules.Details {
		for _, pkg := range rule.PackageDetails {
			if pkg.Pass {
				continue
			}
			findings = append(findings, Finding{
				Path:    strings.TrimPrefix(pkg.Package, module+"/"),
				Message: "arch-go: " + strings.Join(pkg.Details, "; "),
			})
		}
	}
	return findings, nil
}
