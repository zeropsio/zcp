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
//   - grok:        `grok --yolo` — xAI grok CLI's YOLO mode: "auto-approve all
//     tool executions" (deny rules + PreToolUse hooks still apply). Verified
//     live on grok 0.2.73: `grok --yolo -p x` parses + runs (contrast: a bogus
//     flag errors "unexpected argument"); grok's own docs use `--yolo`.
//   - cursor:      `cursor-agent --force --approve-mcps` — Cursor CLI's
//     interactive agent with `--force` (alias `--yolo`): "force allow
//     commands unless explicitly denied" for shell/file edits, PLUS
//     `--approve-mcps`: "automatically approve all MCP servers" — a
//     separate axis from --force (server approval, not command approval).
//     Per-tool-call approval within an approved server is covered by the
//     project .cursor/cli.json Mcp(zerops:*) entry written by `zcp init`'s
//     generateCursorProjectConfig step. Verified against live
//     `cursor-agent --help` output (2026.07.01-41b2de7) + cursor.com/docs/cli.
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
		"grok":        "grok --yolo",
		"cursor":      "cursor-agent --force --approve-mcps",
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

	// Label + bin pins: two agents are branded differently in the current
	// Zerops GUI (grok/cursor CLIs show as "Grok Build"/"Cursor CLI" there),
	// and `bin` is the PATH-probed executable name isAgentInstalled uses.
	for _, label := range []string{"Grok Build", "Cursor CLI"} {
		if !strings.Contains(tmpl, label) {
			t.Errorf("template missing agent label %q", label)
		}
	}
	wantBins := map[string]string{
		"claude-code": "claude",
		"codex":       "codex",
		"antigravity": "agy",
		"grok":        "grok",
		"cursor":      "cursor-agent",
	}
	for id, bin := range wantBins {
		if !strings.Contains(tmpl, `bin: "`+bin+`"`) {
			t.Errorf("template missing bin pin for %q: %q", id, bin)
		}
	}
}

// TestBootstrapExtension_AgentStatusModelPinned pins the single auth-aware
// launcher model against the gist contract (zcp-envs.md): every agent in
// REGISTRY is always rendered — filtered only by resolveAvailableAgentIds
// (ZCP_AGENTS presentation policy) — with its per-agent auth status attached.
// Markers lock: the three suffixed env families, the `authorized` formula
// (OAuth-done OR token-present), the always-consider-all-5 registry, Claude's
// two open modes (extension + terminal), and the single render path. The
// token VALUE must never be surfaced — only its presence drives `authorized`
// — so we assert the launch message carries no token field.
func TestBootstrapExtension_AgentStatusModelPinned(t *testing.T) {
	t.Parallel()
	tmpl, err := content.GetTemplate("vscode-bootstrap-extension.js")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	markers := []string{
		"ZCP_AGENT_AUTH_TYPE_",                  // per-agent auth-type env family
		"ZCP_AGENT_OAUTH_",                      // per-agent oauth-done flag family
		"ZCP_AGENT_TOKEN_",                      // per-agent token-presence family
		`=== "true" || !!env["ZCP_AGENT_TOKEN_`, // authorized = OAuth-done OR token-present
		`["claude-code", "codex", "antigravity", "grok", "cursor"]`,                           // always consider all 5
		`{ mode: "extension", command: CLAUDE_OPEN_COMMAND }`,                                 // Claude opens via its plugin
		`{ mode: "terminal", command: "claude --dangerously-skip-permissions --effort max" }`, // ...and a max-effort claude terminal
		"renderLauncherHtml", // the single render path
		`type: "launch"`,     // launch message
	}
	for _, m := range markers {
		if !strings.Contains(tmpl, m) {
			t.Errorf("template missing agent-status marker %q", m)
		}
	}
	// Each agent's env suffix must be the uppercase, "-"→"_" form.
	for _, suffix := range []string{`"CLAUDE_CODE"`, `"CODEX"`, `"ANTIGRAVITY"`, `"GROK"`, `"CURSOR"`} {
		if !strings.Contains(tmpl, suffix) {
			t.Errorf("template missing agent env suffix %s", suffix)
		}
	}
}

// TestBootstrapExtension_LiveContract pins the behavioral contract of the
// launcher: it reads the agent set from the LIVE zembed env store (not the
// frozen process env) via ZCP_AGENTS, reacts to changes via fs.watch (no
// polling), and never falls back to legacy ZCP_AGENT_TYPES filtering. Markers
// are deliberately coarse — they lock the architecture, not the
// implementation.
func TestBootstrapExtension_LiveContract(t *testing.T) {
	t.Parallel()
	tmpl, err := content.GetTemplate("vscode-bootstrap-extension.js")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	markers := []string{
		"ZCP_AGENTS",                    // the presentation env driving availability
		"/etc/zerops-zembed",            // the live env store, not process.env
		"fs.watch",                      // live reaction without polling
		"claude-vscode.editor.open",     // Claude's extension open mode
		"vscode.TerminalLocation.Panel", // agents run in the integrated terminal panel
		"registerWebviewViewProvider",   // always-available activity-bar launcher
	}
	for _, m := range markers {
		if !strings.Contains(tmpl, m) {
			t.Errorf("template missing contract marker %q", m)
		}
	}
	if strings.Contains(tmpl, "ZCP_AGENT_TYPES") {
		t.Errorf("template still references legacy ZCP_AGENT_TYPES — it must be fully deleted")
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

// TestBootstrapExtension_WelcomeLazyPins is the source-level guard for W3
// (dark/lazy welcome load — docs/spec-welcome-mode.md §1). The BEHAVIORAL
// guarantee — welcome.js never loads and no panel exists before the command
// runs, a broken welcome.js can't take the launcher down — is proven by the
// welcomejs node:test suite (TestWelcomeJS, internal/content package); this
// pins the two textual invariants that make that behavior possible: the
// command is registered, and require("./welcome.js") is never hoisted to
// module top level (which would defeat the whole dark contract).
func TestBootstrapExtension_WelcomeLazyPins(t *testing.T) {
	t.Parallel()
	tmpl, err := content.GetTemplate("vscode-bootstrap-extension.js")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	if !strings.Contains(tmpl, `registerCommand("zerops.welcome"`) {
		t.Errorf("template missing zerops.welcome command registration")
	}
	if !strings.Contains(tmpl, `require("./welcome.js")`) {
		t.Errorf("template missing lazy require of welcome.js")
	}
	for line := range strings.SplitSeq(tmpl, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "const") && strings.Contains(trimmed, `require("./welcome`) {
			t.Errorf("require(\"./welcome.js\") hoisted to a top-level const: %q", line)
		}
	}

	pkg, err := content.GetTemplate("vscode-bootstrap-package.json")
	if err != nil {
		t.Fatalf("GetTemplate package.json: %v", err)
	}
	if !strings.Contains(pkg, `"zerops.welcome"`) {
		t.Errorf("package.json missing zerops.welcome command contribution")
	}
}
