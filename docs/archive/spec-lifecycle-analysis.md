# Full Lifecycle Analysis: Bootstrap → Strategy → Develop → Deploy

> Cross-flow analysis showing how state evolves through the complete lifecycle.
> Identifies bugs, discrepancies, and design gaps. As-coded, 2026-04-14.

---

## 1. ServiceMeta Through the Lifecycle

### 1.1 State at Each Stage

| Stage | DeployStrategy | StrategyConfirmed | BootstrappedAt | IsComplete() | EffectiveStrategy() |
|-------|---------------|-------------------|---------------|--------------|---------------------|
| After provision (incomplete) | `""` | `false` | `""` | `false` | `""` |
| After bootstrap complete | `""` | `false` | `"2026-04-14"` | `true` | `""` |
| After auto-adopt (develop start) | `""` | `false` | `"2026-04-14"` | `true` | `""` |
| After strategy set (any) | `"push-dev"` | `true` | `"2026-04-14"` | `true` | `"push-dev"` |
| Old metas (pre-fix, unconfirmed) | `"push-dev"` | `false` | `"2026-..."` | `true` | `""` |

**Key insight**: Bootstrap NEVER sets a strategy. Strategy is empty until user
explicitly calls `action="strategy"`. The backward compat in `EffectiveStrategy()`
ensures old metas with unconfirmed `push-dev` also appear as empty.

### 1.2 Where Strategy Gets Set

There is exactly ONE code path that sets `DeployStrategy`:

```
handleStrategy() → meta.DeployStrategy = strategy; meta.StrategyConfirmed = true
```

File: `tools/workflow_strategy.go:55-56`

No other code writes to `DeployStrategy`. Not bootstrap, not develop, not deploy.

---

## 2. The Transition Gap

### 2.1 What happens between bootstrap and develop

```
Bootstrap completes
  → writeBootstrapOutputs (DeployStrategy="", BootstrappedAt=now)
  → Session cleaned up (no active session)
  → TransitionMessage: "Start develop workflow" (NO strategy mention)

[GAP — agent or user must explicitly set strategy, but nothing tells them to]

Develop starts
  → Reads ServiceMetas → all have EffectiveStrategy()=""
  → buildStrategyStatusNote: "No deploy strategy set for: X"
  → Agent continues to prepare step
  → Prepare guidance is strategy-aware (writeDevelopmentWorkflow checks strategy)
  → But strategy is still empty → shows "no strategy" path
```

### 2.2 The Missing Prompt

The transition message (`BuildTransitionMessage`) after bootstrap lists:
- Service list
- Deploy model primer
- "Start develop workflow" hint
- Utility offerings

It does NOT mention strategy at all. The agent is told to start develop, which
then immediately tells the agent "no strategy set." This is a jarring UX break:

1. Bootstrap says "all done, start develop"
2. Develop says "wait, you have no strategy, discuss with user first"
3. Agent must pause the develop flow to discuss strategy

### 2.3 Strategy-Agnostic Bootstrap Design

Bootstrap's close step guidance explicitly says: "strategy selection happens during
deploy or cicd workflows, not here." This confirms the separation is intentional.

But the develop flow's prepare guidance (`buildPrepareGuide`) switches behavior
based on strategy — if empty, it tells the agent to discuss strategy with the user
before proceeding. This means develop's first step becomes a strategy-selection
conversation, not a code preparation step.

---

## 3. Identified Bugs

### BUG 1: Abandoned bootstrap leaves hostname in degraded limbo

**Where**: `writeProvisionMetas` writes incomplete metas after provision step.
`Engine.Reset()` does NOT delete ServiceMeta files — only session files.

**Scenario**:
1. Bootstrap starts, provision completes → incomplete meta written to `services/{hostname}.json`
2. User resets/abandons bootstrap → `Reset()` deletes session file + unregisters, but meta persists
3. User starts develop workflow
4. `PruneServiceMetas` checks liveness, not completeness → live hostname survives pruning
5. `len(metas) >= 1` (incomplete meta exists) → auto-adopt path (`adoptUnmanagedServices`) is SKIPPED
   - Auto-adopt only triggers at `len(metas) == 0` (line 48)
6. `IsComplete()` filter (lines 69-79) → incomplete meta goes to `skippedIncomplete`
7. `len(runtimeMetas) == 0` → error: "No deployable services — [hostname] still bootstrapping (incomplete)"
   - Suggestion: "Finish bootstrap for those services first, then start deploy"

