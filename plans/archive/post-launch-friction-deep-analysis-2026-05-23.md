# Post-launch friction — ultra-deep analysis + structural fixes

**Date:** 2026-05-23 (drafted), updated 2026-05-24 (Phase 2 demoted to error-feedback; Phase 5 dropped — sentinel pattern grows linearly with incidents, not structural improvement)
**Trigger:** 3 live behavioral evals (`suite=20260523-{181931,182949,184005}`) on `git-push-setup-then-actions` scenario. Phase 1-7 of the git-push systemic fix shipped + 2 eval-driven iterations landed. Remaining friction surfaced consistently across all 3 retrospectives.
**Method:** 5 explore agents (read-only) + 3 transcript walks + 3 Codex review rounds. Each phase below comes with empirical turn-cost evidence and a concrete handler/render touch point.
**Effort:** ~4 phases, ~90 LOC total, ~2 days focused work.

---

## How to resume implementation (post-compaction)

Paste this command to Claude in a fresh session inside the repo:

```
Implement plans/archive/post-launch-friction-deep-analysis-2026-05-23.md.

Vertical-slice each phase (handler change + tests + atom updates in
one commit). Phase order: 1 → 2 → 3 → eval re-run → 4. Each phase
ships independently; verify go build + go test -short + make lint-fast
clean before commit.

Constraints:
- Karel's CLAUDE.local.md Engineering Priority applies. User-facing
  surfaces (atoms, .mcp.json, .claude.json, .zcp/state files, mcp__zerops__*
  permissions) MUST stay backward compatible.
- Phase 2 is NOT a refactor — only the UnmarshalJSON error message
  changes (returns full corrected JSON). Wire shape stays nested,
  .zcp/state stays nested. Tolerant shim is BACKLOG.
- No overengineering. Each phase has a single thematic concern.
- After each phase: short status update (1-2 sentences + commit hash).
  Don't re-analyze or re-justify — plan is settled.

Reference companion plan (already shipped, do not redo): plans/git-push-flow-systemic-fix-2026-05-23.md.

After Phases 1-3 commit, run one behavioral eval:
  export ZCP_E2E_GITHUB_PAT=$(sed -n '2p' /Users/macbook/Documents/Zerops-MCP/zcp/.zcp/manual/cred.txt)
  ./eval/behavioral/flow-eval.sh git-push-setup-then-actions
  (background mode, ~15min)
Then read self-review.md, evaluate, and proceed to Phase 4 if no
regression. If eval surfaces NEW friction not in the plan, stop and
ask before iterating.

Plans dir untracked WIP files (launch-production-friction-fixes-*,
multi-agent-container-support-*) must stay out of every commit
(use targeted git add by file, never -A on plans/).
```

---

## Decision history (for context after compaction)

