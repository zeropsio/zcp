# Workflow Family Audit — Drift, Contradictions, Dead Code + Remediation Roadmap

**Date:** 2026-05-28
**Scope:** bootstrap/greenfield · adopt · export · launch-production + their shared spine (engine/session/plan/envelope/atoms), env handling, deploy/develop-side actions, topology + ServiceMeta state model.
**North-star measured against:** `plans/workflow-family-architecture-2026-05-14.md` ("single canonical path per concern", "reuse existing knowledge, not parallel implementations").
**Out of scope:** recipe-generation engine internals (`internal/recipe/`, `workflow_recipe.go`, recipe atoms) — Aleš's ownership. Flagged where a finding's deletion crosses that seam.

**Provenance:** produced by a 53-agent dynamic workflow — 9 area readers + 7 plan-fidelity audits → 6 cross-cutting hunters → 30 adversarial verifiers (default-refute) → synthesis. 74 raw findings → 73 deduped → 30 high/medium verified, **30 confirmed, 0 refuted**. Every finding carries file:line evidence.

**Confidence caveat:** 0/30 refuted is unusual for a default-refute verify pass. The keystone live bug (Theme 2) was independently re-verified by hand against the code and is solid. Before acting on the *medium spec-drift* findings (Theme 9), spot-check 2-3 — they are low-stakes but the doc-vs-code direction should be confirmed case by case.

---

## 1. Executive summary — the dominant structural problem

A systemic split between **unified WRITE/STORE paths and fragmented READ/CONSUME paths.** Across all four workflows, plans repeatedly landed the canonical *store* or the foundational *lower layer* — ServiceMeta fields, a `ResolveCanonicalSetup` cascade, a 5-state `AdoptionState` enum, a `bundle.Variant` enum, an `ops.BuildGitAuthProbeCommand` primitive, an `ops/cicd` composer — **but then did NOT route the consumers through it.** The old per-call-path resolution stayed alive *beside* the new canonical one.

The result is the exact north-star violation the codebase exists to avoid: one concern (which setup applies to a hostname; where `buildFromGit:` comes from; is this service adoptable; what is the deploy-config) is answered by **4–5+ disagreeing codepaths**, with the declared-canonical resolver wired at only 1–2 of them.

This is not random rot. It is a recurring **"the last deletion phase was silently skipped"** pattern: *delete-the-fallback* (P5/P6 of multiple plans) was the hardest, last step and was repeatedly deferred-as-safety-net with self-documenting commit hedges ("kept until eval re-verifies"). Compounding it, several whole unification deliverables shipped as **dead code** — scaffolding for unifications that were never finished, now indistinguishable from live code.

---

## 2. Root pattern (one cause, many faces)

> **Foundation lands, consumers don't get rewired, fallback never deleted.**

| Concern | Canonical thing that shipped | How many divergent consumers still bypass it |
|---|---|---|
| setup-name resolution | `ResolveCanonicalSetup` | 5+ (wired at 2) |
| fresh ServiceMeta | (container path is canonical) | 3 local writers omit fields |
| deploy-dimension normalization | `CloseDeployMode` normalized at snapshot | `GitPushState`/`BuildIntegration` not; 4 ad-hoc sites |
| adoptability | `AdoptionState` 5-state enum | 4 (1 consumer; retired none) |
| git ls-remote probe | `ops.BuildGitAuthProbeCommand` | 3 stances (1 caller) |
| bundle composition | `bundle.Variant` / single `Compose` | 2 Variant enums, export half dead |
| launch status/state | `StateEnvelope`/`Plan`/session spine | launch runs its own parallel model |

---

## 3. Remediation themes (severity-ordered)

| # | Theme | Sev |
|---|---|---|
| T1 | **Setup-name resolution** — collapse 5+ resolvers onto `ResolveCanonicalSetup`; finish P5/P6 | high |
| T2 | **Local ServiceMeta construction** — one canonical fresh-meta + one normalization boundary | high |
| T3 | **Launch multi-runtime** — close read-side/scope/pipeline gating gaps before they burn a launchKey | high |
| T4 | **Launch state model** — fold into the spine OR formalize one shared stateless-narrowing primitive | med |
| T5 | **buildFromGit + git-auth-probe** — one trust model + one ls-remote primitive (export + launch) | med |
| T6 | **Dead-code removal** — delete the unfinished-unification scaffolding (owner-gated where it crosses recipe) | med |
| T7 | **Adoptability classification** — make `AdoptionState` the one classifier | med |
| T8 | **Container vs local adopt parity** — stamp `FirstDeployedAt` + correct false cascade comments | med |
| T9 | **Spec/doc reconciliation** — bring rank-3 specs back in sync with code+tests | med |

