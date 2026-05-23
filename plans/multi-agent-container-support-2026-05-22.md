# Multi-Agent ZCP Container Support — Final Plan (revised 2026-05-23)

> **Status**: Plan finalized after research + Codex second-opinion + user-driven scope refinement.
> **Scope**: shipped ZCP binary's container-init flow. Add Codex CLI support, future-proof architecture for Cursor / Gemini / Antigravity.
> **Constraint**: ZCP is published. Backward-compat for user-facing surfaces is mandatory.

---

## 1. Goal & Scope

### What this plan covers

The `zcp` binary inside a Zerops dev container, when invoked as `zcp init`, needs to configure WHATEVER coding-agents are installed in the container (today only Claude Code; after this plan, Claude Code + Codex CLI; later optionally Cursor, Gemini, Antigravity). Init configures everything detected — user can run multiple agents in the same container in parallel.

### Confirmed assumptions

1. **Zerops dev container template pre-installs agent binaries.** ZCP detects + configures; doesn't install.
2. **Init configures all detected agents by default.** Parallel multi-agent in same container is first-class. User can switch between Claude and Codex (or use simultaneously) without re-init.
3. **One init flow, idempotent merge.** Re-running `zcp init` is safe; never overwrites user edits.
4. **Backward compat is mandatory.** ZCP is published; existing user projects with CLAUDE.md, REFLOG, .mcp.json, .zcp/state must continue working transparently.

### Out of scope

