# Deep Research — Workflow / Recovery cluster (Findings D, G + 2 cross-cuts)

Investigation of four findings surfaced by `plans/eval-review-20260518-subset/synthesis.md`
covering recovery-dance regressions on the develop-adopt path, composite-type plan/yaml
shape asymmetry, atom "no second build" misclaim, and a stalled cadence-build replay.

---

## Finding D — READY_TO_DEPLOY recovery still broken on develop-adopt path

### Root cause (verified)

Two independent defects compound to produce the same agent-visible symptom (agent reads
buried atom paragraph, hesitates on destructive warning) on the develop-adopt path that
`33fb9358` fixed for launch-production:

**D1 — `develop-ready-to-deploy` atom never fires in bootstrap-adopt because the
service-snapshot has no `Status` field.** During the bootstrap discover/provision steps,
`internal/workflow/bootstrap_guide_assembly.go:162-183::planTargetSnapshots` constructs
`ServiceSnapshot` from the agent's plan with only `Hostname / TypeVersion / RuntimeClass /
Mode` populated — **`Status` is left empty**. The atom
`internal/content/atoms/develop-ready-to-deploy.md:1-10` is gated on
`serviceStatus:[READY_TO_DEPLOY]`. The match predicate at
`internal/workflow/synthesize.go:398` does
`!slices.Contains(axes.ServiceStatuses, svc.Status)` — with `svc.Status == ""` the atom
short-circuits to "no match" and never renders, even though the live service IS
`READY_TO_DEPLOY` (visible in transcript line 8). Snapshots in `compute_envelope.go::buildOneSnapshot:222`
DO populate Status from `svc.Status`, but that path runs for `handleLifecycleStatus` only,
not during a bootstrap session — `handleBootstrapComplete` calls `b.buildGuide` →
`b.synthesisEnvelope` (the empty-Status path).

**D2 — `verify::service_running` Recovery points at `zerops_logs`, never at
`zerops_import`, when `appVersion.status=WAITING_TO_BUILD` (no real build entry).**
`internal/ops/non_running_recovery.go:30-67` discriminates on
`LatestFailedAppVersionContext != nil`. That helper at
`internal/ops/failed_context.go:104-117` calls `FailurePhaseFromStatus(av.Status)` and
skips entries returning `""`. `FailurePhaseFromStatus` at
`internal/ops/deploy_failure.go:40-50` only returns non-empty for `BUILD_FAILED`,
`PREPARING_RUNTIME_FAILED`, `DEPLOY_FAILED`. **`WAITING_TO_BUILD` (the platform
state when the builder container never started — Karel's launch reproducer, also seen
in the eval transcript) returns `""` and the appVersion is silently skipped.** Result:
`failed == nil` even though the service has explicit failure history → Recovery falls
through to the `zerops_logs` branch (line 48-56). Agent gets pointed at "fetch logs",
not at "re-import with override".

### Parallel paths checked

| Path | Same defect? | Notes |
|---|---|---|
| `handleLifecycleStatus` (status flow) | **No** — `buildOneSnapshot` populates Status from live `svc.Status` | Atoms fire correctly when called via `action="status"` |
| `planTargetSnapshots` (bootstrap-active synthesis envelope) | **Yes** — Status field omitted | Confirmed root cause D1 |
| `develop` workflow synthesis envelope | Different code path; develop-active sessions DO inject live Status (via `compute_envelope.synthesisEnvelope`) | Develop-adopt forced agent into bootstrap-adopt FIRST, so D1 fires before develop ever gets a chance |
| `NonRunningRecovery` callers | All three callers (`verify`, `deploy_preflight_gate`, `dev_server` pre-spawn) use the same `LatestFailedAppVersionContext` helper | D2 affects all three uniformly |
| `gateOverrideOnFailedHistory` (import override gate) | Same helper → same blind-spot | An agent who DOES call `zerops_import override=true` on a `WAITING_TO_BUILD`-stuck service bypasses the gate (no failed history detected) and the override succeeds without diagnosis. **In this transcript that's actually the happy path the agent stumbled into.** |
| `launch-production`'s `33fb9358` fix | Different — produces `launchFirstDeployFailedResponse` for the new-project mutation pipeline's poll-failure branch. Does NOT use `NonRunningRecovery`. Develop-adopt has no equivalent surface | Launch fix is single-purpose; develop-adopt has no analogous "build never started" detector |

### Blast radius

| Layer | Specific files |
|---|---|
| Atom firing | `internal/workflow/bootstrap_guide_assembly.go::planTargetSnapshots:162` (set Status field); golden tests under `internal/workflow/testdata/atom-goldens/bootstrap/adopt/` + `internal/workflow/testdata/atom-goldens/bootstrap/classic/` will need regeneration if the atom starts firing |
| Recovery helper | `internal/ops/non_running_recovery.go:36-47` + the `LatestFailedAppVersionContext` consumer chain at `internal/ops/failed_context.go:104-117` |
| Failure-phase enum | `internal/ops/deploy_failure.go:40-50` (`FailurePhaseFromStatus`) + `internal/ops/deploy_failure_test.go:625-642` |
| Tests pinning current shape | `internal/ops/failed_context_test.go::TestLatestFailedAppVersionContext_*`; `internal/ops/verify_recovery_test.go::TestVerify_*`; `internal/ops/non_running_recovery_test.go::TestNonRunningRecovery_*` |
| Atoms with `serviceStatus:` axes | `develop-ready-to-deploy.md` (only confirmed consumer). Audit other atoms via `grep -rn "serviceStatus:" internal/content/atoms/` before changing snapshot construction |

### Fix shape

Two-part structural fix:

**D1 fix** — in `planTargetSnapshots` (and any other site that builds a `ServiceSnapshot`
from a plan target), populate `Status` from live `liveServices` (already passed to
`ValidateBootstrapTargets` via the plumbing chain). For the adopt route this means
threading `liveServices` into `synthesisEnvelope` and looking up
`liveServiceNames[devHostname].Status` — for the classic route, status is "" until
provision completes (no live service yet) which is correct (atom doesn't need to fire
on services that don't exist yet).

**D2 fix** — extend `FailurePhaseFromStatus` to recognise the "build never started"
states: `WAITING_TO_BUILD` (without a paired pipelineStart timestamp) and
`UPLOADING_QUEUED`. Alternatively, change the discriminator in `NonRunningRecovery`
from "has failed appVersion history" to "READY_TO_DEPLOY + has any appVersion history"
— a service in READY_TO_DEPLOY with any deploy attempt at all (failed or stuck) wants
the re-import recovery, never `zerops_logs` first. Invariant restored: agents in
READY_TO_DEPLOY with prior build failure (regardless of failure shape) see the
re-import Recovery on first read.

### Risks / non-obvious

- D2 changes recovery semantics for a third case beyond Karel's launch reproducer: a
  service whose ONLY appVersion is `WAITING_TO_BUILD` (queue-stuck) — the recovery hint
  shifts from "read logs" to "re-import with override", which is correct (override
  unblocks the queue), but agents who manually retry via `zerops_deploy` on a service
  with this state will now also see the import recovery hint and might choose the more
  destructive path. Mitigation: leave the recovery hint as `zerops_import` but state
  `WAITING_TO_BUILD detected — try git push first` in the wouldDestroy guidance.
- D1 fix requires snapshot construction to consume live API state at every bootstrap
  step (currently plan-driven only). Adds an API call to the discover→provision
  transition. Acceptable cost — bootstrap already calls `client.ListServices` in
  `ValidateBootstrapTargets`.
- Atom-fanout regression risk: once `develop-ready-to-deploy` starts firing in
  bootstrap-adopt, the `bootstrap-env-var-discovery` atom's READY_TO_DEPLOY paragraph
  (the "buried" one — `internal/content/atoms/bootstrap-env-var-discovery.md:20`)
  becomes redundant. Strip it from `bootstrap-env-var-discovery.md` in the same commit
  to avoid double-render.

### Verification test

- `internal/workflow/bootstrap_guide_assembly_test.go::TestSynthesisEnvelope_PopulatesServiceStatus` — assert that a bootstrap-adopt envelope built from a plan where dev-hostname matches a live service in `READY_TO_DEPLOY` produces a snapshot with `Status == "READY_TO_DEPLOY"` (currently it would assert `Status == ""`).
- `internal/workflow/scenarios_test.go` — add a pinned scenario for bootstrap-adopt + service-in-READY_TO_DEPLOY and assert `develop-ready-to-deploy` atom fires (currently no scenario exercises this — gap).
- `internal/ops/non_running_recovery_test.go::TestNonRunningRecovery_ReadyToDeployWithWaitingToBuildAppVersion_RecoveryToImport` — fixture an `appVersion.Status=WAITING_TO_BUILD` + live `READY_TO_DEPLOY` and assert Recovery.Tool == "zerops_import".
- `internal/ops/failed_context_test.go::TestLatestFailedAppVersionContext_RecognizesWaitingToBuild` — same scenario, assert helper returns non-nil context (currently nil).

---

## Finding G — Composite-type plan-vs-yaml shape asymmetry

### Root cause (verified)

Plan submission validator and import-yaml schema have different strictness for the
same `type:` field; the strict path (plan) demands the fully-qualified
composite-string form (`ubuntu/nodejs@22`, `postgresql:single@18`), the permissive path
(import yaml) accepts BOTH bare (`nodejs@22`, `postgresql@16 + mode: NON_HA`) and
fully-qualified forms.

**Plan validator** at `internal/workflow/validate.go:387-397::typeExists` does literal
string match against `liveTypes[].Versions[].Name`. The platform API populates
`Versions[].Name` with the canonical `ubuntu/nodejs@22` / `postgresql:single@18`
strings (verified in transcript `availableStacks` block at
`eval/behavioral/runs/20260518-160251/recover-failed-buildfromgit-missing-dep/transcript.jsonl:10`).
Bare-form input falls through `typeExists → false → "type not found in available
service types"` at `validate.go:250`.

