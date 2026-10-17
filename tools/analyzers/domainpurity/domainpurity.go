// Package domainpurity keeps domain packages free of state leaks and hidden inputs: aggregates expose behaviour
// instead of exported fields, carry no serialization tags, and never read the clock.
package domainpurity

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

const (
	defaultSegment = "/internal/domain"
	directive      = "//agent:allow-exported-fields"
)

// New returns a fresh analyzer so flag state is not shared through a package variable.
func New() *analysis.Analyzer {
	segment := defaultSegment
	a := &analysis.Analyzer{
		Name:     "domainpurity",
		Doc:      "report exported fields, struct tags, and clock reads in domain packages",
		Requires: []*analysis.Analyzer{inspect.Analyzer},
	}
	a.Flags.StringVar(&segment, "segment", defaultSegment, "import path segment that marks a domain package")
	a.Run = func(pass *analysis.Pass) (any, error) {
		check(pass, segment)
		return nil, nil //nolint:nilnil // the analysis framework requires a nil result when ResultType is unset
	}
	return a
}

func check(pass *analysis.Pass, segment string) {
	if !strings.Contains(pass.Pkg.Path()+"/", segment+"/") {
		return
	}
	insp, ok := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !ok {
		return
	}

	insp.Preorder([]ast.Node{(*ast.GenDecl)(nil), (*ast.CallExpr)(nil)}, func(n ast.Node) {
		if strings.HasSuffix(pass.Fset.Position(n.Pos()).Filename, "_test.go") {
			return
		}
		switch node := n.(type) {
		case *ast.GenDecl:
			checkTypes(pass, node)
		case *ast.CallExpr:
			checkClock(pass, node)
		}
	})
}

func checkTypes(pass *analysis.Pass, decl *ast.GenDecl) {
	for _, spec := range decl.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			continue
		}
		allowed := allowsExportedFields(pass, decl, ts)
		for _, field := range st.Fields.List {
			if field.Tag != nil {
				pass.Reportf(field.Tag.Pos(),
					"struct tag in a domain type: serialization belongs in infrastructure or interfaces")
			}
			if allowed {
				continue
			}
			for _, name := range exportedNames(field) {
				pass.Reportf(name.Pos(), "exported field %s on domain type %s: expose behaviour and accessors, "+
					"not state (%s <reason> marks a pure data carrier)", name.Name, ts.Name.Name, directive)
			}
		}
	}
}

// allowsExportedFields reads the opt-out directive; a directive without a reason is itself reported.
func allowsExportedFields(pass *analysis.Pass, decl *ast.GenDecl, ts *ast.TypeSpec) bool {
	for _, doc := range []*ast.CommentGroup{ts.Doc, decl.Doc} {
		if doc == nil {
			continue
		}
		for _, c := range doc.List {
			rest, found := strings.CutPrefix(c.Text, directive)
			if !found {
				continue
			}
			if strings.TrimSpace(rest) == "" {
				pass.Reportf(ts.Name.Pos(), "%s directive needs a reason", directive)
			}
			return true
		}
	}
	return false
}

func exportedNames(field *ast.Field) []*ast.Ident {
	if len(field.Names) == 0 {
		if id := embeddedName(field.Type); id != nil && id.IsExported() {
			return []*ast.Ident{id}
		}
		return nil
	}
	var out []*ast.Ident
	for _, name := range field.Names {
		if name.IsExported() {
			out = append(out, name)
		}
	}
	return out
}

func embeddedName(expr ast.Expr) *ast.Ident {
	switch e := expr.(type) {
	case *ast.Ident:
		return e
	case *ast.SelectorExpr:
		return e.Sel
	case *ast.StarExpr:
		return embeddedName(e.X)
	}
	return nil
}

func checkClock(pass *analysis.Pass, call *ast.CallExpr) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "time" {
		return
	}
	switch fn.Name() {
	case "Now", "Since", "Until":
		pass.Reportf(call.Pos(), "domain reads the clock (time.%s): take the time as a parameter", fn.Name())
	}
}
