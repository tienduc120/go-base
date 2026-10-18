package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/tienduc120/go-base/tools/internal/debt"
	"github.com/tienduc120/go-base/tools/internal/doctor"
	"github.com/tienduc120/go-base/tools/internal/proc"
	"github.com/tienduc120/go-base/tools/internal/verify"
)

const (
	defaultDebtTop      = 10
	defaultCoverageMin  = 70.0
	nextUnconfirmedRows = 3
)

const debtUsage = `usage: agentctl debt <command>

  scan [--coverage] [--all] [--json] [--top N]
      list the debt the gates allow: suppressions, TODOs nobody owns, functions near the complexity limit, and with
      --coverage (which runs the tests) packages with weak or no tests. Nothing is written.
  confirm <id> --status confirmed|accepted|false-positive [--reason text] [--expires YYYY-MM-DD] [--coverage]
      record a decision, with the scan's own evidence as proof. Accepting debt needs a reason and an expiry within 180 days.
  next
      the worst confirmed item with the safe way to pay it off. Recovery starts from confirmed debt only.
  baseline --reason text
      accept all existing untriaged debt once, so that from then on only new debt fails the check.
  check
      fail on debt nobody has decided about and on acceptances that expired. This is the ratchet.
  prune
      drop register entries whose debt has been paid.
`

func (e environment) today() time.Time {
	if e.now != nil {
		return e.now()
	}
	return time.Now()
}

func runDebt(ctx context.Context, args []string, stdout, stderr io.Writer, env environment) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, debtUsage)
		return exitUsage
	}
	root, err := doctor.FindRoot(env.cwd)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "agentctl debt:", err, "(run it inside the repository)")
		return exitUsage
	}
	d := &debtRun{ctx: ctx, root: root, out: stdout, errOut: stderr, env: env, path: debt.RegisterPath(root)}

	switch args[0] {
	case "scan":
		return d.scan(args[1:])
	case "confirm":
		return d.confirm(args[1:])
	case "next":
		return d.next(args[1:])
	case "baseline":
		return d.baseline(args[1:])
	case cmdCheck:
		return d.check()
	case "prune":
		return d.prune()
	}
	_, _ = fmt.Fprint(stderr, debtUsage)
	return exitUsage
}

type debtRun struct {
	ctx    context.Context
	root   string
	out    io.Writer
	errOut io.Writer
	env    environment
	path   string
}

type debtView struct {
	items    []debt.Item
	resolved []debt.Entry
	register debt.Register
}

// load scans, optionally measures coverage, and applies the register.
func (d *debtRun) load(withCoverage bool) (debtView, error) {
	items, err := debt.Scan(d.root, debt.Options{})
	if err != nil {
		return debtView{}, err
	}
	if withCoverage {
		more, covErr := d.coverage()
		if covErr != nil {
			return debtView{}, covErr
		}
		items = append(items, more...)
		debt.Rank(items)
	}
	reg, err := debt.LoadRegister(d.path)
	if err != nil {
		return debtView{}, err
	}
	merged, resolved := debt.Apply(items, reg, d.env.today())
	if !withCoverage {
		// Coverage was not measured, so its entries are not "paid off", just not looked at.
		resolved = filterEntries(
			resolved,
			func(e debt.Entry) bool { return !strings.HasPrefix(e.ID, debt.KindCoverage+":") },
		)
	}
	return debtView{items: merged, resolved: resolved, register: reg}, nil
}

func filterEntries(in []debt.Entry, keep func(debt.Entry) bool) []debt.Entry {
	var out []debt.Entry
	for _, e := range in {
		if keep(e) {
			out = append(out, e)
		}
	}
	return out
}

func (d *debtRun) coverage() ([]debt.Item, error) {
	mods, err := verify.Modules(d.root)
	if err != nil {
		return nil, err
	}
	runner := d.env.procRunner
	if runner == nil {
		runner = proc.Exec{}
	}
	var items []debt.Item
	for _, m := range mods {
		if !m.HasGo {
			continue
		}
		got, covErr := debt.Coverage(d.ctx, runner, m.Dir, defaultCoverageMin)
		if covErr != nil {
			return nil, covErr
		}
		items = append(items, got...)
	}
	return items, nil
}

