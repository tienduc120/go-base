package debt

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	idDigits         = 10
	maxEvidence      = 120
	suppressionScore = 3.0
	blanketScore     = 6.0
	unexplainedExtra = 1.0
	todoScore        = 2.0
	complexityBase   = 4.0
	complexityWeight = 6.0
	lintComplexity   = 30.0
	lintLines        = 100.0
	largeComplexity  = 25
	largeLines       = 100
)

var marker = regexp.MustCompile(`^\s*(TODO|FIXME|HACK|XXX)\b(?:\(([^)]*)\))?`)

func skippedDir(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
		slices.Contains([]string{"testdata", "vendor", "node_modules"}, name)
}

// Scan reads every Go file under root and returns the debt in it, worst first.
func Scan(root string, opts Options) ([]Item, error) {
	opts = opts.withDefaults()

	var items []Item
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && p != root && skippedDir(d.Name()):
			return fs.SkipDir
		case d.IsDir() || !strings.HasSuffix(d.Name(), ".go"):
			return nil
		}
		found, scanErr := scanFile(root, p, opts)
		items = append(items, found...)
		return scanErr
	})
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Status = StatusUnconfirmed
	}
	Rank(items)
	return items, nil
}

func relPath(root, p string) string {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return filepath.ToSlash(p)
	}
	return filepath.ToSlash(rel)
}

// fingerprint is a short stable id: the same debt in the same place keeps it however the lines move.
func fingerprint(kind string, parts ...string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + strings.Join(parts, "\x00")))
	return kind + ":" + hex.EncodeToString(sum[:])[:idDigits]
}

type fileScan struct {
	rel         string
	fset        *token.FileSet
	file        *ast.File
	occurrences map[string]int
}

func scanFile(root, path string, opts Options) ([]Item, error) {
	rel := relPath(root, path)
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rel, err)
	}
	if generated(file) {
		return nil, nil
	}

	fs := &fileScan{rel: rel, fset: fset, file: file, occurrences: map[string]int{}}
	items := fs.comments()
	if !strings.HasSuffix(path, "_test.go") {
		items = append(items, fs.complexity(opts)...)
	}
	return items, nil
}

func generated(file *ast.File) bool {
	for _, cg := range file.Comments {
		if cg.Pos() > file.Package {
			break
		}
		for _, c := range cg.List {
			if strings.HasPrefix(c.Text, "// Code generated") {
				return true
			}
		}
	}
	return false
}

// occurrence numbers identical findings in the same place, so two identical directives are two debts.
func (f *fileScan) occurrence(key string) int {
	n := f.occurrences[key]
	f.occurrences[key]++
	return n
}

func (f *fileScan) comments() []Item {
	var items []Item
	for _, cg := range f.file.Comments {
		for _, c := range cg.List {
			if !strings.HasPrefix(c.Text, "//") {
				continue
			}
			line := f.fset.Position(c.Slash).Line
			enclosing := f.enclosing(c.Pos())
			if it, ok := f.suppression(c.Text, enclosing, line); ok {
				items = append(items, it)
			}
			if it, ok := f.untrackedMarker(c.Text, enclosing, line); ok {
				items = append(items, it)
			}
		}
	}
	return items
}

func (f *fileScan) suppression(text, enclosing string, line int) (Item, bool) {
	rest, ok := strings.CutPrefix(text, "//nolint")
	if !ok || (rest != "" && rest[0] != ':' && rest[0] != ' ') {
		return Item{}, false
	}

	linters := ""
	if after, hasLinters := strings.CutPrefix(rest, ":"); hasLinters {
		linters, rest, _ = strings.Cut(after, " ")
	}
	reason := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), "//"))

	subject, score := linters, suppressionScore
	if linters == "" {
		subject, score = "(all linters)", blanketScore
	}
	if reason == "" {
		score += unexplainedExtra
	}
	n := f.occurrence(strings.Join([]string{KindSuppression, enclosing, subject, reason}, "|"))
	return Item{
		ID:       fingerprint(KindSuppression, f.rel, enclosing, subject, reason, strconv.Itoa(n)),
		Kind:     KindSuppression,
		Where:    fmt.Sprintf("%s:%d", f.rel, line),
		Subject:  subject,
		Evidence: clip(text),
		Cost:     "S",
		Score:    score,
	}, true
}