### T1 — Setup-name resolution
**Problem:** "Which `zerops.yaml` setup block applies to a hostname" is answered by `ResolveCanonicalSetup` (wired at 2 local-adopt sites only), `DeployIntent.Resolve` (hardcoded `RecipeSetupProd/Dev`, never reads the `snapshot.SetupName` fields P0 added for it), `deploy_preflight`'s own role→prod→hostname cascade, two identical launch cascades, `ops/bundle/launch.go` `legacyDefaultSetupName="prod"`, git-push hostname fallbacks, and `export_probe`'s near-verbatim copy of `PickSetupNameFromNames`. They disagree: a user who renames a stage setup via `set-default-setup` gets the real name in `deployPreFlight` but `prod`/hostname in the BuildPlan next-action, the launch bundle, and the **committed CI workflow** — producing "Cannot find corresponding setup" deploy failures and a permanently-wrong `--setup` baked into `.github/workflows/zerops.yml`.
**Direction:** Make `ResolveCanonicalSetup` (or its pure inner cascade over snapshot fields) the single resolver. `DeployIntent.Resolve` reads `target.SetupName`/`target.StageSetupName` and **OMITS** the setup arg when empty (never emits the prod/dev constant); delete `RecipeSetupProd/Dev` from the deploy path; route launch's two cascades + `ops/bundle/launch.go` default + git-push hostname fallbacks + `export_probe`'s `pick*` through the shared resolver; surface the existing `requiresSetupInput`/`staleMetaSetup` blocker on empty rather than defaulting. Completes setup-name P5/P6 — the largest single duplication surface.

### T2 — Local ServiceMeta construction (the keystone live bug)
**Problem:** Writing a fresh ServiceMeta is one concern implemented divergently: container bootstrap writes canonical `GitPushUnconfigured`/`BuildIntegrationNone`, but two local-adopt branches (`adopt_local.go` case 1 + default) omit both, persisting empty-string on-disk. The atom matcher (`synthesize.go:378`) reads `svc.GitPushState`/`BuildIntegration` **raw** via `slices.Contains`, and the snapshot path normalizes **only** `CloseDeployMode` (`compute_envelope.go:231-235`), so `empty ∉ [unconfigured,broken]/[none]` → **the entire git-push-setup → build-integration atom chain silently never fires for the exact local modes those atoms target.** `render.go` normalizes all three for *display*, masking the bug. *(Independently hand-verified 2026-05-28.)*
**Direction:** (1) a single canonical `NewServiceMeta` constructor all writers call, always stamping the three sentinels; (2) normalize the three deploy dimensions exactly once at the read/snapshot boundary — `parseMeta`-on-read is the single-path choice (it also heals existing on-disk user metas; backward-compat seam). Then delete the ad-hoc `== ""` checks in `render.go`, `launch_source_control_gate.go:220`, `deploy_local_git.go:317`. Add a local-stage atom-firing golden.

### T3 — Launch multi-runtime gating gaps
**Problem:** Multi-runtime launch is half-wired: composer (`BuildLaunch` loops), publish-side gate (loops all runtimes), and `resolveLaunchRuntimes` accept N, but the handler funnel — `missingScopeFields`, read-side source-control gate, `readAndValidateSourceState`, pipeline check — is hard-coded to single `input.TargetService`. A Promotables-only call fails scope; a 2-runtime launch where runtime B is unwired advances cleanly scope→classify→ready-to-launch, the user spends the one-shot launchKey, **then publish refuses on B** — exactly the precondition the read-side `source-control-required` status exists to surface *before* key generation. Spec §10 + CLAUDE.md claim per-runtime behavior the code does not deliver; a test comment even calls multi-runtime "a scope cut", contradicting the doc.
**Direction:** Resolve promotables at the read-side gate + scope check + pipeline check (same `resolveLaunchRuntimes` the publish-side already uses) so all runtimes validate before launchKey generation; relax `missingScopeFields` to accept Promotables-only. **OWNER-GATED:** confirm whether multi-runtime is intended live for v1 before investing — if not, scope down to single-runtime explicitly and align spec+CLAUDE.md rather than leave half-wired plumbing.

### T4 — Launch state model
**Problem:** Launch runs an entirely parallel state model (`launchState` → `.zcp/state/launch-production/`) and parallel status-recovery (`launchActiveEnvelope`), bypassing the `StateEnvelope`/Plan/session-registry spine that bootstrap/develop/status use. Compaction-survival, the typed Plan trichotomy, and session-registry guarantees don't apply to launch; any spine improvement must be hand-ported. The plan's Phase 4a/4b promotion (`WorkflowState.Launch` + Version bump + side-channel deletion) never shipped. Export independently ships raw `nextSteps[]` arrays — two bespoke stateless mechanisms for one "what's my status after compaction" concern.
**Direction:** Decide one of two end-states (see Open Decisions): (A) fold launch into `WorkflowState.Launch` + StateEnvelope + a launch BuildPlan branch (the §9.6 plan), bumping Version with a one-way idempotent migration; or (B) formally accept export+launch as stateless-narrowing workflows and extract ONE shared stateless-narrowing dispatch+recovery helper both consume, putting launch into the `immediateWorkflows` registry alongside export. Either way, delete the bespoke second mechanism.

