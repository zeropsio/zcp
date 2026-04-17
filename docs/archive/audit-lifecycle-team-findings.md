# Lifecycle Audit — Team Findings

> Cross-cutting synthesis of a 5-agent investigation into the ZCP development
> lifecycle: recipe adoption → init → LLM work → deploy → loop closure.
> Produced 2026-04-17. Complements the `spec-*-current.md` series and
> `audit-session-design.md`.

---

## 0. Executive Summary

The develop workflow has been silently refactored from a **3-step session state
machine** (as described in `spec-develop-flow-current.md`, dated 2026-04-14)
to a **stateless briefing** (`handleDevelopBriefing` in
`internal/tools/workflow_develop.go:15`). This refactor resolved multiple
documented contradictions but introduced a new class of lifecycle-integrity
problems specific to the user's target use case (long LLM-driven work,
compaction survival, parallel Claude Code instances).

**The specs in `docs/spec-*.md` are now partially outdated and must be revised
before any further design work.**

The five lifecycle guarantees the user wants:

| Guarantee | Status |
|-----------|--------|
| G1. Recipe → adopt → develop flows cleanly into first code task | **Broken** — strategy prompt gap + contradictory signals |
| G2. Multiple parallel Claude Code instances don't corrupt each other | **Partially broken** — bootstrap/recipe collide, develop is safe by accident |
| G3. Lifecycle survives context compaction | **Broken for develop** — no state hint in system prompt |
| G4. Lifecycle survives Claude Code / zcp binary restart | **Works for bootstrap/recipe**, **broken for develop** (PID-keyed marker) |
| G5. LLM knows when to close and loop | **Weakened** — explicit "start new develop" nudge was removed; closure is now implicit |

---

## 1. Current Architecture (As of 2026-04-17)

### 1.1 Workflow Taxonomy

Three workflows, two lifecycle classes:

| Workflow | Class | State | Session file | Iteration limit |
|----------|-------|-------|--------------|-----------------|
| `bootstrap` | Stateful | `BootstrapState` (5 steps) | Yes | 10 (shared) |
| `recipe` | Stateful | `RecipeState` (4+ substeps) | Yes | 10 (shared) |
| `develop` | **Stateless** | None | **No** — marker file only | N/A |

The develop workflow returns a briefing on `action="start" workflow="develop"`.
No session is created. The briefing is strategy-aware and self-contained — the
LLM is expected to act on its guidance without further workflow calls.

**Evidence:** `internal/workflow/briefing.go:19-26` (DevelopBriefing),
`internal/tools/workflow.go:219-221` (develop dispatch → briefing),
`internal/workflow/state.go` (no DeployState alongside Bootstrap/Recipe).

### 1.2 What Replaces Session State for Develop

A **develop marker file** is written to `{stateDir}/develop/{pid}.json`:

- Keyed by PID (not session ID)
- Multiple processes can hold markers simultaneously
- Used only for tool gating via `HasDevelopMarker()`
  (`internal/tools/guard.go:21`) — prevents `zerops_mount`, `zerops_import`,
  etc. from firing outside an active development context
- **Never surfaced to the LLM** — no entry in `buildSessionHint`

Evidence: `internal/workflow/develop_marker.go:25-52`.

---

## 2. G1 — Recipe Adoption Bridge (Investigator #1)

### 2.1 First 5 minutes after user opens Claude Code

On cold start, inside ZCP service with recipe pre-imported:

1. LLM reads MCP base instructions (always in system prompt):
   > "Every code task = one develop workflow. Start before ANY code changes:
   >   zerops_workflow action=\"start\" workflow=\"develop\""
2. Router offers P1: either `bootstrap (adopt)` **or** `develop` (via
   `strategyOfferings`). The LLM has to pick one.
3. If LLM picks `develop`: `handleDevelopBriefing` detects no ServiceMetas,
   calls `adoptUnmanagedServices` transparently (bootstrap fast path runs
   internally). Metas are written with `DeployStrategy=""`.
4. Briefing is returned. It tells the LLM: "No deploy strategy set.
   Discuss with user: push-dev / push-git / manual."

### 2.2 The coherence gap

- Base instructions command "start develop" — strategy-blind
- Briefing says "you have no strategy, discuss with user first"
- LLM must pause the flow, call `action="strategy"`, then re-call develop

There is no way for the LLM to know at step 1 that strategy selection is the
first blocker. It only discovers this inside the briefing.

**Recommendation:** Merge strategy offering into the base instructions or into
the orientation section, so the LLM sees "before calling develop, ensure a
strategy exists" on the very first turn.

Source: `internal/server/instructions.go:17-23`, `internal/workflow/briefing.go:106-193`.

