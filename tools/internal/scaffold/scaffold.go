// Package scaffold generates a new service that already satisfies every gate: layer rules, domain purity, port
// placement, supervised goroutines, a debug listener, and a test beside each piece of logic.
//
// The templates are the source of truth. A committed witness service is regenerated in a test and compared, so a
// template change that breaks a gate, or a gate change the templates no longer meet, fails here before a user sees it.
package scaffold

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"go/format"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"text/template"
	"unicode"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

var (
	serviceName   = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	aggregateName = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
)

const (
	dirMode  = 0o750
	fileMode = 0o600

	// Template delimiters that cannot appear in Go source, so no Go construct needs escaping.
	leftDelim  = "[["
	rightDelim = "]]"
)

// PostgresDir is where a service keeps its migrations, queries, sqlc configuration, and Postgres adapter, relative to
// the service. sqlc writes its generated Go into the db directory beneath it.
const PostgresDir = "internal/infrastructure/postgres"

// Spec says what to generate.
type Spec struct {
	// Module is the root module path the service's imports are rooted at.
	Module string
	// Service is the kebab-case service name; it becomes the directory under services/.
	Service string
	// Aggregate is the PascalCase name of the first aggregate; it defaults to the service name in PascalCase.
	Aggregate string
}

// File is one generated file; Path is slash-separated and relative to the repository root.
type File struct {
	Path    string
	Content []byte
}

// data is what the templates see. Every spelling of the aggregate is computed here, so a template never has to
// transform a name itself.
type data struct {
	Module    string
	Service   string
	Aggregate string
	// AggregateWords is the spaced form for messages, Route the plural path segment, and Table the plural table name.
	AggregateWords string
	Route          string
	Table          string
}

// Generate renders every file of the service. It does not touch the disk.
func Generate(spec Spec) ([]File, error) {
	d, err := newData(spec)
	if err != nil {
		return nil, err
	}

	var files []File
	for _, entry := range layout(d) {
		raw, readErr := templateFS.ReadFile(path.Join("templates", entry.source+".tmpl"))
		if readErr != nil {
			return nil, fmt.Errorf("template %s: %w", entry.source, readErr)
		}
		tmpl, parseErr := template.New(entry.source).
			Delims(leftDelim, rightDelim).
			Option("missingkey=error").
			Parse(string(raw))
		if parseErr != nil {
			return nil, fmt.Errorf("template %s: %w", entry.source, parseErr)
		}
		var buf bytes.Buffer
		if execErr := tmpl.Execute(&buf, d); execErr != nil {
			return nil, fmt.Errorf("render %s: %w", entry.source, execErr)
		}
		content := buf.Bytes()
		if strings.HasSuffix(entry.dest, ".go") {
			if content, err = format.Source(content); err != nil {
				return nil, fmt.Errorf("render %s produced invalid Go: %w", entry.source, err)
			}
		}
		files = append(files, File{Path: path.Join("services", d.Service, entry.dest), Content: content})
	}
	return files, nil
}

type layoutEntry struct{ source, dest string }

func layout(d data) []layoutEntry {
	snake := snakeOf(d.Aggregate)
	return []layoutEntry{
		{"main.go", "cmd/" + d.Service + "-api/main.go"},
		{"main_test.go", "cmd/" + d.Service + "-api/main_test.go"},
		{"domain.go", "internal/domain/" + snake + ".go"},
		{"domain_test.go", "internal/domain/" + snake + "_test.go"},
		{"application.go", "internal/application/create_" + snake + ".go"},
		{"application_test.go", "internal/application/create_" + snake + "_test.go"},
		{"repository.go", "internal/infrastructure/memory/" + snake + "_repository.go"},
		{"repository_test.go", "internal/infrastructure/memory/" + snake + "_repository_test.go"},
		{"migration.sql", PostgresDir + "/migrations/0001_init.sql"},
		{"queries.sql", PostgresDir + "/queries/" + snake + ".sql"},
		{"sqlc.yaml", PostgresDir + "/sqlc.yaml"},
		{"pg_repository.go", PostgresDir + "/" + snake + "_repository.go"},
		{"pg_repository_test.go", PostgresDir + "/" + snake + "_repository_test.go"},
		// Not "httpapi": Bitdefender quarantines a test binary of exactly that name.
		{"load_main.go", "cmd/" + d.Service + "-load/main.go"},
		{"load_main_test.go", "cmd/" + d.Service + "-load/main_test.go"},
		{"load_scenario.go", "cmd/" + d.Service + "-load/scenario.go"},
		{"load_scenario_test.go", "cmd/" + d.Service + "-load/scenario_test.go"},
		{"handler.go", "internal/interfaces/rest/handler.go"},
		{"handler_test.go", "internal/interfaces/rest/handler_test.go"},
	}
}

func newData(spec Spec) (data, error) {
	if spec.Module == "" {
		return data{}, errors.New("module path is empty")
	}
	if !serviceName.MatchString(spec.Service) {
		return data{}, fmt.Errorf(
			"service name %q must be lowercase words joined by hyphens, such as billing or order-intake",
			spec.Service,
		)
	}
	aggregate := spec.Aggregate
	if aggregate == "" {
		aggregate = pascal(spec.Service)
	}
	if !aggregateName.MatchString(aggregate) {
		return data{}, fmt.Errorf("aggregate name %q must be PascalCase, such as Invoice", aggregate)
	}
	words := strings.ToLower(strings.ReplaceAll(snakeOf(aggregate), "_", " "))
	return data{
		Module:         spec.Module,
		Service:        spec.Service,
		Aggregate:      aggregate,
		AggregateWords: words,
		Route:          plural(strings.ReplaceAll(snakeOf(aggregate), "_", "-")),
		Table:          plural(snakeOf(aggregate)),
	}, nil
}

func pascal(kebab string) string {
	var b strings.Builder
	for part := range strings.SplitSeq(kebab, "-") {
		b.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	return b.String()
}

// snakeOf turns InvoiceLine into invoice_line.
func snakeOf(pascalName string) string {
	var b strings.Builder
	for i, r := range pascalName {
		if i > 0 && unicode.IsUpper(r) {
			b.WriteByte('_')
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

func plural(word string) string {
	switch {
	case strings.HasSuffix(word, "y") && len(word) > 1 && !strings.ContainsRune("aeiou", rune(word[len(word)-2])):
		return word[:len(word)-1] + "ies"
	case slices.ContainsFunc([]string{"s", "x", "ch", "sh"}, func(end string) bool { return strings.HasSuffix(word, end) }):
		return word + "es"
	default:
		return word + "s"
	}
}

// Write puts the files under root. It refuses to touch a service that already exists, because a generator that
// overwrites hand-written code is worse than no generator.
func Write(root string, files []File) error {
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(f.Path))); err == nil {
			return fmt.Errorf("%s already exists: remove it or pick another service name", f.Path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("check %s: %w", f.Path, err)
		}
	}
	for _, f := range files {
		target := filepath.Join(root, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(target), dirMode); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(f.Path), err)
		}
		if err := os.WriteFile(target, f.Content, fileMode); err != nil {
			return fmt.Errorf("write %s: %w", f.Path, err)
		}
	}
	return nil
}