**Import YAML schema** declares an enum at
`https://api.app-prg1.zerops.io/api/rest/public/settings/import-project-yml-json-schema.json:73-396`
listing ONLY composite forms — but live API empirically accepts bare forms too,
verified by `internal/platform/project_admin_api_test.go:35,76,153` which use
`type: nodejs@22 + mode: NON_HA` against a real Zerops endpoint and SUCCEED. Recipe
templates rely on this leniency:
`internal/knowledge/recipes/python-hello-world.import.yml:15,28,43` uses
`type: python@3.11` and `type: postgresql@16 + mode: NON_HA`. The schema is
**misleading** — it documents the strict enum but the API itself is permissive.

**`mode:` field deprecation:** the schema explicitly says
`"mode": { "type": "string", "description": "Deprecated, use Type version only." }`
at line 1400-1403. So the agent's complaint "I put `mode: NON_HA` on the dependency
block, and it silently disappeared" is correct semantics by the schema's own
documentation: mode is deprecated, the type-version is the canonical encoding. But
the plan validator at `validate.go:338-345` accepts `mode:` AND auto-defaults to
`NON_HA` when omitted (`defaulted = append(defaulted, dep.Hostname)`), then the import
yaml generator uses the type-string form (`postgresql:single@18`) → the agent sees
their input echoed back as `NON_HA [defaulted]` but the actual import uses the
type-suffix. Two valid encodings of the same fact, neither obviously preferred from
the agent's view.

