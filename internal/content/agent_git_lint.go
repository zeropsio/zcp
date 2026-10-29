package content

import (
	"regexp"
	"strings"
)

// The git commands zcp's agent-facing texts give for a Mate's dev
// repositories run in the dev service — `zcp git <host> …`, or over ssh in a
// lease — never on the sshfs mount `/var/www/<host>`, in a worktree of it, or
// in a command substitution the agent's own shell works out; a substitution
// of `zcp git` itself is an assignment chained with && before its value is
// used, so a failed call stops the chain before anything is decided
// (docs/spec-mate.md §6.8). AgentGitViolations is that one rule;
// TestNoMountGitInAgentContent applies it to source literals and embedded
// content, TestAgentGitTexts_RenderedCommandsRunInTheService to the texts as
// rendered.

// agentGitPatterns are the phrases and commands that run git outside the dev
// service wherever they stand.
var agentGitPatterns = []struct {
	why string
	re  *regexp.Regexp
}{
	{"git in the agent's own shell", regexp.MustCompile(`(?i)\bown shell\b`)},
	{"git kept off the dev service", regexp.MustCompile(`(?i)\bnever over ssh\b`)},
	{"git in a mounted or zcp-side checkout", regexp.MustCompile(`\bcd\s+(?:/var/www/\S+|%s|<[^>\s]+>|/tmp/\S+)\s+&&\s+git\s`)},
	{"a worktree forced or pruned", regexp.MustCompile(`\bworktree\s+(?:add\s+(?:-f|--force)\b|prune\b)`)},
	{"index.lock deleted", regexp.MustCompile(`(?i)\b(?:delete|remove|rm)\b[^;\n]{0,40}?index\.lock`)},
	{"git in a command substitution", regexp.MustCompile(`\$\(\s*git\s`)},
}

// mountedCheckout is a checkout named as the place git runs; a sentence that
// says never to (a "never" or "not" shortly before it) is the warning.
var mountedCheckout = regexp.MustCompile(`(?i)\b(?:in|from|on)\s+(?:the|its|your)\s+mounted\s+(?:checkout|repository|folder)`)

// worktreeAdd is `worktree add`, its options and its path.
var worktreeAdd = regexp.MustCompile(`\bworktree\s+add\b((?:\s+-\S+)*)\s+([^\s` + "`" + `"']+)`)

// gitCommand is a git invocation where a shell command starts — the text's
// or a backtick span's start, or after &&, ||, ;, a pipe or `$(` — git and a
// subcommand or option of its own, so prose ("git in its sessions") is none;
// never `zcp git`, whose git is preceded by its own name.
var gitCommand = regexp.MustCompile("(?:(?:^|[`(]|&&|\\|\\||;|\\|)\\s*|;\\s*(?:then|else|do)\\s+)git\\s+" + gitSubcommands + "\\b")

// gitSubcommands are the git subcommands and leading options a command names.
const gitSubcommands = `(?:-[cC]|add|am|apply|archive|bisect|blame|branch|bundle|cat-file|checkout|cherry-pick|clean|clone|commit|commit-tree|config|describe|diff|fetch|for-each-ref|fsck|gc|grep|hash-object|init|log|ls-files|ls-remote|ls-tree|merge|merge-base|mktree|mv|notes|pull|push|read-tree|rebase|remote|reset|restore|rev-list|rev-parse|revert|rm|shortlog|show|sparse-checkout|stash|status|submodule|switch|symbolic-ref|tag|update-ref|worktree|write-tree)`

// zcpGitSubstitution opens a substitution of zcp git.
const zcpGitSubstitution = "$(zcp git"

// assignedBefore is a shell variable assignment ending where a substitution
// starts.
var assignedBefore = regexp.MustCompile("(?:^|[\\s;&(`])[A-Za-z_][A-Za-z0-9_]*=$")

// AgentGitViolations names each way text tells an agent to run git outside a
// dev service — each "<why>: <what it says>" — nil for none.
func AgentGitViolations(text string) []string {
	var out []string
	say := func(why, what string) { out = append(out, why+": "+strings.TrimSpace(what)) }
	for _, p := range agentGitPatterns {
		for _, m := range p.re.FindAllString(text, -1) {
			say(p.why, m)
		}
	}
	for _, loc := range mountedCheckout.FindAllStringIndex(text, -1) {
		before := strings.ToLower(text[max(0, loc[0]-30):loc[0]])
		if !strings.Contains(before, "never") && !strings.Contains(before, "not ") {
			say("git in the mounted checkout", text[loc[0]:loc[1]])
		}
	}
	for _, m := range worktreeAdd.FindAllStringSubmatch(text, -1) {
		if !strings.HasPrefix(m[2], "/tmp/wt-") {
			say("a worktree outside the dev service's /tmp/wt-", m[0])
		}
	}
	for _, loc := range gitCommand.FindAllStringIndex(text, -1) {
		if !inSSH(text, loc[0]) {
			say("git run where no dev service is named", text[loc[0]:min(len(text), loc[1]+40)])
		}
	}
	out = append(out, failOpenSubstitutions(text)...)
	return out
}

// inSSH reports whether the command at i is in an ssh to a lease's (or a
// service's) container: the backtick span holding it starts with `ssh `, as
// does the text when it holds no span.
func inSSH(text string, i int) bool {
	start := strings.LastIndex(text[:i+1], "`")
	if start < 0 || strings.Count(text[:start+1], "`")%2 == 0 {
		// No span opens before i: the text itself is the command line.
		return strings.HasPrefix(strings.TrimSpace(text), "ssh ")
	}
	return strings.HasPrefix(strings.TrimSpace(text[start+1:]), "ssh ")
}

// failOpenSubstitutions names each `$(zcp git …)` that is not an assignment
// chained with && before its value is used: a test like
// `[ -z "$(zcp git …)" ]` is true when zcp git fails.
func failOpenSubstitutions(text string) []string {
	var out []string
	for i := 0; ; {
		j := strings.Index(text[i:], zcpGitSubstitution)
		if j < 0 {
			return out
		}
		at := i + j
		end := closingParen(text, at+1)
		what := text[at:min(len(text), end+1)]
		after := ""
		if end < len(text) {
			after = strings.TrimLeft(text[end+1:], " ")
		}
		if !assignedBefore.MatchString(text[:at]) || !strings.HasPrefix(after, "&&") {
			out = append(out, "a zcp git substitution that fails open: "+what)
		}
		i = at + len(zcpGitSubstitution)
	}
}

// closingParen is the index of the parenthesis closing the one at open, the
// text's end when none does.
func closingParen(text string, open int) int {
	depth := 0
	for k := open; k < len(text); k++ {
		switch text[k] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return k
			}
		}
	}
	return len(text)
}
