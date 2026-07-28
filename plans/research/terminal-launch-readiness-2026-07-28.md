# Research: terminal launch + readiness facts

Ticket: `plans/agent-first-onboarding-2026-07-28/tickets/03-research-terminal-launch-facts.md`

---

## 1. Does `claude "prompt"` (positional argv, interactive mode) auto-submit?

### FACTS

- Official CLI reference (`https://code.claude.com/docs/en/cli-reference`, fetched
  2026-07-28), verbatim commands table:

  | Command | Description | Example |
  |---|---|---|
  | `claude` | Start interactive session | `claude` |
  | `claude "query"` | Start interactive session with initial prompt | `claude "explain this project"` |

  and flags table:

  | Flag | Description | Example |
  |---|---|---|
  | `--print`, `-p` | Print response without interactive mode (see Agent SDK docs for programmatic usage) | `claude -p "query"` |

  The doc prose itself does not use the word "submit" — "start interactive session
  with initial prompt" is the only description given. Taken alone, the docs page is
  ambiguous between "prefills the composer" and "submits it as the first turn."

- Primary disambiguation comes from the `anthropics/claude-code` GitHub repo (the
  official upstream), not the docs page:
  - Issue **#11476**, `[FEATURE] Command line arg to not auto-submit the provided
    prompt` (fetched 2026-07-28): the reporter states as the current-behavior premise
    — *"Currently, when Claude Code is launched with a prompt as an argument, Claude
    will launch and automatically run that prompt."* — and requests a new flag to
    **disable** that auto-run so a prompt can be reviewed before it fires. No
    maintainer pushback on the premise is visible.
  - Issue **#17284**, `[BUG] UserPromptSubmit hook does not anymore trigger with an
    initial prompt as argument` (fetched 2026-07-28), includes an actual terminal
    transcript on v2.0.76:
    ```
    % claude 'test'
     Claude Code v2.0.76 · Opus 4.5
    > test
      ⎿ UserPromptSubmit hook error
      ⎿ Interrupted · What should Claude do instead?
    ```
    i.e. the positional prompt was actively being processed (interruptible) the
    moment the session started — direct empirical confirmation of auto-submit, not
    just a doc claim. The bug in the issue is narrower: on v2.1.3 the
    `UserPromptSubmit` **hook** stopped firing for this launch path, but the v2.1.3
    transcript quoted in the same issue *still* shows the prompt line immediately
    followed by "Interrupted · What should Claude do instead?" — i.e. the prompt is
    still being submitted/run; only the hook-trigger notification regressed, not the
    submission itself.
  - `--append-system-prompt` is unrelated: per the CLI reference it appends to the
    system prompt, not the user turn — confirmed out of scope per the ticket, not
    re-verified in depth here since it's explicitly not the mechanism in question.

### ASSESSMENT

`claude "prompt"` (no `-p`) auto-submits the prompt as a real first user turn in
interactive mode — this is corroborated by primary sources (an accepted-premise
GitHub feature request + a literal transcript), even though the docs page's own
wording is soft. This is exactly the assumption the repo's own JS registry code
relies on for `claude-code`'s **terminal fallback** open mode (see §2) — worth
knowing that the assumption is real-world confirmed, not merely inferred, but that
the only primary-source evidence found is community/issue-tracker, not an explicit
docs-page guarantee — a docs regression or hook-timing edge case (per #17284) is a
plausible failure mode if this path is leaned on harder for the agent-first launch
flow's readiness signal.

---

## 2. Registry code: exact initial-prompt argv per agent, as encoded on `main`

### FACTS

Registry lives in `internal/content/templates/vscode-bootstrap-extension.js:44-73`
(the `REGISTRY` object, `bin`/`opens[]` per agent id), pinned by
`internal/init/adapters/launcher_test.go:41` (`TestBootstrapExtension_AgentCommandsPinned`).
Only `antigravity`'s entry carries an `initialPromptFlag`:

```js
// vscode-bootstrap-extension.js:44-73
"claude-code": { ..., opens: [
  { mode: "extension", command: CLAUDE_OPEN_COMMAND },
  { mode: "terminal", command: "claude --dangerously-skip-permissions --effort max" },
]},
"codex": { ..., opens: [{ mode: "terminal", command: "codex --dangerously-bypass-approvals-and-sandbox" }] },
"antigravity": { ..., opens: [{ mode: "terminal", command: "agy --dangerously-skip-permissions", initialPromptFlag: "--prompt-interactive" }] },
"grok": { ..., opens: [{ mode: "terminal", command: "grok --yolo" }] },
"cursor": { ..., opens: [{ mode: "terminal", command: "cursor-agent --force --approve-mcps" }] },
```

The prompt is actually spliced in at launch time by
`internal/content/templates/vscode-bootstrap-welcome.js:1619-1631`
(`seedOpenWithPrompt`), called from `handleOnboard` (1643-1706):

```js
// vscode-bootstrap-welcome.js:1628-1631
const promptFlag = typeof open.initialPromptFlag === "string" && open.initialPromptFlag
  ? " " + open.initialPromptFlag
  : "";
