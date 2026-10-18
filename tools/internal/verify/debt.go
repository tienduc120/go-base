package verify

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tienduc120/go-base/tools/internal/debt"
)

// maxDebtLines keeps the first run on a repository with a lot of untriaged debt from printing all of it.
const maxDebtLines = 15

// DebtStep is the ratchet: it fails on debt nobody has decided about and on acceptances that have run out, so debt
// cannot grow silently. It reads the code and the committed register and runs no tests.
func DebtStep(env Env) Step {
	return Step{Name: "debt ratchet", Run: func(context.Context) Outcome {
		items, err := debt.Scan(env.Root, debt.Options{})
		if err != nil {
			return Outcome{Detail: err.Error()}
		}
		reg, err := debt.LoadRegister(debt.RegisterPath(env.Root))
		if err != nil {
			return Outcome{Detail: err.Error()}
		}

		merged, _ := debt.Apply(items, reg, time.Now())
		problems := debt.Check(merged)
		if len(problems) == 0 {
			return Outcome{Pass: true}
		}
		if len(problems) > maxDebtLines {
			rest := len(problems) - maxDebtLines
			problems = append(
				problems[:maxDebtLines],
				fmt.Sprintf("... and %d more; run `agentctl debt scan` to see them all", rest),
			)
		}
		return Outcome{Detail: strings.Join(problems, "\n")}
	}}
}
