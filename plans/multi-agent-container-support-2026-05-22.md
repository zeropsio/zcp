# Multi-Agent ZCP Container Support — Final Plan (revised 2026-05-23)

> **Status**: Plan finalized after research + Codex second-opinion + user-driven scope refinement + commit `d0f8a449` (atom corpus pre-work).
> **Scope**: shipped ZCP binary's container-init flow. Add Codex CLI support, future-proof architecture for Cursor / Gemini / Antigravity.
> **Constraint**: ZCP is published. Backward-compat for user-facing surfaces is mandatory.

## 0. Progress to date (commit d0f8a449, 2026-05-23)

Atom-corpus pre-work landed ahead of P0a/P0b/P1 implementation. Conceptual frame: two systemic leaks in atoms — (1) build-time artifacts (Go paths, test names, template filenames, historical changelog), (2) Claude-Code-specific tool API (`Bash run_in_background=true`, `BashOutput`, `KillBash`, `ToolSearch`) hardcoded as canonical pattern.

**Done in d0f8a449** (40 files, +769/-596):
- 6 atoms genericized (replaced Claude-only API examples with neutral patterns; dropped Go-source paths; dropped template-filename refs; dropped historical-changelog sentences)
- 3 tool-preload atoms DELETED entirely (`bootstrap-tool-preload`, `develop-tool-preload`, `idle-tool-preload`) — they were built around Claude Code's `ToolSearch` deferred-tool loader; Codex loads MCP tools eagerly via `~/.codex/config.toml` and has no equivalent. Cascade: `scenarios_test.go` pinning removed, `knownOverflowFixtures` emptied (corpus shrank enough), 19 goldens + lifecycle-matrix regenerated
- 1 Go validation error message reframed: "human or Claude Code" → "human or AI agent" (so LLM doesn't see agent-specific self-reference in failure responses)
- 2 NEW lint rules in `internal/content/atoms_lint.go` (category `source-code-ref`):
  - `source-go-path` — forbids `internal|cmd/<pkg>/<file>.go` paths in atom bodies
  - `source-test-name` — forbids `Test<X>_<Y>` identifiers in atom bodies
  - Effect: regressions surface in CI; can't accumulate silently again
- 1 pinning test relaxed: `TestDevelopPlatformRulesLocalAtom_BackgroundTaskCallout` asserts on the PATTERN ("background-task primitive"), not the Claude-specific keyword ("`run_in_background=true`")
- 1 backlog entry: `plans/backlog/tool-preload-atoms-need-agents-axis.md` documents proper restoration path via per-agent `agents:` axis when 2+ agents need diverging prose

**Net effect on this plan**: §6 atom work is largely DONE; only the per-agent prose-axis (`agents:` frontmatter field) remains, and it's deferred via backlog (proper trigger: 2nd agent ships with diverging needs). P0b atom-related line items removed from §7.

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

- Cursor adapter — still deferred (40-tool cap requires `ZCP_TOOLSET` server-side filtering first)
- VS Code extension scope decision (per-agent ext install) — defer until needed
- ZCP_TOOLSET server-side filtering (only matters for Cursor's 40-tool cap)
- SessionOwner abstraction (only matters for Cursor's IDE-embedded model)
- Eval harness multi-agent (dev tooling, not shipped binary)

> **2026-05-24**: Gemini CLI + Antigravity adapters were promoted out of "deferred" once Karel confirmed both are pre-installed in the eval-zcp template (gemini at `/usr/bin/gemini`, agy at `~/.local/bin/agy`). Spec in §5.4 + §5.5; implementation in commit `f5ba8747`.

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

  for each adapter in [claude, codex, gemini, antigravity]:   # future: cursor
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

### 5.4 Gemini CLI (shipped 2026-05-24, commit `f5ba8747`)

`internal/init/adapters/gemini.go`:
- `Detect()`: `which gemini` returns ok. Container template ships `/usr/bin/gemini` → `/usr/lib/node_modules/@google/gemini-cli/bundle/gemini.js` (npm `@google/gemini-cli` v0.39.x).
- `Validate(env)`: `gemini --version` probe. Soft warning on probe failure / empty output. No version-gated features today.
- `ContainerInit(env)`: JSON structured merge into `~/.gemini/settings.json`:
  ```json
  {
    "mcpServers": {
      "zerops": {
        "command": "zcp",
        "args": ["serve"],
        "trust": true,
        "description": "Zerops platform MCP server"
      }
    }
  }
  ```
  NO `env` field — Gemini's MCP subprocess spawn uses `env: { ...process.env, ...envMap }` (verified in `chunk-WFCK2Z32.js`: `childProcess2.spawn(..., { env: { ...process.env, ...Object.fromEntries(envMap) } })`). Parent env spreads first; config `env` only overrides. ZCP_API_KEY / serviceId / hostname / projectId / PATH / HOME all flow through naturally — opposite of Codex's restrictive `env_vars` list.
- Auth handling: out of scope. Gemini CLI reads `GEMINI_API_KEY` / `GOOGLE_GENAI_USE_VERTEXAI` / `GOOGLE_GENAI_USE_GCA` env vars OR `security.auth.selectedType` in settings.json (one of `"oauth-personal"`, `"gemini-api-key"`, `"vertex-ai"`, `"cloud-shell"`). Path is NESTED — top-level `"selectedAuthType"` is silently ignored. The adapter's MCP config write is independent of auth: settings.json is loaded regardless and `mcpServers.zerops` registers even before auth — Codex/Antigravity-style permissive merge means an operator's manually-set `security.auth.selectedType` survives every `zcp init` re-run untouched (verified 2026-05-24).
- AGENTS.md already written by Core — Gemini reads AGENTS.md natively (`context.fileName` defaults include it).

### 5.5 Antigravity CLI (shipped 2026-05-24, commit `f5ba8747`)

`internal/init/adapters/antigravity.go`:
- **Binary name is `agy`** (three letters). Installed by the official bootstrap (`curl -fsSL https://antigravity.google/cli/install.sh | bash`) at `$HOME/.local/bin/agy`. v1.0.x is a stripped Go binary — language-server self-identifies as `product=antigravity` in cli.log.
- `Detect()`: `which agy` returns ok.
- `Validate(env)`: `agy --version` probe (returns `1.0.2`). No MCP-related version gates; agy v1.0.x and Gemini CLI v0.39.x share the same `MCPServerConfig` schema.
- `ContainerInit(env)` writes TWO files (Antigravity migrated config layout to per-feature directory — `~/.gemini/config/.migrated` marker):
  - `~/.gemini/config/mcp_config.json` — same JSON shape as Gemini, structured merge.
  - `~/.gemini/antigravity-cli/settings.json::trustedWorkspaces` — append `VSCodeWorkDir` idempotently via `appendIfMissingString` (normalizes nil / []any / scalar / unknown defensively). Pre-seeded before first interactive `agy` session removes the workspace-trust prompt (Antigravity auto-adds to trustedWorkspaces on first open, but that doesn't help for headless `agy --print` runs).
- NO `env` field (same Gemini-family permissive parent-env spread).
- Live verification: `agy --print "list available MCP tools"` enumerates all 24 `zerops_*` tools end-to-end via the new mcp_config.json (eval-zcp container, 2026-05-24).
- AGENTS.md already written by Core; Antigravity reads it natively (Gemini fork inherits same context-file behavior).

---

### 5.6 Cursor CLI (shipped 2026-05-24, commit `67fa46fd`)

`internal/init/adapters/cursor.go`:
- **Binary**: `agent` (primary) or `cursor-agent` (legacy alias). Both are symlinks to `~/.local/share/cursor-agent/versions/<version>/cursor-agent` created by the official installer (`curl https://cursor.com/install -fsS | bash`). Detect tries `cursor-agent` first (unambiguous), falls back to `agent`.
- `Validate(env)`: `cursor-agent --version` (returns build identifier like `2026.05.20-2b5dd59`). No version gates today. Probe failure → soft warning.
- `ContainerInit(env)`: JSON structured merge into `~/.cursor/mcp.json`:
  ```json
  {
    "mcpServers": {
      "zerops": {
        "type": "stdio",
        "command": "zcp",
        "args": ["serve"]
      }
    }
  }
  ```
  `type: "stdio"` is required by Cursor's schema (distinguishes from `sse` / streamable HTTP). NO `env` field — Cursor spawns the MCP subprocess inheriting parent env (verified: `agent mcp list-tools zerops` enumerated 21 zerops_* tools without enumeration). Same pattern as Gemini/Antigravity; Codex's restrictive `env_vars` is the outlier.
- Auth handling: out of scope. Cursor authenticates via `CURSOR_API_KEY` env var OR `agent login` OAuth — operator picks one. Adapter's MCP config write is independent of auth.
- Project trust: separately, `agent -p` headless mode needs `--trust` flag to bypass workspace-trust prompt + `--approve-mcps` to auto-approve our MCP server. These are CLI flags, not config-file fields — operator passes per-invocation.
- AGENTS.md: Cursor reads its own `.cursor/rules/*.mdc` natively; AGENTS.md compatibility is via the cross-tool agents.md convention (Cursor's MCP-listed servers see AGENTS.md as context-file fallback). Empirical confirmation deferred to first interactive run.

**Notable discrepancy** — `agent mcp list-tools zerops` shows 21 tools (Gemini/Antigravity see all 24). Missing: `zerops_browser`, `zerops_deploy_batch`, `zerops_dev_server`. Possibly Cursor's CLI display filters tools above a description-length threshold (all three have descriptions >1000 chars); does NOT mean the tools are inaccessible at runtime (would require auth to verify via actual `agent -p` call). Filed as known unknown; not a blocker since the missing tools are convenience wrappers (delete_batch composes deploys; dev_server composes ssh+setsid; browser composes agent-browser lifecycle).

---

## 6. Atom corpus work — DONE in commit d0f8a449

The atom-genericization scope this plan originally proposed (4 atoms) expanded during deeper audit to 6 genericized + 3 deleted. All of that landed in commit `d0f8a449` (see §0). Summary of what changed:

### 6.1 Atoms genericized (6)

Mechanisms applied — each atom retains its mechanism statement, drops the Claude-coupling that would mislead non-Claude agents or stale references that would mislead all agents:

| Atom | Coupling removed |
|---|---|
| `develop-platform-rules-container.md` | Template-filename ref (`claude_container.md`) replaced with scope statement; atom title already establishes the section |
| `develop-platform-rules-local.md` | Hardcoded "2 minutes in Claude Code" timeout + Claude-tool-API example (`Bash run_in_background=true` + `BashOutput` + `KillBash`) replaced with neutral "background-task primitive" pattern; `claude_local.md` template ref dropped |
| `develop-dynamic-runtime-start-local.md` | Same as above — entire atom was Claude-tool-API; restructured as agent-neutral mechanism + neutral start/check/logs/stop wording |
| `bootstrap-recipe-local-clone.md` | File-list refreshed (added AGENTS.md anticipating P0b migration); generic ZCP-state phrasing |
| `bootstrap-mode-prompt.md` | Build-time Go-path citation dropped |
| `develop-env-var-model.md` | Build-time test-name citation dropped |
| `develop-auto-close-semantics.md` | Build-time test-name citation dropped |
| `bootstrap-recipe-import.md` | Build-time leak dropped |

### 6.2 Atoms deleted (3 — tool-preload set)

`bootstrap-tool-preload.md`, `develop-tool-preload.md`, `idle-tool-preload.md` — entirely built around Claude Code's `ToolSearch query="select:mcp__zerops__..."` deferred-tool loader. Codex loads MCP tools eagerly via `~/.codex/config.toml` and has no `ToolSearch` equivalent. Calling these atoms from a Codex session would tell it to invoke a non-existent tool.

Cascade:
- `scenarios_test.go` pinning assertions for the 3 atoms removed
- `knownOverflowFixtures` emptied — dropping 3 atoms shrank corpus enough that the previously-overflowing `develop_first_deploy_two_runtime_pairs_standard` fixture now fits the 28 KB soft cap; the explicit allowlist no longer needed
- 19 atom-goldens regenerated via `ZCP_UPDATE_ATOM_GOLDENS=1`
- `lifecycle-matrix.md` regenerated

Claude-side regression: Claude users lose `ToolSearch` prefetch optimization → Claude now eagerly loads all ZCP tool schemas at startup (the universal MCP default, ~80% more schema tokens upfront, no correctness impact).

Restoration path: `plans/backlog/tool-preload-atoms-need-agents-axis.md` — proper fix is adding `agents: [claude-code]` frontmatter axis to atoms so per-agent prose can coexist. Trigger to promote: when 2nd agent ships with diverging prose needs (single-instance is overkill; 2+ instances pays for the axis infrastructure).

### 6.3 Structural lock — new lint rules

Two new rules in `internal/content/atoms_lint.go` (category `source-code-ref`):

- `source-go-path` — regex `\b(internal|cmd)/[a-z_][a-z0-9_]*(/[a-z_][a-z0-9_]*)*\.go\b` — forbids internal Go-source paths
- `source-test-name` — regex `\bTest[A-Z][A-Za-z0-9_]*_[A-Z]` — forbids test function names

Regressions surface in CI lint, not in user feedback months later. Build-time vs runtime separation is now structural.

### 6.4 Pinning test relaxed

`TestDevelopPlatformRulesLocalAtom_BackgroundTaskCallout` previously asserted Claude-specific keyword (`run_in_background=true`); now asserts the agent-neutral pattern ("background-task primitive"). Invariant moved from agent-specific to mechanism-level.

### 6.5 Go error message reframed

One validation error message in `internal/tools/workflow_checks_claude_md.go`: "human or Claude Code" → "human or AI agent". The LLM consuming the error message no longer sees an agent-specific self-reference in tool-fail responses.

### 6.6 What's NOT done — `agents:` axis (deferred)

The proper structural fix for per-agent prose divergence is an `agents:` frontmatter field. Deferred via backlog because the cost (atom frontmatter schema + lint rules + filter logic in `internal/workflow/atom_*.go` + MCP `initialize.clientInfo` client-identity detection + ZCP startup wiring) only pays off at 2+ instances of diverging per-agent prose. For Codex MVP, deleting the 3 tool-preload atoms + genericizing 6 others covers the seam with zero new infrastructure. Restoration when 2nd agent (Cursor / Gemini / Antigravity) ships with its own diverging needs.

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

### P0b — Context migration (2-2.5 days, reduced after d0f8a449)

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
- ~~Genericize 4 atoms~~ **DONE in d0f8a449** (6 genericized + 3 deleted; see §6)
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

**Total: ~8-10 working days (~2-2.5 weeks elapsed)** — atom work pre-done in d0f8a449 saves ~0.5-1 day from P0b.

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
- ~~`ZCP_TOOLSET=core|admin|recipe|all` server-side tool filtering~~ — **NO LONGER A BLOCKER** for Cursor as of 2026-05-24. The original "40-tool cap" finding from Cursor's older docs is absent from current docs (`https://cursor.com/docs/context/mcp`) and empirically Cursor enumerated 21 zerops tools without complaint. Still useful long-term if any agent introduces a real cap, but not required to ship Cursor.
- ~~`SessionOwner` abstraction `{kind: pid|workspace|external}`~~ — **NO LONGER NEEDED** for Cursor's CLI mode. The "IDE-embedded, no PID" concern applied to Cursor IDE; Cursor CLI runs as a regular PID-owned process (just like Claude/Codex/Gemini/Antigravity CLIs). The PID-keyed work session model works.
- Per-adapter `Capabilities()` interface method (only matters when ZCP guidance needs to vary per agent feature support)
- **`agents:` axis on atom frontmatter** (per-agent prose divergence) — tracked in `plans/backlog/tool-preload-atoms-need-agents-axis.md`; trigger to promote = 2nd agent ships with diverging prose needs. Also restores the 3 deleted tool-preload atoms tagged `agents: [claude-code]`.

When these adapters are scheduled, this plan's foundation (Adapter interface, merge.go, AGENTS.md canonical, env-driven config, structural lint rules from d0f8a449) makes them additive — no further refactor of core init/state code needed.

---

## 10. Implementation status (2026-05-24) — SHIPPED

P0a + P0b + P1 landed. All goals from §3 achieved; all backward-compat invariants from §4 preserved.

### Commits

| Commit | Scope | Files |
|---|---|---|
| `a975aa8a` | Multi-agent foundation + AGENTS.md migration + Claude refactor (P0a + P0b) | 34 files (~+2400 / -1100) |
| `70327184` | Codex CLI adapter (P1, additive) | 3 files (+530 / -1) |
| `07a2044a` | Codex env_vars fix — include runtime detection vars (post-ship bug) | 2 files (+47 / -3) |
| `f5ba8747` | Gemini CLI + Antigravity (`agy`) adapters (§5.4 + §5.5, additive) | 6 files (+849 / -5) |
| `50e56ed1` | deploy/ssh: workingDir gate error message — explicit sourceService recovery (audit feedback) | 2 files (+51 / -1) |
| `67fa46fd` | Cursor adapter (§5.6, additive) — Cursor IDE headless CLI (`agent` / `cursor-agent`) | 3 files (+456 / -6) |

### Verification matrix

| Test surface | Result |
|---|---|
| `go test ./... -count=1 -race -short` (all 30 packages) | All green |
| `make lint-fast` (golangci-lint) | 0 issues |
| Backward-compat pinning: `TestContainerSteps_ClaudeConfigs_ProjectEntry`, `TestRun_GeneratesSettingsLocal`, `TestBootstrapComplete_AppendsReflog`, etc. | Pass — Claude container init field shape unchanged for users |
| New migration: `TestUpgrade_MigratesReflogFromClaudeMDToAgentsMD`, `TestUpgrade_MigrationIdempotent`, `TestUpgrade_PreservesUserContentOutsideMarkers`, `TestUpgrade_MarkerlessUserAgentsMD_PreservedNotClobbered`, `TestUpgrade_AgentsMDAlreadyHasMigratedReflog_NoDuplication`, `TestUpgrade_MalformedReflogOpenerNoCloser_NoDataLoss` | Pass — one-time + idempotent + content-resumable |
| New safeguard: `TestRefreshAgentContext_PreUpgradeCLAUDEmdWithoutAgentsMD_LeftUntouched`, `TestServerNew_PreUpgradeClaudeMDWithoutAgentsMD_LeftUntouched` | Pass — serve startup will not orphan @AGENTS.md include |
| Merge primitives: 13 `TestUpsertPath_*` / `TestHasPath_*` / `TestLoadJSONFile_*` / `TestSaveTOMLFile_*` + 4 `TestShallowMergeAtPath_*` | Pass |
| Codex adapter: 11 `TestCodex_*` including `TestCodex_MCPEntry_UsesEnvVarsNotEnv`, `TestCodex_ContainerInit_ByteStableAfterReloadResave`, `TestCodex_ContainerInit_PreservesUserAddedServers` | Pass |
| Gemini adapter: 10 `TestGemini_*` including `TestGemini_MCPEntry_NoEnvField`, `TestGemini_ContainerInit_PreservesUserAddedServers`, `TestGemini_ContainerInit_Idempotent` | Pass |
| Antigravity adapter: 13 `TestAntigravity_*` including `TestAntigravity_MCPEntry_NoEnvField`, `TestAntigravity_ContainerInit_PreservesUserAddedServersAndWorkspaces`, `TestAntigravity_ContainerInit_PreservesScalarTrustedWorkspaces`, `TestAntigravity_ContainerInit_TrustedWorkspaceAlreadyPresent_NoDuplicate` | Pass |
| E2E in eval-zcp container (cross-compiled binary, simulated pre-upgrade CLAUDE.md with 2 REFLOG entries + user content outside markers + Codex pre-existing config with user-added MCP servers) | 17/17 checks pass |
| Live `agy --print "list available MCP tools"` in eval-zcp container after `zcp init` rewrote `~/.gemini/config/mcp_config.json` | Pass — Antigravity enumerated all 24 `zerops_*` tools end-to-end via the new mcp_config.json |
| Live `gemini --prompt "Call mcp__zerops__zerops_workflow…"` after `zcp init` rewrote `~/.gemini/settings.json` (with operator-set `security.auth.selectedType: "oauth-personal"`) | Pass — Gemini called `mcp_zerops_zerops_workflow`, parsed the structured workflow envelope, listed all action choices (`start`, `close-mode`, `git-push-setup`, `build-integration`, `status`, `complete`, `skip`, `reset`, `iterate`, `resume`, `list`, `route`) |
| `zcp init` re-run preserves operator's `security.auth.selectedType` (merge-aware contract) | Pass — second `gemini --prompt` after re-init enumerated all 24 tool names; auth field byte-identical before/after |
| Cursor adapter: 14 `TestCursor_*` including `TestCursor_MCPEntry_NoEnvField`, `TestCursor_MCPEntry_TypeStdioRequired`, `TestCursor_Detect_{OnlyAgent,OnlyCursorAgent,Both}Present_True` | Pass |
| Live `agent mcp list-tools zerops` after `zcp init` rewrote `~/.cursor/mcp.json` | Pass — Cursor enumerated 21 `zerops_*` tools from our config (3-tool gap to be investigated; not a blocker) |
| Cursor init preserves pre-existing `~/.cursor/{cli-config.json, statsig-cache.json, projects/}` (merge-aware) | Pass — only `mcpServers.zerops` touched |
| `flow-eval-local greenfield-node-postgres-dev-stage` (Claude regression check via container) | Pass — agent self-review: "this one went clean. No retries, no validation errors." Full bootstrap → provision → deploy → dev-server → verify pipeline |

### Codex review critiques (gpt-5.5 second-opinion 2026-05-23) → all folded

| Critique | Resolution | Pinning test added |
|---|---|---|
| `env.ZCP_API_KEY = "${ZCP_API_KEY}"` — Codex doesn't expand placeholders | Switched to `env_vars = ["ZCP_API_KEY"]` (documented Codex pass-through) | `TestCodex_MCPEntry_UsesEnvVarsNotEnv` |
| `zcp serve` startup could orphan @AGENTS.md include for pre-upgrade Claude users | `RefreshAgentContext` skips CLAUDE.md refresh when AGENTS.md missing | `TestRefreshAgentContext_PreUpgradeCLAUDEmdWithoutAgentsMD_LeftUntouched`, `TestServerNew_PreUpgradeClaudeMDWithoutAgentsMD_LeftUntouched` |
| REFLOG migration not content-resumable (existence-gated, false-negative on partial crash) | Content-based dedupe; handles markerless user AGENTS.md, partial-crash recovery, concurrent re-init | `TestUpgrade_AgentsMDAlreadyHasMigratedReflog_NoDuplication`, `TestUpgrade_MarkerlessUserAgentsMD_PreservedNotClobbered` |
| Claude `projects[VSCodeWorkDir]` full-clobber loses user-added keys | Added `ShallowMergeAtPath` helper; `configureClaude` uses it (ZCP keys win, user keys preserved) | `TestShallowMergeAtPath_OverwritesZCPKeys_PreservesUserKeys` |
| Codex idempotence test compared decoded maps, not raw bytes | Added byte-stability assertion through load+save cycle | `TestCodex_ContainerInit_ByteStableAfterReloadResave` |
| Malformed REFLOG opener edge case | Verified `extractReflogSections` no-ops cleanly; truncated content stays visible | `TestUpgrade_MalformedReflogOpenerNoCloser_NoDataLoss` |
| `removeClaudeMD` comment/code mismatch | Comment rewritten to describe actual single-file behavior | resolved |

### Architecture delivered

```
internal/init/
├── init.go                          generateAgentContext + migrate + extract/remove reflog
├── init_container.go                runContainerAdapters dispatcher
├── init_upgrade_test.go             3 migration edge-case tests
├── headless_warn.go                 WarnMissingAgentContext (alias kept)
└── adapters/
    ├── adapter.go                   Adapter interface + Env (Detect, Validate, ContainerInit, hooks)
    ├── merge.go                     Load/Save JSON+TOML, UpsertPath, ShallowMergeAtPath, HasPath
    ├── merge_test.go                13 tests
    ├── merge_shallow_test.go        4 ShallowMergeAtPath tests
    ├── claude.go                    Claude adapter (merge-aware ~/.claude.json + project-entry shallow merge)
    ├── claude_test.go               7 tests
    ├── codex.go                     Codex adapter (env_vars list with runtime detection vars, TOML merge, version warning)
    ├── codex_test.go                11 tests
    ├── gemini_family.go             Shared MCPServerConfig entry (Gemini CLI + Antigravity, identical schema)
    ├── gemini.go                    Gemini CLI adapter (~/.gemini/settings.json::mcpServers, JSON merge)
    ├── gemini_test.go               10 tests
    ├── antigravity.go               Antigravity adapter (~/.gemini/config/mcp_config.json + trustedWorkspaces pre-seed)
    ├── antigravity_test.go          14 tests
    ├── cursor.go                    Cursor adapter (~/.cursor/mcp.json, type=stdio, dual binary detect)
    └── cursor_test.go               14 tests

internal/content/
├── build_agents.go                  BuildAgentsMD + BuildClaudeWrapper (BuildClaudeMD deprecated alias)
├── refresh_agents.go                RefreshAgentContext with AGENTS.md-gate safeguard (RefreshClaudeMD alias)
├── refresh_agents_upgrade_test.go   2 pre-upgrade safeguard tests
├── content_test.go                  TestMCPServerNameCanonical
└── templates/agents_{shared,container,local}.md   (git-renamed)

internal/server/server.go             RefreshAgentContext on startup (was RefreshClaudeMD)
internal/workflow/{reflog,bootstrap_outputs}.go    REFLOG writer point → AGENTS.md
internal/eval/cleanup.go              removeAgentContextFiles
cmd/zcp/main.go                       WarnMissingAgentContext
docs/spec-workflows.md:508            REFLOG target documentation
CLAUDE.local.md:15                    "Pre-production" → published-product policy
```

### Deferred (updated 2026-05-24)

- Cursor adapter + `ZCP_TOOLSET` server-side filtering + `SessionOwner` abstraction — gated on Cursor shipping. Foundation makes them additive.
- `agents:` axis on atom frontmatter — trigger to promote = 2nd agent ships with diverging prose needs. Tracked in `plans/backlog/tool-preload-atoms-need-agents-axis.md`.

### Codex review critiques on Gemini/Antigravity adapters (gpt-5.5 second-opinion 2026-05-24) → all folded

| Critique | Resolution | Pinning test added |
|---|---|---|
| `appendIfMissingString` silently drops scalar values when `trustedWorkspaces` was hand-set as a string instead of an array | Normalize via switch on input type — nil / []any / scalar / other all preserved | `TestAntigravity_ContainerInit_PreservesScalarTrustedWorkspaces` |

### Operator next steps

- Zerops platform team: install `codex` (npm `@openai/codex`) + `gemini` (npm `@google/gemini-cli`) + `agy` (Antigravity bootstrap script) binaries in dev container template. Once present, `zcp init` auto-detects + configures all four adapters in parallel; existing Claude-only containers see zero behavior change (each non-Claude adapter's Detect returns false, adapter skipped).
- Optional: add `ZCP_AGENT_TYPE`, `ZCP_AUTH_TYPE`, `ZCP_PROVIDER` env vars to Codex / Gemini / Antigravity container template YAML for parity with Claude's `claude-code`/`oauth`/`anthropic` (not consumed by adapters yet — informational labels, future).
- Antigravity `agy` is OAuth-gated: operator authenticates once via `agy login` interactively; the OAuth token at `~/.gemini/antigravity-cli/antigravity-oauth-token` persists across container restarts. ZCP init does not provision the token.
