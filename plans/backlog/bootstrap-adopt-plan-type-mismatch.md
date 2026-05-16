# Bootstrap adopt: plan-vs-live type mismatch sealed, iterate refused

- **Surfaced**: 2026-05-17, during env-var audit (`plans/audit-env-vars-20260515/`) review of Karel's pre-fix screenshots — separate friction observed alongside the env-var SSH-paste issue (#8); see images #9 and #10 in chat history `99b203c9-f421-4ba2-b30d-9777ffe97601`.
- **Why deferred**: orthogonal to env-vars audit scope. Different root cause (bootstrap plan sealing vs. live runtime type validation), different fix path. Env-vars work is in flight and shouldn't grow.
- **Trigger to promote**: another live run (any phase) where build→serve pair authoring trips the same seal-without-cross-check; OR a recipe-author run where the static-stage half forces a plan rewrite mid-bootstrap. One more eval-evidence instance is the bar.

## What happens

Adopt route, build→serve pair scenario (Vite SPA pattern: `appdev` runs nodejs@22 to compile, `appstage` serves static):

1. Agent submits plan with `appdev: static` (probably copied from prod-half intent).
2. Engine completes discover step and **seals the plan** in session state.
3. Provision check fails: `appdev_type: expected static, got nodejs@22`.
4. Agent calls `iterate` to correct the plan.
5. **Engine refuses** — *"plan in session state still has `appdev: static`. Bootstrap doesn't iterate plans mid-flight; the discover step was sealed with the wrong type."*
6. Only recovery is `action="reset"` — discard the entire session, re-run bootstrap with the corrected plan.

## Sketch — three candidate fixes

1. **Pre-seal validation** (preferred): in adopt route, before sealing the discover step, cross-check each `plan.targets[].runtime.type` against the live `serviceStack.serviceStackTypeVersionName`. If mismatch, refuse the `complete discover` call with a corrective diagnostic naming each `(expected, got)` pair. Cheap — adopt already enumerates live services.
2. **Allow plan iterate post-seal for type-correction** (narrower scope): permit `zerops_workflow action="iterate"` to mutate only `plan.targets[].runtime.type` fields, leaving hostname / role / dependencies frozen. Cheaper than full mutation but adds an iterate-mode complication that bootstrap-active doesn't have today.
3. **Teach build→serve pair pattern explicitly** (atom-level only — does NOT fix the seal trap): a new atom on adopt-active explaining that dev-half and stage-half can have different types (compiler runtime + serve-only static), and the plan's `runtime.type` must match the DEV-half's actual `serviceStackTypeVersionName`. Doesn't prevent the seal but at least frames the recovery faster.

Combination of (1) + (3) is the strongest: catch at submit time, teach the pattern in the recipe-author atom that surfaces during plan authoring.

## Risks

- Build→serve isn't the only mixed-type pair pattern. PHP-FPM + nginx, Bun + static dist, etc. Pre-seal validation must work for ANY type mismatch, not hardcoded to nodejs@→static. Drives the check to be generic: `plan.runtime.type == liveStack.serviceStackTypeVersionName`.
- Adopt route operates on already-live services; types are observable. Classic / recipe routes don't have a live runtime yet at plan time — fix would NOT apply there.
- A pre-seal validation that's too strict could block legitimate "I want to RENAME the runtime type via override import" workflows. Verify that adopt + override-import flow still works after the gate lands.

## Refs

- `internal/workflow/engine_*.go` — bootstrap step sealing logic.
- `internal/ops/checks/*` — would be the right place for a cross-check helper.
- Screenshot evidence: `~/.claude/image-cache/99b203c9-f421-4ba2-b30d-9777ffe97601/9.png`, `10.png` (chat 2026-05-17 with Karel).
- Adjacent atoms: `bootstrap-adopt-discover.md`, `bootstrap-classic-plan-dynamic.md`, `bootstrap-classic-plan-static.md` — none currently mention the build→serve pair type-asymmetry.
