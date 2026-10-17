// Package barego reports go statements outside the one package allowed to start goroutines.
// A goroutine started anywhere else has no owner to cancel it, bound it, or wait for it.
package barego

import (
	"go/ast"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

const (
	defaultAllow = "platform/async"
	message      = "bare go statement: start goroutines through platform/async " +
		"so they are supervised, bounded, and cancellable"
)

// New returns a fresh analyzer so flag state is not shared through a package variable.
func New() *analysis.Analyzer {
	allow := defaultAllow
	a := &analysis.Analyzer{
		Name:     "barego",
		Doc:      "report go statements outside the supervised-goroutine package",
		Requires: []*analysis.Analyzer{inspect.Analyzer},
	}
	a.Flags.StringVar(
		&allow,
		"allow",
		defaultAllow,
		"comma-separated import path suffixes allowed to use go statements",
	)
	a.Run = func(pass *analysis.Pass) (any, error) {
		check(pass, strings.Split(allow, ","))
		return nil, nil //nolint:nilnil // the analysis framework requires a nil result when ResultType is unset
	}
	return a
}

func check(pass *analysis.Pass, allowed []string) {
	for _, suffix := range allowed {
		if suffix != "" && strings.HasSuffix(pass.Pkg.Path(), suffix) {
			return
		}
	}

	insp, ok := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !ok {
		return
	}
	insp.Preorder([]ast.Node{(*ast.GoStmt)(nil)}, func(n ast.Node) {
		if strings.HasSuffix(pass.Fset.Position(n.Pos()).Filename, "_test.go") {
			return
		}
		pass.Reportf(n.Pos(), "%s", message)
	})
}