### T5 — buildFromGit + git-auth-probe
**Problem:** Two adjacent defects in the same family. (1) `buildFromGit:` source-of-truth uses **opposite trust models**: export embeds the live git remote unconditionally into the bundle (`workflow_export.go:155`); launch trusts `meta.RemoteURL` and hard-fails on live drift (the recipe-template loophole guard). A real export-side exposure: `refreshRemoteURLCache`'s docstring claims the bundle uses the probe-proven URL on a configured+drift service, but the handler already captured the live value into `repoURL` and never reassigns it → export ships the recipe-template URL with only a warning. (2) The authenticated `ls-remote` probe `ops.BuildGitAuthProbeCommand` has 1 caller; launch re-implements it inline (netrc + ls-remote) + duplicates `ops.parseGitHost` as `launchPushProofHost`; export gate uses neither — three stances on "prove the remote has our HEAD", self-documented as a layering dodge that `workflow_git_push_setup.go` disproves by already importing `ops`.
**Direction:** Export `ops.BuildGitAuthProbeCommand` + `parseGitHost` as the single primitive; call from launch gate (container + local) + export gate; delete the inline netrc block + `launchPushProofHost`. Fix `refreshRemoteURLCache` so the bundle actually uses the probe-proven URL it claims. Settle buildFromGit trust as one shared model **or** document the two justified policies in spec next to both sites. Finishes git-push-flow Phase 3.

### T6 — Dead-code removal
**Problem:** Multiple whole deliverables shipped as dead code, now indistinguishable from live: `internal/ops/cicd` (`ComposeActionsHandoff` + `ZEROPS_TOKEN_STAGE/PROD`) fully built with zero callers while live build-integration uses bare `ZEROPS_TOKEN`; `bundle.Variant`'s export half + `IsExport()` vestigial beside `topology.ExportVariant`; the v2 recipe engine (`engine_recipe.go` etc.) sits in the **shared** workflow package, hard-rejected at the dispatcher yet still consuming dispatch surface and forcing every spine refactor to keep it compiling; `InitSession` non-atomic, `InferServicePairing`/`AdoptCandidate`, `FormatEnvFile`/`ServiceEnvGroup` orphans.
**Direction:** Stage deletions by ownership (see §6). Per Clean Code, none stay "just in case".

### T7 — Adoptability classification
**Problem:** The adoptable/resumable/adopted distinction is computed in 4 places (`route.go:291` `adoptableServices` with ServiceID-preferring `isSelf`, `workflow_route.go:53` inline with plain hostname match, `discover.go:164` `classifyAdoptionState` switch, `compute_envelope.go:126` snapshot-bool form). The `isSelf` divergence is a latent bug: in a local/mock env where ServiceID is empty the two route paths classify self differently. `AdoptionState` was added as a 5th representation with exactly one consumer, retiring none.
**Direction:** Export a single `classifyAdoption(meta, self) -> AdoptionState` including the ServiceID-preferring `isSelf`; have `workflow_route.go`, `discover.go`, `deriveIdleScenario` consume it. Extend the parity test to cross-pin all four (incl. the empty-ServiceID fallback).

### T8 — Container vs local adopt parity
**Problem:** For the identical adopt-an-ACTIVE-service scenario, on-disk `meta.IsDeployed()` diverges by environment: local stamps `FirstDeployedAt`, container's `writeBootstrapOutputs` never checks Status. Spec line 95 ("stamped on adoption-at-ACTIVE") is honored only locally. Compounding: `bootstrap_outputs.go:66-72` comment asserts a container adopt-time Gate A cascade that **does not exist**; `DeriveDeployed`'s comment describes local-only behavior as general.
**Direction:** Bring container adopt to parity (stamp `FirstDeployedAt` for container-adopted ACTIVE services) OR accept the `DeriveDeployed` signal-#3 compensation and correct both false comments. Owner-decision on which.

