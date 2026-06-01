package adapters

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/content"
)

// TestBootstrapExtension_AgentCommandsPinned is the drift guard for the agent
// launcher. The bootstrap extension (vscode-bootstrap-extension.js) resolves
// ZCP_AGENT_TYPES against an inline registry whose commands BYPASS permission
// prompts — a wrong flag is a correctness + safety bug. The launcher logic
// lives in JS (so it can read the live zembed env store and react via fs.watch
// without a process spawn), so this test pins the safety-critical invocations
// against the shipped template. Each command was verified against the real CLI
// binary / official docs:
//
//   - claude-code: opens the installed Claude Code VS Code plugin (no CLI).
//   - codex:       `codex --dangerously-bypass-approvals-and-sandbox` — skips
//     all approvals AND disables codex's own sandbox (the Zerops container is
//     the sandbox; agents get full host access). Parses on codex-cli 0.125.0.
//   - opencode-ai: `opencode --dangerously-skip-permissions` (bare/interactive
//     accepts it).
//   - antigravity: `agy --dangerously-skip-permissions` (auto-approve all tool
//     permission requests; verified via `agy --help` + repo issue #36).
//   - grok:        bare `grok` — superagent-ai/grok-cli has no bypass flag and
//     needs none (interactive agent runs tools without per-action prompts).
func TestBootstrapExtension_AgentCommandsPinned(t *testing.T) {
	t.Parallel()
	tmpl, err := content.GetTemplate("vscode-bootstrap-extension.js")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}

	// id → verified terminal/plugin command that MUST appear verbatim.
	wantCommands := map[string]string{
		"claude-code": "claude-vscode.editor.open",
		"codex":       "codex --dangerously-bypass-approvals-and-sandbox",
		"opencode-ai": "opencode --dangerously-skip-permissions",
		"antigravity": "agy --dangerously-skip-permissions",
		"grok":        "grok",
	}
	for id, cmd := range wantCommands {
		if !strings.Contains(tmpl, `"`+id+`"`) {
			t.Errorf("template missing agent id %q", id)
		}
		if !strings.Contains(tmpl, cmd) {
			t.Errorf("template missing verified command for %q: %q", id, cmd)
		}
	}
}

// TestBootstrapExtension_LiveContract pins the behavioral contract of the
// launcher: it reads the agent set from the LIVE zembed env store (not the
// frozen process env), reacts to changes via fs.watch (no polling), and falls
// back to the Claude plugin. Markers are deliberately coarse — they lock the
// architecture, not the implementation.
func TestBootstrapExtension_LiveContract(t *testing.T) {
	t.Parallel()
	tmpl, err := content.GetTemplate("vscode-bootstrap-extension.js")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	markers := []string{
		"ZCP_AGENT_TYPES",               // the env knob driving the launcher
		"/etc/zerops-zembed",            // the live env store, not process.env
		"fs.watch",                      // live reaction without polling
		"anthropic.claude-code",         // Claude plugin fallback path
		"claude-vscode.editor.open",     // ...via its open command
		"vscode.TerminalLocation.Panel", // agents run in the integrated terminal panel
		"registerWebviewViewProvider",   // always-available activity-bar launcher
	}
	for _, m := range markers {
		if !strings.Contains(tmpl, m) {
			t.Errorf("template missing contract marker %q", m)
		}
	}
}

// TestBootstrapExtension_ActivityBarEntry pins the always-available entry point:
// the package.json contributes a Zerops-branded activity-bar container hosting a
// webview view, and the logo asset exists. Clicking the icon opens the launcher
// so the user can start another agent at any time, not just on startup.
func TestBootstrapExtension_ActivityBarEntry(t *testing.T) {
	t.Parallel()
	pkg, err := content.GetTemplate("vscode-bootstrap-package.json")
	if err != nil {
		t.Fatalf("GetTemplate package.json: %v", err)
	}
	for _, m := range []string{
		"viewsContainers", "activitybar", "zcpLauncher",
		`"icon": "logo.svg"`, "zcpAgents", `"type": "webview"`, "onView:zcpAgents",
	} {
		if !strings.Contains(pkg, m) {
			t.Errorf("package.json missing activity-bar contribution %q", m)
		}
	}
	svg, err := content.GetTemplate("vscode-bootstrap-logo.svg")
	if err != nil {
		t.Fatalf("GetTemplate logo.svg: %v", err)
	}
	if !strings.Contains(svg, "<svg") || !strings.Contains(svg, "</svg>") || !strings.Contains(svg, "viewBox") {
		t.Errorf("logo.svg is not a well-formed SVG: %.80q", svg)
	}
}
