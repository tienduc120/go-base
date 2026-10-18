// Package txboundary keeps a use case's transaction to what can be rolled back. Inside the function passed to the
// transaction runner only repositories, the outbox, and the clock may be used: an email, a payment, or any network call
// made there cannot be undone when the transaction fails, and holds its locks for as long as the call takes.
package txboundary

import (
	"go/ast"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"
)

const (
	defaultMethod  = "InTransaction"
	defaultAllow   = "Repository,Outbox,Clock"
	defaultSegment = "/internal/application"
)

// networkPackages are the standard-library packages whose every call reaches the network.
func networkPackages() []string {
	return []string{"net/http", "net/smtp", "net/rpc", "net"}
}

type config struct {
	method  string
	allow   []string
	segment string
}

// New returns a fresh analyzer so flag state is not shared through a package variable.
func New() *analysis.Analyzer {
	method, allow, segment := defaultMethod, defaultAllow, defaultSegment
	a := &analysis.Analyzer{
		Name:     "txboundary",
		Doc:      "report effects that cannot be rolled back inside a transaction, and transactions opened outside application",
		Requires: []*analysis.Analyzer{inspect.Analyzer},
	}
	a.Flags.StringVar(&method, "method", defaultMethod, "name of the method that runs a function in a transaction")
	a.Flags.StringVar(
		&allow,
		"allow",
		defaultAllow,
		"comma-separated interface name suffixes usable inside a transaction",
	)
	a.Flags.StringVar(&segment, "segment", defaultSegment, "import path segment that marks the application layer")
	a.Run = func(pass *analysis.Pass) (any, error) {
		check(pass, config{method: method, allow: strings.Split(allow, ","), segment: segment})
		// The framework requires a nil result when ResultType is unset.
		var none any
		return none, nil
	}
	return a
}

type checker struct {
	pass *analysis.Pass
	cfg  config
	// seen stops a call reported once from being reported again when a nested transaction is walked a second time.
	seen map[ast.Node]bool
}

func check(pass *analysis.Pass, cfg config) {
	insp, ok := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !ok {
		return
	}
	c := &checker{pass: pass, cfg: cfg, seen: make(map[ast.Node]bool)}

	insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call, _ := n.(*ast.CallExpr)
		if strings.HasSuffix(pass.Fset.Position(call.Pos()).Filename, "_test.go") || !c.isRunner(call) {
			return
		}
		if !c.inApplication() {
			c.report(
				call,
				"transaction opened outside application: a transaction belongs to a use case (ADR-0015)",
			)
		}
		if lit, isLit := call.Args[len(call.Args)-1].(*ast.FuncLit); isLit {
			c.checkBody(lit)
		}
	})
}

func (c *checker) isRunner(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == c.cfg.method && len(call.Args) > 0
}

// inApplication is true for the application layer and for any package outside an internal directory, such as cmd.
func (c *checker) inApplication() bool {
	path := c.pass.Pkg.Path() + "/"
	return strings.Contains(path, c.cfg.segment+"/") || !strings.Contains(path, "/internal/")
}

func (c *checker) report(at ast.Node, message string) {
	if c.seen[at] {
		return
	}
	c.seen[at] = true
	c.pass.Reportf(at.Pos(), "%s", message)
}

func (c *checker) checkBody(lit *ast.FuncLit) {
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			c.checkCall(call)
		}
		return true
	})
}

func (c *checker) checkCall(call *ast.CallExpr) {
	if c.isRunner(call) {
		c.report(call, "nested transaction: a use case is one transaction, opened once (ADR-0015)")
		return
	}
	if fn, ok := typeutil.Callee(c.pass.TypesInfo, call).(*types.Func); ok && fn.Pkg() != nil {
		if pkg := fn.Pkg().Path(); isNetwork(pkg) {
			c.report(
				call,
				pkg+" call inside a transaction: it cannot be rolled back and holds the transaction's "+
					"locks while it waits; write an outbox row and send it after commit (ADR-0015)",
			)
			return
		}
	}
	c.checkReceiver(call)
}

func isNetwork(pkg string) bool { return slices.Contains(networkPackages(), pkg) }

// checkReceiver reports a call on an interface the transaction may not use. Interfaces from the standard library,
// such as context.Context and error, are not effects, and an allowed suffix names a repository, outbox, or clock.
func (c *checker) checkReceiver(call *ast.CallExpr) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	named, ok := c.pass.TypesInfo.TypeOf(sel.X).(*types.Named)
	if !ok || !types.IsInterface(named) || named.Obj().Pkg() == nil || isStandard(named.Obj().Pkg().Path()) {
		return
	}
	name := named.Obj().Name()
	for _, suffix := range c.cfg.allow {
		if suffix != "" && strings.HasSuffix(name, suffix) {
			return
		}
	}
	c.report(call, "call on "+name+" inside a transaction: only repositories, the outbox, and the clock belong "+
		"here; an external effect happens after commit, through the outbox (ADR-0015)")
}

// isStandard reports a standard-library import path, which has no dot in its first element.
func isStandard(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}