return Object.assign({}, open, { command: open.command + promptFlag + " " + shellQuoteArg(prompt) });
```

`shellQuoteArg` (line 1594) POSIX single-quotes the prompt. So the **resulting
argv per agent**, for `ONBOARD_PROMPT = "Onboard me to Zerops."` (line 1590):

| agent id | resulting terminal command (verbatim) | shape |
|---|---|---|
| `codex` | `codex --dangerously-bypass-approvals-and-sandbox 'Onboard me to Zerops.'` | bare positional (no flag) |
| `cursor` | `cursor-agent --force --approve-mcps 'Onboard me to Zerops.'` | bare positional (no flag) |
| `grok` | `grok --yolo 'Onboard me to Zerops.'` | bare positional (no flag) |
| `antigravity` | `agy --dangerously-skip-permissions --prompt-interactive 'Onboard me to Zerops.'` | flagged (`--prompt-interactive`) |
| `claude-code` (terminal **fallback** only — `runAgentAction`'s catch path when `editor.open` fails, `vscode-bootstrap-extension.js:326-334`) | `claude --dangerously-skip-permissions --effort max 'Onboard me to Zerops.'` | bare positional (no flag) — same shape as codex/cursor/grok, relies on the §1 auto-submit behavior |
| `claude-code` (primary — extension mode) | no argv at all; prompt delivered by the kickoff wrapper (§4) | out-of-band, not argv |

These exact strings are pinned by the JS test suite:
`internal/content/welcomejs/onboard.test.js:86` (claude terminal fallback),
`:130` (codex), `:141` (antigravity — the only one asserting the flag form).
No test pins the `cursor`/`grok` onboard argv specifically (found no
`registry.cursor.opens[0].command + ... ONBOARD_PROMPT` or `registry.grok...`
assertion in `onboard.test.js` — only claude/codex/antigravity are asserted there).

Ticket's premise says "spec §7 claims codex/cursor positional and antigravity
`--prompt-interactive` are live-verified" — `docs/spec-welcome-mode.md:279-280`
does say this:
> **Terminal agents:** the prompt is appended as the CLI's live-verified
> initial-prompt argv (Codex/Cursor positional; Antigravity `--prompt-interactive`),
> which auto-submits on start.

But the *code-level* provenance comment that verifies each launch command
(`internal/init/adapters/launcher_test.go:10-40`) documents where each **base**
command (`--dangerously-bypass-approvals-and-sandbox`, `--yolo`,
`--dangerously-skip-permissions`, `--force --approve-mcps`) was verified (CLI
`--help` output / vendor docs / live runs) — it does **not** independently document
where the *positional-prompt-auto-submits* claim or the `--prompt-interactive` flag
itself were verified for antigravity, codex, or cursor. The only assertion tying
`--prompt-interactive` to reality is the string-literal pin at
`launcher_test.go:75` (`initialPromptFlag: "--prompt-interactive"` must appear in
the template) — that pins the template's *content*, not the *external CLI's
behavior*.

### ASSESSMENT

The registry itself is unambiguous and directly readable — that part of the ticket
is fully answered from code. The "live-verified" claim in spec §7 is **not**
independently substantiated inside the repo (no doc-comment analogous to the
`launcher_test.go` provenance block exists for the prompt-argv shapes); it rests on
whatever out-of-band verification produced that spec sentence. Given §1 above (only
GitHub-issue-level primary evidence for `claude`'s own auto-submit, no official docs
guarantee), it's worth treating "live-verified" for codex/cursor/grok positional
argv and antigravity's `--prompt-interactive` as an *external, undocumented-in-repo*
fact rather than something this research ticket can re-confirm from primary sources
without actually invoking each CLI.

---

## 3. VS Code terminal-start detection APIs (code-server engine `^1.94.0`)

Engine pin: `internal/content/templates/vscode-bootstrap-package.json:7` —
`"engines": { "vscode": "^1.94.0" }`.

### FACTS

Fetched the authoritative `vscode.d.ts` from `microsoft/vscode` (`main` branch,
`https://raw.githubusercontent.com/microsoft/vscode/main/src/vscode-dts/vscode.d.ts`,
2026-07-28) and the VS Code 1.93 release notes
(`https://code.visualstudio.com/updates/v1_93`).

