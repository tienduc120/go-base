// Package dbcheck judges the SQL a service runs before it runs: query files against fixed rules, and each query's
// plan on a throwaway Postgres.
//
// sqlc compiles the queries and generates the Go; it hands a rule the compiled text, in which SELECT * is already
// expanded. So the rules live here, read both the source as written and the compiled text, and no service can loosen
// them by editing its own sqlc configuration.
package dbcheck

import (
	"fmt"
	"regexp"
	"strings"
)

// Block is one named query in a .sql file, as its author wrote it.
type Block struct {
	File string
	// Line is where the `-- name:` header sits.
	Line int
	Name string
	// Cmd is the sqlc command: one, many, exec, execrows, and so on.
	Cmd string
	// Raw is the statement with comments removed and string contents blanked, so no rule fires on prose.
	Raw string
	// Allow maps a rule to the reason its finding is accepted; an empty reason is kept so it can be refused.
	Allow map[string]string
}

var (
	headerLine = regexp.MustCompile(`^\s*--\s*name:\s*(\w+)\s+:(\w+)\s*$`)
	allowLine  = regexp.MustCompile(`^\s*--\s*agent:allow\s+([\w-]+)\s*(?::\s*(.*?))?\s*$`)
)

// ParseSource splits a sqlc query file into its named queries.
func ParseSource(file, content string) ([]Block, error) {
	p := &parser{file: file, seen: map[string]bool{}}
	for i, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		if err := p.add(line, i+1); err != nil {
			return nil, err
		}
	}
	p.flush()
	return p.blocks, nil
}

// parser accumulates the lines of the query it is inside, and closes it when the next header arrives.
type parser struct {
	file   string
	blocks []Block
	lines  []string
	seen   map[string]bool
}

func (p *parser) add(line string, number int) error {
	if m := headerLine.FindStringSubmatch(line); m != nil {
		return p.open(m[1], m[2], number)
	}
	if len(p.blocks) == 0 {
		if strings.TrimSpace(sanitize(line)) != "" {
			return fmt.Errorf("%s:%d: SQL before any `-- name: Name :cmd` header belongs to no query", p.file, number)
		}
		return nil
	}
	if m := allowLine.FindStringSubmatch(line); m != nil {
		p.blocks[len(p.blocks)-1].Allow[m[1]] = m[2]
	}
	p.lines = append(p.lines, line)
	return nil
}

func (p *parser) open(name, cmd string, number int) error {
	p.flush()
	if p.seen[name] {
		return fmt.Errorf("%s:%d: query %s is defined twice", p.file, number, name)
	}
	p.seen[name] = true
	p.blocks = append(p.blocks, Block{File: p.file, Line: number, Name: name, Cmd: cmd, Allow: map[string]string{}})
	return nil
}

func (p *parser) flush() {
	if len(p.blocks) > 0 {
		p.blocks[len(p.blocks)-1].Raw = strings.TrimSpace(sanitize(strings.Join(p.lines, "\n")))
	}
	p.lines = nil
}

// sanitize blanks comments and the inside of string literals, keeping every newline so positions stay put.
func sanitize(sql string) string {
	var out strings.Builder
	runes := []rune(sql)
	for i := 0; i < len(runes); i++ {
		switch {
		case hasPrefixAt(runes, i, "--"):
			i = skipLineComment(runes, i, &out)
		case hasPrefixAt(runes, i, "/*"):
			i = skipBlockComment(runes, i, &out)
		case runes[i] == '\'':
			out.WriteRune('\'')
			i = skipString(runes, i+1, &out)
		default:
			out.WriteRune(runes[i])
		}
	}
	return out.String()
}

func hasPrefixAt(runes []rune, i int, prefix string) bool {
	return strings.HasPrefix(string(runes[i:min(i+len(prefix), len(runes))]), prefix)
}

// skipLineComment blanks a -- comment and returns the index of the newline that ends it.
func skipLineComment(runes []rune, i int, out *strings.Builder) int {
	for i < len(runes) && runes[i] != '\n' {
		i++
	}
	out.WriteRune('\n')
	return i
}

// skipBlockComment blanks a /* */ comment, keeping its newlines, and returns the index of its closing slash.
func skipBlockComment(runes []rune, i int, out *strings.Builder) int {
	for i += 2; i+1 < len(runes) && !hasPrefixAt(runes, i, "*/"); i++ {
		if runes[i] == '\n' {
			out.WriteRune('\n')
		}
	}
	out.WriteRune(' ')
	return i + 1
}

// skipString consumes a string literal body starting at i, writing blanks, and returns the index of its closing
// quote. A doubled quote is an escaped quote and does not end the string.
func skipString(runes []rune, i int, out *strings.Builder) int {
	for ; i < len(runes); i++ {
		if runes[i] == '\'' {
			if i+1 < len(runes) && runes[i+1] == '\'' {
				out.WriteString("  ")
				i++
				continue
			}
			out.WriteRune('\'')
			return i
		}
		if runes[i] == '\n' {
			out.WriteRune('\n')
		} else {
			out.WriteRune(' ')
		}
	}
	return i
}