### T9 — Spec/doc reconciliation
**Problem:** ≥8 spec drifts where code/test changes landed without the paired doc edit (see findings F19-F30). The spec is the authoritative map the north-star depends on.
**Direction:** A doc-reconciliation pass (mostly doc-only): strike deleted `gitPushState='unknown'`; regenerate atom inventory + add an auto-generated count assertion; add the setup-name struct fields + replace `managedByZCP` with `adoptionState`; fix the `pickRandom`→`REPLACE_ME` form everywhere; reconcile §1.4/§1.6 with P4 up-front; build the `.env.local` lint or remove the claim; reject bootstrap `iterate` at engine level OR document the counter-bump; decide `RecipeMatch.Repo` intent; archive completed plans.

---

## 4. Confirmed findings (30) — grouped, with file:line

### High (7)
| # | Kind | Finding | Locations |
|---|---|---|---|
| F1 | dup | Setup-name resolution has 5+ divergent implementations; canonical `ResolveCanonicalSetup` wired at 2 | `setup_resolver.go:87`; `deploy_intent.go:149,158,164`; `deploy_preflight.go:80-118`; `launch_promotables.go:152-168`; `workflow_launch_production.go:904-916`; `ops/bundle/launch.go:29,77`; `deploy_git_push.go:317`; `deploy_local_git.go:88`; `workflow_export_probe.go:71,148` |
| F2 | dup | Fresh-meta writing diverges: container canonical, local writers leave GitPushState/BuildIntegration empty | `bootstrap_outputs.go:55-63`; `adopt_local.go:119-126`; `adopt_local.go:153-159`; `workflow_adopt_local.go` |
| F3 | spec-mismatch | Local writers omit the two dims → git-push-setup + build-integration atoms can NEVER fire for local modes (**keystone, hand-verified**) | `adopt_local.go:119-126,153-159`; `workflow_adopt_local.go:113-122`; `compute_envelope.go:234-235`; `synthesize.go:378`; `atoms/develop-close-mode-git-push-needs-setup.md`; `atoms/setup-git-push-local.md`; `atoms/setup-build-integration-actions.md` |
| F4 | partial | `DeployIntent.Resolve` hardcodes setup=prod/dev, never reads the `SetupName`/`StageSetupName` snapshot fields added for it (P6 never shipped) | `deploy_intent.go:149,158,164`; `compute_envelope.go:245-247`; `envelope.go:129`; `workflow_build_integration.go:454-474`; `deploy_preflight.go:80-84` |
| F5 | dead | v2 recipe engine stranded/unreachable yet wired into dispatcher inside the **shared** workflow package | `engine_recipe.go:19`; `workflow_recipe.go:16-22`; `workflow.go:555-556`; `dispatch_brief_envelope.go`; `recipe.go` |
| F6 | partial | Launch read-side source-control gate validates only `TargetService`; publish-side loops all runtimes → multi-runtime passes read-side then fails after launchKey spent | `workflow_launch_production.go:152-155`; `launch_source_control_gate.go:628`; `workflow_launch_production.go:1026-1027`; `launch_pipeline.go:159`; `workflow_launch_production.go:713` |
| F7 | dead | (dup of F5 — v2 recipe engine unreachable yet dispatcher-wired) | same as F5 |

