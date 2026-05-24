package workflow

import (
	"fmt"
	"os"
	"strings"

	"github.com/zeropsio/zcp/internal/topology"
)

// sanitizeReflogIntent strips characters that could inject markdown structure.
func sanitizeReflogIntent(s string) string {
	r := strings.NewReplacer("\n", " ", "\r", " ")
	return strings.TrimSpace(r.Replace(s))
}

// AppendReflogEntry appends a bootstrap history entry to a markdown
// file at agentsMDPath. Each entry is wrapped in ZEROPS:REFLOG markers.
// Entries are append-only.
//
// Post-multi-agent migration the writer point is AGENTS.md (the
// cross-tool canonical context file — Codex, Cursor, Gemini, future
// adapters all read it natively; Claude pulls it via @AGENTS.md
// include in CLAUDE.md). Pre-migration the writer pointed at
// CLAUDE.md; existing CLAUDE.md REFLOG sections are relocated to
// AGENTS.md by init.migrateReflogToAgentsMD on first upgrade.
//
// The function itself is path-agnostic — callers (bootstrap_outputs.go)
// pick the target file. Keeping the parameter generic lets us also
// reuse for any future agent context file without renaming.
func AppendReflogEntry(agentsMDPath string, intent string, targets []BootstrapTarget, sessionID string, date string) error {
	intent = sanitizeReflogIntent(intent)
	var b strings.Builder
	b.WriteString("\n<!-- ZEROPS:REFLOG -->\n")
	b.WriteString(fmt.Sprintf("### %s — Bootstrap: %s\n\n", date, intent))

	for _, target := range targets {
		mode := target.Runtime.EffectiveMode()
		b.WriteString(fmt.Sprintf("- **Runtime:** %s (%s, %s)\n", target.Runtime.DevHostname, target.Runtime.Type, mode))
		if mode == topology.PlanModeStandard {
			b.WriteString(fmt.Sprintf("- **Stage:** %s\n", target.Runtime.StageHostname()))
		}
		if len(target.Dependencies) > 0 {
			deps := make([]string, 0, len(target.Dependencies))
			for _, dep := range target.Dependencies {
				deps = append(deps, fmt.Sprintf("%s (%s)", dep.Hostname, dep.Type))
			}
			b.WriteString(fmt.Sprintf("- **Dependencies:** %s\n", strings.Join(deps, ", ")))
		}
	}

	b.WriteString(fmt.Sprintf("- **Session:** %s\n", sessionID))
	b.WriteString("\n> This is a historical record. Verify current state via `zerops_discover`.\n")
	b.WriteString("<!-- /ZEROPS:REFLOG -->\n")

	f, err := os.OpenFile(agentsMDPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open agent context file for reflog: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(b.String()); err != nil {
		return fmt.Errorf("write reflog entry: %w", err)
	}
	return nil
}
