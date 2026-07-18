# Workflow-family + env-lifecycle remediation — VERIFIED merge (2026-05-28)

Merges two audit bodies into one verified, risk-ordered plan:
- `plans/workflow-family-audit-2026-05-28.md` (53-agent audit, 30 findings, T1–T9).
- `plans/zcp-env-lifecycle-review-findings-2026-05-28.md` (my review of the 23 unpushed env commits, F1–F7).

Every claim a fix depends on was **independently re-verified** by a 28-agent default-refute
workflow (code opened, grep-proven, file:line re-derived). This doc records what SURVIVED,
what the audit OVER-claimed, and the corrected fix sequence.

---

## 0. Meta-finding — read this first

**The workflow-family audit is directionally correct but over-states precision.** Its
structural thesis (unified WRITE paths, fragmented READ paths; deferred deletions) is real and
its bugs mostly exist — but its `0/30 refuted` self-report does **not** survive a genuine
default-refute pass. The weakness is **framing + location + severity**, not existence:

| Over-claim found | Reality |
|---|---|
| File:line cites `internal/tools/` for `internal/workflow/` code (F2/F3/F4) | Anchors systematically wrong by **package**; cited `internal/workflow/workflow.go:555` doesn't exist (real site `internal/tools/workflow.go:552`). Treat audit anchors as topic pointers, re-derive before editing. |
| F3/T2 "the **entire** git-push→build-integration atom chain never fires" | Only **one** atom (`develop-close-mode-git-push-needs-setup`) misfires; the other two come through handlers that hardcode/guarantee non-empty state. **Medium, not high keystone.** |
| F2 "case 0/1/default local writers omit dims" | `adopt_local.go` **case 0 sets them correctly**; only case 1 + default omit. 1 of 3 already canonical. |
| F8 "export ships the recipe-template URL" | **Inverted** — export ships the **live** URL (correct per invariant); the *warning string* lies (says "probe-proven"). Bug = wrong text, not a leak. |
| F14 "docstring claims 5 callers, only 2" | Docstring says "reserved for" (intent list), not "called by". The dup count (5+ resolvers) is the real signal. |
| F18 "iterate doesn't hard-stop, text-only" | Hard-stop **is** code-enforced (`engine.go:204-215` cap+close). Only the per-call counter-bump matches. |
| F25 zerops_export "second impl / dead" | **Live + registered** (`server.go:209`) and **already RETAINED** by archive ruling X9 (2026-04-28). No new decision. |
| F5/F7 v2 recipe "dead code wired into dispatcher" | **Hard-rejected** at dispatcher + **intentionally retained** for the open-session query window. F7 is an explicit dup of F5 (inflates the count by 1). Aleš scope. |

**Net:** ~6 of the high/medium findings are materially mis-stated; none of the underlying
structural problems are fully refuted. The audit is trustworthy as a map, untrustworthy as
coordinates.

---

## 1. The two efforts — different size, risk, urgency (the scope boundary)

