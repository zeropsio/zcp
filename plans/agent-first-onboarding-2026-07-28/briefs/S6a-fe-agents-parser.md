# Slice brief: S6a — FE: `ZCP_AGENTS` parser + shared fixture

Self-contained. Cite spec §s, never the plan.
Repo: /Users/macbook/Documents/Zerops-MCP/frontend-legacy, branch `feat/agent-first-onboarding`
(fe-base `0d6423924`). Contract home:
`/Users/macbook/Documents/Zerops-MCP/zcp/docs/spec-welcome-mode.md` — never copy contract
prose into this repo.

**FE-repo protocol (binding — `frontend-legacy/CLAUDE.md` "Agent Directives")**: ≤5 files;
owner-present (explicit approval closes the phase); verify with `npx tsc --noEmit` +
`npx eslint . --quiet` (project equivalents; if absent, say so) + the jest targets. Re-read
any file before editing.

**Outcome** (observable): the FE parses `ZCP_AGENTS` with semantics IDENTICAL to the
container's §3 rules, pinned by the FE half of the shared fixture.

**Allowed scope** — exactly 3 files:
- `apps/zerops/src/modules/core/zerops-services/zerops-services.utils.ts`
- `apps/zerops/src/modules/core/zerops-services/zerops-services.model.ts`
- NEW `apps/zerops/src/modules/core/zerops-services/zerops-services.utils.spec.ts`
Excluded: everything else; a 4th production file is a stop condition.

**Spec citations**: `spec-welcome-mode.md` §3 (parse rules), §8.1 (roster source + shared
fixture obligation).

**Facts**:
- Parse rules (§3, mirror exactly): trim + lowercase + drop unknown ids + dedupe
  (first-occurrence order preserved); absent key → every registry agent;
  present-but-unusable or resolves-to-nothing → ZERO agents, fail-closed (never "all").
- Read-path constraint (live-settled): the value comes via the UserDataEntity path
  (`extractServiceMetadataMulti`-style, utils.ts:206-239) — NEVER `stack.userData`
  (utils.ts:26-27: secret-typed vars may be absent there).
- Known-agent universe: `SUPPORTED_AGENT_TYPES` (model.ts:35-41) / `ZagentAgentType`.
- Commit-message context: the old `ZCP_AGENT_TYPES` CSV was deliberately deleted for
  deploy-time-snapshot drift (utils.ts:41-43, model.ts:134-138) — the `embed-ready` roster
  (later phases) is the FIX for that drift, not a reversal; say so.

**RED test list** (jest): fixture cases — missing key → full registry · empty/whitespace →
zero, fail-closed · unknown ids dropped · duplicates deduped first-occurrence · order
preserved · non-string/unusable value → zero. These exact cases mirror to the zcp side (the
shared-fixture contract, §8.1).

**Protocol**: RED (right-reason failure) → GREEN → REFACTOR → tsc + eslint → report → owner
approval closes the phase.

**BUILD addendum**: one named test at a time · independent oracle (spec literals) · assert
on public seams · lint/typecheck clean before done.

**Report contract**: RED + GREEN outputs with exit codes · files touched (must be exactly
these 3) · tsc/eslint pass lines · independent-oracle note.

**Stop conditions**: scope drift (any 4th production file) · a material unknown · an
acceptance-criteria change · a repeated unexplained check failure.

**Definition of Done**: RED replay valid · named specs pass · tsc + eslint clean · report
filled.