### Medium (21) + Low (3)
| # | Kind | Finding | Key locations |
|---|---|---|---|
| F8 | contradiction | buildFromGit source-of-truth: opposite trust models export vs launch | `workflow_export.go:155-176`; `launch_source_control_gate.go:108-193` |
| F9 | dup | Authenticated `ls-remote` probe re-implemented inline in launch instead of reusing `ops.BuildGitAuthProbeCommand` | `git_auth_probe.go:37`; `workflow_git_push_setup.go:375`; `launch_source_control_gate.go:319-368`; `deploy_git_push.go:70` |
| F10 | dead | Two parallel Variant enums; §9.2 single-Compose never built; `bundle.Variant` export half dead | `ops/bundle/variant.go:25-77`; `topology/types.go:118-133`; `ops/bundle/export.go:16,119`; `ops/bundle/launch.go:54,98` |
| F11 | dup | Adoptability classified in 4 places with real `isSelf` divergence; `AdoptionState` unified none | `route.go:291-313`; `workflow_route.go:35,53`; `discover.go:157-180`; `compute_envelope.go:126-156` |
| F12 | dup | Empty-string deploy-dim normalization ad-hoc at 4+ sites instead of once at the boundary | `render.go:205-214`; `compute_envelope.go:231-235`; `launch_source_control_gate.go:220`; `deploy_local_git.go:317` |
| F13 | state-model | Launch runs a parallel state + status-recovery model instead of the canonical spine | `launch_state.go:25,117-218`; `launch_status_recovery.go:18,52,137`; `state.go:6-17`; `envelope.go`/`compute_envelope.go` |
| F14 | spec-mismatch | `setup_resolver.go` docstring claims 5 write-back callers; only 2 (local adopt) actually call it | `setup_resolver.go:84-86`; `adopt_local.go:141`; `workflow_adopt_local.go:135`; `deploy_preflight.go:80-84`; `bootstrap_outputs.go:66-72` |
| F15 | dup | Three parallel setup-name 'prod' fallback cascades, all marked 'P5 deferred' | `ops/bundle/launch.go:29,69-77`; `launch_promotables.go:152-167`; `workflow_launch_production.go:901-916` |
| F16 | partial | `RecipeMatch.Repo` + `{{recipe.repo}}` atom populated but never consumed; local-clone consumer absent | `route.go:46-50`; `recipe_corpus_store.go:50,75`; `atoms/bootstrap-recipe-local-clone.md` |
| F17 | spec-mismatch | Container-adopt comment claims a Gate A adopt-time cascade that doesn't exist; FirstDeployedAt diverges local vs container | `bootstrap_outputs.go:66-72`; `adopt_local.go:127-129`; `workflow_adopt_local.go:120-121`; `compute_envelope.go:276-278` |
| F18 | drift | Bootstrap `iterate` doesn't hard-stop/reset per spec §2.1/B5 — silently bumps counter; hard-stop is text-only | `spec-workflows.md:404-410,927`; `engine.go:205-215`; `bootstrap_guide_assembly.go:24-26` |
| F19 | spec-mismatch | spec lists deleted `GitPushState='unknown'` value | `spec-workflows.md:90,240,668`; `topology/types.go:62-80` |
| F20 | drift | atom inventory stale: spec says 74, reality 109; launch+scaffold prefixes absent | `spec-knowledge-distribution.md:23,325-333` |
| F21 | contradiction | §1.4/§1.6 assert every response carries envelope+plan; P4 later revises to status-only | `spec-workflows.md:181,257,1097` |
| F22 | spec-mismatch | §9.3 external-secret emit shape contradicts implemented literal `REPLACE_ME` (platform-rejected form documented) | `spec-workflows.md:1144`; `ops/bundle/helpers.go:40-56` |
| F23 | drift | §1.1 ServiceMeta omits `PrimarySetupName`/`StageSetupName`; §3.1 references removed `managedByZCP` | `spec-workflows.md:84-96,561`; `service_meta.go:70-71` |
| F24 | spec-mismatch | spec-env-handling §7.1/§13 claims a single-writer `.env.local` lint never built; fictional test names | `spec-env-handling.md §7.1,§13`; `env_local_overlay.go:37` |
| F25 | dup | Legacy `zerops_export` tool — second export impl with no atom/workflow refs | `tools/export.go:17-41`; `ops/export.go:40-101`; `server.go` |
| F26 | dead | `bundle.Variant` export half (`VariantExportDev/Stage`) + `IsExport()` dead; `BuildExport` ignores variant arg (`_ = variant`) | `ops/bundle/variant.go`; `ops/bundle/export.go:119`; `topology/types.go` |
| F27 | dead | `adopt.go` `InferServicePairing`+`AdoptCandidate` fully dead with stale 'auto-adoption' docs | `adopt.go:7,39` |
| F28 | dead | `InitSession` (non-atomic) production-dead; engine uses only `InitSessionAtomic`; 9 tests pin abandoned path | `session.go:48,184`; `engine.go:174` |
| F29 | drift | `deploy-config-central-point.md` stale header 'action=strategy is canonical' (now test-FORBIDDEN) | `deploy-config-central-point.md` |
| F30 | drift | `DeriveDeployed` comment describes local-only behavior as general | `compute_envelope.go` (DeriveDeployed) |

---

## 5. Phased remediation roadmap

Each phase is independently shippable + verifiable (CLAUDE.md "no half-finished states"). Front-loaded so the foundation unblocks downstream cleanup.

### Phase 1 — Single fresh-meta constructor + single normalization boundary  *(no deps)*
**Goal:** Kill the empty-string GitPushState/BuildIntegration divergence so the git-push/build-integration atom chain fires for local modes; collapse 4 ad-hoc empty-checks to one site. Highest-leverage live-bug fix; unblocks every downstream atom/snapshot assertion.
- Add `NewServiceMeta` canonical constructor stamping the three sentinels; route all fresh-meta writers through it (`bootstrap_outputs.go`, `adopt_local.go` case 0/1/default, `workflow_adopt_local.go`).
- Normalize the three deploy dimensions exactly once at the read/snapshot boundary — extend the existing `CloseDeployMode` normalization in `compute_envelope.go` `buildOneSnapshot` (and the synthetic tail) to GitPushState/BuildIntegration; prefer `parseMeta`-on-read to heal existing on-disk metas (backward-compat seam).
- Delete ad-hoc `== ""` checks: `render.go:206-214`, `launch_source_control_gate.go:220`, `deploy_local_git.go:317`.
- Add a local-stage golden/atom-firing test (none exists today).
- **Risk:** read-side normalization changes on-disk semantics; pin with a legacy empty-value meta fixture. **Gate:** `go test ./internal/workflow/... ./internal/content/... -race`; new local-stage develop golden; flow-eval-local where local-stage sets closeMode=git-push and expects the atom to fire.

