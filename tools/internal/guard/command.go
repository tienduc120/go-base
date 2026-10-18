package guard

import (
	"regexp"
	"slices"
	"strings"
)

var (
	heredocStart = regexp.MustCompile(`<<-?\s*['"]?([A-Za-z_][A-Za-z0-9_]*)['"]?`)
	quoted       = regexp.MustCompile(`"([^"]*)"|'([^']*)'`)
	simpleWord   = regexp.MustCompile(`^[\w./:+@=-]*$`)
	separators   = regexp.MustCompile(`&&|\|\||;|\||\n`)
	envAssign    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	shortNoVerif = regexp.MustCompile(`^-[a-zA-Z]*n[a-zA-Z]*$`)
	timeoutFlag  = regexp.MustCompile(`^--?timeout(=|$)`)
	followFlag   = regexp.MustCompile(`^-[a-zA-Z]*[fF]`)
	sedInPlace   = regexp.MustCompile(`^-[a-zA-Z]*i`)
)

// subcommandIndex is where a tool's subcommand sits in its argument list: go [test], git [commit].
const subcommandIndex = 1

// mutators are commands that write, move, or delete the files they are given.
func mutators() []string {
	return []string{
		"rm", "mv", "cp", "tee", "truncate", "dd", "del", "erase", "rmdir", "ln",
		"set-content", "add-content", "out-file", "remove-item", "move-item", "copy-item",
		"rename-item", "clear-content", "new-item",
	}
}

// CheckCommand decides about one shell command line. approved lifts only the protected-file rule: approval to change
// a rule is not approval to skip the gates or to hang the session.
func CheckCommand(cmd string, approved bool) Decision {
	for _, seg := range separators.Split(mask(stripHeredocs(cmd)), -1) {
		tokens := strings.Fields(seg)
		env, command := splitEnv(tokens)
		if len(command) == 0 && len(env) == 0 {
			continue
		}
		if d := bypassesHooks(env, command); d.Block {
			return d
		}
		if !approved {
			if d := changesProtected(seg, command); d.Block {
				return d
			}
		}
		if d := neverReturns(seg, command); d.Block {
			return d
		}
	}
	return Decision{}
}

// stripHeredocs drops heredoc bodies: a commit message that merely mentions a file or a flag is not running it.
func stripHeredocs(cmd string) string {
	var out []string
	terminator := ""
	for line := range strings.SplitSeq(cmd, "\n") {
		if terminator != "" {
			if strings.TrimSpace(line) == terminator {
				terminator = ""
			}
			continue
		}
		out = append(out, line)
		if m := heredocStart.FindStringSubmatch(line); m != nil {
			terminator = m[1]
		}
	}
	return strings.Join(out, "\n")
}

// mask keeps simple quoted words, such as paths, and blanks quoted prose, such as commit messages.
func mask(cmd string) string {
	return quoted.ReplaceAllStringFunc(cmd, func(m string) string {
		inner := m[1 : len(m)-1]
		if simpleWord.MatchString(inner) {
			return inner
		}
		return `""`
	})
}

// splitEnv separates leading NAME=value assignments from the command they apply to.
func splitEnv(tokens []string) ([]string, []string) {
	i := 0
	for i < len(tokens) && envAssign.MatchString(tokens[i]) {
		i++
	}
	return tokens[:i], tokens[i:]
}

func bypassesHooks(env, command []string) Decision {
	const reason = "this skips the git hooks that run the gates. Fix what the hook reports instead of bypassing it."
	for _, e := range env {
		key, val, _ := strings.Cut(e, "=")
		if (key == "HUSKY" || key == "LEFTHOOK") && val == "0" || key == "LEFTHOOK_EXCLUDE" {
			return block("%s", reason)
		}
	}
	if len(command) == 0 {
		return Decision{}
	}
	switch command[0] {
	case "lefthook":
		if len(command) > 1 && command[1] == "uninstall" {
			return block("%s", reason)
		}
	case "git":
		if slices.ContainsFunc(command, func(t string) bool { return strings.Contains(t, "core.hooksPath") }) ||
			slices.Contains(command, "--no-verify") {
			return block("%s", reason)
		}
		if slices.Contains(command, "commit") &&
			slices.ContainsFunc(command, func(t string) bool { return shortNoVerif.MatchString(t) }) {
			return block("%s", reason)
		}
	}
	return Decision{}
}