| | EFFORT A — env-branch cleanup | EFFORT B — workflow-family remediation |
|---|---|---|
| Size | ~5 commits | 8 phases |
| Risk | Low (internal, no migration) | Higher (spine/launch/state model; one phase needs a state-Version migration) |
| Urgency | **Push-blocking** (23 commits can't land: 2 CI lint fails + 1 HIGH bug) | Not urgent; mostly owner-gated |
| Owner-gate | None | 13 decisions |
| Touches | env mutation, launch warnings, preflight tests | ServiceMeta model, setup-name resolution, launch state, dead-code, specs |

**The one Effort-B item worth doing regardless of B's owner-gates:** the keystone (T2/F3) —
no migration if done at construction, pure correctness.

---

## 2. Verification ledger (severity AFTER re-verification; ↓ = downgraded from audit)

| ID | Audit→Verified | Claim | Scope |
|---|---|---|---|
| **env-F1** | HIGH (confirmed) | launch-into-existing drops ALL warnings (`launch_existing.go` never sets `state.Warnings`) | act — Effort A |
| **env-F2** | HIGH (confirmed) | preflight lifecycle partition (managed→FAIL / never-deployed→WARN) has zero tests; plan enumerated it | act — Effort A |
| **LINT-1/2** | CI-blocker (confirmed) | `env.go:380` exhaustive missing `EnvLayerProject` (not a runtime bug); `env_effective_test.go:10` unparam | act — Effort A |
| **F3/T2** | HIGH ↓ **MED** | local-adopt case1+default leave GitPushState/BuildIntegration empty → `develop-close-mode-git-push-needs-setup` misfires on repeat local-stage status | act — keystone |
| **F1/T1** | HIGH (partly) | setup-name: `DeployIntent.Resolve` hardcodes prod/dev, ignores `snap.SetupName` → wrong `--setup prod` baked into committed CI YAML. **Launch harm refuted** (launch reads meta cascade correctly) | act |
| **F4/T1** | HIGH ↓ **LOW** | same root as F1 — `SetupName`/`StageSetupName` denorm fields are dead (zero readers). Fix WITH F1 | act |
| **F14/T1** | NIT (partly) | `setup_resolver.go` docstring drift | act (doc) |
| **F6/T3** | MED (partly) | launch read-side gate single-`TargetService` vs publish-side loops all → 2-runtime spends launchKey then refuses on B | **karel** (multi-runtime live v1?) |
| **F8/T5** | MED ↓ **LOW** | `refreshRemoteURLCache` warning text contradicts code (says probe-proven, ships live). **Not a leak** | act + karel (trust model) |
| **F9/T5** | LOW (confirmed) | launch re-implements `ls-remote`/netrc + dup `parseGitHost`; 3 stances on one probe | act |
| **F10/F26/T6** | LOW (confirmed) | `bundle.Variant` export half dead; `IsExport()` 0 callers; `_=variant`. (Note `VariantExportDev`=iota 0 = zero value) | act |
| **F11/T7** | MED ↓ **LOW** | adoptability classified 4 ways; `isSelf` divergence real in empty-ServiceID env | act |
| **F12/T2** | MED (confirmed) | empty-string deploy-dim normalization ad-hoc at 4 sites; only CloseDeployMode normalized at boundary | act (folds into keystone) |
| **F13/T4** | MED (confirmed) | launch runs a genuinely parallel state model + status-recovery, bypassing the spine | **karel** (fold vs helper; state migration) |
| **F27** | LOW (confirmed) | `adopt.go` InferServicePairing/AdoptCandidate/isControlPlaneType — grep-proven 0 callers | act (delete) |
| **F28** | LOW (confirmed) | `InitSession` non-atomic — 0 prod callers, engine uses Atomic; 9 tests pin it | act (delete) |
| **F16** | MED (confirmed) | atom ships unsubstituted `git clone {{recipe.repo}}`; `RecipeMatch.Repo` never consumed | karel (bootstrap) |
| **F17/T8** | LOW (confirmed) | container vs local adopt `FirstDeployedAt` divergence + 2 false comments | karel |
| **F19/F20/F22/F23** | LOW (confirmed) | spec drift: `gitPushState='unknown'` gone from code; "74 atoms" actually 109; `pickRandom`→`REPLACE_ME`; `managedByZCP` removed | act (doc pass) |
| **F18** | LOW (partly) | iterate hard-stop IS enforced; only counter-bump + 3 contradictory spec lines | defer/doc |
| **F5/F7** | MED (confirmed) | v2 recipe cluster hard-rejected + intentionally retained | **Aleš — FLAG only** |
| **F25** | already-decided | zerops_export retained by X9 | no decision |
| CheckEnvRefs | LOW (confirmed) | dead slim-blind orphan; sole caller is a CLI shim with empty `liveHostnames` (FAIL unreachable). **Correction: NOT Aleš scope** — generic `ops/checks`, deletable standalone | act |

---

## 3. The corrected fix sequence

### EFFORT A — push-blockers (land FIRST; env touches `deploy_preflight.go` + `workflow_launch_production.go` which are also Effort-B files → land env to keep B on a stable base)
1. **Lint blockers** — `env.go:380` add explicit `case ops.EnvLayerProject:` → default, with a comment (project is the lowest layer, never wins); drop the unused `name` param in `runtimeSvc`. *Unblocks CI.* ~5 ln.
2. **env-F1** — extract ONE finalizer `finalizeLaunchedState(state, bundle){ state.Warnings = bundle.Warnings }`; call it in BOTH `executeLaunchMutation` (replacing the inline `:673`) and `executeExistingProjectMutation` (before `:376`). **Must run on every launched return — also the resume path** (`:482/:745/:1182`), or warnings drop on resume. Pin: per-path e2e asserting `warnings[]` in the rendered response. ~30 ln.
   *How it works after:* an agent launching into an existing prod project now sees "promoted db X is unreferenced → unreachable" + `REPLACE_ME` advisories that vanish silently today.
3. **env-F3/F4 transient parity** — propagate `RefResolveTransientError` from BOTH app-version-fetch consumers (`deploy_preflight.go` + `env_generate.go:228`). *After:* a VPN/API blip yields "run `zcli vpn up` and retry", not a false "fix your yaml" deploy-block. ~15 ln.
4. **env-F2/F5/F6 test pins** — preflight both branches; generate-dotenv never-deployed keep-literal; `Port.Scheme` mapper fixture. Pure test additions locking shipped behavior (env-F2 was enumerated by the authoritative plan → skipping again repeats the scope-cut). ~120 ln.
5. **env-F7 sensitive contract** — derive `Sensitive: ud.Type.String()=="SECRET"` in `GetAppVersionUserData` + mapper test. *After:* a literal secret baked in `run.envVariables` is redacted in shadow warnings instead of printed verbatim. ~40 ln.

→ **Then push the env branch** (push/release is a separate explicit Karel decision — CLAUDE.local.md).

### EFFORT B — verified do-now (after env lands; scope=act, no owner-gate)
6. **Keystone (T2/F3/F12)** — `NewServiceMeta(hostname, mode)` constructor stamping the three sentinels; route `adopt_local.go` case1+default (NOT `workflow_adopt_local.go` — it mutates an existing meta) + bootstrap writers through it. Normalize GitPushState/BuildIntegration once at the read boundary (alongside CloseDeployMode) — **`parseMeta`-on-read** heals existing on-disk metas (backward-compat seam → needs a legacy-fixture test). Delete the 4 ad-hoc `==""` checks. Pin: a **local-stage atom-firing golden driven through `buildOneSnapshot`** (none exists). *Note: same constructor the setup-name work needs — coordinate so they don't add competing constructors.*
7. **Setup-name (T1/F1/F4/F14)** — `Resolve()` reads `snap.SetupName`/`StageSetupName`, falls back to the recipe dev/prod convention when **empty** (the audit's "omit the arg" is UNSAFE — omitting makes the deploy tool default to hostname-as-setup, which breaks recipe yamls). Populate `SetupName` in `anticipatedBuildTarget`/`resolveBuildTargetForHost`/`SnapshotsFromMetas`. Dedup `export_probe`'s `pick*` onto `workflow.PickSetupNameFromNames`. Fix the docstring. Pin: assert committed CI YAML carries the renamed setup, not `prod`. *After:* `set-default-setup` rename is honored everywhere (deploy next-action, CI YAML, build-integration), not just preflight.
8. **Adoptability (T7/F11) + dead-code (F27)** — one `classifyAdoption(meta,self)→AdoptionState` (ServiceID-preferring `isSelf`) consumed by all 4 sites; cross-pin incl. empty-ServiceID. Delete `adopt.go` dead trio.
9. **git-auth/buildFromGit (T5/F8/F9)** — export `ops.BuildGitAuthProbeCommand`+`parseGitHost` as the single primitive; call from launch+export gates; delete `launchPushProofHost`+inline netrc. **Fix F8 minimally regardless of trust decision: correct the lying warning text** (export uses the live URL by invariant).
10. **dead-code (F28/F10/F26)** — delete `InitSession` (repoint 9 tests to Atomic); delete `Variant` export half + `IsExport`, rename `LaunchVariant` (verify `VariantExportDev`=iota-0 has no implicit zero-value reliance first).
11. **spec pass (F19/F20/F22/F23)** — strike `'unknown'`; auto-generated atom-count assertion (not hand prose); `pickRandom`→`REPLACE_ME`; add `PrimarySetupName`/`StageSetupName` to §1.1, replace `managedByZCP`→`adoptionState`.

### Phase-dependency correction (the reorder win)
Audit's `P2 deps P1` and `P4 deps P2` are **FALSE**. P1 touches the deploy **dimensions**
(CloseDeployMode/GitPushState/BuildIntegration); P2 touches setup **names** (orthogonal
fields, seeded by the already-running Gate-A cascade); P4 (`git_auth_probe`) has zero setup
refs. **True critical path = just the keystone (6) standalone.** 7 and 9 parallelize. Group
6/7/8 edits only because they touch the same ServiceMeta writers, not for correctness.

---

## 4. Owner decisions (Karel) — surface BEFORE touching the gated items
1. **Keystone normalization site:** writer-only zeros vs `parseMeta`-on-read heal (recommended — heals existing on-disk metas) vs `buildOneSnapshot`. (b) changes on-disk interpretation → backward-compat decision.
2. **Setup-name on empty:** recipe dev/prod fallback (recommended/safe) vs structured `requiresSetupInput` blocker (north-star, but changes happy-path on unseeded metas).
3. **Launch multi-runtime live for v1?** Test comment says "scope cut per §10"; CLAUDE.md + composer say shipped. → wire the gating gaps OR scope to single-runtime + align spec/CLAUDE.md. Leaving as-is = a 2-runtime launch spends the one-shot launchKey then refuses on B.
4. **Launch state model (T4):** fold into `WorkflowState.Launch`+spine (State Version bump → **one-way idempotent tested migration**, published `.zcp/state` seam) vs extract one shared stateless-narrowing helper for export+launch.
5. **buildFromGit trust:** one probe-proven model vs two documented policies. (Independent: the F8 lying-warning text must be fixed either way.)
6. **ops/cicd `ZEROPS_TOKEN_STAGE/PROD`:** wire `ComposeActionsHandoff` vs delete the dead package.
7. **F16 `RecipeMatch.Repo`:** wire local-clone vs delete the field + the broken atom line.
8. **F17 container `FirstDeployedAt`:** stamp to parity vs accept `DeriveDeployed` signal-3 + fix the two false comments.

### Aleš — FLAG only (do not act)
- F5/F7 v2 recipe cluster (intentional backward-compat retention; hard-rejected at dispatcher).

### No decision needed
- F25 zerops_export — already RETAINED by X9.

---

## 5. Invariants / risks any fix must keep
- `run.envVariables` canonical; never read top-level `entry.EnvVariables`.
- Architecture: ops/workflow peers (no mutual import); platform imports no internal; tools→platform via ops.
- Every behavior change pinned; remove (don't disable) dead code.
- **Migration seam:** keystone `parseMeta`-heal and any State-Version bump touch the published
  `.zcp/state` seam → one-way, idempotent, tested with a legacy-meta fixture. Ship neither without it.
- **Do NOT `make release` / `make install`** — separate explicit Karel decisions (CLAUDE.local.md).

---

## 6. Verification coverage caveat
Deep-verified: all 7 audit HIGH (F1–F7), F8–F12, F14, F19/F20/F22/F23, F25, F27/F28, both env
findings, LINT-1/2, and the phase-dependency claims. NOT individually re-opened: F13(detail),
F15, F16(detail), F17(detail), F21, F24, F26(launch half), F29, F30 — "likely-confirmed,
verify-before-fixing" per the audit's own caveat. One spec-spot-check agent (F24) failed to
return structured output; the F19/F20/F22/F23 spot-checks all confirmed, so the doc-drift
corpus is directionally trustworthy but not fully proven.
