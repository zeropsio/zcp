package adapters

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/content"
)

// TestBootstrapExtension_AgentCommandsPinned is the drift guard for the agent
// launcher. The bootstrap extension (vscode-bootstrap-extension.js) maps each
// agent to launch commands that BYPASS permission prompts — a wrong flag is a
// correctness + safety bug. The launcher logic lives in JS (so it can read the
// live zembed env store and react via fs.watch without a process spawn), so
// this test pins the safety-critical invocations against the shipped template.
// Each command was verified against the real CLI binary / official docs:
//
//   - claude-code: opens the installed Claude Code VS Code plugin via its
//     `claude-vscode.editor.open` command, or a terminal running
//     `claude --dangerously-skip-permissions --effort max` (bypass all
//     permission prompts; --effort max is the top reasoning level, verified
//     `low|medium|high|xhigh|max` on claude-cli 2.1.160).
//   - codex:       `codex --dangerously-bypass-approvals-and-sandbox` — skips
//     all approvals AND disables codex's own sandbox (the Zerops container is
//     the sandbox; agents get full host access). Parses on codex-cli 0.125.0.
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

	// Claude's terminal open mode bypasses permission prompts (and runs at max
	// effort) — safety-critical, pin it verbatim.
	if !strings.Contains(tmpl, "claude --dangerously-skip-permissions --effort max") {
		t.Errorf("template missing Claude terminal bypass command")
	}

	// opencode-ai was dropped (the gist's auth model covers 4 agents and the
	// platform writes no ZCP_AGENT_*_OPENCODE_AI envs); it must not linger.
	if strings.Contains(tmpl, "opencode") {
		t.Errorf("template still references opencode — it was dropped from the launcher")
	}
}

// TestBootstrapExtension_AuthModelPinned pins the auth-aware (new-GUI) mode of
// the dual-mode launcher against the gist contract (zcp-envs.md). The extension
// switches to this mode by feature-detecting per-agent auth envs in the live
// store; without them it keeps the legacy ZCP_AGENT_TYPES behavior. Markers
// lock: the three suffixed env families, the namespace-based mode switch, the
// `authorized` formula (OAuth-done OR token-present), the always-render-all-4
// list, Claude's two open modes (extension + terminal), and the auth render
// path. The token VALUE must never be surfaced — only its presence drives
// `authorized` — so we assert the launch message carries no token field.
func TestBootstrapExtension_AuthModelPinned(t *testing.T) {
	t.Parallel()
	tmpl, err := content.GetTemplate("vscode-bootstrap-extension.js")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	markers := []string{
		"ZCP_AGENT_AUTH_TYPE_",                                // per-agent auth-type env family
		"ZCP_AGENT_OAUTH_",                                    // per-agent oauth-done flag family
		"ZCP_AGENT_TOKEN_",                                    // per-agent token-presence family
		"ZCP_AGENT_(AUTH_TYPE|OAUTH|TOKEN)_",                  // namespace switch: any present → auth mode
		`=== "true" || !!env["ZCP_AGENT_TOKEN_`,               // authorized = OAuth-done OR token-present
		`["claude-code", "codex", "antigravity", "grok"]`,     // always render all 4
		`{ mode: "extension", command: CLAUDE_OPEN_COMMAND }`, // Claude opens via its plugin
		`{ mode: "terminal", command: "claude --dangerously-skip-permissions --effort max" }`, // ...and a max-effort claude terminal
		"renderAuthHtml", // the auth-aware render path
		`type: "launch"`, // auth-mode launch message
	}
	for _, m := range markers {
		if !strings.Contains(tmpl, m) {
			t.Errorf("template missing auth-model marker %q", m)
		}
	}
	// Each agent's env suffix must be the uppercase, "-"→"_" form.
	for _, suffix := range []string{`"CLAUDE_CODE"`, `"CODEX"`, `"ANTIGRAVITY"`, `"GROK"`} {
		if !strings.Contains(tmpl, suffix) {
			t.Errorf("template missing agent env suffix %s", suffix)
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
