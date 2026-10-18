package verify

import "time"

const dbTimeout = 10 * time.Minute

// DBStep judges every service's SQL with the agentctl built by the build step, so it must run after that step. The
// fast gate checks the query rules and that generated code is current, which need no Docker. The full gate also reads
// each query's plan on a throwaway Postgres.
func DBStep(env Env, level Level) Step {
	args := []string{"db", "check"}
	if level != Full {
		args = append(args, "--no-plan")
	}
	return command(env, "db check", env.Root, dbTimeout, builtBin(env.Root, "agentctl"), args...)
}