### Phase 2 — Collapse setup-name onto ResolveCanonicalSetup; finish P5/P6  *(deps: P1)*
**Goal:** One resolver for "which setup applies to a hostname". Fixes the wrong-`prod`-in-committed-CI bug + "Cannot find corresponding setup" failures. Unblocks build-integration, launch composer, git-push cleanups.
- `DeployIntent.Resolve` reads `target.SetupName`/`StageSetupName`, OMITS the setup arg when empty; delete `RecipeSetupProd/Dev` from the deploy path; pin test.
- `anticipatedBuildTarget` populates `SetupName` on its synthetic snapshot so build-integration CI YAML carries the canonical name.
- Route launch's two cascades + `ops/bundle/launch.go` default + git-push hostname fallbacks (`deploy_git_push.go:317`, `deploy_local_git.go:88`) through the shared resolver; on empty, emit `requiresSetupInput` blocker instead of defaulting.
- Replace `export_probe`'s `pick*` with the exported `workflow.PickSetupNameFromNames/ListSetupNames/SetupCandidatesFor`.
- Wire the `staleMetaSetup` blocker to its emitter (built + test-pinned, zero callers); reconcile with `EnrichSetupNotFound` at `deploy_validate_api.go:82`.
- Correct `setup_resolver.go:84-86` + `bootstrap_outputs.go:66-72` false comments.
- **Risk:** empty-setup now blocks instead of defaulting to `prod` — changes happy-path on unseeded metas; seed `PrimarySetupName`/`StageSetupName` in launch/flow-eval fixtures first. **Gate:** `-race` workflow+tools+ops; new `TestResolveReadsSetupName`; flow-eval greenfield-node-postgres with renamed setup; verify committed CI YAML carries canonical setup not `prod`.

### Phase 3 — Adoptability classifier + container/local adopt parity  *(deps: P1)*
**Goal:** `AdoptionState` becomes the one classifier (retiring 4 parallels incl. `isSelf` divergence); container adopt matches local on `FirstDeployedAt` + comments.
- Export `classifyAdoption(meta, self) -> AdoptionState` (ServiceID-preferring `isSelf`); route `workflow_route.go:53`, `route.go:291`, `discover.go:164`, `deriveIdleScenario` through it.
- Extend parity test to cross-pin all 4 incl. empty-ServiceID fallback.
- Stamp `FirstDeployedAt` for container-adopted ACTIVE services in `writeBootstrapOutputs` (parity + spec line 95) OR accept signal-#3 + correct comments (**owner-decision**).
- Delete `adopt.go` `InferServicePairing`/`AdoptCandidate`/`isControlPlaneType` + test.
- **Gate:** `-race` workflow+tools; extended parity test; container-adopt e2e on eval-zcp asserting `meta.IsDeployed()` parity.

### Phase 4 — git-auth-probe + buildFromGit trust unification  *(deps: P2)*
**Goal:** One ls-remote/host-parse primitive across git-push-setup + launch + export; fix export-side `refreshRemoteURLCache` exposure; settle buildFromGit trust.
- Export `ops.BuildGitAuthProbeCommand` + `parseGitHost` as the single primitive; call from launch gate container (`:320-340`) + local (`:392-396`) + export gate; delete `launchPushProofHost`.
- Fix `refreshRemoteURLCache`: on GitPushConfigured+drift, the bundle must use the probe-proven URL (reassign `repoURL`), not ship the recipe-template URL with a warning.
- Decide+implement buildFromGit trust (one model OR two documented policies pinned next to both sites).
- **Gate:** `-race` ops+tools; e2e export-buildfromgit-self-snapshot on configured+drift asserting bundle does NOT carry a recipe-template URL.

### Phase 5 — Launch multi-runtime gating gaps  *(deps: P2 — OWNER-GATED)*
**Goal:** Make multi-runtime correct end-to-end OR explicitly scope to single-runtime + align docs. No half-wired plumbing.
- **CONFIRM with Karel:** is multi-runtime live for v1?
- If live: resolve promotables at read-side gate (`:152`), `missingScopeFields` (`:1021` accept Promotables-only), `readAndValidateSourceState`, pipeline check (`:710`) using the publish-side's `resolveLaunchRuntimes`.
- If not live: scope handler to single-runtime, update spec §10 + CLAUDE.md, remove/flag the unused N-runtime plumbing.
- Address friction-fixes F3 (subsumed by P2) + F5 (GIT_TOKEN reload recovery) — or formally drop with a note.
- **Risk:** sensitive workflow (one-shot launchKey + real prod creation); must not regress the P-LP-10/11 source-of-truth gate. **Gate:** `-race` tools; a genuine 2-runtime flow-eval with runtime B unwired asserting `source-control-required` fires at read-side BEFORE launchKey generation.

