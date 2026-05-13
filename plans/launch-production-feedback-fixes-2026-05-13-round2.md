# Launch-Production Feedback Fixes — Round 2

**Date:** 2026-05-13
**Continues:** `plans/launch-production-feedback-fixes-2026-05-13.md` (round 1, F1-F4 shipped as v9.88.4..v9.89.2; plan-doc stamped in v9.90.0).
**Trigger:** Real-world test on `laravel-showcase-agent` post-F1-F4 (session `3238877f-…`) + `flow-eval launch-production-from-standard-pair` 2026-05-13. Two new findings — one critical (discoverability), one high (mutation error surface).

---

## 1. Findings

### F6 — `launch-production` is invisible to agents in idle phase 🔴 CRITICAL

**Symptom:** flow-eval ran scenario `launch-production-from-standard-pair` with prompt explicitly mentioning "I want to launch production now. Create a separate Zerops project ... one-shot Zerops API key". Agent **never called `zerops_workflow`** — went straight to `zerops_discover` → `zerops_knowledge` → hand-rolled `import.yaml` + `zcli project create`.

Self-review explicit cite:

> "On the CLAUDE.md routing table — it nudged me wrong. The routing table says 'No service yet, or infra/topology change → `zerops_workflow action=start workflow=bootstrap`'. That's true for infra changes within the bound project. But this task was creating a separate project entirely... The right surface for a new external project is `zcli` with a user-supplied API key. **The routing table doesn't carve that case out.**"

**Root cause (catch-22):**

| Surface | Mentions `launch-production`? | Agent reads in idle? |
|---|---|---|
| CLAUDE.md routing table (`claude_shared.md`) | ❌ no | ✅ always loaded |
| Atom corpus — 12 launch-* atoms | ✅ yes | ❌ no — all gated `phases: [launch-production-active]` |
| Atom corpus — non-launch atoms | ❌ no (verified via grep) | — |
| `zerops_workflow` tool description | ✅ trigger phrases EN+CZ | ✅ ish — long text, weaker signal than routing table |
| `zerops_workflow action="status"` "Next" hint | ❌ no | ✅ always |

Agent needs to call `workflow=launch-production` to load launch atoms, but no surface tells the agent to call it. The only path that worked in Karel's real session ("nasaď to na prod") was the explicit-trigger-phrase match in the tool description — strong enough only for verbatim prompts.

**Severity:** CRITICAL — F1-F4 ergonomics are invisible to agents that don't trip a trigger phrase. The workflow exists but is undiscoverable through the primary signals.

**Fix scope:** defense-in-depth across both idle surfaces:
1. `claude_shared.md` routing table row + smell + workflow-detail bullet
2. New atom `idle-launch-entry.md` (`phases: [idle]`, `idleScenarios: [bootstrapped]`, `envelopeDeployStates: [deployed]`) — fires only when project legitimately is a promotion candidate

---

### F5 — Mutation error surface collapses to "See metadata" 🟠 HIGH

**Symptom (Karel session 3238877f):** workflow successfully reached `ready-to-launch`; user pasted launchKey; mutation phase failed with:

```json
{"workflow":"launch-production","status":"failed","blockers":[
  {"id":"create-import-failed","severity":"block","category":"auth",
   "message":"CreateAndImportProject failed: See metadata"}]}
```

No `apiCode`, no `apiMeta`, no actionable suggestion. Hardcoded `auth` category. Agent misled to ask user for fresh tokens. User regenerated — same failure (because real cause was something else: project name collision, schema validation, account quota, etc. — never visible).

**Root cause:**

`internal/tools/workflow_launch_production.go` emits 3 mutation failure sites that all collapse `*platform.PlatformError` via `+ err.Error()`. That method (errors.go:123) returns only `pe.Message` — which for Zerops's `errorList` shape is the literal string `"See metadata"`. The structured detail (`pe.APICode`, `pe.APIMeta`, `pe.Suggestion` already expanded by `formatAPIMetaActionable`) is silently dropped.

Same defect propagates to `state.LastError` and audit-log `ErrorMessage` — even forensic recovery is empty.