**Not permanently stuck**: A fresh bootstrap WILL fix it — `checkHostnameLocks` correctly
identifies the orphaned meta (dead/missing session in registry) and allows overwriting.
The new bootstrap proceeds normally.

**The UX problem**: The error says "Finish bootstrap for those services first" but the
bootstrap session is already gone (Reset deleted it). There is no bootstrap to finish.
The correct action is to start a NEW bootstrap, but the error message doesn't say this.

**Impact**: MEDIUM-HIGH. Not a data-loss bug, but creates a confusing degraded state where:
- Develop fails with a misleading error message
- Auto-adopt is blocked (incomplete meta makes `len(metas) > 0`)
- Recovery requires knowing to start a fresh bootstrap (not "finish" the old one)

**Root cause**: `Engine.Reset()` does not clean up ServiceMeta files from the abandoned session.

**Root cause**: `writeProvisionMetas` creates a persistent artifact that outlives the session.
Session cleanup (`ResetSessionByID`) deletes the session file but not the service metas.

### BUG 2: Strategy during active develop workflow creates inconsistency

**Where**: `handleStrategy` has no session check.

**Scenario**:
1. Develop workflow active at prepare step, strategy is empty
2. Prepare guidance says "discuss strategy with user"
3. Agent calls `action="strategy"` to set push-dev
4. ServiceMeta is updated on disk
5. But the current deploy session was built from the OLD meta state
6. `DeployTarget.Strategy` (set at `handleDeployStart`) still has the old empty value
7. Guidance for execute/verify steps was generated from session state, not live metas

**Partial mitigation**: The prepare step's guidance is built fresh each time
`buildPrepareGuide` is called (reads metas). But `DeployTarget.Strategy` in the
session state is never refreshed. The strategy status note is appended at start
only — not updated mid-session.

**Impact**: Medium. The in-session `DeployTarget.Strategy` field is stale after
strategy change. This affects guidance generation and step guidance personalization
but does not prevent actual deploys. The execute step guidance is strategy-BLIND
(always shows deploy commands regardless of strategy).

### BUG 3: Execute guidance ignores strategy — always shows deploy commands

**Where**: `deploy_guidance.go` — `buildDeployGuide()` is strategy-BLIND.

**Scenario**:
1. Strategy is set to "manual"
2. Prepare step says "User controls deployment timing"
3. Execute step says "Deploy now using zcli push" (ignores manual strategy)

This was documented in spec-develop-flow-current.md (Gap 1). The contradiction
is not just a UX issue — it's an active conflict that could lead the agent to
deploy when the user explicitly chose manual control.

**Impact**: HIGH. The agent receives contradictory instructions within the same
workflow. Prepare says "user controls," execute says "deploy now."

### BUG 4: Verify step checker is nil — no enforcement

**Where**: `tools/workflow_checks_deploy.go` — verify checker.

The verify step of the develop workflow has no checker (nil). It can be completed
with any attestation. Combined with Bug 3 (execute always shows deploy commands),
this means the develop workflow can be completed without any actual verification
even when it should be enforced.

**Impact**: Medium. The verify step exists as a suggestion, not a gate.
Develop was designed with lighter enforcement than bootstrap, but having NO
checker means there's no way to detect failed deploys programmatically.

---

## 4. Design vs Implementation Discrepancies

### 4.1 "Strategy is informational" vs "Strategy controls guidance"

**Design intent** (from `tools/workflow_develop.go:192`):
```go
// Per spec D2: strategy is informational at start, not a gate.
```

**Implementation reality**:
- Strategy IS informational at develop start (the status note)
- But strategy controls prepare guidance via `writeDevelopmentWorkflow()`
- And strategy is IGNORED by execute/verify guidance

This is inconsistent: strategy matters in step 1, is invisible in steps 2-3.

### 4.2 "Bootstrap doesn't set strategy" vs "Develop needs strategy"

**Bootstrap design**: Strategy is explicitly deferred. Close step guidance says
strategy selection happens in deploy/cicd workflows.

**Develop design**: Prepare step immediately asks about strategy. The first thing
the agent does in develop is something bootstrap said would happen "later."

This creates a workflow where "later" means "the very next workflow call."

### 4.3 "Manual = user controls deployment" vs "Develop always offers develop"

**Strategy handler** for manual:
```
zerops_deploy targetService="..." (manual strategy — deploy directly)
```

**Router** (`strategyOfferings`): Always offers develop at P1, regardless of strategy.

**Orientation** for manual: "Call zerops_deploy directly."