---

## 3. G2 — Parallel Claude Code Instances (Investigator #2)

### 3.1 Concurrency model

Each Claude Code instance gets its own `zcp` STDIO subprocess with its own
`Engine`. Instances share `.zcp/state/` but do not share in-memory state.

Coordination primitives:

| Resource | Lock | Scope |
|----------|------|-------|
| `.zcp/state/registry.json` | `flock` exclusive (5s timeout) | Session index |
| `.zcp/state/sessions/{id}.json` | None (per-instance owned) | Session state |
| `.zcp/state/active_session` | **None** | Per-instance (but shared file!) |
| `.zcp/state/services/{hostname}.json` | **None** | Shared state |
| Hostname locks | Registry-based | Bootstrap plan only |

### 3.2 Green zones (safe in parallel)

- Bootstrap on different hostnames (hostname lock isolates)
- Read-only operations (discover, knowledge)
- Develop on different services — by accident, since develop is stateless

### 3.3 Red zones (collision risk)

1. **`active_session` file is last-write-wins.** Every instance writes its
   session ID to the same file. Instance A's session ID is overwritten by
   Instance B's. If Instance A then restarts, it reads Instance B's session ID
   and fails to recover. Evidence: `session.go:229-250`.
2. **Bootstrap hostname lock, but develop has no equivalent.** Two instances
   can call develop briefing on the same hostname simultaneously — no conflict
   detection. For stateless briefing this is usually fine, but `zerops_mount`
   and `zerops_deploy` called by two instances on the same service can race.
3. **ServiceMeta writes are unlocked.** If two instances bootstrap the same
   hostname with `BootstrapSession=""` metas (corrupted/orphan state), both
   can write the file.
4. **Registry lock 5s timeout has no escalation.** Under contention, an
   instance gets an error and must retry manually.

**Recommendation:** Either give `active_session` a per-PID suffix
(`active_session.{pid}`) so instances don't overwrite each other, or drop the
file entirely and rely on registry lookup by PID. The current global-file
design assumes single-instance operation.

---

## 4. G3 — Compaction Survival (Investigator #3)

### 4.1 Always-visible signals

Every MCP response injects:

- `baseInstructions` (instructions.go:17-23) — static text, strategy-blind
- `buildSessionHint` (instructions.go:114-142) — dynamic, handles **bootstrap
  and recipe only**
- Orientation — per-service strategy & mount info

### 4.2 After compaction: what can the LLM rediscover?

| State to recover | Bootstrap | Recipe | Develop |
|------------------|-----------|--------|---------|
| "I am mid-workflow" | YES (session hint) | YES (session hint) | **NO** |
| "I am at step X" | YES (`action=status`) | YES (`action=status`) | **NO** (no status action) |
| "Retry / iterate" | YES | YES | N/A (no session) |
| "Resume on new PID" | YES (`claimSession`) | YES | **NO** (marker keyed by PID) |

### 4.3 The critical gap

If an LLM session is compacted mid-develop:

- System prompt still says "start develop workflow before any code changes"
- No signal says "a develop workflow is already active"
- LLM's safest action is to re-call `action="start" workflow="develop"` — which
  silently succeeds, generates a fresh briefing, and orphans the old marker

**Recommendation:** Either:
- (A) Extend `buildSessionHint` to detect the develop marker for the current
  PID and emit: "Active develop briefing: intent=... — deploy to close."
- (B) Accept that develop is fire-and-forget and restructure base instructions
  to say so explicitly: "develop returns a briefing; you don't re-call it
  until you start a new task."

Source: `internal/server/instructions.go:114-142`,
`internal/workflow/develop_marker.go:25-52`,
`internal/tools/guard.go:21`.

---

## 5. G4 — Process/Binary Restart Survival (Investigator #5)

### 5.1 Bootstrap and recipe: OK

`NewEngine()` (engine.go:31-52) auto-recovers:

1. Reads `active_session` file
2. Loads session JSON
3. If the old PID is dead, calls `claimSession()` — updates PID in state +
   registry, session resumes

### 5.2 Develop: orphaned markers

The develop marker file is keyed by the **old** PID. On Claude Code restart
(new PID), the new process writes a new marker at a new filename. The old
marker is orphaned until `CleanStaleDevelopMarkers()` reaps it.

The LLM has no signal that it was previously in develop mode. Work-in-progress
(unpushed code, uncommitted SSHFS edits) remains on disk, but the lifecycle
context is lost.

### 5.3 Recipe iteration exhaustion

- Hard limit: 10 iterations (`ZCP_MAX_ITERATIONS`)
- Iteration 11: hard error "max iterations reached (10), reset session to
  continue"
