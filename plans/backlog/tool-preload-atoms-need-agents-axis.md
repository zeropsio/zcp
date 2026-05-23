# Tool-preload atoms dropped — needs `agents:` axis to restore

**Surfaced**: 2026-05-23, during multi-agent container support planning (`plans/multi-agent-container-support-2026-05-22.md`). Deep semantic atom audit identified `bootstrap-tool-preload`, `develop-tool-preload`, `idle-tool-preload` as Claude-Code-only — they instruct the agent to use `ToolSearch query="select:mcp__zerops__..."` which is Claude Code's deferred-tool loader. Codex CLI (the first new agent being added) loads all MCP tools eagerly via `~/.codex/config.toml` and has no `ToolSearch` equivalent. The atoms would mislead non-Claude agents into calling a non-existent tool.

**Action taken**: 3 atoms deleted in the same commit; `scenarios_test.go` pinning assertions removed; goldens regenerated via `ZCP_UPDATE_ATOM_GOLDENS=1`. Claude users lose ToolSearch prefetch optimization in the meantime — Claude loads all ZCP tool schemas eagerly at startup (the default MCP behavior, ~80% more schema tokens upfront, but no correctness impact).

**Why deferred** (not solved structurally now): the proper fix needs an `agents:` axis on atom frontmatter so per-agent prose can coexist in the corpus. That's a non-trivial infra change cutting across:
- atom frontmatter schema (`agents: [claude-code]`, `agents: [codex]`, `agents: [claude-code, codex]`)
- `internal/content/atoms_lint.go` axis validation rules
- atom filter logic in `internal/workflow/atom_*.go` (Synthesize / Plan)
- MCP server client-type detection (parse `initialize` handshake's `clientInfo.name` — Claude Code identifies as `"claude-ai"` per the MCP TS client; Codex identifies as `"codex-cli"` or similar — verify per agent)
- ZCP startup wiring to plumb client identity into the corpus filter

For the Codex MVP scope, dropping these 3 atoms entirely is the smaller move — the Claude users' UX regression is bounded (eager tool load is the universal MCP default; ToolSearch was a Claude-specific optimization).

**Trigger to promote**:
1. Second agent (Cursor / Gemini / Antigravity) ships and we have at least one more case where per-agent prose matters — at 2 instances the `agents:` axis pays for itself, at 1 it's overkill
2. Claude users surface measurable regression from eager tool loading (token budget pressure, slow first turn) — surface in flow-eval-local Phase 5 runs
3. Atom corpus grows another category of agent-specific guidance (e.g., per-agent bootstrap differences) needing the same axis

**Sketch** (when promoted):
- Add `agents: [<agent-id>...]` optional frontmatter field; absent = applies to all agents
- Atom filter: agent-id pulled from `Env.Agent` (new field on the synthesis envelope), populated from MCP `initialize` clientInfo
- Lint additions: `axis-agents-id-allowlist` (only known agent IDs), `axis-agents-id-required-with-claude-tool-api` (atoms mentioning `Bash run_in_background=true`/`BashOutput`/`KillBash` MUST tag `agents: [claude-code]`)
- Restore the 3 dropped atoms with `agents: [claude-code]` tag; content unchanged
- Optional follow-up: Codex-specific equivalents if there's a meaningful Codex-side preload pattern (currently none — Codex tool loading is config-driven, not runtime-discovered)

**Risks**:
- Client identity detection could be unreliable across agent versions — `clientInfo.name` is in the MCP spec but agents may not all populate it consistently. Fallback: env var override `ZCP_AGENT_HINT=claude-code|codex|...`
- `agents:` axis could become a magnet for premature per-agent forking — many "Claude-specific" patterns are actually agent-API-shape differences solvable by neutralization (see the 4-atom genericization work in `plans/multi-agent-container-support-2026-05-22.md` §6). Use sparingly; prefer agent-neutral atoms when the underlying behavior is universal.

**Refs**:
- `plans/multi-agent-container-support-2026-05-22.md` §6 (atom genericization)
- Audit findings 2026-05-23: 3 tool-preload atoms among 7 multi-agent-blocking content fixes
- MCP spec `initialize` clientInfo: <https://spec.modelcontextprotocol.io/specification/server/lifecycle/#initialization>
