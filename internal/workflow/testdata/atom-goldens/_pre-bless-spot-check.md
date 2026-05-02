# Pre-bless spot-check — Phase 2 Step 0

**Status**: Awaiting human approval (Phase 2 Step 1 PAUSE POINT).
**Plan**: `plans/atom-corpus-verification-2026-05-02.md` §2.

## Mandate

Per the plan: "Pick 5 fixtures spanning phases (one each from idle /
bootstrap / develop / strategy-setup / export). For each: provision/
ensure matching service exists in eval-zcp; drive into target state;
run `zerops_workflow action="status"`; diff real envelope vs scenario
fixture envelope; investigate any divergence."

Realistic budget per the plan: 4-8 hours of provisioning + driving.

## Scope of this artifact

This document is the **desk-check version** of Step 0. It maps each
candidate fixture to its envelope-shape construction site in production
code so the human reviewer can either:

1. **Accept the desk-check** — fixtures derive cleanly from documented
   construction paths; no live-platform divergence is flagged. Proceed
   to Step 2 (review pass) without per-fixture eval-zcp runs.
2. **Request live verification** — pick which of the 5 fixtures should
   be driven against eval-zcp before bless, and we run those.
3. **Defer to Phase 5 live-eval** — Phase 5.5 already pins quarterly
   live-eval runs against 5 representative scenarios with explicit
   owner/CODEOWNERS/issue mechanism. Treat the post-merge Phase 5 first
   run as the authoritative live cross-check.

## eval-zcp current state

`zerops_discover` (2026-05-02) shows the `eval-zcp` project carries one
service today: `zcp@1` (the ZCP runtime itself). To drive each of the 5
fixture shapes against eval-zcp, the workflow would need to provision
fresh services per scenario — none of the 5 shapes maps cleanly to the
existing single-service state.

## The 5 spot-check fixtures

| # | Fixture id | Phase | Envelope shape construction | Platform-state inputs |
|---|---|---|---|---|
| 1 | `idle/empty` | PhaseIdle | `compute_envelope.go::ComputeEnvelope` returns `{Phase: idle, Services: nil}` when ListServices is empty (or the `selfHostnameFromRT` filter removes the only service). `deriveIdleScenario` returns `IdleEmpty` per `compute_envelope.go:127`. | `client.ListServices` empty; no ServiceMeta on disk. |
| 2 | `bootstrap/recipe/provision` | PhaseBootstrapActive | Synthesized in `bootstrap_guide_assembly.go::synthesisEnvelope` per render. Live `BootstrapState.Route=BootstrapRouteRecipe` + `Step=StepProvision`; `Services` populated from project ListServices + ServiceMeta after provision lands. | `client.ListServices` returns ACTIVE service; ServiceMeta says `Bootstrapped=true`. Bootstrap session state from disk. |
| 3 | `develop/standard-auto-pair` | PhaseDevelopActive | `compute_envelope.go::buildOneSnapshot` per service. Pair shape: dev half emits ONE snapshot with `StageHostname` populated (per `buildOneSnapshot:217-219`). `Mode` derived by `resolveEnvelopeMode` (`compute_envelope.go:301-321`); `CloseDeployMode` from `meta.CloseDeployMode`. WorkSession from `CurrentWorkSession`. | Pair ServiceMetas; both halves ACTIVE; deploy history; close-mode confirmed. |
| 4 | `strategy-setup/configured-build-integration` | PhaseStrategySetup | `derivePhase` returns PhaseStrategySetup when `meta.CloseDeployMode == git-push` AND `meta.GitPushState == configured` AND `meta.BuildIntegration == none`. `buildOneSnapshot` emits `GitPushState` + `BuildIntegration` from meta. | ServiceMeta carrying close-mode=git-push + GitPushState=configured + BuildIntegration=none. |
| 5 | `export/publish-ready` | PhaseExportActive (export workflow direct call) | NOT computed by `ComputeEnvelope`. `BuildExportEnvelope` (synthesize_export.go) builds the envelope on demand from the export tool handler. Single-entry Services per audit decision; ExportStatus set by handler branch. | `client.ListServices` for target; `FindServiceMeta` for target hostname; the handler-side branching that decides the status. |

## Risk analysis

### What could diverge between fixture and production?

**Field-shape divergence** — the fixture sets a field that the
production envelope omits, or vice versa. Mitigated by:

- ServiceSnapshot / WorkSessionSummary / BootstrapSessionSummary types
  are shared between fixture and production (no parallel struct hierarchy).
  A field added to ServiceSnapshot in production code surfaces in
  fixtures via compile failure.
- The `compute_envelope_test.go` suite tests `buildOneSnapshot` against
  fixed inputs producing fixed outputs — drift between meta-derivation
  rules and snapshot output is gated there. Phase 1 fixtures call
  `buildOneSnapshot` shape (mode, close-mode, deploy state) with the
  same inputs the production handler uses.

**Field-value divergence** — a field's typical production value differs
from the fixture's. Mitigated by:

- All 5 fixtures use field values from the closed enums (Mode,
  CloseDeployMode, GitPushState, BuildIntegration, ExportStatus,
  RuntimeClass). Closed-enum values are pinned by topology tests
  (TestModeValues, TestExportStatusValues, etc.) so a typo in a fixture
  string doesn't compile.

**Order divergence** — Services slice or WorkSession.Services order
matters for render output. Production sorts:

- `compute_envelope.go::buildServiceSnapshots` sorts services by
  hostname. Fixture order matches alphabetical (`appdev` < `appstage`,
  `appdev` < `db`).