`7e4cd5aa` (forbid `mode` on runtime services) fixes ONE narrow case — runtimes
where `mode` is a Cartesian-product accident — but doesn't address the underlying
asymmetry: plan and yaml use different representations of the same concept.

### Parallel paths checked

| Path | Same shape? | Notes |
|---|---|---|
| Plan `runtime.type` field | Composite only (`ubuntu/nodejs@22`) — strict validation against live catalog | Source of friction |
| Plan `dependencies[].type` field | Composite (`postgresql:single@18`) OR base (`postgresql@18` + `mode:`) — both valid (mode-defaulting kicks in) | Two-form asymmetry within the plan itself |
| Import YAML `services[].type` field | Schema enum is composite-only; live API permissive (accepts both) | Schema lies about strictness |
| Recipe import yaml templates | All use bare form (`python@3.11`, `postgresql@16`) | Inconsistent with plan validator |
| `availableStacks` formatter (`internal/knowledge/versions_format.go:94`) | Outputs composite strings (`alpine/nodejs@22, ubuntu/nodejs@22`) | Correct — but only matches plan validator, not the recipe yaml the agent sees right next to it |
| Atom `bootstrap-classic-plan-dynamic.md` guidance | Tells agent to "match a string from availableStacks" — only addresses the plan path | Doesn't bridge plan↔yaml gap |
| `7e4cd5aa` runtime-mode forbid | Adds 8-line callout in `bootstrap-provision-rules.md:52-59` | Symptom-axis fix; doesn't address plan↔yaml asymmetry |