- **The shell-integration terminal API finalized as stable in VS Code 1.93**
  (August 2024) — `onDidStartTerminalShellExecution`,
  `onDidEndTerminalShellExecution`, `Terminal.shellIntegration`,
  `shellIntegration.executeCommand()`. This predates the repo's `^1.94.0` engine
  pin, so it is available (not proposed-API-only) for this extension.
- `window.onDidStartTerminalShellExecution: Event<TerminalShellExecutionStartEvent>`
  — doc comment (`vscode.d.ts:11197-11202`): *"This will be fired when a terminal
  command is started. This event will fire **only when shell integration is
  activated** for the terminal."* Same gating language for
  `onDidEndTerminalShellExecution` (`:11204-11209`).
- `Terminal.shellIntegration: TerminalShellIntegration | undefined`
  (`vscode.d.ts:7708-7718`): *"This will **always be `undefined` immediately after
  the terminal is created**. Listen to `window.onDidChangeTerminalShellIntegration`
  to be notified when shell integration is activated... this object **may remain
  undefined if shell integration never activates**. For example Command Prompt does
  not support shell integration and a **user's shell setup could conflict** with the
  automatic shell integration activation."*
- `Terminal.state: TerminalState` (`vscode.d.ts:7795-7823`) —
  `TerminalState.isInteractedWith: boolean`: *"Whether the Terminal has been
  interacted with. Interaction means that the terminal has sent data to the process
  which... by default input is sent when a key is pressed **or when a command or
  extension sends text**, but... it can also happen on a pointer click/scroll/move
  event or terminal focus in/out."* — i.e. this flips true the moment the extension
  itself calls `term.sendText(...)`; it is not a confirmation that a shell or
  process on the other end did anything with that data.
- `window.onDidOpenTerminal: Event<Terminal>` (`vscode.d.ts:11176-11180`): *"Fires
  when a terminal has been created, either through the `createTerminal` API or
  commands."* — fires on Terminal-object creation, before any pty/process work.
- `TerminalShellIntegration.executeCommand` doc example (`vscode.d.ts:7844-7864`,
  literal from the official typings) shows Microsoft's own recommended degrade
  pattern:
  ```js
  window.onDidChangeTerminalShellIntegration(async ({ terminal, shellIntegration }) => {
    if (terminal === myTerm) {
      const execution = shellIntegration.executeCommand('echo "Hello world"');
      window.onDidEndTerminalShellExecution(event => {
        if (event.execution === execution) console.log(`Command exited with code ${event.exitCode}`);
      });
    }
  });
  // Fallback to sendText if there is no shell integration within 3 seconds of launching
  setTimeout(() => {
    if (!myTerm.shellIntegration) {
      myTerm.sendText('echo "Hello world"');
      // Without shell integration, we can't know when the command has finished or what the exit code was.
    }
  }, 3000);
  ```
  i.e. even Microsoft's own docs treat shell-integration activation as
  best-effort/timing-raced, with a documented "we can't know" fallback state.

### ASSESSMENT

Under code-server `^1.94.0` the shell-integration events exist and are stable, but
they are conditioned on shell integration having *activated*, which is: (a) not
instant (`undefined` right after terminal creation), (b) not guaranteed at all in
every shell/config, and (c) itself only observable via a *third* event
(`onDidChangeTerminalShellIntegration`) the extension would have to race against a
timeout, mirroring Microsoft's own documented pattern.

What the extension can **honestly** claim, layered by strength:
1. **"A terminal object exists"** — `onDidOpenTerminal` / the terminal handle
   returned by `createTerminal`. Says nothing about any process inside it.
2. **"We sent text to the pty"** — `term.sendText(cmd, true)` returning, or
   `TerminalState.isInteractedWith` flipping true. This is what the *current*
   `runTerminal()` in `vscode-bootstrap-extension.js:294-314` already does — it
   fires immediately (`sendText`) and relies on a **blind `setTimeout(..., 250)`**
   (line 306) to re-focus the panel; it has no confirmation the shell, let alone the
   agent CLI, received or acted on that text.
