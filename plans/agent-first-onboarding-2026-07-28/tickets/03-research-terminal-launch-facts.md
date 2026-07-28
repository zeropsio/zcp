# 03 — Research: terminal launch + readiness facts

- `status:` closed
- `type:` research
- `assignee:` research-subagent (fired 2026-07-28)
- `blocked-by:` —

## Question

Surface the facts the launch + handshake decisions wait on:

1. Does the `claude` CLI accept an initial prompt as argv that **auto-submits in interactive
   mode** (e.g. `claude "Onboard me to Zerops."`) — as distinct from `-p` print mode? Primary
   sources: official Claude Code docs/CHANGELOG.
2. Confirm the initial-prompt argv for the other registry agents (codex / cursor-agent /
   antigravity positional or flag forms) as currently encoded on main (spec §7 claims
   live-verified; locate the registry code that holds them).
3. What VS Code APIs available under code-server (engine ^1.94) can detect that a terminal
   command actually **started** — shell-integration events
   (`window.onDidStartTerminalShellExecution` etc.), their availability/degradation without
   shell integration, and what the extension can honestly know about a running TUI process.
4. The §7.1 kickoff wrapper (`~/.zcp/bin/claude-kickoff`, `claudeCode.claudeProcessWrapper`
   setting, HOME kickoff marker): if onboarding goes terminal-only and the plugin path survives
   only as the panel's promptless `Open extension` action — is the wrapper machinery deletable,
   and which init/install sites reference it?

Findings: `plans/research/terminal-launch-readiness-2026-07-28.md`.

## Answer

Findings: [`plans/research/terminal-launch-readiness-2026-07-28.md`](../../research/terminal-launch-readiness-2026-07-28.md).

1. `claude "prompt"` **auto-submits in interactive mode** — confirmed by primary-but-community
   sources (GitHub issues #11476, #17284); the official CLI reference is soft on it.
2. Registry argv (`vscode-bootstrap-extension.js:44-73` + splice in
   `vscode-bootstrap-welcome.js:1619-1631`): antigravity alone uses `--prompt-interactive`;
   codex/cursor/grok/claude-terminal get a bare positional prompt. Only claude/codex/antigravity
   shapes are test-pinned; cursor/grok aren't, and spec §7's "live-verified" claim has no in-repo
   provenance.
3. **No VS Code API can prove the agent TUI is running.** Shell integration
   (`onDidStartTerminalShellExecution`, stable since 1.93) fires only if integration activates
   (not guaranteed) and only proves "the shell started a command line" — never that the CLI
   rendered or accepted the prompt. Honest observables: terminal created; text sent; (maybe)
   command started. The kickoff wrapper's stream-json trick exists precisely for this gap —
   ticket 02 must pick its honesty level with this ceiling in mind.
4. Kickoff wrapper is deletable across 5 sites (template, `claude.go:264,511-567`, the marker-arm
   branch in `handleOnboard` — NOT the shared `seedOpenWithPrompt`/`shellQuoteArg`/`ONBOARD_PROMPT`
   — `onboard.test.js` assertions, spec §7/§7.1 prose; no invariant-table row, no e2e refs).
   Surfaced-not-resolved design question for tickets 01/02: `handleOnboard` launches `opens[0]`,
   which for claude is the plugin — the terminal-only onboarding decision means the auto-launch
   path can't reuse it as-is (flip claude terminal-primary, or bypass `opens[0]`).