- Cursor adapter, Gemini adapter, Antigravity adapter — research preserved in Appendix C, deferred to future iterations
- VS Code extension scope decision (per-agent ext install) — defer until needed
- ZCP_TOOLSET server-side filtering (only matters for Cursor's 40-tool cap)
- SessionOwner abstraction (only matters for Cursor's IDE-embedded model)
- Eval harness multi-agent (dev tooling, not shipped binary)

---

## 2. Current state — what's already in place

### Foundation already agent-neutral

- `.zcp/state/` — work session, services, launches, git-push (path: `filepath.Join(cwd, ".zcp", "state")` in `internal/server/server.go:74`)
- MCP tool surface — works via standard MCP STDIO protocol
- Error envelopes, classification, recovery hints — all read/written through MCP JSON
- AGENTS.md exists at repo root (26-line adapter pointing to CLAUDE.md — Codex on this dev machine already uses it)
- `~/.codex/config.toml` on this dev machine already has `[mcp_servers.zcp]` + `[projects."<path>"] trust_level="trusted"` (working Codex+ZCP MVP locally)

### Container-provisioning YAML envs (declared today, not yet consumed by init code)

```yaml
envSecrets:
  ZCP_VSCODE: "true"              # consumed: triggers VS Code extension setup
  ZCP_AGENT_TYPE: "claude-code"   # NOT consumed by init — informational label only
  ZCP_AUTH_TYPE: "oauth"          # NOT consumed
  ZCP_PROVIDER: "anthropic"       # NOT consumed
```

These three labels become per-adapter hints after this plan (auth method, provider).

### Claude-coupled surface (what this plan touches)

| Surface | Location | Effort |
|---|---|---|
| Container Claude config writer | `internal/init/init_container.go:69-106` (`buildClaudeJSON`) | Extract to `adapters/claude.go`; refactor merge-aware |
| Container `.claude/settings.json` writer | `init_container.go:59` | Move with Claude adapter |
| Local `.mcp.json` + `.claude/settings.local.json` writer | `init.go:124-130` | Move with Claude adapter |
| CLAUDE.md composition | `internal/content/build_claude.go:24-48` + templates `claude_{container,local,shared}.md` | Rename to `build_agents.go`; split rendering target |
| Reflog inside CLAUDE.md | `internal/workflow/reflog.go` + marker section `<!-- ZEROPS:REFLOG -->` | Move writer to AGENTS.md (LLM-visibility preserved) |
| 4 hard-coded atoms in `internal/content/atoms/` | (listed in §6.4) | Genericize Claude-specific bits |

---

## 3. Architecture

### 3.1 Init flow — declarative multi-adapter

```
zcp init container:

  ┌─ Core (runs ALWAYS) ──────────────────────────────────
  │  • write AGENTS.md (canonical body + ZCP-managed marker)
  │  • one-time migrate CLAUDE.md REFLOG → AGENTS.md REFLOG
  └────────────────────────────────────────────────────────

  for each adapter in [claude, codex]:        # future: cursor, gemini, antigravity
    if !adapter.Detect():                     # binary in PATH?
      log debug "X not installed, skip"
      continue
    if warnings, err := adapter.Validate(env); err != nil:
      log warn "X: %v, skip"
      continue
    if err := adapter.ContainerInit(env):     # idempotent merge writes
      return err
    log info "X configured"
```

**Key properties:**
- Idempotent — re-running is safe, never overwrites user edits to existing entries
- Disk is source of truth — no registry to drift
- One init flow — no `--update` flag, no dichotomy
- Per-adapter independence — adapters write to disjoint config paths, no coordination needed
- Backward compat — existing Claude containers see no behavior change (Claude adapter runs identically; Codex adapter Detect fails and skips)

### 3.2 Adapter interface

```go
type Adapter interface {
    Name() string                              // "claude", "codex"

    // Quick presence check: is the binary in PATH?
    // For future IDE-based adapters (Cursor), checks project-level marker instead.
    Detect() bool

    // Deeper check: version compat, auth coherence.
    // Returns informational warnings + hard error (skip-this-adapter).
    Validate(env Env) (warnings []string, err error)

    // Idempotent merge-aware writes to per-agent config paths.
    // Pre-condition: Detect && Validate ok.
    ContainerInit(env Env) error
}
```

Three methods, clear ownership. No `Update` (ContainerInit is itself idempotent). No `Capabilities` (no consumer). No registry tracking.

### 3.3 Per-adapter ownership (no overlap)

| Adapter | Writes to |
|---|---|
| **Core** (always) | `AGENTS.md` (canonical body, env-specific: container OR local; ZCP-managed marker section; REFLOG section preserved/migrated) |
| **Claude** | `~/.claude.json` (mcpServers.zerops + trust + customApiKeyResponses) + `~/.claude/settings.json` (permissions) + **`CLAUDE.md`** (thin wrapper with `@AGENTS.md` include + Claude-specific deltas) + VS Code Claude extension setup (only when `ZCP_VSCODE=true`) + SSH config |
| **Codex** | `~/.codex/config.toml` (`[mcp_servers.zerops]` MCP stanza + `[projects."<container-workspace-path>"] trust_level="trusted"`) |

AGENTS.md is single-writer (Core). CLAUDE.md is single-writer (Claude adapter). `~/.claude*` only Claude. `~/.codex*` only Codex.

### 3.4 Canonical MCP server name = `zerops`

**Drift today**: `internal/content/templates/mcp-config.json:3` template uses `"zerops"` (deployed product). This dev repo's hand-curated `.mcp.json` uses `"zcp"` (Karel's personal pref). 116 atoms + recipes + tests all reference `mcp__zerops__*`.

**Resolution**: canonicalize on `zerops` (matches corpus + product). Fix this dev repo's `.mcp.json` (one file). Add `TestMCPServerNameCanonical` that scans every config-writing path + every corpus reference to ensure consistency.

**End users not affected** — they universally already get `zerops` from both local and container init paths (`generateMCPConfig` in `init.go:124-126` uses the same `"zerops"` template). Only this dev repo cleanup.

### 3.5 Context model — AGENTS.md canonical, env-specific

**AGENTS.md** is the cross-tool canonical context file (Codex / future Cursor / future Gemini all read it natively):

```
AGENTS.md  (env-specific: container OR local, NOT both — current
            env-separation in `build_claude.go:24` preserved)
├── <!-- ZCP-MANAGED:start -->
│   Boot shim content for THIS env
│   Zerops platform invariants
│   Per-runtime guidance
├── <!-- ZCP-MANAGED:end -->
├── <!-- ZEROPS:REFLOG -->
│   ### YYYY-MM-DD — Bootstrap: <intent>
│   - **Session:** <id>
│   - **Targets:** <runtimes>
│   …append-only history…
├── <!-- /ZEROPS:REFLOG -->
└── (user-authored content outside markers — preserved by init)
```

**CLAUDE.md** is Claude-specific thin wrapper (written by Claude adapter only):

```
CLAUDE.md
├── <!-- ZCP-MANAGED:start -->
│   @AGENTS.md
│   
│   ## Claude-specific deltas
│   - `Bash run_in_background=true` for long-running processes
│   - `BashOutput` / `KillBash` for managing background tasks
│   - Slash commands at .claude/commands/
├── <!-- ZCP-MANAGED:end -->
└── (user-authored content outside markers — preserved)
```

Claude reads CLAUDE.md → `@AGENTS.md` include inlines canonical body → Claude agent sees full context including REFLOG.

Codex reads AGENTS.md natively → sees same content (minus Claude-specific deltas).

### 3.6 REFLOG stays LLM-visible

**Why this matters**: REFLOG tells the LLM at startup "this project already has nodejs+postgres provisioned with intent X — don't suggest re-bootstrapping." Load-bearing for "don't redo work."

**Where it lives**: AGENTS.md REFLOG section (append-only, marker-delimited).

**Why AGENTS.md not `.zcp/reflog.jsonl`**: agent doesn't read JSONL at startup. Must be in the agent context file. AGENTS.md is the cross-tool canonical → all current and future agents see it.

**Writer**: `internal/workflow/reflog.go::AppendReflogEntry` (called from `bootstrap_outputs.go:83`) — change target path from CLAUDE.md to AGENTS.md.

**Migration for existing users**: detect old `<!-- ZEROPS:REFLOG -->` section in CLAUDE.md + no AGENTS.md present → extract REFLOG, write to AGENTS.md, remove from CLAUDE.md, leave CLAUDE.md as thin wrapper. One-time, idempotent.

### 3.7 Multi-PID concurrent safety (already works today)

Two simultaneous agent sessions (e.g., Claude in one terminal, Codex in another) spawn TWO independent `zcp serve` STDIO subprocesses. Both read/write `.zcp/state/`:

- **Work Session files** — keyed by PID, naturally segregated (`internal/workflow/work_session.go:42`)
- **Stale session cleanup** — `CleanStaleWorkSessions` runs at server boot (`engine.go:71-72`)
- **Service meta files** — per-hostname, atomic file replace via `os.Rename` (POSIX guarantee)
- **REFLOG append** — `O_APPEND` is POSIX-atomic for writes < `PIPE_BUF` (~512 bytes); REFLOG entries fit easily

No new locking layer needed; multi-PID is baked into today's design.

---

## 4. Backward compatibility

### 4.1 What MUST NOT break for existing users

| User-facing surface | Pinning test (today) |
|---|---|
| `~/.claude.json` field shape (trust + customApiKeyResponses) | `TestContainerSteps_ClaudeConfigs_ProjectEntry` (`init_container_test.go:99-150`) |
| `~/.claude.json customApiKeyResponses.approved=[last20]` | `TestContainerSteps_ClaudeConfigs_*ApiKey*` |
| `.claude/settings.local.json permissions.allow=["mcp__zerops__*"]` | `TestRun_GeneratesSettingsLocal` (`init_test.go:128-146`) |
| `.zcp/state/` layout (work sessions, services, launches) | `TestCleanStaleWorkSessions`, work-session schema tests |
| CLAUDE.md REFLOG content preserved across re-init | `TestCLAUDEMD_PreservesReflog` |
| `init` doesn't blow away user content outside ZCP markers | `TestUpgrade_PreservesUserContentOutsideMarkers` (NEW) |

All existing pinning tests MUST stay green throughout the refactor. New tests cover migration paths.

### 4.2 Migration strategy per change

| Change | User-visible delta | Migration | Risk |
|---|---|---|---|
| MCP server name canonicalize | NONE (end users already at `zerops` everywhere) | Dev-repo only `.mcp.json` rename | trivial |
| Adapter interface + Claude extract | NONE (code MOVE; behavior identical) | none needed; pinning tests guard | trivial |
| `merge.go` for Claude container | POSITIVE — user-added fields in `~/.claude.json` now preserved (today they're overwritten) | none needed | low (behavior improves) |
| AGENTS.md split + CLAUDE.md wrapper | One-time git diff: CLAUDE.md shrinks, AGENTS.md appears | First-run detection: `<!-- ZCP:BEGIN/END -->` markers present + no AGENTS.md → split. Idempotent (second run no-op). | medium — touches refresh path; tested heavily |
| REFLOG promotion CLAUDE.md → AGENTS.md | One-time git diff: REFLOG section moves files | First-run: parse old `<!-- ZEROPS:REFLOG -->` → write to AGENTS.md → remove from CLAUDE.md, leave pointer. Idempotent. | low — content preserved, format unchanged |
| Atom genericization (4 atoms) | Minor: Claude users still have `run_in_background` available, just less prescriptive guidance in the corpus | none needed | low |
| Codex adapter | NONE for Claude users; Codex users get NEW capability | none — purely additive | trivial |

**Key invariant**: every migration is ONE-WAY + AUTOMATIC + IDEMPOTENT + TESTED. No user manual steps required. All triggered transparently on first `zcp init` (or `zcp serve` startup refresh) after the upgrade.

### 4.3 CLAUDE.local.md "Pre-production" flip

Current `CLAUDE.local.md:15`:

> Pre-production: no backward-compat shims, no field-keep "for safety". Rename types, reshape tool JSON, restructure atoms, move packages — whatever the cleaner design needs.

Replace with:

> Published product: backward-compat for user-facing surfaces is mandatory. Existing users have ZCP installed; their projects carry CLAUDE.md (with REFLOG sections), .mcp.json, .claude.json fields, .zcp/state files, and `mcp__zerops__*` permission allowlists — these MUST continue to work transparently. Compatibility shims for INTERNAL refactors stay forbidden (rename types, reshape internal packages, restructure atoms freely). The rule applies at the seam between ZCP-managed files and user content: pinning tests lock the seam; migrations must be one-way + automatic + idempotent + tested. Symptom-level patches still accumulate into mess; structural correction still wins for INTERNAL changes — but never at the cost of breaking what users already have on disk.

---

## 5. Per-agent integration spec

### 5.1 Claude Code (current behavior — code MOVE only, zero behavior change)

`internal/init/adapters/claude.go`:
- `Detect()`: `which claude` returns ok (Claude binary in PATH)
- `Validate(env)`: returns ok; no version-gated features today
- `ContainerInit(env)`:
  - Container mode: merge into `~/.claude.json` (trust + customApiKeyResponses if `ANTHROPIC_API_KEY` set) + write `~/.claude/settings.json` + SSH config + VS Code Claude extension (only if `ZCP_VSCODE=true`)
  - Local mode: merge into `.mcp.json` + write `.claude/settings.local.json` permissions
  - Write `CLAUDE.md` as thin wrapper with `@AGENTS.md` include + Claude-specific deltas section

All pinning tests for Claude unchanged; just relocated to `adapters/claude_test.go`.

### 5.2 Codex CLI

`internal/init/adapters/codex.go`:
- `Detect()`: `which codex` returns ok
- `Validate(env)`: check `codex --version`; warn if < v0.131.0 (hooks support); hard error never (Codex works regardless of version, just feature-gated)
- `ContainerInit(env)`:
  - TOML structured merge into `~/.codex/config.toml`:
    ```toml
    [mcp_servers.zerops]
    command = "zcp"
    args = ["serve"]
    startup_timeout_sec = 30
    tool_timeout_sec = 600

    [mcp_servers.zerops.env]
    ZCP_API_KEY = "${ZCP_API_KEY}"

    [projects."<container-workspace-path>"]
    trust_level = "trusted"
    ```
  - Auth handling:
    - `ZCP_AUTH_TYPE=api-key` + `OPENAI_API_KEY` set → no extra config (Codex CLI reads env directly; no `customApiKeyResponses` equivalent)
    - `ZCP_AUTH_TYPE=oauth` or unset → user runs `codex login` interactively (ZCP doesn't pre-configure)
  - AGENTS.md already written by Core — Codex adapter just verifies presence (warn if missing)

### 5.3 Multi-agent runtime story (post-this-plan)

Container with both Claude + Codex installed + `zcp init` ran:
- `~/.claude.json` has `mcpServers.zerops` + trust + (optional) customApiKeyResponses
- `~/.codex/config.toml` has `[mcp_servers.zerops]` + project trust
- `AGENTS.md` exists with canonical body + REFLOG
- `CLAUDE.md` exists as thin `@AGENTS.md` wrapper
- VS Code Claude extension installed (if `ZCP_VSCODE=true`)

User opens terminal A → runs `claude` → Claude sees CLAUDE.md, calls ZCP MCP, spawns zcp serve process A.
User opens terminal B → runs `codex` → Codex sees AGENTS.md, calls ZCP MCP, spawns zcp serve process B.
Both sessions parallel, independent PIDs, shared `.zcp/state/` (safe).

User makes a bootstrap in either session → `bootstrap_outputs.go::AppendReflogEntry` appends to AGENTS.md REFLOG → both sessions see updated history on next read.

---

## 6. Atom genericization — the 4 atoms touched

### 6.1 `internal/content/atoms/develop-platform-rules-container.md`

**What it says today** (line 14):
> Mount basics in `claude_container.md` (boot shim).

**Why genericize**: `claude_container.md` is the template filename; after AGENTS.md migration the boot shim moves to `agents_container.md`. Reference becomes stale.

**Fix**: replace with generic "boot shim file" or "container section of AGENTS.md". Single-line edit.

### 6.2 `internal/content/atoms/develop-platform-rules-local.md`

**What it says today** (lines 19-25):
> a foreground `Bash` call to `npm run dev` / `php artisan serve` / `bun --watch` blocks your turn until the per-call bash timeout fires (**2 minutes in Claude Code**, then the harness kills it).
>
> In **Claude Code**, the canonical pattern is `run_in_background=true`:
>
> ```
> Bash run_in_background=true  command="npm run dev"
> Bash                         command="curl -s -o /dev/null -w '%{http_code}' http://localhost:5173/"
> BashOutput                   bash_id={task-id}
> KillBash                     shell_id={task-id}
> ```

And line 49:
> use `zcli vpn up <projectId>` from `claude_local.md`

**Why genericize**: Codex CLI has NO `run_in_background=true` parameter, NO `BashOutput`, NO `KillBash` tool. Codex backgrounds processes via shell `&` / `nohup` / `screen`. If Codex agent reads this atom, it tries to call non-existent tools. Also `claude_local.md` reference stale post-migration.

**Fix**:
- Replace hardcoded "2 minutes in Claude Code" with "per-call tool timeout fires (typically 60-120s, agent-specific)"
- Generalize the pattern: "use your agent's background-task primitive". Add labeled examples:
  - Claude Code: `Bash run_in_background=true` + `BashOutput` + `KillBash`
  - Codex CLI: shell `&` + `wait` / `nohup` / `screen`
- Replace `claude_local.md` reference with `AGENTS.md (local section)`

### 6.3 `internal/content/atoms/develop-dynamic-runtime-start-local.md`

**What it says today** (line 18 + lines 22-46):
> **Claude Code: `Bash run_in_background=true`.**
>
> **Start:**
> ```
> Bash run_in_background=true  command="{start-command}"
> ```
> **Logs:**
> ```
> BashOutput bash_id={task-id}
> ```
> **Stop:**
> ```
> KillBash shell_id={task-id}
> ```

**Why genericize**: same as 6.2 — entire atom is Claude-Code-tool-API specific. Codex doesn't have these tools.

**Fix**: restructure as agent-neutral guidance with per-agent example block:
- Agent-neutral body: "Start dev server via your agent's background-task primitive; check via `curl`; manage via your agent's task-listing/stop calls"
- Per-agent example section (small block at bottom):
  - Claude Code: `Bash run_in_background=true` + `BashOutput` + `KillBash`
  - Codex CLI: `bash -c "cmd &"` + `ps`/`pgrep` + `kill <pid>`

### 6.4 `internal/content/atoms/bootstrap-recipe-local-clone.md`

**What it says today** (lines 19, 33):
> The CWD typically has ZCP state already (`.claude`, `.mcp.json`, `.zcp`, CLAUDE.md). Anything OUTSIDE that set is the user's work — stop and ask before continuing if you see it.
>
> If the recipe ships its own CLAUDE.md / README.md and you want it to win…

**Why genericize**: file list incomplete for multi-agent world. Post-migration, AGENTS.md appears. Future Codex/Cursor/Gemini may add `.codex/`, `.cursor/` directories. The "is this user's work?" heuristic needs updating.

**Fix**: expand file list:
- `.claude`, `.codex` (now), future `.cursor`, `.gemini` directories
- `.mcp.json`, `.zcp/`
- **CLAUDE.md AND AGENTS.md** (both ZCP-managed context files)
- Generic phrasing: "ZCP-managed files include: AGENTS.md (canonical context), CLAUDE.md (Claude wrapper, when Claude adapter ran), .mcp.json, .zcp/, plus per-agent config directories (.claude/, .codex/, …)"

### 6.5 Why this matters specifically for Codex

If atoms 6.2 + 6.3 ship to a Codex agent unchanged, Codex tries to call `Bash run_in_background=true` (parameter doesn't exist in Codex's shell tool) → tool call fails → agent confused. Atoms must work for ALL agents that could read them, not just Claude.

Atom 6.1 + 6.4 are about file references becoming stale post-migration — agent-neutral, just need updating.

---

## 7. Phasing — 3 verifiable milestones

### P0a — Foundation (2-3 days)

- Canonical MCP server name `zerops` everywhere
  - Fix this dev repo's `.mcp.json` (`"zcp"` → `"zerops"`)
  - Add `TestMCPServerNameCanonical` (scans config writers + corpus for consistency)
- `Adapter` interface in `internal/init/adapters/adapter.go` (`Name`, `Detect`, `Validate`, `ContainerInit`)
- Extract Claude logic from `init_container.go` + `init.go` into `internal/init/adapters/claude.go`
- `internal/init/adapters/merge.go` — JSON + TOML structured merge helpers
- Refactor Claude container init to merge-aware (preserves user-added fields — POSITIVE behavior delta)
- Init dispatcher: iterate registered adapters, run Detect → Validate → ContainerInit
- Verification: ALL existing pinning tests stay green; `make lint-local && go test ./... -short && go test ./... -race`

**Tests added**: `TestMCPServerNameCanonical`, `TestClaude_MergePreservesUserMCPServers`, `TestClaude_MergePreservesUnknownTopLevelFields`, `TestAdapter_Interface_AllMethodsCovered`

### P0b — Context migration (3 days)

- Rename `internal/content/build_claude.go` → `build_agents.go` (compat alias kept); templates `claude_*.md` → `agents_*.md`
- Core writes AGENTS.md (env-specific: container OR local, NOT consolidated — preserves env-separation invariant)
- Claude adapter writes CLAUDE.md as thin `@AGENTS.md` wrapper + Claude-specific deltas section
- REFLOG writer point: `internal/workflow/reflog.go::AppendReflogEntry` writes to AGENTS.md path (not CLAUDE.md)
- `bootstrap_outputs.go:83` updates `claudeMDPath` → `agentsMDPath` parameter
- One-time migration logic: detect old CLAUDE.md REFLOG + no AGENTS.md → extract + write AGENTS.md + remove REFLOG from CLAUDE.md; idempotent on second run
- `internal/server/server.go::RefreshClaudeMD` → `RefreshAgentContext`; refreshes AGENTS.md + per-adapter wrappers on serve startup
- `internal/init/headless_warn.go:10` warning text references AGENTS.md
- `docs/spec-workflows.md:508` ("Append reflog to CLAUDE.md") updated
- `internal/eval/cleanup.go:181` comment updated; eval cleanup logic adjusted to remove AGENTS.md (not CLAUDE.md) for cross-scenario isolation
- Genericize 4 atoms per §6
- Flip CLAUDE.local.md:15 "Pre-production" → published-product policy

**Tests added**: `TestUpgrade_FirstRun_SplitsCLAUDEmdToAGENTSmd`, `TestUpgrade_Idempotent_NoOpOnSecondRun`, `TestUpgrade_PreservesUserContentOutsideMarkers`, `TestReflog_MigratesExistingClaudeMDOnFirstRun`, `TestReflog_AppendedToAGENTSmdAfterUpgrade`, `TestReflog_PreservesContentOnIdempotentRerun`, `TestAGENTSmd_NoContainerLocalLeak`

### P1 — Codex adapter (4-5 days)

- Implement `internal/init/adapters/codex.go`:
  - `Detect()`: `which codex`
  - `Validate(env)`: parse `codex --version`; warn if < v0.131.0 (hooks support); no hard error
  - `ContainerInit(env)`: TOML structured merge into `~/.codex/config.toml`:
    - `[mcp_servers.zerops]` stanza with `command="zcp"`, `args=["serve"]`, timeouts, env passthrough
    - `[projects."<container-workspace-path>"] trust_level="trusted"`
  - Honor `ZCP_AUTH_TYPE` (oauth | api-key) and `ZCP_PROVIDER` (openai default)
  - If `ZCP_AUTH_TYPE=api-key` + `OPENAI_API_KEY` set: no extra config (Codex reads env)
- Register Codex adapter in init dispatcher (after Claude in iteration order)
- AGENTS.md size test (≤32 KiB Codex `project_doc_max_bytes` cap)
- E2E test in eval-zcp dev container:
  - Container has codex binary (per Zerops template assumption)
  - Run `zcp init`
  - Verify `~/.codex/config.toml` contains expected stanzas (merge-aware: user's other servers preserved)
  - Spawn Codex session, verify `zerops_discover` tool call works through Codex

**Tests added**: `TestCodex_Detect_BinaryPresent`, `TestCodex_Detect_BinaryMissing`, `TestCodex_Validate_VersionTooOld`, `TestCodex_ContainerInit_FreshConfig`, `TestCodex_ContainerInit_MergesIntoExistingConfig`, `TestCodex_ContainerInit_PreservesUserMCPServers`, `TestCodex_ContainerInit_Idempotent`, `TestCodex_AGENTSmdUnderSizeCap`

**Total: ~9-11 working days (~2.5-3 weeks elapsed).**

P0a → P0b → P1 strictly sequential (P0a unblocks P0b unblocks P1). P0a and P0b can land as separate PRs that are independently verifiable.

---

## 8. Backward-compat pinning test inventory

Tests that LOCK existing behavior (must stay green throughout refactor):

- `TestContainerSteps_ClaudeConfigs_ProjectEntry` — `~/.claude.json` field shape
- `TestContainerSteps_ClaudeConfigs_*ApiKey*` — customApiKeyResponses format
- `TestRun_GeneratesSettingsLocal` — `.claude/settings.local.json` `mcp__zerops__*` permission
- `TestRun_GeneratesMCPConfig` — local `.mcp.json` server key
- `TestCLAUDEMD_PreservesReflog` — re-init preserves REFLOG (will be replaced by migration tests in P0b)
- `TestContainerSteps_NoGitSetup` — container init doesn't touch git
- `TestNoStdoutOutsideJSONPath` — MCP STDIO protocol invariant
- All work-session tests in `internal/workflow/work_session_test.go`

Tests added by this plan (cover new behavior + migrations) listed per-phase in §7.

---

## 9. Open questions / operator decisions

1. **`ZCP_AGENT_TYPE` / `ZCP_AUTH_TYPE` / `ZCP_PROVIDER` consumption depth** — minimum is "Codex adapter consumes them like Claude consumes `ANTHROPIC_API_KEY`". Optional extension: VS Code extension installer branches on `ZCP_AGENT_TYPE` (install Claude ext for `claude-code`, Codex ext for `codex`). Defer to operator preference; default = minimum.

2. **Atom genericization strategy** — two approaches in §6:
   - (a) Single atom with labeled per-agent example blocks (recommended)
   - (b) Per-agent atom variants (`-claude`, `-codex` suffixes)
   - (a) keeps atom count low; (b) keeps each atom focused. For now (a) since Codex is the only new agent; revisit if 3+ agents.

3. **Codex adapter ordering in init dispatcher** — Claude before or after Codex? Both run independently; order irrelevant for content (no overlap in write paths). Recommend Claude first for backward-compat ergonomics (existing behavior runs first in logs).

4. **`AGENTS.md` ZCP-managed marker convention** — proposed `<!-- ZCP-MANAGED:start --><!-- ZCP-MANAGED:end -->`. Matches Codex review suggestion. Verify edge cases (empty file, marker missing, malformed marker pair) in P0b migration tests.

5. **Zerops platform team coordination** — pre-installing `codex` binary in dev container template is platform team's work, not ZCP code. ZCP binary is ready once P1 ships; usable only when container template has `codex`.

---

## Appendix A — Research source citations

### Codex CLI
- [MCP docs](https://developers.openai.com/codex/mcp)
- [Config Reference](https://developers.openai.com/codex/config-reference)
- [AGENTS.md guide](https://developers.openai.com/codex/guides/agents-md)
- [Hooks](https://developers.openai.com/codex/hooks)
- [Authentication](https://developers.openai.com/codex/auth)
- [openai/codex GitHub](https://github.com/openai/codex)
- Active issues: [#7827](https://github.com/openai/codex/issues/7827), [#20925](https://github.com/openai/codex/issues/20925)

### ZCP coupling map
- Internal Explore agent scan 2026-05-22: `internal/init/`, `internal/content/`, `internal/workflow/`, `internal/server/`, `internal/eval/`. Specific files/lines referenced inline above.

### Codex second-opinion review (2026-05-22)
- Output preserved at `/tmp/codex-out-1779424218-79130-14086.md`
- Three load-bearing critiques folded in:
  1. AGENTS.md must be env-specific, not consolidated (§3.5)
  2. Structured merge mandatory, not overwrite (§4.2)
  3. Adapter `Detect` + `Validate` separation (§3.2)

---

## Appendix B — Deferred adapters (research preserved for future plans)

Full research available for when these are scheduled:

### Cursor IDE (deferred)
- **MCP support**: `.cursor/mcp.json` (project) + `~/.cursor/mcp.json` (user); STDIO + SSE + Streamable HTTP
- **Hard blocker**: 40-tool MCP cap across ALL servers — requires `ZCP_TOOLSET=core|admin|recipe|all` server-side filtering before Cursor adapter ships
- **Context file**: `.cursor/rules/*.mdc` + AGENTS.md (cross-tool); does NOT read CLAUDE.md
- **Container model**: Cursor desktop on Mac + Remote-SSH; MCP spawns on remote host (confirmed by Cursor staff); ZCP writes only project-level configs
- **Work Session**: requires `SessionOwner` abstraction (workspace-scoped, not PID-scoped) — IDE has no PID; promised composer-id keying NOT viable since MCP tool calls don't carry conversation_id
- **Estimated effort**: 1.5 weeks once toolset filtering exists

### Gemini CLI (deferred)
- **MCP support**: `~/.gemini/settings.json mcpServers` (configurable timeout); STDIO + SSE + HTTP
- **Context file**: configurable via `context.fileName: ["AGENTS.md", "GEMINI.md"]`
- **Pre-trust**: `~/.gemini/trustedFolders.json` (undocumented, empirical verification needed)
- **Hooks**: 10 events with regex matcher (richer than Claude Code)
- **Extensions**: `gemini-extension.json` manifest bundles MCP+commands+hooks atomically — killer install path
- **Known bug**: MCP timeout (#7324) — mitigate via explicit `timeout: 600000`
- **Estimated effort**: 1 week

### Antigravity CLI (deferred)
- **Migration timeline**: Gemini CLI consumer-tier EOL was 2026-06-18; Antigravity is replacement for non-enterprise
- **Different config**: `~/.gemini/antigravity/mcp_config.json` uses `serverUrl` instead of `url`; 100-tool cap per server
- **Public docs thin** — empirical reverse-engineering for trust/context/slash command behavior
- **Estimated effort**: 1 week, AFTER Gemini adapter verified

### Common deferred infrastructure
- `ZCP_TOOLSET=core|admin|recipe|all` server-side tool filtering (only matters when Cursor's 40-tool cap bites)
- `SessionOwner` abstraction `{kind: pid|workspace|external}` (only matters for Cursor's IDE-embedded model)
- Per-adapter `Capabilities()` interface method (only matters when ZCP guidance needs to vary per agent feature support)

When these adapters are scheduled, this plan's foundation (Adapter interface, merge.go, AGENTS.md canonical, env-driven config) makes them additive — no further refactor of core init/state code needed.