func (f *fileScan) untrackedMarker(text, enclosing string, line int) (Item, bool) {
	m := marker.FindStringSubmatch(strings.TrimPrefix(text, "//"))
	if m == nil || strings.TrimSpace(m[2]) != "" {
		return Item{}, false
	}
	n := f.occurrence(strings.Join([]string{KindUntrackedTodo, enclosing, m[1], text}, "|"))
	return Item{
		ID:       fingerprint(KindUntrackedTodo, f.rel, enclosing, m[1], strings.TrimSpace(text), strconv.Itoa(n)),
		Kind:     KindUntrackedTodo,
		Where:    fmt.Sprintf("%s:%d", f.rel, line),
		Subject:  m[1],
		Evidence: clip(text),
		Cost:     "S",
		Score:    todoScore,
	}, true
}

func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxEvidence {
		return s[:maxEvidence] + "..."
	}
	return s
}

// enclosing names the top-level declaration a position sits in or documents, so an id does not depend on line numbers.
func (f *fileScan) enclosing(pos token.Pos) string {
	for _, d := range f.file.Decls {
		start := d.Pos()
		switch decl := d.(type) {
		case *ast.FuncDecl:
			if decl.Doc != nil {
				start = decl.Doc.Pos()
			}
			if pos >= start && pos <= decl.End() {
				return funcName(decl)
			}
		case *ast.GenDecl:
			if decl.Doc != nil {
				start = decl.Doc.Pos()
			}
			if pos >= start && pos <= decl.End() {
				return "(declaration)"
			}
		}
	}
	return "(file)"
}

func funcName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	return receiverName(fn.Recv.List[0].Type) + "." + fn.Name.Name
}

func receiverName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return receiverName(e.X)
	case *ast.IndexExpr:
		return receiverName(e.X)
	case *ast.IndexListExpr:
		return receiverName(e.X)
	}
	return "?"
}

func (f *fileScan) complexity(opts Options) []Item {
	var items []Item
	for _, d := range f.file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		cyclo := cyclomatic(fn)
		lines := f.fset.Position(fn.End()).Line - f.fset.Position(fn.Pos()).Line + 1
		if cyclo < opts.ComplexityMin && lines < opts.LinesMin {
			continue
		}

		cost := "M"
		if cyclo >= largeComplexity || lines >= largeLines {
			cost = "L"
		}
		ratio := max(float64(cyclo)/lintComplexity, float64(lines)/lintLines)
		name := funcName(fn)
		items = append(items, Item{
			ID:       fingerprint(KindComplexity, f.rel, name),
			Kind:     KindComplexity,
			Where:    fmt.Sprintf("%s:%d", f.rel, f.fset.Position(fn.Pos()).Line),
			Subject:  name,
			Evidence: fmt.Sprintf("cyclomatic %d, %d lines", cyclo, lines),
			Cost:     cost,
			Score:    complexityBase + complexityWeight*ratio,
		})
	}
	return items
}

// cyclomatic counts the independent paths through a function the way gocyclo does.
func cyclomatic(fn *ast.FuncDecl) int {
	n := 1
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch x := node.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
			n++
		case *ast.CaseClause:
			if len(x.List) > 0 {
				n++
			}
		case *ast.CommClause:
			if x.Comm != nil {
				n++
			}
		case *ast.BinaryExpr:
			if x.Op == token.LAND || x.Op == token.LOR {
				n++
			}
		}
		return true
	})
	return n
}