- `bootstrap_guidance.go:105-140` escalates at iteration 5 ("ask user") — soft
  threshold differs from hard limit

### 5.4 Recipe state staleness

The recipe plan is frozen at submit time. If the user scales a service, adds
env vars, or changes type mid-recipe, the recipe guidance stays on the old
model. No mid-session refresh exists.

---

## 6. G5 — Loop Closure (Investigator #4)

### 6.1 Closure is now strategy-aware, per briefing

The develop briefing ends with strategy-specific close instructions
(`briefing.go:268-314`):

- **Manual:** "Code is ready. Inform the user." — explicit stop
- **Push-git:** "Ask user: push only, or full CI/CD?" — offers two paths
- **Push-dev:** Lists deploy commands — implicit "do these, then done"

### 6.2 The loop-nudge is gone

The old completion message — "Deploy complete. Start a new develop workflow
for the next task" — was removed along with the session. The briefing does
**not** explicitly tell the LLM to start a new develop on the next task.

Verification: `grep "Start a new develop" internal/workflow/briefing.go` →
zero matches.

The base instructions still say "After deploy, immediately start a new
workflow for the next task" (instructions.go:22) — this is now the only loop
signal.

### 6.3 Contradictions resolved

The pre-refactor bugs (Bug 3 strategy-blind execute, Gap 9.1 manual deadlock,
Gap 9.4 zerops_deploy decoupling) are **eliminated by construction** — there
are no separate execute/verify steps to contradict prepare anymore.

### 6.4 New closure weaknesses

1. **No enforcement.** Stateless briefing means nothing blocks the LLM from
   calling develop 10 times without deploying once.
2. **No completion attestation.** Nothing records "task X was deployed." The
   only signal is `zerops_deploy` succeeding, which is tool-local.
3. **Loop nudge depends on base instructions alone.** If compaction shifts the
   LLM's attention off base instructions (long tool history dominates), the
   "start a new workflow for next task" nudge can be forgotten.

---

## 7. Cross-Cutting Risk Ranking

Ranked by severity for the user's stated use case ("long LLM work, compaction,
parallel instances, clean closure"):

| Rank | Risk | Affects | Root cause |
|------|------|---------|------------|
| 1 | **Develop state invisible after compaction** | G3, G4 | No session hint path for develop |
| 2 | **Strategy gap at recipe→develop bridge** | G1 | Base instructions & briefing give contradictory order |
| 3 | **active_session file collision between parallel instances** | G2 | No per-instance scope on the file |
| 4 | **Loop nudge softened** | G5 | Completion message removed without replacement |
| 5 | **Recipe state staleness on long runs** | G4 | No mid-session plan refresh |
| 6 | **Orphaned ServiceMeta after abandoned bootstrap** | G1, G2 | `Reset()` doesn't clean provision metas (pre-existing bug from specs) |

---

## 8. Spec Debt

The following files in `docs/` describe the **pre-refactor** architecture
and need revision:

- `spec-develop-flow-current.md` — entire document describes a 3-step session
  that no longer exists
- `spec-lifecycle-analysis.md` — Bugs 2, 3, 4 and Gaps 9.1–9.6 are
  architecturally eliminated; Bug 1 (abandoned bootstrap) still applies
- `audit-session-design.md` — the "redesign" it proposes appears to have been
  implemented, but not documented; this file is now a post-facto change log

**Recommendation:** Either move these to `docs/archive/` and write new
current-state specs, or update them in place with a "Status: superseded"
banner referencing this file.

---

## 9. Recommended Next Steps (Not Implemented — Research Output Only)

Prioritised by lifecycle-integrity payoff:

1. **Add develop marker to `buildSessionHint`.**
   Detect `{stateDir}/develop/{pid}.json` and emit a session hint so compacted
   LLMs can rediscover their position.
2. **Refresh the spec docs.** The 4 affected files need to be aligned with
   the stateless briefing architecture before any further design work.
3. **Decide: should the loop be enforced, or remain advisory?**
   If enforced: bring back a lightweight session (even just
   `lastDeployedAt`-style metadata) so the LLM gets "start a new task" nudges
   post-deploy. If advisory: document this intent explicitly in base
   instructions.
4. **Scope `active_session` per PID** or drop it entirely. Current design
   assumes single-instance usage.
5. **Add a strategy prompt** at the bootstrap→develop transition so the
   briefing doesn't have to detect "no strategy" and interrupt its own flow.
6. **Close Bug 1** (abandoned bootstrap leaves orphan metas) — carry-over
   from `spec-lifecycle-analysis.md` section 3, still applicable.