func (d *debtRun) fail(err error) int {
	_, _ = fmt.Fprintln(d.errOut, "agentctl debt:", err)
	return exitUsage
}

func (d *debtRun) scan(args []string) int {
	fs := flag.NewFlagSet("debt scan", flag.ContinueOnError)
	fs.SetOutput(d.errOut)
	withCoverage := fs.Bool("coverage", false, "also run the tests and report weak or missing coverage")
	all := fs.Bool("all", false, "also list accepted debt")
	asJSON := fs.Bool("json", false, "print everything as JSON")
	top := fs.Int("top", defaultDebtTop, "rows to list")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	v, err := d.load(*withCoverage)
	if err != nil {
		return d.fail(err)
	}
	if *asJSON {
		enc := json.NewEncoder(d.out)
		enc.SetIndent("", "  ")
		if err = enc.Encode(struct {
			Items    []debt.Item  `json:"items"`
			Resolved []debt.Entry `json:"resolved"`
		}{emptyItems(v.items), emptyEntries(v.resolved)}); err != nil {
			return d.fail(err)
		}
		return exitOK
	}
	printScan(d.out, v, *top, *all)
	return exitOK
}

func emptyItems(in []debt.Item) []debt.Item {
	if in == nil {
		return []debt.Item{}
	}
	return in
}

func emptyEntries(in []debt.Entry) []debt.Entry {
	if in == nil {
		return []debt.Entry{}
	}
	return in
}

func printScan(w io.Writer, v debtView, top int, all bool) {
	counts := map[string]int{}
	total := 0
	for _, it := range v.items {
		counts[it.Status]++
		if it.Status != debt.StatusFalsePositive {
			total++
		}
	}
	_, _ = fmt.Fprintf(
		w,
		"debt: %d items (%d unconfirmed, %d confirmed, %d accepted, %d expired), %d false positives, %d resolved\n",
		total,
		counts[debt.StatusUnconfirmed],
		counts[debt.StatusConfirmed],
		counts[debt.StatusAccepted],
		counts[debt.StatusExpired],
		counts[debt.StatusFalsePositive],
		len(v.resolved),
	)

	shown := 0
	for _, it := range v.items {
		listed := it.Status == debt.StatusUnconfirmed || it.Status == debt.StatusExpired ||
			it.Status == debt.StatusConfirmed ||
			(all && it.Status == debt.StatusAccepted)
		if !listed || shown == top {
			continue
		}
		shown++
		_, _ = fmt.Fprintf(
			w,
			"  [%s] %s  %s  %s  %s  (id %s)\n",
			it.Status,
			it.Kind,
			it.Where,
			it.Subject,
			it.Evidence,
			it.ID,
		)
	}
	if len(v.resolved) > 0 {
		_, _ = fmt.Fprintf(w, "%d register entries have no debt left: run `agentctl debt prune`\n", len(v.resolved))
	}
}

func (d *debtRun) confirm(args []string) int {
	id := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("debt confirm", flag.ContinueOnError)
	fs.SetOutput(d.errOut)
	status := fs.String("status", "", "confirmed, accepted, or false-positive")
	reason := fs.String("reason", "", "why; required for accepted and false-positive")
	expires := fs.String("expires", "", "when an acceptance runs out, YYYY-MM-DD, within 180 days")
	withCoverage := fs.Bool("coverage", false, "the item is a coverage item, so measure coverage first")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if id == "" || *status == "" {
		_, _ = fmt.Fprint(d.errOut, debtUsage)
		return exitUsage
	}

	v, err := d.load(*withCoverage)
	if err != nil {
		return d.fail(err)
	}
	reg := v.register
	if err = reg.Confirm(
		v.items,
		debt.Decision{ID: id, Status: *status, Reason: *reason, Expires: *expires},
		d.env.today(),
	); err != nil {
		_, _ = fmt.Fprintln(d.errOut, "agentctl debt: refused:", err)
		return exitFail
	}
	if err = reg.Save(d.path); err != nil {
		return d.fail(err)
	}
	_, _ = fmt.Fprintf(d.out, "recorded %s for %s\n", *status, id)
	return exitOK
}

