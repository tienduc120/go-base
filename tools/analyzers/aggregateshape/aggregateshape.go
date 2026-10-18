// Package aggregateshape holds an aggregate root to the shape the rest of the rules assume: a typed ID so one
// aggregate's identity cannot be passed for another's, a New constructor that checks the invariants, and a Restore
// constructor that only repositories call because it checks nothing.
//
// A root is marked with a //agent:aggregate comment in its doc, in the style of //agent:allow-exported-fields. Naming
// the root, rather than guessing it, keeps value objects and plain structs outside the rule.
package aggregateshape

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
	"unicode"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"
)

const (
	marker        = "//agent:aggregate"
	defaultDomain = "/internal/domain"
	defaultRepo   = "/internal/infrastructure"
	restorePrefix = "Restore"
)

type config struct {
	domain string
	repo   string
}

// New returns a fresh analyzer so flag state is not shared through a package variable.
func New() *analysis.Analyzer {
	domain, repo := defaultDomain, defaultRepo
	a := &analysis.Analyzer{
		Name:     "aggregateshape",
		Doc:      "report aggregate roots without a typed id, New, or Restore, and Restore called outside repositories",
		Requires: []*analysis.Analyzer{inspect.Analyzer},
	}
	a.Flags.StringVar(&domain, "domain", defaultDomain, "import path segment that marks a domain package")
	a.Flags.StringVar(&repo, "repository", defaultRepo, "import path segment of the packages allowed to call Restore")
	a.Run = func(pass *analysis.Pass) (any, error) {
		check(pass, config{domain: domain, repo: repo})
		// The framework requires a nil result when ResultType is unset.
		var none any
		return none, nil
	}
	return a
}

func check(pass *analysis.Pass, cfg config) {
	insp, ok := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !ok {
		return
	}
	inDomain := strings.Contains(pass.Pkg.Path()+"/", cfg.domain+"/")

	insp.Preorder([]ast.Node{(*ast.GenDecl)(nil), (*ast.CallExpr)(nil)}, func(n ast.Node) {
		if strings.HasSuffix(pass.Fset.Position(n.Pos()).Filename, "_test.go") {
			return
		}
		switch node := n.(type) {
		case *ast.GenDecl:
			if inDomain && node.Tok == token.TYPE {
				checkTypes(pass, node)
			}
		case *ast.CallExpr:
			checkRestoreCall(pass, cfg, node)
		}
	})
}

func checkTypes(pass *analysis.Pass, decl *ast.GenDecl) {
	for _, spec := range decl.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok || !marked(decl.Doc, ts.Doc) {
			continue
		}
		obj, ok := pass.TypesInfo.Defs[ts.Name].(*types.TypeName)
		if !ok {
			continue
		}
		named, ok := obj.Type().(*types.Named)
		if !ok {
			continue
		}
		for _, problem := range shapeProblems(pass.Pkg, named) {
			pass.Reportf(ts.Name.Pos(), "aggregate %s %s", named.Obj().Name(), problem)
		}
	}
}

// marked reports whether any of the comment groups holds the aggregate marker on a line of its own.
func marked(groups ...*ast.CommentGroup) bool {
	for _, g := range groups {
		if g == nil {
			continue
		}
		for _, c := range g.List {
			if strings.TrimSpace(c.Text) == marker {
				return true
			}
		}
	}
	return false
}

func shapeProblems(pkg *types.Package, named *types.Named) []string {
	name := named.Obj().Name()
	var problems []string
	if !hasTypedID(named) {
		problems = append(problems, "has no typed id: give it a field `id` of a named type such as "+name+
			"ID, so one aggregate's identity cannot be passed for another's")
	}
	if !returnsFirst(pkg, "New"+name, named) {
		problems = append(problems, "has no constructor New"+name+" returning "+name+
			": new values come only through a constructor that checks the invariants")
	}
	if !returnsFirst(pkg, restorePrefix+name, named) {
		problems = append(problems, "has no "+restorePrefix+name+" returning "+name+
			": a repository rebuilds a stored value through it")
	}
	return problems
}

// hasTypedID reports a struct with a field named id whose type is a defined type, not a predeclared one.
func hasTypedID(named *types.Named) bool {
	st, ok := named.Underlying().(*types.Struct)
	if !ok {
		return false
	}
	for field := range st.Fields() {
		if field.Name() != "id" {
			continue
		}
		idType, isNamed := field.Type().(*types.Named)
		return isNamed && idType.Obj().Pkg() != nil
	}
	return false
}

// returnsFirst reports a package-level function called name whose first result is want or a pointer to it.
func returnsFirst(pkg *types.Package, name string, want *types.Named) bool {
	fn, ok := pkg.Scope().Lookup(name).(*types.Func)
	if !ok {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Results().Len() == 0 {
		return false
	}
	got := sig.Results().At(0).Type()
	if ptr, isPtr := got.(*types.Pointer); isPtr {
		got = ptr.Elem()
	}
	return types.Identical(got, want)
}

// checkRestoreCall reports a call to a domain package's Restore function from anywhere but a repository: it skips the
// checks the New constructor makes, so a value built that way can break an invariant.
func checkRestoreCall(pass *analysis.Pass, cfg config, call *ast.CallExpr) {
	fn, ok := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !ok || fn.Pkg() == nil || !isRestore(fn.Name()) {
		return
	}
	calleePath, callerPath := fn.Pkg().Path(), pass.Pkg.Path()
	if calleePath == callerPath || !strings.Contains(calleePath+"/", cfg.domain+"/") ||
		strings.Contains(callerPath+"/", cfg.repo+"/") {
		return
	}
	pass.Reportf(call.Pos(), "%s is for repositories only: a value rebuilt this way skips the checks that %s makes; "+
		"use the constructor, or do the rebuild in infrastructure", fn.Name(), "New"+strings.TrimPrefix(fn.Name(), restorePrefix))
}

// isRestore reports a name of the form RestoreX, with X starting a new word.
func isRestore(name string) bool {
	rest, found := strings.CutPrefix(name, restorePrefix)
	if !found || rest == "" {
		return false
	}
	first := []rune(rest)[0]
	return unicode.IsUpper(first) || unicode.IsDigit(first)
}