| Site | line | Category | What collapses |
|---|---|---|---|
| `create-import-failed` | 351 | hardcoded `Auth` | full APIMeta + Suggestion + APICode |
| `orphan-project` | 430 | `Orphan` | per-service ImportError stays in state file (agent can't read) |
| `first-deploy-failed` | 436 | `Other` | pollErr collapsed |

Bonus: `BlockerCategoryAuth` is hardcoded regardless of upstream error — project-name collision, schema-validation, permission-denied all surface as `auth`. Misleads agent into token-regeneration loop.

**Severity:** HIGH — manifests every time mutation fails. Round 1's F1-F4 made the workflow more reachable but exposed this layer. Round 2's F6 fix will increase mutation attempts → urgency rises.

**Fix scope:**
- `launchFailedFromPlatformError(err, fallbackCategory) (*mcp.CallToolResult)` helper:
  - `errors.As(err, &pe)` → extract structure
  - Map `pe.Code` → `BlockerCategory` (auth-codes → Auth; ErrAPIError 4xx → Schema; ErrNetworkError → Other; etc.)
  - Surface `pe.Suggestion` (expanded), `pe.APICode`, `pe.APIMeta` on blocker
- Apply across 3 mutation sites + `state.LastError` + audit `ErrorMessage`
- Test reproduces Karel's failure shape with mock returning APIMeta-bearing error; asserts Suggestion + APIMeta surface in response; asserts category derived from code

---

## 2. Sequencing

F6 first (discoverability) — it unblocks the workflow being chosen at all. F5 second — it makes the chosen workflow recoverable on failure. Without F6, F5 stays a latent improvement (no agent reaches the mutation phase anyway).

| Phase | Fix | Ship |
|---|---|---|
| R2-A | F6a (routing table) + F6b (idle-launch-entry atom) | v9.90.1 patch |
| R2-B | F5 (mutation error surface helper + 3 sites) | v9.90.2 patch |

Re-run `flow-eval launch-production-from-standard-pair` after each ship for empirical regression check.

---

## 3. Architectural reasoning (per CLAUDE.local.md "Question the artefact")

### Why the catch-22 happened

The phase-gated atom architecture is correct for *content depth* — once a workflow is active, render only its atoms so the agent isn't drowning in unrelated guidance. But that design depends on the **entry point** being discoverable through OTHER surfaces:

- `idle-*-entry` atoms exist exactly for this — they fire in `phases: [idle]` to bootstrap the agent's choice between develop, adopt, bootstrap routes.
- launch-production never got its idle-* entry. Part 1 (production-lifecycle-2026-05-11.md) shipped the active-phase atoms; Part 2 (production-lifecycle-part2-2026-05-12.md) shipped pipeline atoms. Neither pass added the discovery surface.

Routing table in `claude_shared.md` is the secondary entry point — read by every agent on every turn. Missing row is equally a Part-1/Part-2 omission.

Both are the same root: **launch-production was added to the engine + active phase, but not to the discovery layer.** Fix is parallel addition to both surfaces, not a hack at either.

### Why both fixes, not just one

- Routing table alone: works for agents that consult it (most do, per claude-shared discipline). Fails for agents that bypass it via deep tool-description analysis OR for cases where routing-table inference is non-obvious.
- Atom alone: works for agents that call `zerops_workflow action="status"` before deciding (some do, some don't — depends on agent reasoning style and CLAUDE.md guidance about "discovery floor").
- Both: every plausible discovery path mentions launch-production.

The cost of both is small (one new atom + one row in shared template); the cost of one missing is the same as the cost of both missing (agent goes to zcli).

---

## 4. Testing methodology applied

Per round-1 plan §5 (testing methodology forward):

1. **Test surfaced finding** — flow-eval transcript showed agent's zero `zerops_workflow` calls; self-review explicit cite.
2. **Triaged root cause vs symptom** — eval-time looked one layer up. Symptom: agent used zcli. Symptom-layer fix: tell agent "use workflow=launch-production". Root: routing table + atom corpus discovery gap (catch-22).
3. **Severity classified** — F6 critical (CRITICAL > HIGH because it masks F1-F4 entirely); F5 high (manifests every mutation failure once F6 is fixed).
4. **Structural + small** — F6 is two-surface (template + atom) but both are localized 1-file changes. F5 is helper + 3 call sites.
5. **Each fix lands with a test** — atom contract test + scenarios pin + atom golden refresh (F6); mock-error reproduction test (F5).

---

## 5. Risks + non-goals

**Non-goal:** redesigning the atom phase system. The phase-gated load is correct; we add discovery atoms, not change the dispatch model.

**Non-goal:** removing tool-description trigger phrases. They keep working as a third backup (cz/en natural-language).

**Risk:** new `idle-launch-entry` atom firing in `idleScenarios: [bootstrapped]` will surface alongside `idle-develop-entry` (priority 1). Could be noise for users who just want to develop. Mitigation: gate on `envelopeDeployStates: [deployed]` so the atom fires only when the project HAS deployed code (= legitimate promotion candidate). Greenfield in-progress projects don't see it.

**Risk:** `claude_shared.md` row addition pushes some other content below the fold for context-limited agents. Mitigation: routing table is 5 → 6 rows; minimal addition.

---

## 6. Files affected

| Phase | Modified | New |
|---|---|---|
| R2-A (F6) | `internal/content/templates/claude_shared.md`, `internal/workflow/scenarios_test.go`, atom-corpus goldens (testdata) | `internal/content/atoms/idle-launch-entry.md` |
| R2-B (F5) | `internal/tools/workflow_launch_production.go` (3 emit sites + state.LastError + audit), tests | helper file (likely `internal/tools/launch_failure_response.go` or inline) |

---

**End of plan. Implementation starts: F6a → F6b → re-eval → F5 → re-eval → ship.**