**Phase 1 — Read-back render:** Codex round 2 settled compact tuple format `deployConfig=closeMode:X, gitPush:Y, ci:Z` over multi-line emit. Render unconditionally for bootstrapped runtimes (defaults aren't harmless to LLM decisions). `remoteUrl=...` separate line when non-empty.

**Phase 2 — Adopt plan stays nested, improve error feedback only:** discussion arc went: (a) "keep nested + improve instructions" → believed empirically failed → (b) "handler auto-fold" → Codex endorsed → (c) "fact-check why nested" → nesting NOT load-bearing at wire level → (d) "clean refactor to flat" → Codex round 3 found `.zcp/state` is user-facing surface (explicitly listed in CLAUDE.local.md); refactor would break Karel's own active session JSON → (e) **empirical re-check of 3 eval transcripts** revealed 0 of 3 hit flat-form rejection. The "future agent might flatten" concern is a hypothetical self-review observation, not measured turn-cost data. **Final decision: keep nested wire shape, improve error message to return full corrected JSON (paste-and-resend), pin teaching with sentinel.** Tolerant shim moved to BACKLOG (promote only if ≥2 future evals hit flat-form with >2-turn cost each).

**Phase 3 — Build-integration topology fields:** Codex round 2 critical caveat: compute `pushSource` from canonical `meta.Hostname`, NOT input echo. Agent passing `service="appstage"` gets `pushSource:"appdev"` (truth) not `pushSource:"appstage"` (input echo lie). Atom drops "rejects stage-half" claim (handler is permissive via pair-keyed lookup; atom was misaligned).

**Phase 4 — Close-mode conflict detection:** preflight grouping by canonical pair meta. Reject only divergent both-halves; collapse same-value duals to single canonical write. Avoid tie-break (no canonical winner without arbitrary choice).

**Phase 5 — DROPPED 2026-05-24:** Karel pointed out that the sentinel test pattern (forbidden exact-phrase list) grows linearly with each incident — existing 6 entries are all post-mortem reactions, adding entry #7 doesn't solve the underlying atom/handler drift, just memorializes one specific phrase. Sentinel can be bypassed by rewording the same false claim. Atom claim fix for `"handler rejects stage-half targets"` rolls into Phase 3 commit (1-line atom edit). Structural atom-drift detection v3 is BACKLOG (`plans/backlog/atom-drift-detection-v3.md` — design when more drift data accumulates).

**Original Phase 4 (`discoveryBypassed` flag) — NO-GO:** Codex round 2 sanity check: discovery state is not persisted; handler can't truthfully discriminate "agent skipped menu" from "agent saw menu then committed". Backlog until route-first eval surfaces concrete turn-cost.

**Original Phase 5 (a) (build-integration stage-half tighten) — NO-GO:** Codex round 2: build-integration is structurally fine permissive (pair-keyed lookup); git-push-setup is the strict sibling. Atom claim was wrong, fix atom not handler.

---

## TL;DR

Phase 1-7 of `git-push-flow-systemic-fix-2026-05-23.md` fixed the lying-flag class (probe-first verifier, atom drift). Three evals confirm the load-bearing fixes work end-to-end (probe gives "safe to retry without cleanup" guarantee; SSHFS-push warning saved ~20 turns worst-case).

Remaining friction items are **observability + input ergonomics**, not new lying flags:

1. **Read-back surface incomplete.** `ServiceMeta` has the data (gitPushState, buildIntegration, remoteUrl) — `RenderStatus` markdown strips it. Single render filter is the chokepoint. Agents work around via `status:noop` idempotent re-call.
2. **Adopt plan nested-shape friction is hypothetical, not measured.** Self-review across 3 evals: agent passed (read schema carefully), but flagged "future agent might flatten by reflex." Cost when hit: 1 retry turn (error message has actionable correction). Teaching stays nested; error feedback improves to paste-and-resend.
3. **Build-integration response carries pair-aware redirect (input=appdev, buildTarget=appstage) without signaling it.** Asymmetric to verify/record-deploy which DO emit a note/warning on redirect.
4. **Bootstrap two-phase silent-skip path.** Agent passing `route` on first call loses recipe fit warnings + adopt candidates silently.
5. **Per-pair vocabulary uniformity** (Codex round 1 catch): close-mode is documented per-service but persisted per-pair; build-integration stage-half is atom-rejected but handler-accepted. Same root class — surfaces vs. handlers diverge on per-pair semantics.

None of these are lying flags. All are surface ergonomics where the data is correct but visibility/ingest paths are uneven.

---

## 1. Diagnosis evidence base

### Empirical turn-count signal across 3 evals

| Eval | Friction triggered | Tool-call count |
|---|---|---|
| `20260523-181931` (before any fix) | gh-auth retry | 20 |
| `20260523-182949` (gh-auth fixed; SSHFS-push not) | SSHFS-push retries | 40 (**+20 vs #1**) |
| `20260523-184005` (both fixes shipped) | Remaining items | 21 (close to #1 baseline) |

The SSHFS-push warning we shipped during round 1 of iteration saved ~19 turns worst-case. Remaining items in eval #3 cost ~1 turn each. None individually approach the SSHFS magnitude, but each is recurring across scenarios.

### Per-friction empirical evidence

| Friction | Eval evidence | Verbatim agent observation |
|---|---|---|
| Status missing `buildIntegration` | All 3 evals | "I couldn't confirm from status alone — I had to re-call `build-integration` and got `status: noop` which confirmed the prior call had stuck." |
| buildTarget=appstage redirect not signaled | Evals #2, #3 | "The response's `service` field still says `appdev`, making it look like it honored your input when it actually redirected." |
| Adopt plan nesting | All 3 evals (mitigated by careful agents) | "When you're constructing the JSON by hand it's easy to put `stageHostname` as a sibling of `runtime` rather than inside it. A future agent scanning quickly would likely flatten it and eat a retry." |
| Bootstrap two-phase | All 3 evals | "If you pass `route` on the first call, it works — you just skip seeing the route options... a future agent who sends `route=adopt` on the first call and gets `session-active` back might not realize they skipped a step that could have surfaced important information." |
| ghAuthPrecondition as data not steps | Eval #3 (after gh-auth fix landed) | "ghAuthPrecondition field hints at this but it's presented as a data structure, not as a step sequence." |

---

## 2. Architectural diagnoses (per-friction)

### Friction A — Render strips deploy-config axes (item 1+5 unified)

**Site:** `internal/workflow/render.go:198-214` (`renderBootstrappedFields`). Emits only `mode`, `closeMode`, `stage`, `deployed`, `bootstrapped`.

**Data is present:** `ServiceSnapshot` (envelope.go:104-120) correctly carries `gitPushState`, `buildIntegration`, `remoteUrl`. `compute_envelope.go:217-248` projects them. JSON consumers see them. Markdown consumers (status response, develop briefing) see truncated subset.

**Asymmetry root cause:** the render function pre-dates the post-Phase-1 systemic fix where `remoteUrl` became probe-proven (load-bearing for launch-production). Pre-fix, the field was a soft cache; post-fix it's authoritative — but render never caught up.

**Adjacent symmetric gap:** `closeModeListEntry` (workflow_close_mode.go:28-36) carries `gitPushState` + `buildIntegration` but NOT `remoteUrl`. The JSON read-back path for close-mode list has the same gap.

**Recommended fix:**
- Render unconditionally for bootstrapped runtimes (Codex round 1 — defaults aren't harmless to LLM decisions):
  - `closeMode={value}` — already rendered (normalize empty → `unset`)
  - `gitPushState={value}` — NEW (normalize empty → `unconfigured`)
  - `buildIntegration={value}` — NEW (normalize empty → `none`)
- Render `remoteUrl={url}` only when non-empty.
- Add `remoteUrl` to `closeModeListEntry` JSON shape.

Render output for a wired pair:
```
appdev (nodejs@22) — bootstrapped=true, mode=dev, closeMode=git-push, stage=appstage, deployed=true, gitPushState=configured, buildIntegration=actions, remoteUrl=https://github.com/owner/repo.git
```

**Test pin update:** `TestRenderStatus_IdleRenders` (render_test.go:142) — expand expectations to include the 3 new tokens.

**Effort:** ~5 LOC + 2 LOC for close-mode list + test update.

### Friction B — Adopt plan nested-shape friction is hypothetical (revised post Codex round 3)

**3-surface teaching is consistent and exhaustive:**
- `internal/tools/workflow.go:43` — jsonschema description: shape + 2 inline JSON examples + sentence `"bootstrapMode and stageHostname MUST nest inside the runtime object — flattened top-level placement is hard-rejected"`
- `internal/workflow/validate.go:74` — UnmarshalJSON error: `plan target: [<fields>] must nest inside runtime, e.g. {"runtime":{<field>:...}}`
- `internal/content/atoms/bootstrap-{classic-plan-dynamic,recipe-match}.md` — full nested JSON example + warning sentence

**Empirical re-check (Codex round 3 sanity question: measured friction or hypothesis?):**

| Eval | Adopt-plan friction triggered? | Self-review observation |
|---|---|---|
| `20260523-181931` | No (gh-auth was the friction) | not mentioned |
| `20260523-182949` | No (SSHFS-push was the friction) | not mentioned |
| `20260523-184005` | No | "I didn't hit this error, but only because I read the schema carefully. A future agent scanning quickly would likely flatten it and eat a retry." |

**3 evals, 0 actual hits.** Self-review #3 is HYPOTHETICAL ("future agent might"), not MEASURED turn-cost. Adopt-plan friction was added to plan based on self-review concern, not empirical data.

**Architectural verdict (revised):**
- Nesting IS semantically clean — 1 runtime + N deps as separate sub-shapes
- Wire shape exposed via MCP input schema + persisted in `.zcp/state` → any refactor breaks active user sessions (Karel's own session 7c3b9577bcfee117 currently nested + `active=true`)
- Current 3-surface teaching is consistent + exhaustive
- Cost of fall: 1 retry turn (error has corrected JSON)

**Recommended fix (cost-proportional, ~15 LOC):**
- Sharpen `BootstrapTarget.UnmarshalJSON` error to return COMPLETE corrected JSON (not just one-field example) so retry is paste-and-resend:
  ```
  plan target: flattened fields [bootstrapMode, stageHostname] auto-rejected. Resubmit as:
  [{"runtime":{"devHostname":"appdev","stageHostname":"appstage","type":"...","bootstrapMode":"standard"}}]
  ```
- Sentinel pin: `TestUnmarshalJSON_ErrorReturnsCorrectedNestedJSON` — error contains literal `{"runtime":` substring + every flattened field name
- 1-line atom addition in `bootstrap-classic-plan-dynamic.md` + `bootstrap-recipe-match.md`: "if you flatten by reflex, the error response includes the corrected JSON; paste-and-resend in 1 turn"

**Backward compat:** zero. Wire shape unchanged, `.zcp/state` schema unchanged, schema description unchanged.

**Tolerant-shim backlog promotion criteria:**
- ≥2 future evals hit flat-form rejection AND each costs >2 turns
- THEN reconsider Codex's tolerant UnmarshalJSON shim (~15 LOC) as proper fix
- Until then: status quo + sharper error feedback only

**Effort:** ~15 LOC + 1 sentinel test + 2 atom 1-line additions.

### Friction C — Build-integration buildTarget redirect not signaled

**Site:** `internal/tools/workflow_build_integration.go:262` (`actionsConfirmResponse`) + `:359` (`webhookConfirmResponse`) + `:116` (walkthrough). Response carries `service:"appdev"` AND `buildTarget:"appstage"` side-by-side with no inline explanation.

**Asymmetric to existing convention:** `handleVerify` (verify.go:73) and `handleRecordDeploy` (workflow_record_deploy.go:153) BOTH emit a note/warning when input hostname differs from resolved build target:
> "verify: input serviceHostname=… is a push source; verified build target … instead (git-push standard pair builds on stage). Pass the build-target hostname directly next time."

build-integration uses the same `anticipatedBuildTarget` resolution (line 420) but doesn't carry the explanation through to the response.

**Atom corpus does explain it** (`develop-close-mode-git-push.md:16` "Push source vs build target" headline) but only fires AFTER strategy-setup → too late for first-read of confirm response.

**Recommended fix:** add `pushSource` + `buildTarget` + conditional `topologyNote` to build-integration confirm responses (BOTH actions and webhook per Codex round 1):

```go
body["service"] = hostname            // unchanged — input echo + meta-mutation target
body["pushSource"] = hostname         // explicit role naming
body["buildTarget"] = buildHost       // resolved
body["buildSetup"] = buildSetup       // existing
if buildHost != hostname {
    body["topologyNote"] = fmt.Sprintf(
        "Standard-pair build-integration: configured per-pair from the dev half %q (input = push source = meta-mutation target); CI runs `zcli push --service-id <%s-id> --setup %s` so the build lands on the stage half %q (build target). Secrets below reflect %q. Simple/single-runtime modes have pushSource == buildTarget.",
        hostname, buildHost, buildSetup, buildHost, buildHost)
}
```

Drop `topologyNote` when simple modes (pushSource == buildTarget).

**Companion atom edit:** `develop-strategy-awareness.md` adds 1-line preface: "the build-integration response will name the dev half as `service`/`pushSource` and the stage half as `buildTarget` (where Zerops rebuilds)."

**Backward compat (Karel policy):** adding fields safe. Keep `service` field name unchanged — renaming to `inputService` would break downstream consumers; Codex round 1 explicitly disqualified.

**Effort:** ~25 LOC + 1 test pin + 1-line atom edit.

### Friction D — Bootstrap two-phase silent-skip

**Site:** `internal/tools/workflow.go:898-966` (`handleBootstrapStart`). Pure shape dispatch: empty route → `BootstrapDiscover` (route-menu); non-empty route → `BootstrapStartWithRoute` (session-active). No warning, no merge, no error when agent passes route on first call.

**Three load-bearing reasons for two-phase split:**
1. Resume detection — `sessionId` chicken-and-egg, MUST come from menu
2. Recipe fit warnings — `recipeMatch.{fit, fitExtras, collisions}` computed only in discovery; NEVER reach session-active response
3. Adopt-vs-bootstrap-over disambiguation — `adoptServices[]` only in route-menu

**Silent-skip consequences:** route-first agent loses #2 and #3 information entirely. Discover step re-derives `adoptServices` via `zerops_discover`, but recipe collisions + fit info NEVER surface again.

**Recommended fix (Phase 1 only per Codex round 1):**
- Add `discoveryBypassed: true` boolean field on `BootstrapResponse` (bootstrap.go:60) when `handleBootstrapStart` commits with route already set.
- Atom warning (1-line edit to `bootstrap-route-options.md` or develop-step intro): "If you passed route on the first call, you skipped the discovery scan — recipe collisions, fit warnings, and adopt candidates were NOT surfaced. If you need that scoring, call `reset` and `start` without route."

**Codex round 1 caveat:** don't claim `status` recovers all skipped info — it doesn't recover recipe fit/collisions. Frame as "continue if intentional, or reset/start without route if you need discovery scoring."

**Phase 2 plumbing** (~150 LOC) is BACKLOG. Wire shape extension carrying `recipeMatch{fit, collisions}` + `adopt{candidates[]}` into session-active response would make single-call info-equivalent to two-call for those routes. Defer until route-first evals show recipe/adopt friction concretely.

**Effort:** ~30 LOC + 1 test + 1 atom edit.

### Friction E — Per-pair vocabulary uniformity (Codex round 1 catch)

**Site:** Multiple. The root class is: deploy-config axes are PERSISTED per-pair (ServiceMeta is pair-keyed), but DOCUMENTED inconsistently as per-pair vs per-service across atoms and handlers.

**Concrete divergences:**
- `build-integration` stage-half rejection: atom `develop-strategy-awareness.md:41` says "handler rejects stage-half targets", but `workflow_build_integration.go::handleBuildIntegration` uses pair-keyed `FindServiceMeta` lookup that resolves stage→dev silently. Only `git-push-setup` enforces via `PushSourceCheckFor` (workflow_git_push_setup.go:110). Atom + handler diverge.
- `close-mode` map iteration: `closeMode={"appdev":"git-push", "appstage":"manual"}` — both halves of the same pair with conflicting values. Map iteration order is undefined in Go; effectively "last-write-wins" nondeterministic. Handler doesn't reject the conflict.

**Recommended Phase 5 audit:**
1. `build-integration` stage-half: either ADD `PushSourceCheckFor` enforcement matching atom (close discrepancy by tightening handler), OR update atom to match handler permissiveness. Codex round 1 recommends tightening — atoms describe the rule; handler should enforce it.
2. `close-mode` conflicting-both-halves: detect when input map contains both halves of a pair with different values; reject with explicit error.

**Effort:** ~40 LOC + 2 test pins + 1 atom alignment edit (whichever direction wins).

---

## 3. Phase plan (post-Codex-round-3 + empirical re-check)

Each phase is independently shippable, has a single thematic concern, and includes test + atom updates in the same commit per the systemic-fix vertical-slice rule.

### Phase 1 — Read-back completeness (compact deployConfig tuple)

**Scope:**
- `internal/workflow/render.go::renderBootstrappedFields` — emit compact deploy tuple unconditionally for bootstrapped runtimes (Codex round 2 — defaults aren't harmless to LLM decisions). Shape:
  ```
  deployConfig=closeMode:unset, gitPush:unconfigured, ci:none
  remoteUrl=https://github.com/owner/repo.git   # only when non-empty
  ```
  Normalize empty values: `unset` / `unconfigured` / `none`.
- `internal/tools/workflow_close_mode.go::closeModeListEntry` — add `remoteUrl` field for JSON read-back symmetry.
- `internal/workflow/render_test.go::TestRenderStatus_IdleRenders` — expand expectations for new tokens.
- `internal/content/atoms/develop-strategy-awareness.md:13` — update render output citation to compact tuple format (Codex round 3 — atom cites old render line; must match new render).
- `internal/content/atoms/develop-first-deploy-intro.md:31` — same atom citation update.

**Effort:** ~15 LOC + 1-2 test edits + 2 atom citation updates + golden regen.

### Phase 2 — Adopt plan: improve error feedback (nested stays canonical)

**Decision (post Codex round 3 + empirical re-check):** the original Phase 2 refactor was scoped against a self-review concern, not measured friction. 3 evals showed 0 hits on flat-form rejection — adopt-plan nesting is NOT a load-bearing turn-cost item. Refactor (flat canonical) AND tolerant-shim (auto-fold) are both YAGNI right now. Tolerant shim is BACKLOG (promote criteria below).

**Scope (~15 LOC total):**
- `internal/workflow/validate.go::BootstrapTarget.UnmarshalJSON` — sharpen error to return COMPLETE corrected JSON instead of single-field example. Agent paste-and-resends; 1 turn cost stays bounded.
  ```go
  // Before:
  return fmt.Errorf(`plan target: [%s] must nest inside runtime, e.g. {"runtime":{%q:...}}`,
      strings.Join(quoted, ", "), leaked[0])

  // After: render full corrected JSON with the leaked fields placed inside runtime
  return fmt.Errorf(`plan target: flattened fields [%s] auto-rejected. Resubmit: %s`,
      strings.Join(quoted, ", "), renderCorrectedTarget(data, leaked))
  ```
- `internal/content/atoms/bootstrap-classic-plan-dynamic.md` — 1-line addition: "if you flatten by reflex, the error returns the corrected JSON; paste-and-resend in 1 turn"
- `internal/content/atoms/bootstrap-recipe-match.md` — same 1-line addition
- New test: `TestUnmarshalJSON_FlattenedFields_ErrorReturnsCorrectedNestedJSON` — assert error string contains `{"runtime":` substring + every leaked field name actually placed inside the runtime object

**Backward compat:** zero risk. Wire shape, MCP input schema, atom semantic content, `.zcp/state` JSON shape — all unchanged. Only the rejection error string format improves.

**What is NOT in scope (explicitly):**
- Type refactor — `BootstrapTarget {Runtime, Dependencies}` stays as-is
- Schema description in `workflow.go:43` — stays as-is (3 surfaces stay consistent)
- Sunset shim / migration — none needed (nothing changes on disk)
- `RuntimeTarget` deletion / `flattenedRuntimeFields` deletion — both kept

**Backlog promotion criteria for tolerant shim:**
- ≥2 future evals hit flat-form rejection in `BootstrapTarget.UnmarshalJSON`
- Each hit costs >2 turns (i.e. agent doesn't paste-and-resend within 1 turn from sharpened error)
- THEN open `plans/bootstrap-target-flat-tolerance-promoted-YYYY-MM-DD.md` and implement Codex's tolerant UnmarshalJSON shim (~15 LOC; accepts nested-only + flat-only + same-value duals; rejects divergent duals).

**Effort:** ~15 LOC + 1 sentinel test + 2 atom 1-line additions.

### Phase 3 — Build-integration topology fields (canonical role echo)

**Scope:**
- `internal/tools/workflow_build_integration.go::actionsConfirmResponse` + `webhookConfirmResponse` + walkthrough body — add `pushSource`, `buildTarget`, conditional `topologyNote`.
- **Codex round 2 caveat:** compute `pushSource` from canonical meta (`meta.Hostname`), NOT from input hostname blindly. If agent passes `service="appstage"`, response is `service:"appstage"`, `pushSource:"appdev"`, `buildTarget:"appstage"`. Today's input echo would lie.
- **Codex round 3 add:** `workflow_build_integration.go:185` no-op branch (idempotent re-call when integration already set) must include the SAME pushSource/buildTarget/topologyNote fields. Otherwise first call response and re-call response differ in shape, breaking the "noop = already set" verification path called out in agent self-reviews.
- Atom alignment: drop the `develop-strategy-awareness.md:41` "handler rejects stage-half targets" claim (Codex round 2 — handler is already permissive via pair-keyed FindServiceMeta lookup; atom describes git-push-setup's stricter behavior, not build-integration's).
- Test: `TestHandleBuildIntegration_ActionsConfirmEnrichesResponse` expanded for pushSource computation; new `TestHandleBuildIntegration_StageHalfInput_ResolvesToPair`; new `TestHandleBuildIntegration_SimpleNoTopologyNote`; new `TestHandleBuildIntegration_NoOpReCall_MatchesFirstCallShape`.

**Effort:** ~35 LOC + 4 test pins + 1 atom edit + golden regen.

### Phase 4 — Close-mode conflicting-both-halves reject (preflight)

**Scope (Codex round 2 split from original Phase 5):**
- `internal/tools/workflow_close_mode.go::handleCloseMode` — preflight pass groups input `closeMode` map by canonical pair meta (via `FindServiceMeta`). Reject when one pair receives conflicting values across both halves. Accept same-value duals (collapse to canonical write). Do NOT tie-break.
- Test: new `TestHandleCloseMode_ConflictingBothHalves_Rejected` + `TestHandleCloseMode_SameValueDualHalves_Collapsed`.
- Atom alignment: `develop-strategy-awareness.md` `close-mode` description note — already says "per-service" but persisted per-pair; clarify that conflicting both-halves input is rejected, same-value duals are collapsed.

**Effort:** ~25 LOC + 2 test pins + 1 atom edit.

### Phase 5 — DROPPED

Sentinel test pattern is reactive memorialization of past phrases. Each incident → one more entry → grows linearly. Doesn't solve atom/handler drift; can be bypassed by rewording. Atom claim fix moved into Phase 3 commit. Structural atom-drift detection v3 is BACKLOG.

---

## 4. Dropped phases (from initial plan)

### Original Phase 4 — `discoveryBypassed` flag — NO-GO

**Codex round 2 sanity check (fatal flaw):**
> "`discoveryBypassed` is not implementable truthfully as written. Discovery is stateless: no-route start returns `route-menu`, but that fact is not persisted; the later route commit in `workflow.go:918` looks identical whether the agent saw the menu or skipped it. A boolean would be false-positive for normal two-call users."

The handler at workflow.go:918 has NO way to discriminate "agent skipped menu" from "agent ran menu then committed". Setting the flag on every committing call would false-positive every normal two-call workflow.

**Options:**
- Add a discovery nonce/token from route-menu response that the agent must echo on commit — adds protocol complexity to no demonstrated turn-cost benefit
- Rename to a truthful generic notice (e.g., a generic "route-menu warnings not surfaced in this response" note attached to every session-active response when route was given on first call — but this is just dodge phrasing)

**Verdict:** backlog until route-first eval surfaces actual turn-cost from missed recipe collisions / adopt candidates.

### Original Phase 5 (a) — build-integration stage-half tightening — NO-GO

**Codex round 2:** "build-integration uses pair-keyed lookup via FindServiceMeta and is structurally fine permissive — git-push-setup is the stricter sibling because it mutates/probes the push source specifically. Loosen the atom claim instead of tightening the handler."

Replaced with atom alignment in Phase 3 (drop the "rejects stage-half targets" assertion).

---

## 5. Backlog / out-of-scope

| Item | Why deferred |
|---|---|
| **Tolerant `BootstrapTarget.UnmarshalJSON` shim (~15 LOC)** | 3 evals × 0 hits on flat-form rejection. Promote when ≥2 future evals hit flat-form AND each costs >2 turns (i.e. sharpened error in Phase 2 didn't bound it to 1 retry). |
| **Full flat-canonical refactor** | Would break `.zcp/state` JSON shape (user-facing per CLAUDE.local.md). Tolerant shim is the milder option if shim ever needed. |
| `discoveryBypassed` flag with nonce/token protocol | No turn-cost evidence yet; protocol cost > benefit |
| Bootstrap Phase 2 — plumb `existing[]` through `BootstrapStartWithRoute` (~150 LOC) | No turn-cost evidence for route-first agent missing recipe collisions yet |
| Cross-handler `ActionStep[]` standardization (~200 LOC) | Only 1 handler proven friction (build-integration); ad-hoc shape works empirically for 27 of 28 handlers |
| `ghAuthPrecondition` field reorder | Map literal order is not a wire contract |
| SSHFS perpetual `package-lock.json` modification | Platform-level (Zerops SSHFS quirk), not ZCP |

---

## 6. References

### Eval evidence
- `eval/behavioral/runs/20260523-181931/git-push-setup-then-actions/{self-review,transcript}.md/jsonl`
- `eval/behavioral/runs/20260523-182949/...`
- `eval/behavioral/runs/20260523-184005/...`

### Codex reviews
- Round 1 diagnose: `/tmp/codex-out-1779566660-38960-260.md`
- Round 2 solution review: `/tmp/codex-out-1779567133-42667-15946.md`
- Round 3 pre-implementation sanity check (post-clean-refactor proposal): `/tmp/codex-out-1779613848-10555-27137.md` — surfaced `.zcp/state` backward-compat blocker; Karel re-checked evidence base and demoted Phase 2 to error-feedback only.

### Codex round 2 deeper pattern (added to architectural diagnoses)
> "The recurring bug class is 'canonical state with undocumented aliases.' The system accepts or stores normalized pair-level state, but responses and atoms sometimes speak in raw hostnames or per-service terms. The structural rule should be: **Accept aliases only where safe, normalize once at the boundary, then always echo both the input and canonical roles** (`inputService`, `pushSource`, `buildTarget`, canonical pair host)."

This unifies Phases 2 (flat→nested alias acceptance), 3 (input→canonical role echo), 4 (per-pair canonical lookup). The phases below operationalize this rule.

### Explore agent findings
- ServiceMeta envelope projection: agent `a41e8f4cf5eaea116`
- Adopt plan archaeology: agent `a0a4d0f72a636909e`
- DeployIntent visibility: agent `a33f9b5965792f51e`
- Response shape patterns: agent `a105c700da422ec18`
- Bootstrap two-phase start: agent `a27d6b0493997bd2d`

### Touched files (planned, not yet edited)
- Phase 1: `internal/workflow/render.go:198-214`, `internal/tools/workflow_close_mode.go:28-36`, `internal/workflow/render_test.go::TestRenderStatus_IdleRenders`, `internal/content/atoms/develop-strategy-awareness.md:13`, `internal/content/atoms/develop-first-deploy-intro.md:31`
- Phase 2: `internal/workflow/validate.go:58-83` (UnmarshalJSON error message only — type stays nested), `internal/content/atoms/bootstrap-classic-plan-dynamic.md`, `internal/content/atoms/bootstrap-recipe-match.md`, `internal/workflow/validate_test.go` (new `TestUnmarshalJSON_FlattenedFields_ErrorReturnsCorrectedNestedJSON`)
- Phase 3: `internal/tools/workflow_build_integration.go:116, 185, 262, 359`, `internal/content/atoms/develop-strategy-awareness.md:41`
- Phase 4: `internal/tools/workflow_close_mode.go::handleCloseMode` (preflight grouping), `internal/content/atoms/develop-strategy-awareness.md` (close-mode dual-half clarification)

### Spec touches
- `docs/spec-workflows.md` — invariants for `discoveryBypassed`, build-integration stage-half rule (Phase 5 may surface need to add explicit invariant), `close-mode` per-pair conflict detection.