func (d *debtRun) next(args []string) int {
	fs := flag.NewFlagSet("debt next", flag.ContinueOnError)
	fs.SetOutput(d.errOut)
	withCoverage := fs.Bool("coverage", false, "include coverage debt")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	v, err := d.load(*withCoverage)
	if err != nil {
		return d.fail(err)
	}

	for _, it := range v.items {
		if it.Status != debt.StatusConfirmed {
			continue
		}
		_, _ = fmt.Fprintf(
			d.out,
			"next: %s\n  %s  %s  %s\n  %s  (cost %s)\n\n%s\n",
			it.ID,
			it.Kind,
			it.Where,
			it.Subject,
			it.Evidence,
			it.Cost,
			debt.Playbook(it.Kind),
		)
		return exitOK
	}

	var waiting []debt.Item
	for _, it := range v.items {
		if it.Status == debt.StatusUnconfirmed || it.Status == debt.StatusExpired {
			waiting = append(waiting, it)
		}
	}
	if len(waiting) == 0 {
		_, _ = fmt.Fprintln(d.out, "no debt is waiting: nothing is unconfirmed or confirmed")
		return exitOK
	}
	_, _ = fmt.Fprintln(
		d.out,
		"nothing is confirmed yet. Confirm one first (agentctl debt confirm <id> --status confirmed), worst first:",
	)
	for _, it := range waiting[:min(nextUnconfirmedRows, len(waiting))] {
		_, _ = fmt.Fprintf(d.out, "  %s  %s  %s  %s\n", it.ID, it.Where, it.Subject, it.Evidence)
	}
	return exitOK
}

func (d *debtRun) baseline(args []string) int {
	fs := flag.NewFlagSet("debt baseline", flag.ContinueOnError)
	fs.SetOutput(d.errOut)
	reason := fs.String("reason", "", "why this debt is accepted as it stands")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if *reason == "" {
		_, _ = fmt.Fprint(d.errOut, debtUsage)
		return exitUsage
	}

	v, err := d.load(false)
	if err != nil {
		return d.fail(err)
	}
	reg := v.register
	n, err := reg.Baseline(v.items, *reason, d.env.today())
	if err != nil {
		_, _ = fmt.Fprintln(d.errOut, "agentctl debt: refused:", err)
		return exitFail
	}
	if err = reg.Save(d.path); err != nil {
		return d.fail(err)
	}
	_, _ = fmt.Fprintf(
		d.out,
		"baselined %d items as accepted for %d days; they come back for review when that runs out\n",
		n,
		debt.MaxAcceptDays,
	)
	return exitOK
}

func (d *debtRun) check() int {
	v, err := d.load(false)
	if err != nil {
		return d.fail(err)
	}
	problems := debt.Check(v.items)
	if len(problems) == 0 {
		_, _ = fmt.Fprintln(d.out, "debt: nothing untriaged")
		return exitOK
	}
	for _, p := range problems {
		_, _ = fmt.Fprintln(d.out, p)
	}
	_, _ = fmt.Fprintf(d.out, "debt check failed: %d\n", len(problems))
	return exitFail
}

func (d *debtRun) prune() int {
	v, err := d.load(false)
	if err != nil {
		return d.fail(err)
	}
	reg := v.register
	reg.Prune(v.resolved)
	if err = reg.Save(d.path); err != nil {
		return d.fail(err)
	}
	_, _ = fmt.Fprintf(d.out, "pruned %d entries whose debt is paid\n", len(v.resolved))
	return exitOK
}
