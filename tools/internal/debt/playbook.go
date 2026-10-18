package debt

const common = `
Do one item per commit, with a refactor: or test: subject. Pin behaviour before you change it, never edit an existing test to make a change pass, and never add a //nolint or raise a threshold to make the debt go away. Scan again afterwards: the item must be gone, and nothing new may have appeared.`

// Playbook is the safe way to pay off one kind of debt, in the order to do it.
func Playbook(kind string) string {
	switch kind {
	case KindSuppression:
		return `Remove the //nolint by fixing what it hides.
1. Read the reason on the directive and the rule it silences.
2. Write a test that pins what the code does now, if none covers it.
3. Change the code so the rule no longer fires, then remove the directive.
4. Run the tests.` + common
	case KindUntrackedTodo:
		return `Either do the work or give it a reference.
1. If it is small, do it test-first and delete the marker.
2. If not, give the marker a reference that someone can find: TODO(#123), or TODO(adr-0008) for a decision.
3. If it is stale and nobody would do it, delete it.` + common
	case KindComplexity:
		return `Reduce the function without changing what it does.
1. Write characterization tests that pin the current behaviour on every branch, including the odd ones. Run them; they must pass before you touch the code.
2. Extract one cohesive piece at a time (a loop body, a validation, a branch) into a named function.
3. Run the tests after every extraction; they must stay green untouched.
4. Stop when the function reads as a sequence of named steps.` + common
	case KindCoverage:
		return `Add tests that pin behaviour.
1. List the exported behaviour of the package and which of it no test exercises.
2. Write a test for one behaviour at a time, through the public API.
3. If a branch cannot be reached by any test, ask whether it is needed; dead code is debt too.` + common
	}
	return `Understand it, pin behaviour with a test, make the smallest change that removes it, then scan again.` + common
}