### Phase 6 — Launch state model decision + stateless-narrowing unification  *(deps: P5 — OWNER-GATED)*
**Goal:** Launch stops re-implementing the spine. Fold into `WorkflowState.Launch`+StateEnvelope+registry, OR extract one shared stateless-narrowing dispatch/recovery helper for export+launch. Delete the second mechanism.
- DECIDE (open decision A/B); implement chosen path; delete the bespoke alternative.
- Reconcile spec §1.4/§1.6 with P4 at the §1.4 site.
- **Risk:** State Version bump needs a tested one-way migration for existing on-disk `.zcp/state` files (published-product backward-compat seam). Highest internal-refactor risk; defer behind P1-P5. **Gate:** `-race` workflow+tools; `action=status` recovery flow-eval after simulated compaction on a launch session; migration test loading a v1 state file.

### Phase 7 — Dead-code deletion (recipe portion OWNER-GATED) + ops/cicd decision  *(deps: P6)*
**Goal:** Remove the unfinished-unification scaffolding so live and dead code stop being indistinguishable. Atomic, independently shippable.
- Delete `InitSession` non-atomic + repoint 4 subject-tests at `InitSessionAtomic`.
- Delete `bundle.Variant` export half + rename to `bundle.LaunchVariant`; remove `_ = variant` in `BuildExport`; OR complete §9.2 single `Compose.Build` (open decision).
- Delete `FormatEnvFile`/`ServiceEnvGroup` dead `.env` writer.
- `ops/cicd` `ComposeActionsHandoff` + `ZEROPS_TOKEN_STAGE/PROD`: decide wire-vs-delete; if delete, remove the package; if wire, retire `actionsWorkflowYAML`'s bare `ZEROPS_TOKEN`.
- **FLAG for Aleš:** v2 recipe cluster in shared workflow package — cleared for deletion only once v3 ownership + closed on-disk-session window confirmed. Do not act unilaterally.
- Decide legacy `zerops_export`: rename to disambiguate (documented orthogonal raw primitive per archive ruling X9 RETAIN) or delete `RegisterExport` + `ops.ExportProject/ExportService`.
- **Risk:** v2 recipe deletion crosses Aleš's scope; `zerops_export` is a published user surface (backward-compat decision); a pre-9.0.1 on-disk recipe session could still load v2 handlers. **Gate:** `go test ./... -race`; `annotations_test`+`server_test` after any tool-surface change.

### Phase 8 — Spec/doc reconciliation pass (mostly doc-only)  *(deps: P7)*
**Goal:** Bring rank-3 specs back in sync with code+tests; add auto-generated assertions where hand-maintenance rotted.
- Strike `'unknown'` from gitPushState rows (`spec-workflows.md:90,240,668`); keep it on the runtimes axis.
- Regenerate atom inventory + add an auto-generated count assertion test (not hand prose).
- Add `PrimarySetupName`/`StageSetupName` to §1.1; replace §3.1 `managedByZCP=false` with `adoptionState='adoptable'`.
- Fix §9.3 + 3 atoms + type doc + tool schema `pickRandom(["REPLACE_ME"])` → literal `REPLACE_ME`.
- Build the `.env.local` single-writer lint OR remove the §7.1/§13 claim + fix the false `env_local_overlay.go:37` comment + fictional §13 test names.
- Reject bootstrap `iterate` at engine level (true hard-stop) OR document the counter-bump; delete/correct stale §7.4 row.
- Decide `RecipeMatch.Repo`/`{{recipe.repo}}` intent (wire substitution OR delete field+atom line — the atom currently ships a literally-broken `git clone {{recipe.repo}}`). Bootstrap scope.
- Archive completed plans (`git mv` → `plans/archive/`): discover-adoption-state-enum, the two launch-production plans, setup-name-local-canonical (once P2 lands); fix `deploy-config-central-point.md` stale header.
- **Gate:** `-race` topology+content; new atom-count assertion test; `lint-local` atom-tree gates.

---

## 6. Open decisions (owner-gated — Karel)