func changesProtected(segment string, command []string) Decision {
	if len(command) == 0 {
		return Decision{}
	}
	for _, rule := range protectedRules() {
		tok := rule.token()
		if !strings.Contains(segment, tok) {
			continue
		}
		redirect := regexp.MustCompile(`>>?\s*\S*` + regexp.QuoteMeta(tok))
		if redirect.MatchString(segment) || mutates(command) {
			return block("this changes a protected file (%s) through the shell. Rule files, gate code, fixtures, and "+
				"licenses are protected; fix the cause instead. %s", tok, approvalHint())
		}
	}
	return Decision{}
}

func mutates(command []string) bool {
	name := strings.ToLower(command[0])
	switch {
	case slices.Contains(mutators(), name):
		return true
	case name == "sed":
		return slices.ContainsFunc(command[1:], func(t string) bool { return sedInPlace.MatchString(t) })
	case name == "git" && len(command) > 1:
		return command[1] == "rm" || command[1] == "mv"
	}
	return false
}

func neverReturns(segment string, command []string) Decision {
	if len(command) == 0 || strings.Contains(segment, "agentctl run") {
		return Decision{}
	}
	switch command[0] {
	case "go":
		return goNeverReturns(command)
	case "docker", "docker-compose":
		return dockerNeverReturns(command)
	case "kubectl":
		return followNeverReturns(command, "kubectl logs --tail 100")
	case "tail":
		if slices.ContainsFunc(
			command[1:],
			func(t string) bool { return followFlag.MatchString(t) || t == "--follow" },
		) {
			return block("following output with -f never returns, so a session waits forever. " +
				"Read a bounded slice instead: tail -n 100 <file>.")
		}
	}
	return Decision{}
}

func goNeverReturns(command []string) Decision {
	if len(command) <= subcommandIndex {
		return Decision{}
	}
	switch command[1] {
	case "test":
		if !slices.ContainsFunc(command[2:], func(t string) bool { return timeoutFlag.MatchString(t) }) {
			return block("go test without a timeout can hang forever. Run it through " +
				"`go run ./tools/agentctl run -- go test ...` (it adds a timeout and kills the process tree) " +
				"or add -timeout 120s.")
		}
	case "run":
		for _, arg := range command[2:] {
			if strings.Contains(arg, "/cmd/") && !strings.Contains(arg, "tools/") {
				return block("a service runs in the foreground until it is stopped, which hangs the session. " +
					"Start it detached, or run it through `go run ./tools/agentctl run --timeout 30s -- ...`.")
			}
		}
	}
	return Decision{}
}

func dockerNeverReturns(command []string) Decision {
	compose := command[0] == "docker-compose" || len(command) > 1 && command[1] == "compose"
	if compose && slices.Contains(command, "up") {
		if !slices.Contains(command, "-d") && !slices.Contains(command, "--detach") {
			return block("docker compose up in the foreground never returns. Use `docker compose up -d` " +
				"(detach) and read output with `docker compose logs --tail 100`.")
		}
		return Decision{}
	}
	return followNeverReturns(command, "docker logs --tail 100 <container>")
}

func followNeverReturns(command []string, instead string) Decision {
	if slices.Contains(command, "logs") &&
		slices.ContainsFunc(command, func(t string) bool { return t == "-f" || t == "--follow" }) {
		return block(
			"following logs never returns, so a session waits forever. Read a bounded slice instead: %s.",
			instead,
		)
	}
	return Decision{}
}