- `WorkSession.Services` order is preserved as the agent adds
  hostnames; fixtures put the dev hostname first (matching session
  start convention).

**Time-derived divergence** — production uses `time.Now()`; fixtures
use a fixed `2026-05-02 09:00 UTC` seed. Goldens are byte-stable across
runs. Production envelope's `Generated` field IS `time.Now()`, but no
atom body references `Generated` (it's a metadata field for compaction
keying). Render output is unaffected.

### Specific risk flags per fixture

1. `idle/empty` — fully derivable from "no services + no metas".
   Lowest divergence risk.
2. `bootstrap/recipe/provision` — `BootstrapSessionSummary.Step` is
   set to `StepProvision`. Production `synthesisEnvelope` derives this
   from `BootstrapState.CurrentStep`, which advances through the
   bootstrap conductor. Risk: if production's CurrentStep is ever empty
   when atoms expect "provision", atoms gated on `steps: [provision]`
   would not fire. Tested by `bootstrap_guide_assembly_test.go`.
3. `develop/standard-auto-pair` — Both halves get distinct snapshots
   (`appdev` mode=ModeStandard, `appstage` mode=ModeStage)? No —
   `buildOneSnapshot:217` only stamps StageHostname when
   `svc.Name == meta.Hostname` (the dev half), so the standard pair
   surfaces as ONE snapshot keyed by appdev with `StageHostname=appstage`.
   The fixture matches this shape (`fixSnapDeployedPairAuto` returns
   one snapshot, not two). **Risk to verify**: production may also emit
   a stage-half snapshot when listed separately by ListServices —
   needs confirmation. (Looking at `buildServiceSnapshots:185-193`,
   each service gets its own snapshot, so production yields TWO
   snapshots for a standard pair: dev-half with StageHostname populated,
   stage-half with empty StageHostname.) **Fixture currently emits ONE
   snapshot — this may be a divergence.**
4. `strategy-setup/configured-build-integration` — `derivePhase`
   logic decides when to enter PhaseStrategySetup. Fixture sets the
   phase directly. Risk: production's phase derivation may not enter
   strategy-setup for the (close-mode=git-push, gps=configured,
   bi=none) shape — needs verification of `derivePhase` rules.
5. `export/publish-ready` — single-entry Services per audit. Production
   `BuildExportEnvelope` produces this shape exactly. Lowest divergence
   risk because the construction path is the new one we wrote.

### Findings flagged for human review

**Finding F-1 (`develop/standard-auto-pair` may emit two snapshots in production)**:

Production `buildServiceSnapshots` iterates `services []platform.ServiceStack`
and emits ONE ServiceSnapshot per stack. A standard pair has TWO
stacks (`appdev` + `appstage`), so production produces TWO snapshots.
The fixture today (`fixSnapDeployedPairAuto`) emits ONE snapshot with
StageHostname populated.

`resolveEnvelopeMode` returns `ModeStandard` for the dev half AND
`ModeStage` for the stage half — these are DISTINCT modes, so atoms
gated on `modes: [standard]` and `modes: [stage]` see different
services per the snapshot ordering.

**Action needed**: extend `develop/standard-auto-pair` fixture to emit
both snapshots (matches production). Same likely applies to
`develop/git-push-configured-webhook` and `develop/git-push-unconfigured`
which use `fixSnapGitPushIntegration` (single snapshot today).

**Finding F-2 (`develop/post-adopt-standard-unset` similarly)**:

Same structural issue — fixture emits one snapshot for the pair via
`fixSnapDeployedPairUnset`. Production yields two.

**Finding F-3 (`develop/first-deploy-recipe-implicit-standard`)**:

`fixSnapBootstrappedNeverDeployedPair` — one snapshot for never-deployed
pair. Production would yield two.

## Summary of findings

| Finding | Affected fixtures | Severity | Proposed fix |
|---|---|---|---|
| F-1 | develop/standard-auto-pair, develop/git-push-configured-webhook, develop/git-push-unconfigured | High — atoms gated on stage-half mode never fire under fixture | Extend `fixSnapDeployedPairAuto` / `fixSnapGitPushIntegration` to return two snapshots |
| F-2 | develop/post-adopt-standard-unset | High — same as F-1 | Extend `fixSnapDeployedPairUnset` to return two snapshots |
| F-3 | develop/first-deploy-recipe-implicit-standard | High — same as F-1 | Extend `fixSnapBootstrappedNeverDeployedPair` to return two snapshots |

These three findings are **structural fixture bugs caught by the desk-
check before bless** — exactly what Step 0 is designed to surface.
Fixing them BEFORE bless prevents 4 goldens from pinning incorrect
fire-sets that would later need to be rewritten.

## Recommendation to human reviewer

1. **Approve** F-1/F-2/F-3 fix path: extend the pair-fixture helpers to
   emit two snapshots each, regenerate the affected 4 goldens.
2. **Decide** on live-platform verification:
   - **Option A** (recommended): defer live verification to Phase 5's
     post-merge protocol. Desk-check is sufficient at the unit-test
     level since fixture types share with production code.
   - **Option B**: pick 1-2 fixtures (e.g. `develop/standard-auto-pair`
     post-fix + `export/publish-ready`) and drive eval-zcp through
     them before bless. Adds 4-6 hours.
3. **Approve** the rest of the desk-check findings (no other risks
   surfaced) and unblock Step 2 (review pass).

After approval, I'll fix F-1/F-2/F-3, regenerate the 4 affected
goldens, then proceed to Step 2.