3. **"The shell began running a command line"** — `onDidStartTerminalShellExecution`,
   *only if* shell integration activated for that terminal before the command was
   sent (race condition against activation timing; not available at all for some
   shells). This is the closest true "started" signal VS Code offers, and it is
   still shell-level: it confirms the shell parsed and started a command line, not
   that the child CLI's own TUI initialized, rendered a prompt box, or accepted a
   positional-argv prompt internally — none of that is observable through any VS
   Code terminal API, because it happens inside the child process's own TTY
   rendering.
4. **Nothing in the VS Code terminal API can confirm "the agent TUI is now running
   with the prompt [accepted]."** That claim requires either an out-of-band signal
   from the CLI itself (which is exactly what the existing kickoff wrapper does for
   the Claude-plugin path — see §4 — by hooking the CLI's own stream-json
   `control_response`, not any VS Code API) or a fixed-delay heuristic (the
   wrapper's own 12s stdin-injection safety net, `vscode-claude-kickoff-wrapper.py:175-179`,
   is exactly that kind of heuristic). For terminal-launched agents there is
   currently no equivalent protocol-level hook in this repo — the honest ceiling is
   signal 3 above (or signal 2, if shell integration doesn't activate in time),
   which is a “the command line started” signal, not a “the agent is ready and the
   prompt was accepted” signal.

---

## 4. Kickoff wrapper machinery — deletability + reference sites

### FACTS

**Files:**
- `internal/content/templates/vscode-claude-kickoff-wrapper.py` (220 lines) — the
  Python process-wrapper template itself, installed executable at
  `~/.zcp/bin/claude-kickoff`.
- `internal/init/adapters/claude.go:511-551` — `patchVSCodeClaudeWrapper(settingsPath string) error`:
  installs the wrapper (calls `installKickoffWrapper`) and writes
  `settings["claudeCode.claudeProcessWrapper"] = wrapperPath` into the VS Code
  `settings.json`.
- `internal/init/adapters/claude.go:553-567` — `installKickoffWrapper(path string) error`:
  fetches `content.GetTemplate("vscode-claude-kickoff-wrapper.py")` and writes it
  executable (`0o755`) to `~/.zcp/bin/claude-kickoff`, overwritten every init.
- **Call site:** `internal/init/adapters/claude.go:264`, inside
  `configureVSCode(env Env) error` (function starts at line 220) — one call,
  non-fatal on error (`(warning: claude wrapper patch failed: %v)`).
- `internal/content/templates/vscode-bootstrap-welcome.js:1584-1706` — the JS side
  of the contract:
  - `ONBOARD_PROMPT` const (1590)
  - `shellQuoteArg` (1594-1596) — shared by terminal-argv and marker paths
  - `kickoffMarkerPath(deps)` (1603-1605) → `~/.zcp/state/claude-kickoff.json`
  - `armKickoffMarker(prompt, deps)` (1607-1617) — writes the marker JSON
  - `seedOpenWithPrompt(open, prompt)` (1619-1631) — **shared** helper: for
    `mode === "extension"` it's a no-op passthrough (comment explains the wrapper
    handles submission out-of-band); for `mode === "terminal"` it does the argv
    splicing used by *every* terminal agent, not just Claude's fallback. Deleting
    the wrapper does **not** let you delete this function — codex/cursor/grok/
    antigravity's positional-argv delivery goes through the same code path.
  - `handleOnboard(agentId, deps)` (1643-1706) — the one call site of
    `armKickoffMarker`, gated on `primary.mode === "extension"` (1683-1685).
- `docs/spec-welcome-mode.md:272-303` — §7/§7.1 prose describing the whole
  contract. No entry in the pinned Invariants table (`W1`-`W10`,
  `docs/spec-welcome-mode.md:326-339`) names the wrapper/kickoff marker by
  identifier — the invariant table has no dedicated `W-KICKOFF` row, only prose.
- **Tests:** no Go test found for `patchVSCodeClaudeWrapper` / `installKickoffWrapper`
  (`grep -in wrapper internal/init/adapters/claude_test.go` → no hits). JS tests
  exercising the marker/wrapper contract:
  - `internal/content/welcomejs/onboard.test.js:86-90` (arms marker for
    claude-code, asserts marker path + prompt content) and `:132` (asserts a
    terminal agent — codex — never touches the marker).
  - `internal/content/welcomejs/cta_flow.test.js` / `ui_structure.test.js` /
    `open_agent.test.js` reference the general onboarding/kickoff-prompt
    vocabulary but not the marker/wrapper mechanics specifically (`open_agent.test.js`
    explicitly documents it carries **no** kickoff prompt at all, by contrast).