### Blast radius

| Layer | Files |
|---|---|
| Plan validator | `internal/workflow/validate.go:38-397` (BootstrapTarget, RuntimeTarget, ValidateBootstrapTargets, typeExists) |
| Plan → yaml composition | `internal/workflow/bootstrap_guide_assembly.go` + `internal/workflow/recipe_override.go` (RewriteRecipeImportYAML) |
| Stack catalog formatter | `internal/knowledge/versions_format.go:94::FormatServiceStacks` |
| Recipe yamls | `internal/knowledge/recipes/*.import.yml` (every recipe — most use bare form) |
| Guidance atoms | `bootstrap-classic-plan-dynamic.md`, `bootstrap-provision-rules.md`, `bootstrap-adopt-discover.md`, `develop-add-managed-dep*.md` |
| Tests | `internal/workflow/validate_test.go` (plan shape pins); `internal/workflow/recipe_override_test.go`; `internal/platform/project_admin_api_test.go` (live yaml shape) |

### Fix shape

Pick **one canonical encoding** and normalize at the boundary. Per pre-production
policy (no compat shims), the structurally correct shape is:

1. **Canonical type string is the composite form** (`ubuntu/nodejs@22`,
   `postgresql:single@18`). The `mode:` field on the import yaml dep block is dropped
   entirely (already deprecated per schema; agent confusion comes from accepting it).
2. **Plan validator normalizes bare-form input** at `validate.go::typeExists` by
   trying `<bare>` → `<bare>` (no match), `ubuntu/<bare>` (try ubuntu prefix), then
   `<base>:single@<ver>` (managed default). On match, mutate `target.Runtime.Type` /
   `dep.Type` to the canonical form so downstream sees only composites.
3. **Recipe yamls regenerated** with composite type strings (the API accepts both, so
   no live breakage; tests pin the canonical shape).
4. **`mode:` field at dep block** — plan validator REJECTS it (not silent-default).
   Agent must encode via type-string (`postgresql:single@18` or `postgresql:ha@18`).
   Atom guidance updated to "type string carries mode for managed services; no
   separate `mode:` field anywhere".
5. **availableStacks formatter** stays composite — it's already correct.

Invariant restored: one type-string format, one place to encode mode for managed
services, plan↔yaml symmetry.

### Risks / non-obvious

- Live test fixtures in `project_admin_api_test.go` use bare-form input — they'd need
  regeneration. The tests verify the *platform* accepts bare form, which we don't
  break by normalizing in ZCP — but if Zerops ever tightens the API to match its
  schema strictly, those tests would silently break. Mitigation: switch fixtures to
  composite (the canonical form) now.