Manual strategy users are told to use zerops_deploy directly in two places
(strategy handler, orientation) but still see "start develop workflow" in the
router and MCP base instructions. The develop workflow then shows deploy commands
in execute step despite manual strategy.

### 4.4 "Deploy complete → start new workflow" vs session cleanup

When develop completes (all 3 steps done):
```go
resp.Message = "Deploy complete.\n\n" +
    "Start a new develop workflow for the next task:\n" +
    "  zerops_workflow action=\"start\" workflow=\"develop\""
```

But the session is already cleaned up by `DeployComplete` when the last step finishes.
The agent could theoretically start a new develop immediately. This works correctly —
no bug, just noting the session lifecycle aligns properly here.

---

## 5. Flow Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│                     BOOTSTRAP FLOW                               │
│                                                                  │
│  discover ──→ provision ──→ generate ──→ deploy ──→ close        │
│  (plan)       (create)      (code)      (push)     (admin)       │
│                 │                                    │            │
│                 ▼                                    ▼            │
│         writeProvisionMetas              writeBootstrapOutputs   │
│         (incomplete meta)                (complete meta)         │
│         DeployStrategy=""                DeployStrategy=""       │
│                                                                  │
│  Adoption fast path:                                             │
│  provision ──→ [skip generate/deploy/close] ──→ complete         │
│                                                                  │
│  TransitionMessage:                                              │
│  "Start develop workflow" (NO strategy mention)                  │
└───────────────────────────┬──────────────────────────────────────┘
                            │
                            ▼ [NO strategy prompt]
┌───────────────────────────────────────────────────────────────────┐
│                      STRATEGY SETTING                             │
│                                                                   │
│  action="strategy" strategies={"hostname": "push-dev|..."}        │
│  → Sets DeployStrategy + StrategyConfirmed=true                   │
│  → No session required                                            │
│  → Can happen before, during, or after develop                    │
│                                                                   │
│  WHEN does it happen?                                             │
│  - Bootstrap doesn't prompt for it                                │
│  - Develop prepare step says "discuss with user"                  │
│  - Could be skipped entirely (agent might just proceed)           │
└───────────────────────────┬───────────────────────────────────────┘
                            │
                            ▼
┌───────────────────────────────────────────────────────────────────┐
│                      DEVELOP FLOW                                 │
│                                                                   │
│  Entry: handleDeployStart                                         │
│  → Reads metas (or auto-adopts if none)                           │
│  → Filters for IsComplete() + has Mode/StageHostname              │
│  → Creates deploy session with targets                            │
│  → Appends strategy status note                                   │
│                                                                   │
│  prepare ──→ execute ──→ verify                                   │
│  (yaml)     (deploy)    (check)                                   │
│                                                                   │
│  Strategy awareness:                                              │
│  ┌─────────┬──────────────────┬─────────────────┐                 │
│  │ Step    │ Strategy-aware?  │ Checker         │                 │
│  ├─────────┼──────────────────┼─────────────────┤                 │
│  │ prepare │ YES (guidance)   │ checkPrepare    │                 │
│  │ execute │ NO (always push) │ checkResult     │                 │
│  │ verify  │ NO               │ nil             │                 │
│  └─────────┴──────────────────┴─────────────────┘                 │
│                                                                   │
│  Session cleanup: on last step complete or skip                   │
│  → "Deploy complete. Start a new develop workflow."               │
└───────────────────────────────────────────────────────────────────┘
```

---

## 6. Recommendations

### P0 — Fix abandoned bootstrap limbo (Bug 1)

Options:
- A) `Reset()` should delete incomplete ServiceMetas for the session being reset
- B) Develop's auto-adopt should ignore incomplete metas (treat as non-existent)
- C) `PruneServiceMetas` should delete incomplete metas whose `BootstrapSession`
  refers to a dead/nonexistent session

Option A is cleanest — ties cleanup to the cause (reset). Options B and C are
defensive fallbacks.

### P1 — Make strategy flow coherent (Bugs 2, 3)

The strategy lifecycle has three problems:
1. No prompt at bootstrap→develop transition
2. Execute guidance ignores strategy
3. In-session strategy changes don't update session state

Proposed fix:
- Add strategy mention to `BuildTransitionMessage` (not a gate, just awareness)
- Make `buildDeployGuide` strategy-aware (manual = don't show push commands)
- Consider: should develop start refresh `DeployTarget.Strategy` from live metas?

### P2 — Add verify step checker

Even a lightweight checker (service is RUNNING after deploy) would catch
deploy failures. Currently the verify step is pure honor system.
