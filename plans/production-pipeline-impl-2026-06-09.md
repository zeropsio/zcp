# Production pipeline — implementation plan (STREAM A, 2026-06-09)

Synthesis of: open-work-compact-2026-06-09.md STREAM A + production-pipeline-review-2026-06-05.md
+ real-bugs-response-audit-2026-06-05.md + spec-launch-production-platform-spike.md, cross-checked by
a 9-agent deep-map workflow (every item confirmed against code) + 2 adversarial verify passes
(A-P0-1, A-P1-5) + independent Codex (gpt-5.5) design review. All findings code-confirmed unless noted.

## Karel's locked decisions (2026-06-09)

1. **A-P0-2 spike** → **authorize one billable SERIOUS create now** — definitive live read-back as part
   of Phase 3 (needs an admin/launch-window token from Karel; project-scoped ZCP_API_KEY can't create).
2. **A-P0-3 customDomain** → **hard-delete** the field (Karel owns the trade-off: a stale saved launch
   call passing `customDomain` fails `additionalProperties:false` validation; accepted — never worked).
3. **GPS-5 GIT_TOKEN** → **Option A: relocate to service-level secret** on the push-source (systemic;
   touches git-push deploy wiring → full deploy+e2e regression + one-way auto-relocate migration).
4. **A-P1-4 one-collect** → **backlog** the XL ZCP-as-GitHub-actor build; ship the cheap UX gaps
   (LP-2/GPS-3/J1) now; gh-CLI stays the trust boundary; B1's gh-CLI path SURVIVES as fallback.

The chain is git-push-setup → build-integration → export → launch-production. This plan fixes
**production correctness first**, then credential/UX friction, then terminal-surface ergonomics, and
**backlogs two over-scoped items** the verify passes proved infeasible/unjustified as drawn.

---

## Phase 0 — operational (Karel's action, not code)

Rotate the leaked fine-grained PAT (`github_pat_11ARR63TQ0AM3jIR8NtfTk_…`) captured in
`eval/behavioral/runs/**/transcript.jsonl`. B10 stopped NEW echoes; already-captured copies are live.
Granting it `Pull requests: write` on zeropsio/zcp also unblocks the `fix/response-audit-real-bugs` PR.

---

## Phase 1 — A-P0-1: BuildIntegration earned state (M, self-contained)

**Defect (confirmed):** `workflow_build_integration.go:199-208` stamps `meta.BuildIntegration=bi` the
instant the handler runs (GitPushState==configured being the only gate) and returns
`status:"configured"` + a `nextStep` listing the 4 still-undone manual steps. Nothing verifies the
GitHub-side reality. The launch gate (`launch_source_control_gate.go:225`) reads any non-`none` value
as "CI wired" and **suppresses** the build-integration-recommended warn; `trackTriggerMissingWarning`
(`deploy_local_git.go:358`, `deploy_git_push.go:473`) likewise silences "no build will fire". The
test seed helper itself encodes the shortcut (`launch_source_control_gate_helpers_test.go:33,47`).

**State model (chosen):** keep the enum (`none`/`webhook`/`actions` — it names *which* integration, a
genuine user choice) and add a sibling **`BuildIntegrationVerifiedAt string omitempty`** to
ServiceMeta. The enum is the *declared intent*; `VerifiedAt` is the only thing the launch gate trusts.
Migration is XS and zero-code: exact precedent is `ProvisionedFromGit` (`service_meta.go:83`) — an
omitempty field added with zero-value-safe load via the single `parseMeta()`; old on-disk metas load
`VerifiedAt=""` = unverified = warn = **the safe direction** (more honest warnings, never a new block).

**The "earn" signal (verify-corrected — the map's original was impossible):**
- ❌ `git ls-tree -r <remoteURL>` / `git archive --remote` do **not** work (ls-tree reads the local
  object DB; ls-remote returns only refs; archive is disabled on github.com). Drop entirely.
- ✅ **actions** = stat the workflow file in the push-source working tree
  (`test -f /var/www/.github/workflows/zerops.yml` over the SSH the gate already holds; local = CWD
  stat). This proves *present-and-pushed* **only because** push-proof checks 4-5 (DirtyTree +
  LocalHead==RemoteHead, gate order 9 runs them before check 6) guarantee HEAD is pushed + tree clean.
  This dependency MUST be stated; a bare file-stat without the push-proof predecessor falsely verifies
  an uncommitted file. Secrets are NOT gate-verifiable → `VerifiedAt` for actions means
  *workflow-file-present-at-pushed-HEAD*, never *secrets-present*; the warn copy says "secrets verified
  only at first CI run" so the agent never reads VerifiedAt as "CI will succeed".
- ✅ **webhook** = read `client.GetServiceStackIntegrationStatus(buildTarget)` (the standard,
  non-admin client method exists — `platform/client.go:89`, `zerops_integration.go:25`). The dashboard
  OAuth webhook creates a platform-side integration on the service stack; `IntegrationConfigured` →
  earned. (This corrects the verify pass, which assumed webhook is unverifiable — it is, via platform
  read, no git artifact needed.)

**Stamp-site discipline (verify-critical):** stamp `VerifiedAt` ONLY on the **publish-side** gate
(`runPublishSideSourceControlGate`), never inside the shared `validateLaunchSourceControl` (called from
the read-side poll path whose doc-comment says it must not mutate/audit — a meta-write there is a
hidden poll-loop mutation + parallel-path divergence). Use locked RMW (`UpdateServiceMeta`).

**Sub-phases (independently shippable):**
- **1a** add `BuildIntegrationVerifiedAt` field; `NewServiceMeta` leaves it zero (no constructor
  change). Pure additive; `service_meta_test.go:871` forbidden-fields test stays green (omitempty).
- **1b** flip the warn predicate: gate check-6 (`launch_source_control_gate.go:225`) +
  `trackTriggerMissingWarning` (both deploy paths) → fire when `BuildIntegration==none ||
  VerifiedAt==""`. Update the seed helper to set VerifiedAt on the happy-path actions seed.
- **1c** implement the earn (actions working-tree stat publish-side after push-proof; webhook platform
  integration read) + stamp via RMW. The only real engineering risk; gate this sub-phase.
- **1d** reword handler response `status:"configured"` → `"declared"` + gate blocker message +
  webhook-declared message. **Re-target the over-scoped atom audit**: `setup-build-integration-actions.md`
  is already honest ("records the choice and the handoff shape") — the unearned truth lives in the
  handler status + gate, not the atom.

**Pinning:** `TestHandleBuildIntegration_Confirm_DoesNotStampVerified` (core invariant);
`TestValidateLaunchSourceControl_DeclaredButUnverified_WarnsNotSuppressed` +
`_Verified_SuppressesWarn`; update `workflow_phase5_test.go:261,380`, `live_build_integration_test.go:129`,
`launch_source_control_gate_test.go:156,186` + the seed helper.

---

## Phase 2 — A-P0-3: customDomain honesty (S)

**Defect (confirmed):** input `workflow.go:169` jsonschema promises "ZCP synthesizes DNS records +
verification probes"; the handler only round-trips it into `launchInputsEcho`
(`workflow_launch_production.go:1018,1370`); atoms `launch-scope-prompt.md:30` + `launch-post-checklist.md:22`
+ spec §1181 repeat the promise. **Implemented nowhere.**

**Backward-compat constraint (confirmed):** the MCP SDK (modelcontextprotocol/go-sdk v1.5.0 via
google/jsonschema-go ForType) sets `additionalProperties:false` on struct inputs and **validates** —
so a hard-delete of the field would make a client that still passes `customDomain` fail validation
mid-launch. Per the mandatory user-facing backward-compat rule (CLAUDE.local.md), do
**tolerate-and-ignore** (recommended), not hard-delete:
- Reword the `customDomain` jsonschema to "accepted but unused — add custom domains in the Zerops
  dashboard after launch" (the field stays accepted; the lie is gone).
- Remove the DNS-synthesis claims from both atoms + spec §1181 (regenerate goldens).
- Remove `launchInputsEcho.CustomDomain` (it's output-only forensic echo — dropping it can't break an
  input contract).

Honest implementation is NOT cheap (real DNS-record/routing creation is operator-owned per P-PROD-2),
so deleting the *promise* is the right tell==check fix. **Decision:** tolerate-ignore vs hard-delete.

---

## Phase 3 — A-P0-2: SERIOUS core + region (S code, M live-spike)

**Defect (confirmed):** `launch.go:146-158` emits a project block of only name+tags+envVariables;
`project_admin.go:271` discards `opts` (`_ = opts`); `CreateOpts{Location,Tags}` passed at
`workflow_launch_production.go:634` is thrown away. **Every NEW prod project is created LIGHT** and in
the account-default region (input.Region is computed then dropped).

**The lever (corrected — the spike doc was wrong):** SERIOUS is NOT set via `project.mode` (absent
from the import schema; `additionalProperties:false` would reject it) and NOT via a CreateOpts API
field (the import API takes only a yaml string). It is the import-yaml field **`project.corePackage`**
(enum `LIGHT|SERIOUS`, default LIGHT) — and **`project.location`**.

**Region reality (Karel, 2026-06-09 + live-schema verified):** Zerops now has **THREE** regions —
`eu-central`, `us-east-1`, `us-west-1` (confirmed against the LIVE import schema). **ZCP's embedded
schema is STALE — it lists only two (`us-west-1` missing).** So: (a) `make schema-sync` to refresh the
embedded enum; (b) the region list ZCP OFFERS must DERIVE from the live `project.location` schema enum
(single source of truth), never a hardcoded subset — region is a legitimate full-choice menu (where to
run prod), not a trap-laden validation set. The drift sentinel (`zcp schema check`) should have flagged
this; note in the commit.

**Core package (Karel, 2026-06-09):** default + recommended = **SERIOUS** for prod, but the user CAN
choose **LIGHT** for a production project — allowed, not blocked, just nudged. So `corePackage` is an
overridable scope input (default SERIOUS); LIGHT-for-prod yields a warn-severity recommendation, never
a hard fail.

**Fix:**
- **3a** in `launch.go` BuildLaunch VariantLaunchNew project block: emit `corePackage`
  (LaunchBundleInputs.CorePackage, composer default SERIOUS, overridable to LIGHT) + `location`
  (LaunchBundleInputs.Location). Add both fields to LaunchBundleInputs; route `input.Region` → Location.
- **3a-region** scope-prompt offers all live-schema regions (3 today); `input.Region` validated against
  the live `project.location` enum; no eu-central-only hardcode. Refresh embedded schema first.
- **3b** delete the dead `_ = opts` + misleading comment. **Decision:** drop CreateOpts fields entirely
  (clean-code rule) vs keep an empty param — drop touches `project_admin_api_test.go` live-test sites.
- **3c** (XS, defense-in-depth) readiness check `prod-core-package` mirroring
  `readinessCheckSubdomainDisabled` — passes when corePackage=SERIOUS OR when LIGHT was explicitly
  chosen (warn "SERIOUS recommended for prod"); never blocks.
- **3d** (live, gated `ZCP_E2E_PROD_LAUNCH`) **region × core test matrix — Karel authorized billable
  SERIOUS creates (no problem, multiple OK):** for EACH of the 3 regions, BuildLaunch a minimal YAML +
  create + `GetProject` read-back asserting `mode==SERIOUS` AND the requested region landed; plus one
  LIGHT-override create read-back asserting `mode==LIGHT`. Each `t.Cleanup` DeleteProject. **Why
  mandatory:** spike A.10 proved the platform silently drops `project.userRoles[]` from import yaml — so
  corePackage/location *could* also be dropped; the read-back is the only proof, per region.
- **Billing note:** SERIOUS is the recommended prod default but a real cost change — surface in
  `launch-post-checklist` (and that LIGHT is a deliberate cheaper choice the user can make).

**Pinning:** unit `TestBuildLaunch_CorePackageSerious` + `_CorePackageLightOverride` + `_Location`
over the composed YAML; `TestRegionOptionsDeriveFromSchemaEnum` (offer == live enum, not hardcoded).

---

## Phase 4 — A-P1-6: credential/UX gap cluster (proactive discipline)

These reduce credential friction WITHOUT making ZCP a GitHub actor (that XL question is Phase B).

- **LP-2 (S):** the source-control-required blocker (git-push-unconfigured / remote-mismatch in
  `launch_source_control_gate.go:498-519`) + its atom tell the agent to run git-push-setup but never
  "repoUrl + PAT are USER-OWNED — ask (AskUserQuestion) and WAIT, never invent". 4 corpus runs show
  agents fabricating PATs/URLs. Add the wait-for-user STOP (parity with launch-mutation-key-required;
  B6 added it on ERRORS, this is the proactive BLOCKER side). Single-owner the contract const (topology).
- **GPS-3 (M):** container confirm hard-requires inline `gitToken` (`workflow_git_push_setup.go:434`);
  if the user won't paste a raw PAT, dead-end. Add an env-token-probe fallback: when gitToken omitted +
  remoteUrl set, probe with the container's existing `$GIT_TOKEN` (reuse the `.netrc`-from-$GIT_TOKEN
  pattern, `ops/deploy_git_push.go:42`). **Coupled to GPS-5** — a project-singleton token may be
  mis-scoped for this repo; gate behind explicit intent or resolve GPS-5 first.
- **GPS-5 (L, DECISION):** GIT_TOKEN is a project-singleton (`env.go:261`); the atom mandates a
  single-repo-scoped PAT → two push-sources to two repos collide; also dumped by
  `discover includeEnvValues=true` (B10d). **Option A** (recommended, systemic): relocate GIT_TOKEN to
  service-level secret on the push-source (`ops.EnvSetSensitiveService`) — fixes the collision AND the
  read-back leak, but touches git-push DEPLOY wiring (`ops/deploy_git_push.go`) → full deploy+e2e
  regression + one-way auto-relocate-on-next-git-push migration. **Option B** (cheap): document the
  project-singleton limitation + warn on second setup. Karel picks.
- **J1 (L):** the probe (`ops/git_auth_probe.go:43`) checks ONLY auth (`git ls-remote HEAD`), never
  local-vs-remote HEAD divergence / shallow / ahead-behind → recipe-bootstrapped repo reconciliation is
  25 freestyle Bash calls that re-trip the gate. Extend the probe (BOTH container + local, single ops
  owner consumed by the launch gate) to read divergence and return a structured reconcile choice.
  Open: confirm WHERE the template history originates (service_git_init does `git init`, not clone).

---

## Phase 5 — A-P2: terminal/return surfaces (ergonomics)

- **EX-1/F54 (S):** author `export-compose-ready.md` (frontmatter `exportStatus:[compose-ready]`,
  already in the enum) + golden + add the compose-ready row to `export-intro.md`'s status table.
- **J3/F43 (M) — the targeted fix, NOT the A-P1-5 redesign:** add `pipelineSummary` (derived from
  `state.PipelineConfigurations`: per-runtime configured/skipped/pending + deepLink) to
  launchProductionResponse + launchActiveEnvelope; un-drop `ImportedServices` on launchLaunchedResponse
  + renderLaunchTerminalRecovery (LP-6). All additive omitempty; re-pin P-LP-1 launchKey-absence on the
  new fields. (Aligns with flow-eval-fix-master-plan Edits 6+7.)
- **LP-4 (M):** ready-to-launch is empty consent — run the readiness rubric + a bundle/service preview
  into the !publishing/haveCurrent branch before asking for the launchKey; degrade gracefully when
  source not yet readable.
- **LP-5 (S):** push-unsupported mode dead-ends — call `meta.PushSourceCheckFor(hostname)` at the top
  of validateLaunchSourceControl; on `PushSourceModeUnsupported` emit `mode-unsupported-<host>` (recover:
  expand to standard pair) instead of unsatisfiable git-push guidance.
- **LP-8/J4 (S):** populate `Recovery.Args` scope=[devHost,stageHost] on the launch service-not-bootstrapped
  blocker + export meta-missing error so redirected adopt skips its pairing question. (Open: RecoveryHint.Args
  is string-valued — encode []string as comma-join or extend the shape.)
- **BI-2/F34-F61/BI-3/BI-NOOP-1 (M):** add `meta.PushSourceCheckFor` gate (stage-half parity with
  git-push-setup); make the noop re-call recompute + return the full enriched handoff (stateless); omit
  `alternateWorkflowFiles` when setupMandatory (buildSetup!=buildHost). Develop-side BI — **not Aleš's
  recipe scope** (confirmed).
- **EX-2..6 (L, split):** per-file error provenance (fix — tag ValidationError with source file);
  soften `meta.IsComplete()` adopt gate to allow export when live discover proves the service real
  (verify Mode still resolves); fix export→git-push-setup handoff call shape; trim the 23.6 KB
  classify-prompt via auto-classify. **EX-3** strict-vs-lenient zerops.yaml validator seam → **BACKLOG**
  (validator-owner contract change; flag to Karel + B2-related).
- **J5/J6 (S):** make `launch-delete-key` conditional (revoke-now only when no pipeline blockers pending;
  else keep-key + re-call) — verify executeLaunchPipelineResume is truly GetStatus-only (if so, J5 is
  guidance-wording only); add the config-only exit (edit → action=close → re-call) to validation-failed.
- **LP-7:** already-fixed by B8 (full reset section clean) — close the item.

---

## BACKLOG (verify-proven over-scoped / gated on a Karel decision)

- **A-P1-5 grand ProductionState redesign — INFEASIBLE as drawn.** The headline ("status against the
  PRODUCTION project returns ProductionState via by-targetProjectID lookup") cannot work: `projectID` +
  `stateDir` are resolved ONCE at server boot from the source token's scope + cwd; the prod project has
  no MCP session/cwd/state-dir, so `projectID==TargetProjectID` never co-occurs; the dual-index has no
  caller. Also `SourceControlReady` can't be "derived" — the gate result is never persisted (deriving
  needs a stamp, contradicting the rationale, or a read-time gate re-run, violating read-only status).
  And the develop-status overlay is markdown (`textResult`), not a JSON envelope — no structured
  carrier. **Ship instead:** the concrete additive wins already in Phase 5 (F43 pipelineSummary +
  un-drop ImportedServices). Promote the redesign only after a product decision: does ZCP gain a
  cross-project / explicit-`projectID` status path at all? (P-LP-2 admin-client implications.)
- **A-P1-4 one-collect credential flow (XL) — GO/NO-GO on ZCP-as-GitHub-actor.** ZCP has zero GitHub
  API client today; all GitHub work is the agent's `gh` CLI. The end-state (collect ONE GitHub
  credential at git-push-setup, scopes by integration; in-request set repo secrets via GitHub REST with
  NaCl sealed-box + commit the workflow; keep gh-CLI as fallback) is the right consolidation but makes
  ZCP make authenticated WRITES to the user's GitHub account — a new trust surface + external API
  coupling. Codex + the map agent: ship AFTER P0; it does NOT redo B1 (B1's gh-CLI path SURVIVES as
  fallback — deleting it would be a scope-cut). launchKey stays the legitimate 4th ask at the
  project-create boundary (out of one-collect scope). **Decision gates the whole XL build.**
