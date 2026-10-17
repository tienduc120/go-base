// Package rules proves that every enforcer rule fires on a deliberately bad fixture and stays silent on a good one.
// A rule that nothing proves is advice, not a rule.
package rules

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	ViolationsDir = "testdata/violations"
	CleanDir      = "testdata/clean"
	// CleanID names the compliant twin in outcomes and is the staged directory every enforcer must leave alone.
	CleanID  = "clean"
	specFile = "rule.json"

	fileMode = 0o600
	dirMode  = 0o750
	// maxSeen caps how many unexpected findings a failure detail lists.
	maxSeen = 3
)

type Enforcer string

const (
	GolangciLint Enforcer = "golangci-lint"
	Agentlint    Enforcer = "agentlint"
	ArchGo       Enforcer = "arch-go"
)

// configFiles are copied into the staged module; a missing one is an error because the
// enforcer would otherwise run on its defaults and prove nothing about our configuration.
var configFiles = []string{".golangci.yml", "arch-go.yml"} //nolint:gochecknoglobals // read-only table

type Spec struct {
	Enforcer Enforcer `json:"enforcer"`
	// Contains must appear in a finding on the fixture; it pins the rule that fired, not just any finding.
	Contains string `json:"contains"`
	Doc      string `json:"doc"`
}

type Fixture struct {
	ID   string
	Spec Spec
}

// Finding is one report from an enforcer; Path is relative to the staged module with forward slashes.
type Finding struct {
	Path    string
	Message string
}

type Outcome struct {
	ID       string
	Enforcer Enforcer
	Pass     bool
	Detail   string
}

func validEnforcer(e Enforcer) bool {
	switch e {
	case GolangciLint, Agentlint, ArchGo:
		return true
	}
	return false
}

// Discover lists the fixtures under testdata/violations, sorted by id.
func Discover(fsys fs.FS) ([]Fixture, error) {
	entries, err := fs.ReadDir(fsys, ViolationsDir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", ViolationsDir, err)
	}

	var fixtures []Fixture
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		fx, loadErr := loadFixture(fsys, e.Name())
		if loadErr != nil {
			return nil, loadErr
		}
		fixtures = append(fixtures, fx)
	}
	if len(fixtures) == 0 {
		return nil, fmt.Errorf("no fixtures under %s", ViolationsDir)
	}
	sort.Slice(fixtures, func(i, j int) bool { return fixtures[i].ID < fixtures[j].ID })
	return fixtures, nil
}

func loadFixture(fsys fs.FS, id string) (Fixture, error) {
	data, err := fs.ReadFile(fsys, path.Join(ViolationsDir, id, specFile))
	if err != nil {
		return Fixture{}, fmt.Errorf("fixture %s: %w", id, err)
	}
	var spec Spec
	if err = json.Unmarshal(data, &spec); err != nil {
		return Fixture{}, fmt.Errorf("fixture %s: %s: %w", id, specFile, err)
	}
	if !validEnforcer(spec.Enforcer) {
		return Fixture{}, fmt.Errorf("fixture %s: unknown enforcer %q", id, spec.Enforcer)
	}
	if spec.Contains == "" {
		return Fixture{}, fmt.Errorf("fixture %s: contains must not be empty, it would match any finding", id)
	}
	return Fixture{ID: id, Spec: spec}, nil
}

// Stage writes every fixture and the clean twin into dest as one throwaway module, plus the enforcer configs.
func Stage(fsys fs.FS, fixtures []Fixture, dest, goVersion string) error {
	mod := fmt.Sprintf("module %s\n\ngo %s\n", stagedModule, goVersion)
	if err := writeFile(filepath.Join(dest, "go.mod"), []byte(mod)); err != nil {
		return err
	}

	for _, name := range configFiles {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return fmt.Errorf("stage %s: %w", name, err)
		}
		if err = writeFile(filepath.Join(dest, name), data); err != nil {
			return err
		}
	}

	for _, fx := range fixtures {
		if err := copyTree(fsys, path.Join(ViolationsDir, fx.ID), filepath.Join(dest, fx.ID)); err != nil {
			return err
		}
	}
	return copyTree(fsys, CleanDir, filepath.Join(dest, CleanID))
}

const stagedModule = "fixture.test"

func copyTree(fsys fs.FS, src, dst string) error {
	return fs.WalkDir(fsys, src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() == specFile {
			return nil
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(p, src+"/")
		return writeFile(filepath.Join(dst, filepath.FromSlash(rel)), data)
	})
}

func writeFile(p string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), dirMode); err != nil {
		return err
	}
	return os.WriteFile(p, data, fileMode)
}

// Evaluate fails closed: a fixture passes only when its own enforcer reported a finding under its
// directory that contains the expected text. The clean twin passes only when no enforcer touched it.
func Evaluate(fixtures []Fixture, findings map[Enforcer][]Finding) []Outcome {
	outcomes := make([]Outcome, 0, len(fixtures)+1)
	for _, fx := range fixtures {
		outcomes = append(outcomes, evaluateFixture(fx, findings[fx.Spec.Enforcer]))
	}
	return append(outcomes, evaluateClean(findings))
}

func evaluateFixture(fx Fixture, findings []Finding) Outcome {
	out := Outcome{ID: fx.ID, Enforcer: fx.Spec.Enforcer}
	var seen []string
	for _, f := range under(findings, fx.ID) {
		if strings.Contains(f.Message, fx.Spec.Contains) {
			out.Pass = true
			return out
		}
		seen = append(seen, f.Message)
	}

	out.Detail = fmt.Sprintf("%s reported nothing containing %q under %s/; saw %s",
		fx.Spec.Enforcer, fx.Spec.Contains, fx.ID, summarise(seen))
	return out
}

func evaluateClean(findings map[Enforcer][]Finding) Outcome {
	out := Outcome{ID: CleanID, Pass: true}
	var seen []string
	for enforcer, list := range findings {
		for _, f := range under(list, CleanID) {
			seen = append(seen, fmt.Sprintf("%s: %s (%s)", enforcer, f.Message, f.Path))
		}
	}
	if len(seen) > 0 {
		sort.Strings(seen)
		out.Pass = false
		out.Detail = "the compliant twin was flagged: " + summarise(seen)
	}
	return out
}

func under(findings []Finding, id string) []Finding {
	var out []Finding
	for _, f := range findings {
		if strings.HasPrefix(f.Path, id+"/") {
			out = append(out, f)
		}
	}
	return out
}

func summarise(msgs []string) string {
	if len(msgs) == 0 {
		return "no findings"
	}
	if len(msgs) > maxSeen {
		return fmt.Sprintf("%q and %d more", msgs[:maxSeen], len(msgs)-maxSeen)
	}
	return fmt.Sprintf("%q", msgs)
}

// ErrEnforcerFailed means an enforcer could not run at all, which must never be read as "the rule fired".
var ErrEnforcerFailed = errors.New("enforcer failed to run")

// ErrFixtureBroken means a fixture does not compile, so no enforcer can be trusted on the staged module.
var ErrFixtureBroken = errors.New("a fixture does not compile")
