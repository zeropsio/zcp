package init

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// WarnMissingClaudeMD writes a warning to w when no CLAUDE.md exists
// in cwd. Returns true when the warning was emitted (test affordance).
//
// Doctrine (workflow entry points, status recovery primitive, SSHFS
// mount semantics) lives in CLAUDE.md, NOT in the MCP Instructions
// field — TestBuildInstructions_NoStaticRulesLeak forbids that. So a
// headless agent in a working directory without CLAUDE.md has only
// tool descriptions and will lack workflow guidance. Running zcp init
// writes the file. This warning fires at zcp serve startup so the
// operator notices before relying on the agent's behavior.
//
// Stat errors other than "not exist" (permission denied, etc.) are
// treated as "don't warn" rather than emitting a misleading message —
// silent is better than wrong.
func WarnMissingClaudeMD(cwd string, w io.Writer) bool {
	_, err := os.Stat(filepath.Join(cwd, "CLAUDE.md"))
	if !os.IsNotExist(err) {
		return false
	}
	fmt.Fprintln(w,
		"WARNING: no CLAUDE.md in working directory; "+
			"MCP-only mode delivers no workflow doctrine. "+
			"Run `zcp init` here first for full agent guidance.")
	return true
}