- **No references found** in `e2e/`, `integration/`, or `tools/` (including
  `tools/welcome-bridge-harness/`, the bridge E2E rig noted in
  `CLAUDE.local.md`) — `grep -rln "kickoff|Kickoff|claudeProcessWrapper" e2e/
  integration/ tools/` returned nothing.
- **Not a registration-list problem:** `internal/content/content.go:24-30`
  (`GetTemplate`) reads straight off an `embed.FS` glob (`//go:embed all:templates`)
  — there is no separate manifest/allowlist of template names to edit; deleting the
  `.py` template file is sufficient on that side once `GetTemplate("vscode-claude-kickoff-wrapper.py")`
  callers are gone.
- Two unrelated `"kickoff"` hits exist in `internal/ops/deploy_batch.go` and
  `internal/tools/deploy_batch.go` — generic English usage ("deploy kickoff") for
  deploy-operation start, not this mechanism; irrelevant to this deletion.

### ASSESSMENT

If the product decision is "onboarding is terminal-only, and the Claude plugin
survives only as a promptless `Open extension` action" (per the ticket's framing),
the wrapper mechanism is deletable, but it is not a single-file deletion. A
deletion would have to touch, at minimum:

1. Delete `internal/content/templates/vscode-claude-kickoff-wrapper.py`.
2. Delete `patchVSCodeClaudeWrapper` + `installKickoffWrapper` and their call site
   in `internal/init/adapters/claude.go` (264, 511-567) — including whatever
   `settingsPath`/`claudeCode.claudeProcessWrapper` key write is no longer needed
   (check whether anything else in `configureVSCode` still needs `settingsPath`
   threaded through before removing the parameter chain).
3. In `vscode-bootstrap-welcome.js`: delete `kickoffMarkerPath`, `armKickoffMarker`,
   and the `if (primary.mode === "extension") armKickoffMarker(...)` branch inside
   `handleOnboard` (1683-1685) — but **keep** `seedOpenWithPrompt`/`shellQuoteArg`/
   `ONBOARD_PROMPT`, since terminal agents' positional-argv delivery depends on
   them independent of the wrapper. If the Claude plugin's `opens[0]` stops being
   `{mode: "extension", ...}` in the registry (i.e. Claude also becomes
   terminal-primary for onboarding), `seedOpenWithPrompt`'s `mode === "extension"`
   no-op branch and the `launchReg`/`welcomeColumn` special-casing for the
   extension mode in `handleOnboard` (1693-1704) would also need re-examination —
   that's a design decision, not a fact this ticket can settle.
4. Update `internal/content/welcomejs/onboard.test.js:86-90,132` — the
   claude-code-arms-marker assertion no longer applies once Claude has no
   marker-based path; the codex-never-touches-marker assertion becomes moot (or
   should become "no agent ever touches a marker") once the marker concept is gone
   entirely.
5. Rewrite `docs/spec-welcome-mode.md:272-303` (§7 delivery-per-launch-mode prose
   and all of §7.1) to describe the terminal-only delivery contract instead — no
   pinned invariant-table row needs updating since none currently names this
   mechanism (§Invariants table has no `W-KICKOFF`/wrapper entry), but the prose is
   the operative spec text and currently states the plugin-vs-terminal split as a
   design invariant, not just descriptive filler.
6. No `e2e/`, `integration/`, or `tools/welcome-bridge-harness/` cleanup required —
   confirmed no references there.

One open question this research surfaced but did not resolve (it's a design call,
not a fact): whether `CLAUDE_OPEN_COMMAND`/`claude-vscode.editor.open` stays as
`opens[0]` (primary) for `claude-code` in the registry once it's demoted to a
promptless convenience action, or whether the registry should reorder so the
terminal `claude --dangerously-skip-permissions --effort max` command becomes
primary for onboarding purposes specifically (distinct from the general launcher's
own primary-open preference) — `handleOnboard` currently launches via
`reg.opens[0]` (`vscode-bootstrap-welcome.js:1679`), so whichever mode is
`opens[0]` is what "Onboard me" (and the CTA path, `handleStartOnboarding` around
line 1494) will actually use.
