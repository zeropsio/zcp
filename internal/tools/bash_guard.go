package tools

import (
	"fmt"
	"regexp"
	"strings"
)

// zcpMountExecutionRe matches the "boundary-violation" shape:
// `cd /var/www/<host>` followed (after `&&` or `;`) by a token that
// names a target-side executable. Anchored to the start of a simple
// command so a `cd` nested inside `ssh <host> "cd /var/www/... && ..."`
// doesn't trip the guard (ssh-quoted content is target-side by the
// time the quoted shell runs).
//
// The executable token list is deliberately narrow: we flag the things
// the postmortem identified as expensive or incorrect when run zcp-side
// against the SSHFS mount. Adding a new token should be a conscious
// decision backed by a concrete regression, not a speculative addition.
var zcpMountExecutionRe = regexp.MustCompile(
	`(?i)(?:^|\s|[;&]{1,2})cd\s+/var/www/[A-Za-z0-9][A-Za-z0-9_.-]*/?\s*(?:&&|;)\s*` +
		`(?:[A-Z_][A-Z0-9_]*=[^\s]+\s+)*` + // optional leading ENV= assignments
		`(?:npx\s+|npm\s+|pnpm\s+|yarn\s+|bun\s+|sudo\s+)?` + // optional runner prefix
		`(git|npm|npx|pnpm|yarn|bun|nest|vite|tsc|svelte-check|` +
		`composer|bundle|rake|rails|artisan|php|python|pytest|pip|poetry|` +
		`go|cargo|rustc|mvn|gradle)\b`,
)

// sshPrefixRe matches `ssh <host> "<inner>"` / `ssh <host> '<inner>'`.
// When the command is ssh-wrapped, the `cd /var/www/...` inside the
// quoted portion runs target-side and is the correct shape. We peel
// the outer ssh wrapper before scanning.
var sshPrefixRe = regexp.MustCompile(`(?i)^\s*ssh\s+[A-Za-z0-9][A-Za-z0-9_.-]*\s+["']`)

// CheckBashCommand returns a structured error when `cmd` would run a
// target-side executable zcp-side against the SSHFS mount. Safe forms
// (ssh-wrapped commands, file reads through the mount, standalone
// `cd`) pass.
//
// Intended as a pre-execution guard for any tool that forwards bash
// commands on behalf of the agent. The postmortem's v22 acceptance
// criterion is "zero zcp-side `cd /var/www/{host} && <executable>`
// patterns"; this function is the check that would enforce it.
// Architectural note: the agent's own Bash tool belongs to the Claude
// Code runtime and is not interceptable from zcp's MCP surface, so
// this function cannot automatically guard every bash call the agent
// makes — but it is available to any current or future zcp tool that
// does proxy shell commands, and the structured error message points
// the agent at the correct shape without requiring brief re-reading.
func CheckBashCommand(cmd string) error {
	trimmed := strings.TrimSpace(cmd)
	if trimmed == "" {
		return nil
	}
	// If the command is ssh-wrapped, its interior runs target-side —
	// any `cd /var/www/...` + `&&` + exec inside the quotes is the
	// correct pattern, not a violation.
	if sshPrefixRe.MatchString(trimmed) {
		return nil
	}
	loc := zcpMountExecutionRe.FindStringIndex(trimmed)
	if loc == nil {
		return nil
	}
	violation := strings.TrimSpace(trimmed[loc[0]:loc[1]])
	// Best-effort suggested-fix: rewrap as ssh + container-local cd.
	// Pull the hostname out of the violation ("cd /var/www/<host> && <exec>").
	host := "{hostname}"
	if m := regexp.MustCompile(`/var/www/([A-Za-z0-9][A-Za-z0-9_.-]*)`).FindStringSubmatch(violation); len(m) > 1 {
		host = m[1]
	}
	fixInner := strings.TrimSpace(regexp.MustCompile(`(?is)^.*?(?:&&|;)\s*`).ReplaceAllString(violation, ""))
	suggested := fmt.Sprintf(`ssh %s "cd /var/www && %s"`, host, fixInner)
	return fmt.Errorf(
		"ZCP_EXECUTION_BOUNDARY_VIOLATION: `%s` would run on the zcp orchestrator against the SSHFS mount; executable commands must run via SSH on the target container, e.g. %s (see `zerops_guidance topic=\"where-commands-run\"` for the full rule)",
		violation, suggested,
	)
}