- Recipe yaml regen is a coordinated change: `zcp sync push recipes <slug>` would PR
  the upstream Strapi content. Need to confirm no Strapi-side consumer depends on the
  bare form before pushing.
- The `priorContext` echo-back of `NON_HA [defaulted]` is visible to the agent in the
  plan response. After the fix, omitting `mode` becomes the only valid input (since
  the type-string already encodes it). Update echo-back to elide mode field entirely.
- `7e4cd5aa` runtime-mode forbid stays correct — runtimes never had a `mode:` field,
  and now neither do managed services. The atom paragraph generalizes from
  "runtime-only" to "no `mode:` field anywhere in import yaml" with `7e4cd5aa`'s
  paragraph rewritten accordingly.

### Verification test

- `internal/workflow/validate_test.go::TestTypeExists_NormalizesBareToComposite` — input `nodejs@22`, expect mutate to `ubuntu/nodejs@22` (default OS), pass.
- `internal/workflow/validate_test.go::TestValidateBootstrapTargets_RejectsModeOnDep` — input dep `type: postgresql@16` + `mode: NON_HA`, expect error pointing at composite form.
- `internal/workflow/recipe_override_test.go::TestRewriteRecipeImportYAML_ProducesCompositeTypeStrings` — assert all `type:` values in rewritten yaml match composite enum.
- Atom golden regen for `bootstrap-classic-plan-dynamic.md` + `bootstrap-provision-rules.md` with the updated guidance.

---

## Cross-cutting 1 — Cross-deploy "no second build" claim is misleading

**Finding:** Three atoms claim cross-deploy "packages the dev tree without a second build":

- `internal/content/atoms/develop-first-deploy-promote-stage.md::"No second build — cross-deploy packages the dev tree straight into stage"`
- `internal/content/atoms/develop-close-mode-auto-standard.md::"Cross-deploy packages the dev tree into stage with no second build"`
- `internal/content/atoms/develop-standard-unset-promote-stage.md::"Cross-deploy packages the dev tree without a second build"`

The claim is **factually wrong**. Every `zerops_deploy` call (self OR cross) routes through
`internal/ops/deploy_ssh.go::DeploySSH` → `internal/tools/deploy_poll.go::pollDeployBuild:37::ops.PollBuild`,
which polls a real `event.Status` and reports `BuildStatus = event.Status` (line 44).
There is no skip-build branch for cross-deploy. The transcript evidence
(`cross-deploy-stage-promote-from-dev` self-review per synthesis L71-72) confirms
`buildStatus: ACTIVE, buildDuration: 1m9s` — a full build did run.

Semantically the atom likely meant "no second SOURCE FETCH" or "no second
`buildCommands` execution" — but Zerops always re-runs the build container for the
target service, including `buildCommands` per the target setup's zerops.yaml. For
agents whose ask is "promote artifact without rebuilding" (Karel's pipeline-promote
ask), the atom is misleading enough to lead to incorrect decisions.

**Layer:** atom content (LLM-facing prose). Not a code defect.

**Fix shape:** Rewrite the three atom lines to describe what actually happens:
"Cross-deploy uses the dev container's source tree as the build input (no re-clone,
no re-run of dev's `buildCommands`), then runs stage's `buildCommands` and starts the
target with stage's `run.start`. A new build executes — `buildDuration` reflects
stage's build, not dev's." If "skip rebuild for code-identical promote" is a desired
*feature* not a current behavior, file it as a separate platform-feature backlog;
don't paper over with misleading guidance.

**Verification:** Lint rule in `internal/content/atoms_lint.go` — forbid the phrases
"no second build" / "without a second build" / "without rebuilding" anywhere in atoms.
Pinned by atom-authoring lint test. Three atom edits + regen goldens.

---

## Cross-cutting 2 — `cadence-multiservice-build-run2-replay` timeout characterisation

**Finding:** Synthesis claims "stalled at 49s of work" — this is misreading the
metric. Per `eval/behavioral/runs/20260518-133328/cadence-multiservice-build-run2-replay/meta.json`:

- `scenarioWallTime: "49.684s"` — this is the agent's **first-turn** time before any
  user-sim interaction (turn-1 deliberation, not total session).
- `duration: "16m16s"` — total session time.
- `userSim.totalWallTime: "15m0.027s"` with `terminatedBy: ""` — the user sim 15-min
  wall-time budget elapsed.

Inspecting the transcript timestamps: first event `13:33:37`, last event `13:48:38` —
session ran a full 15 minutes. The LAST tool call in the transcript is
`mcp__zerops__zerops_deploy` (line 145, the very last entry), triggered with
`{targetService: "appdev", setup: "dev"}`. **No tool result follows.** The deploy
poll never completed because the eval harness's `userSim.totalWallTime` cap fired
DURING `PollBuild`.

So this is genuinely an **eval-infra timeout issue**, not an agent stall. The agent
went all the way through bootstrap → plan submission → import → mount → write
zerops.yaml + 37 files → trigger deploy — then the 15-min wall clock ran out before
the build polling returned. RC#2 (`READY_TO_DEPLOY` recovery dance) was never
exercised because nothing failed; the build was still in-flight when time ran out.

Also: the self-review from this run is rich in OTHER findings (composite-type
asymmetry, Prisma devDep, dev-mode 502 expected, AskUserQuestion harness rejection).
Don't discard it — it's a primary source for Finding G.

**Layer:** eval harness wall-time budget.

**Fix shape:** Increase `userSim.totalWallTime` budget for build-path scenarios
(20-25 min for multi-service first-deploy) OR split the scenario into two phases
("bootstrap + write code" then "deploy + verify" with resume between). Don't expect
the multi-service deploy poll to fit in 15 minutes — 1m9s per service × 8 services
plus build queue latency is already at the boundary.

**Verification:** `eval/behavioral/scenarios/cadence-multiservice-build-run2-replay.md`
frontmatter — bump `wallTimeBudget` or equivalent harness knob to 25min for this
scenario. Re-run; expect deploy to complete; check whether RC#2 actually fires (the
fixture is `seed: empty` — no pre-existing failed appVersion — so RC#2 path may need
a different scenario seeded with `seed: settled` + a `READY_TO_DEPLOY` service to
genuinely exercise the dance).

---

## Summary

**Priority order** (largest agent-time savings + structural correctness):

1. **G — Composite-type asymmetry** (highest impact: 3 sessions cited as biggest pain
   point; structural fix narrows two surfaces into one). Touches plan validator +
   recipe yamls + atom guidance; structural correctness improvement that subsumes
   `7e4cd5aa`'s narrow runtime-mode patch.

2. **D — READY_TO_DEPLOY recovery dance** (medium impact: every adopt-with-broken-service
   session hits it; two-part fix at D1 [snapshot Status field] + D2 [recognise
   WAITING_TO_BUILD]). Smaller fix, but compounding parallel-path defect — both halves
   of the discriminator chain are blind to the same platform state.

3. **Cross-cutting 1 (atom "no second build" misclaim)** — atom-content fix, three
   atoms + lint rule. Low effort, high clarity gain.

4. **Cross-cutting 2 (eval-infra timeout)** — characterisation only; RC#2 coverage gap
   needs a fresh scenario with `seed: settled` + READY_TO_DEPLOY fixture, not just a
   timeout bump.

**Cross-cutting concern that ties findings together:** The recurring pattern is
**representational asymmetry — the same fact encoded two ways depending on which
boundary the agent crosses**. D1 (snapshot Status vs. live svc.Status), D2
(WAITING_TO_BUILD vs. BUILD_FAILED), G (composite type-string vs. bare type +
mode), and CC1 (atom prose vs. platform reality) are all instances of "two valid
encodings, only one normalized at the surface the agent reads". Fix shape across
all four is the same discipline: pick one canonical encoding per concept, normalize
at every boundary, and remove the alternate form (no compat-shim accommodation —
pre-production policy).
