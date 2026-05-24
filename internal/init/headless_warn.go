package init

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// WarnMissingAgentContext writes a warning to w when neither AGENTS.md
// nor CLAUDE.md exists in cwd. Returns true when the warning was
// emitted (test affordance).
//
// Doctrine (workflow entry points, status recovery primitive, SSHFS
// mount semantics) lives in AGENTS.md (canonical body) and CLAUDE.md
// (Claude's @AGENTS.md include wrapper), NOT in the MCP Instructions
// field — TestBuildInstructions_NoStaticRulesLeak forbids that. So a
// headless agent in a working directory without either file has only
// tool descriptions and will lack workflow guidance. Running
// `zcp init` writes both files. This warning fires at zcp serve
// startup so the operator notices before relying on agent behavior.
//
// Both files missing → warn. Either present → silent (the agent has
// its expected context). Stat errors other than "not exist" (permission
// denied, etc.) are treated as "file present" — silent is better than
// a misleading warning we can't justify.
func WarnMissingAgentContext(cwd string, w io.Writer) bool {
	if hasFile(cwd, "AGENTS.md") || hasFile(cwd, "CLAUDE.md") {
		return false
	}
	fmt.Fprintln(w,
		"WARNING: no AGENTS.md or CLAUDE.md in working directory; "+
			"MCP-only mode delivers no workflow doctrine. "+
			"Run `zcp init` here first for full agent guidance.")
	return true
}

// WarnMissingClaudeMD is a deprecated alias for WarnMissingAgentContext.
//
// Deprecated: use WarnMissingAgentContext.
func WarnMissingClaudeMD(cwd string, w io.Writer) bool {
	return WarnMissingAgentContext(cwd, w)
}

// hasFile reports whether path/name exists. Stat errors other than
// IsNotExist are treated as "present" (fail-safe — don't emit a
// misleading missing-file warning when we can't actually tell).
func hasFile(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return !os.IsNotExist(err)
}