- **EX-3** strict-vs-lenient zerops.yaml validator-owner seam.

---

## Sequencing + gates

1. **Phase 0** rotate PAT (Karel).
2. **Phase 1** A-P0-1 earned state (1a→1b additive/safe; 1c earn = gated; 1d copy). flow-eval:
   `git-push-setup-with-cicd-method-prompt`.
3. **Phase 2** A-P0-3 customDomain honesty (cheap, removes a lie).
4. **Phase 3** A-P0-2 SERIOUS+location (code), then 3d live read-back (gated on Karel's spike authorize).
5. **Phase 4** A-P1-6 credential UX (LP-2 first; GPS-5 decision gates GPS-3; J1).
6. **Phase 5** A-P2 ergonomics.
7. **Backlog** A-P1-5 redesign, A-P1-4 one-collect, EX-3 — each gated on a Karel decision.

Per bug: RED → GREEN → goldens → `make lint-local`. After each phase: flow-eval round-trip on the
touched scenario(s). NEVER `make release`/`make install` without explicit ask. Aleš-scope: none of
P0–P5 touch `internal/recipe/` / `workflow_recipe.go`; BI-2 is develop-side BI (confirmed clear).

## New spec invariants to add (pin with tests)

- P-LP-13 BuildIntegrationVerifiedAt earned-or-warn (actions=workflow-file-at-pushed-HEAD,
  webhook=platform integration-status); stamp publish-side only.
- P-PROD-3 launch NEW project emits corePackage=SERIOUS + location; live read-back pins the honor.