1. **Setup-name on empty:** flip to a structured `requiresSetupInput`/`staleMetaSetup` blocker (P5 intent, north-star-aligned) vs keep a default. The three "P5 deferred" sites cite test-fixture seeding as the only blocker. Flipping changes happy-path on unseeded metas.
2. **Empty-string normalization site:** (a) all writers emit canonical zeros, (b) normalize once in `parseMeta` on read (heals existing on-disk metas — backward-compat seam), or (c) normalize in `buildOneSnapshot` like CloseDeployMode. (b) is the single-canonical-path choice but changes on-disk-value interpretation.
3. **Launch multi-runtime live for v1?** CLAUDE.md + plan say shipped; a test comment says scope-cut. If yes → wire the gating gaps; if no → scope to single-runtime + align spec+CLAUDE.md.
4. **Launch state model end-state:** fold into `WorkflowState.Launch`+spine (Version bump + one-way migration) vs extract one shared stateless-narrowing helper (launch into `immediateWorkflows`). Either way delete the second mechanism.
5. **buildFromGit trust model:** unify export+launch onto one probe-proven-URL model, or formally accept two justified policies (export user-CWD vs launch /var/www guard) + document both. (Independent: the `refreshRemoteURLCache` code-vs-docstring exposure must be fixed regardless.)
6. **§9.2 bundle unification:** finish the single `Compose.Build()` dispatcher over a 4-value Variant, or accept two entry points (`BuildExport`/`BuildLaunch`) + delete the dead export half + rename to `LaunchVariant`.
7. **ops/cicd stage/prod-distinct tokens (`ZEROPS_TOKEN_STAGE/PROD`):** is the workflow-family-architecture thesis still wanted (wire `ComposeActionsHandoff` + retire bare `ZEROPS_TOKEN`) or abandoned (delete the dead package)?
8. **v2 recipe cluster deletion:** needs Aleš's sign-off even though the stranded code lives in the SHARED workflow/tools packages — and confirmation the pre-9.0.1 on-disk recipe-session window is closed.
9. **Legacy `zerops_export` tool:** delete (now that `workflow=export` owns buildFromGit) vs keep as a documented orthogonal raw primitive (archive ruling X9 says RETAIN). Published user-facing surface → backward-compat decision.
10. **Container adopt `FirstDeployedAt`:** stamp for ACTIVE adopted services (parity + spec line 95) vs accept the `DeriveDeployed` signal-#3 compensation + correct the comments (accepting on-disk `meta.IsDeployed()` diverges by environment).
11. **Bootstrap `action=iterate`:** reject at engine/handler level (true hard-stop matching spec B5) vs document the actual counter-bump+guidance behavior + fix the §2.1/§7.4 self-contradiction.
12. **`RecipeMatch.Repo` / `{{recipe.repo}}`:** is local recipe-clone intended live (wire `resolveRecipeMatch` + template substitution) or dead (delete field + doc-comment + the broken atom line)? Bootstrap scope.
13. **Spec edits ownership:** should this remediation own fixing rank-3 spec drift (Tests+Code already win the source-of-truth ladder), or is a doc pass deferred?

---

## 7. Dead-code deletion list (safe / decision-gated)

- `internal/workflow/session.go:InitSession` — non-atomic; zero production callers, engine uses only `InitSessionAtomic` (repoint 4 subject-tests).
- `internal/workflow/adopt.go:InferServicePairing` — zero production callers, stale auto-adoption docs.
- `internal/workflow/adopt.go:AdoptCandidate` — only used by dead `InferServicePairing`.
- `internal/workflow/adopt.go:isControlPlaneType` — only called by `InferServicePairing` + its test.
- `internal/ops/bundle/variant.go:VariantExportDev` / `VariantExportStage` — never constructed as values.
- `internal/ops/bundle/variant.go:Variant.IsExport` — zero callers (production + tests).
- `internal/ops/cicd/actions_handoff.go:ComposeActionsHandoff` — zero production callers — **DECISION-gated** (wire or delete the whole `ops/cicd`).
- `internal/ops/export.go:ExportResult.RuntimeServices`/`ManagedServices`/`ServiceHostnames` — zero production callers, only `export_test.go`.
- `internal/tools/stale_meta_setup.go:StaleMetaSetupResponse` builder — built + test-pinned, zero emitters — **wire in P2 OR delete** if `EnrichSetupNotFound` stays canonical.
- `internal/workflow/engine_recipe.go:RecipeStart` + the whole v2 recipe cluster (`recipe.go`, `dispatch_brief_envelope.go`, `planRecipeActive`, `PhaseRecipeActive`, dispatcher branches in `workflow.go`) — **OWNER-GATED (Aleš)**.
- `internal/tools/workflow_recipe.go` v2 query handlers (unreachable for new sessions) — **OWNER-GATED**.
- `internal/ops/env_export.go:FormatEnvFile` / `ServiceEnvGroup` — dead parallel `<host>_<key>` `.env` writer, zero production callers.
