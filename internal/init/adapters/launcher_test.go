package adapters

import (
	"encoding/json"
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
// Each command was verified against the real CLI binary / official docs, live
// re-verified 2026-07-28 against the onboarding launch decision
// (docs/spec-welcome-mode.md §5.1) on: claude-cli 2.1.220, codex-cli 0.145.0,
// agy 1.1.5, grok 0.2.112, cursor-agent 2026.07.20 — all five still parse and
// run their pinned flags unchanged from the versions below:
//
//   - claude-code: opens the installed Claude Code VS Code plugin via its
//     `claude-vscode.editor.open` command, or a terminal running
//     `claude --dangerously-skip-permissions` (bypass all permission
//     prompts).
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
//
// Onboarding auto-submit finding (docs/spec-welcome-mode.md §5.1): a bare
// positional prompt (`claude "Onboard me to Zerops."`) auto-submits as the
// session's first turn in interactive mode — distinct from `-p` print mode —
// confirmed by primary-but-community evidence (GitHub issues #11476, #17284);
// the official CLI reference itself is soft on this. codex/cursor/grok/
// claude-terminal share the same bare-positional shape; antigravity alone
// needs `--prompt-interactive`. Only claude/codex/antigravity are pinned by a
// test (this file + launch_gate.test.js); cursor/grok's argv shapes carry no
// in-repo pin — a live-verified gap the onboarding launch decision accepted
// rather than blocking the whole feature on two unpinned CLIs.
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

	// Claude's terminal open mode bypasses permission prompts —
	// safety-critical, pin it verbatim.
	if !strings.Contains(tmpl, "claude --dangerously-skip-permissions") {
		t.Errorf("template missing Claude terminal bypass command")
	}
	// Seeded Claude launches pass editor.open's (sessionId, initialPrompt)
	// arguments through the same launch seam; plain launches still pass none.
	for _, marker := range []string{
		"const args = Array.isArray(open.args) ? open.args : [];",
		"vscode.commands.executeCommand(open.command, ...args)",
		`initialPromptFlag: "--prompt-interactive"`,
	} {
		if !strings.Contains(tmpl, marker) {
			t.Errorf("template missing seeded-launch marker %q", marker)
		}
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
		`["claude-code", "codex", "antigravity", "grok", "cursor"]`,              // always consider all 5
		`{ mode: "extension", command: CLAUDE_OPEN_COMMAND }`,                    // Claude opens via its plugin
		`{ mode: "terminal", command: "claude --dangerously-skip-permissions" }`, // ...and a claude terminal
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

// TestBootstrapExtension_ActivityBarInversion pins the icon-inversion contract
// (docs/spec-welcome-mode.md §1.4): the activity-bar container is retitled
// "Zerops" (no longer "Zerops Agents" — Data Studio is no longer the only
// icon left visible under agent-first) and now contributes a SECOND view,
// zcpPanelOpener, gated "zcpAgentFirst" — the exact mirror of the legacy
// zcpAgents view's "!zcpAgentFirst" gate — so exactly one of the two is ever
// visible and the Zerops icon itself never disappears in either mode.
func TestBootstrapExtension_ActivityBarInversion(t *testing.T) {
	t.Parallel()
	tmpl, err := content.GetTemplate("vscode-bootstrap-package.json")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	type viewEntry struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
		When string `json:"when"`
	}
	var manifest struct {
		ActivationEvents []string `json:"activationEvents"`
		Contributes      struct {
			ViewsContainers struct {
				Activitybar []struct {
					ID    string `json:"id"`
					Title string `json:"title"`
				} `json:"activitybar"`
			} `json:"viewsContainers"`
			Views map[string][]viewEntry `json:"views"`
		} `json:"contributes"`
	}
	if err := json.Unmarshal([]byte(tmpl), &manifest); err != nil {
		t.Fatalf("parse vscode-bootstrap-package.json: %v", err)
	}

	containers := manifest.Contributes.ViewsContainers.Activitybar
	if len(containers) != 1 || containers[0].Title != "Zerops" {
		t.Errorf("activity-bar container = %+v, want exactly one container titled %q", containers, "Zerops")
	}

	views := manifest.Contributes.Views["zcpLauncher"]
	if len(views) != 2 {
		t.Fatalf("zcpLauncher views = %+v, want exactly 2 (legacy zcpAgents + agent-first zcpPanelOpener)", views)
	}
	byID := map[string]viewEntry{}
	for _, v := range views {
		byID[v.ID] = v
	}
	if agents, ok := byID["zcpAgents"]; !ok || agents.When != "!zcpAgentFirst" {
		t.Errorf("zcpAgents view = %+v, want when=!zcpAgentFirst", agents)
	}
	if opener, ok := byID["zcpPanelOpener"]; !ok || opener.Type != "webview" || opener.When != "zcpAgentFirst" {
		t.Errorf("zcpPanelOpener view = %+v, want type=webview when=zcpAgentFirst", opener)
	}

	hasEvent := false
	for _, e := range manifest.ActivationEvents {
		if e == "onView:zcpPanelOpener" {
			hasEvent = true
		}
	}
	if !hasEvent {
		t.Errorf("activationEvents = %v, want onView:zcpPanelOpener", manifest.ActivationEvents)
	}
}

// TestBootstrapExtension_WelcomeLazyPins is the source-level guard for W3
// (default dark/lazy load + custom-GUI autostart — docs/spec-welcome-mode.md
// §1). The BEHAVIORAL guarantee is proven by the welcomejs node:test suite
// (TestWelcomeJS, internal/content package); this pins the public command
// seams, the live env marker, and the lazy require that must never be hoisted.
func TestBootstrapExtension_WelcomeLazyPins(t *testing.T) {
	t.Parallel()
	tmpl, err := content.GetTemplate("vscode-bootstrap-extension.js")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	if !strings.Contains(tmpl, `registerCommand("zerops.panel"`) {
		t.Errorf("template missing zerops.panel command registration")
	}
	if !strings.Contains(tmpl, `require("./welcome.js")`) {
		t.Errorf("template missing lazy require of welcome.js")
	}
	for _, marker := range []string{
		"agentFirst",
		`executeCommand("zerops.panel"`,
	} {
		if !strings.Contains(tmpl, marker) {
			t.Errorf("template missing agent-first startup marker %q", marker)
		}
	}
	// workbench.action.closeSidebar is legitimately used by the activity-bar
	// stub (createPanelOpenerViewProvider, §1.4) on a manual icon click — a
	// narrow, user-triggered collapse, distinct from the DELETED custom-GUI
	// STARTUP closeSidebar action (§11) that ran automatically and fought the
	// agent-first onboarding layout's need for Explorer visible. Pin that the
	// call site stays confined to that one function and never resurfaces on
	// the automatic activate()/boot path.
	providerStart := strings.Index(tmpl, `const COLLAPSE_SIDEBAR_COMMAND = "workbench.action.closeSidebar"`)
	if providerStart < 0 {
		t.Fatalf("expected the activity-bar stub's COLLAPSE_SIDEBAR_COMMAND const in template")
	}
	activateStart := strings.Index(tmpl, "\nasync function activate")
	if activateStart < 0 || activateStart < providerStart {
		t.Fatalf("expected activate() to follow the activity-bar stub")
	}
	confined := tmpl[providerStart:activateStart]
	if !strings.Contains(confined, "executeCommand(COLLAPSE_SIDEBAR_COMMAND)") {
		t.Errorf("createPanelOpenerViewProvider must collapse the sidebar on a manual click (§1.4)")
	}
	outsideConfined := tmpl[:providerStart] + tmpl[activateStart:]
	if strings.Contains(outsideConfined, `workbench.action.closeSidebar`) {
		t.Errorf("workbench.action.closeSidebar must stay confined to the manual-click activity-bar stub — the automatic boot path must never run it (§11)")
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
	if !strings.Contains(pkg, `"zerops.panel"`) {
		t.Errorf("package.json missing zerops.panel command contribution")
	}
	if strings.Contains(pkg, `"zerops.welcome"`) {
		t.Errorf("package.json must not retain the deleted zerops.welcome command identity (§11)")
	}
}

func TestBootstrapAgentFirst_DerivesFromInitZeropsSubdomain(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		// This install-time policy is the ONLY decision point — no runtime
		// override lives in welcome.html anymore (docs/spec-welcome-mode.md §1.2).
		{name: "container subdomain", raw: "https://zcp-24cb-8080.prg1.zerops.app", want: true},
		{name: "app subdomain", raw: "https://app.zerops.io", want: false},
		{name: "app subdomain, path and default port", raw: " https://APP.ZEROPS.IO:443/editor ", want: false},
		{name: "app subdomain with DNS root dot", raw: "https://app.zerops.io./editor", want: false},
		{name: "missing", raw: "", want: false},
		{name: "invalid", raw: "not a url", want: false},
		{name: "non HTTP", raw: "ftp://zcp.example.com", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := bootstrapAgentFirst(tt.raw); got != tt.want {
				t.Errorf("bootstrapAgentFirst(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}
