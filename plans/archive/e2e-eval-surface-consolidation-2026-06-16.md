# e2e + eval test surface — consolidation audit (2026-06-16)

**Question Karel asked:** the e2e tests and eval flows now overlap; go through everything,
analyze which e2e tests still make sense given where the project evolved, which overlap,
which are stale — and propose what to delete / rework. Check backlog / plan history / commits
for prior unfinished work on this. Primarily: actually read everything.

**Method (two independent evidence-grounded passes + my own grounding):**
- **Workflow team** — 10 parallel readers (one per cluster), each finding adversarially
  verified against the real code by an independent skeptic (58 agents, 3.66M tokens, 832 tool
  calls). Every disposition cites `path:line`.
- **Codex** (gpt-5.5, xhigh, 536K tokens) — independent file-read audit of the same surface.
- **My own grounding** — gitignore status, harness orphan-chain, old-eval-era identification.

The two passes **agree on the whole skeleton** and diverge on ~6 specific calls — the
divergences are the decision levers (§8). Both corrected two false premises in my brief:
`evalisms_lint_test.go` is NOT `live`-tagged, and the `zsc-noop` dev-start pattern is STILL
shipped (not removed). Code confirmed `adoptionState` is now a **6-state** enum
(`AdoptionBootstrapping` added), not 5.

---

## Verdict

**The damage is temporal stratification + semantic drift, not bad tests.** The `e2e` and `api`
layers are overwhelmingly healthy — they pin genuine live-platform contracts no mock can reach,
and the much-debated "overlaps" (deploy self-vs-cross, export raw-vs-functional, git-delivery
live-vs-fullchain, subdomain tool-vs-platform) almost all resolve to **distinct surfaces, not
redundancy**. Real rot is concentrated in three places:

1. **Dead build-tag mechanisms.** `probe` (3 `logfetcher_probe*` files, self-declared "not
   pinned behavior") is spent investigation scaffolding. `live` has collapsed to a leftover
   bucket: the 3 launch `live_*` tests were correctly deleted 2026-06-16; what remains
   (`live_export` + schema/recipe audits + harness) is real but mis-bucketed. `auth_api_test.go`
   is a permanent-`t.Skip` stub.
2. **Eval museum.** `eval/` carries three stacked eras; only the newest (behavioral flow-eval)
   is alive. Era-1 (closed-loop "Knowledge Eval Agent": `eval/AGENT_PROMPT.md`, `run.sh`,
   `taxonomy.yaml`, `eval/functional/`) and Era-2 (weather-sweep Python scoring in
   `eval/scripts/`) are ~2.3 KLoC of museum-ware with zero live callers. The behavioral corpus
   itself has bloated to ~55 scenarios where the founding plan wanted a curated **12-15** matrix.
3. **Semantic drift the compile-guard can't see.** The 2026-06-16 audit added `make vet-tags`
   (compile-check of tagged files) — but stale **string literals** compile fine. That gap let
   `knowledge_quality_test.go` (lists keydb/rabbitmq, both dropped from `serviceNormalizer`
   2026-06-14 → would FAIL on next run), `instructions_test.go` (6-state enum drift), and several
   eval scenarios (`.netrc`, `closeMode=git-push`, re-supply-launchKey, never-shipped cicd prompt)
   rot invisibly.

**Single highest-value structural fix:** curate the behavioral corpus to a canonical matrix with
a manifest (canonical/owner/behavior/overlaps/gating/last-reviewed) AND extend the drift guard
to catch **semantic** staleness — not just compile rot. Then `knowledge_quality` derives its
key-table from `serviceNormalizer` (single-owner) so that drift class can't re-land.

**Guardrail both passes flagged independently:** behavioral verification is **warn-only today**
(`VerificationConfig`/`behavioral_run.go` write findings without failing the run). So **no e2e
test is deletable on a "covered by an eval" basis** — agent-flow evals are not yet a hard gate.

---

## Target architecture

The surface should collapse to **3 build-tag tiers + behavioral eval + the default suite**:

- **default (untagged)** — fast units, schema/content lint, parser/render, safety helpers. Two
  e2e files belong here (they need NO platform): `update_test.go` (subprocess binary-swap) and
  `orphan_cleanup_safety_test.go` (pure `hasTestPrefix` table). *Today they're `e2e`-tagged, so
  they run neither in `go test ./...` nor in CI — a safety-critical guard that never executes.*
- **`api`** — real-API CONTRACT layer (SDK-decode shapes, status/error-code normalization,
  launch-production platform-spike pins P-LP-9/P-LP-13). Read-mostly, `ZCP_API_KEY`. **NOT
  foldable into e2e** (e2e asserts tool/workflow behavior; `api` asserts SDK protocol).
- **`e2e`** — full ZCP handler/MCP-tool lifecycle vs eval-zcp. Deploy (ssh+local), failure
  classification, bootstrap, export, launch single-token lifecycle, verify, subdomain,
  git-delivery, env-generate.
- **behavioral eval** (`eval/behavioral/`) — non-deterministic AGENT-decision quality (route
  choice, plan-shape, env-wiring, blocker comprehension). Observation-only. Curated matrix, not
  a 55-prompt dump.
- **`live` and `probe` retire as mechanisms** (after rehoming). `probe` → delete outright.
  `live` → its 3 real audits (`live_export`, schema, recipe) rehome; harness deletes with
  `live_export`'s last consumer.

**Division of labor:** `api`/`e2e` = deterministic handler+platform correctness; behavioral eval
= non-deterministic agent navigation. Complementary by design.

> **Divergence (live-tag naming):** Codex folds the 3 live audits into `api` (or a named
> `catalog` tag) → 3 tags = untagged/api/e2e. Workflow keeps `live` narrowed (composer/catalog
> round-trips vs live state). Both land on 3 tags, disagree which 3. Low risk either way; the
> decisive question is whether "live composer/catalog audit" is conceptually distinct from "api
> contract." Do it LAST, after rehoming. (See §8.)

---

## Disposition tables (verified)

Disposition uses the **verified** call where the skeptic revised the original; revisions marked.

### e2e — deploy
| Item | Disposition | Why / unique coverage |
|---|---|---|
| `deploy_test.go` | **keep** (canonical SSH happy path) | Only e2e exercising self+cross SSH transport → RUNNING + HTTP 200 + auto-subdomain (DM-1/DM-2) |
| `deploy_local_test.go` | **keep** | Entire local zcli-push surface (no `sourceService`, `.env` bridge) — unreachable via SSH harness |
| `deploy_failure_classification_e2_test.go` | **keep** (canonical failure) | Live proof signal regexes match REAL Zerops logs |
| `build_logs_test.go` | **merge → failure-classification::BuildPhase** | Same BUILD_FAILED trigger; transplant 4 asserts (`mode=ssh`, `buildLogsSource=build_container`, log-content). **Both passes agree.** Saves one ~10-min live build |
| `deploy_prepare_fail_test.go` | **keep** *(Workflow)* / merge *(Codex)* | Unique: defensive-poll-terminates-fast (<5min) on PREPARING_RUNTIME_FAILED, not unit-tested. **Divergence §8** — either way preserve the fast-terminate assert |

### e2e — launch
| Item | Disposition | Why |
|---|---|---|
| `launch_single_token_test.go` | **keep** (canonical) | Sole live pin of staging→launched→confirm→reset; token-before-create + physical-delete-on-confirm + dual SSH+API read. PASSED 2026-06-16, 20 steps |
| `launch_baseline_test.go` | **rewrite/rehome → `api` envclass** *(revised from delete)* | ~90% dead DTO-reachability dup; BUT the live enum-membership canary is unique — ZCP wrappers do an **unchecked string cast** (`ServiceEnvType(e.Type)`), so a server enum addition is swallowed everywhere else. Keep the canary, strip the 3 spent sub-tests. **BLOCKER:** `defaultAPIHost()` lives here + is used by 2 surviving files → relocate to `helpers_test.go` in the same change |
| `launch_existing_project_test.go::TokenValidationPrimitives_Live` | **delete** | Inline-reimplements the scope switch (a copy that drifts); decode covered by harness, scope logic pinned at `launch_existing_test.go:118` |
| `…::FullMutation_Live` | **delete** | Never runs the mutation (env-gated, never CI'd); only write is a residue sentinel; defers real coverage to evals by its own comment |

### e2e — bootstrap
| Item | Disposition | Why |
|---|---|---|
| `bootstrap_workflow_test.go` | **keep** (canonical) | Envelope Total==3, classic+adopt routes, ACTIVE-stage acceptance + EXISTS-dep env co-discovery |
| `bootstrap_advanced_test.go` | **keep** *(Workflow)* / trim *(Codex)* | Multi-dep / SHARED two-runtime-one-db / simple→standard expansion — not in core. Codex: trim to only non-overlapping cases |
| `bootstrap_helpers_test.go` — `bootstrapAndProvisionExpectFail`, `assertProvisionFailed`, `assertNoEnvVarCheck` | **delete** (3 orphan funcs) | Zero call sites (consumer deleted in `609b1227`). **Both passes + skeptic agree** |
| `bootstrap_helpers_test.go` (remainder), `helpers_test.go`, `process_helpers_test.go` | **keep** | Load-bearing shared fixtures/harness/polling (10-19 callers each) |
| `import_provenance_test.go` | **keep** | Local import.yaml-at-root + SSHFS mount-readiness race |
| `bootstrap_git_init_test.go` | **keep** | GLC-1 real `zerops:zerops` ownership + `.git/config` identity |
| `instructions_test.go` | **fix (investigate)** | adoptionState drifted to 6-state; test switch routes `AdoptionBootstrapping` → `default` ERROR (false-fail). Add it to the accepted set |

### e2e — subdomain + standalone
| Item | Disposition | Why |
|---|---|---|
| `subdomain_lifecycle_test.go` | **keep** (canonical, per Codex) | 5 raw-platform facts (import-flag-doesn't-activate, port-suffix, persist-across-redeploy) |
| `subdomain_test.go` | **keep** *(Workflow)* / merge→lifecycle *(Codex)* | Only test driving `action=enable` through the MCP tool + GET URL. **Divergence §8.** Fix stale companion ref `:12` either way |
| `verify_test.go` | **keep** | Live ACTIVE-status acceptance regression |
| `verify_render_test.go` | **keep** | Browser-rendered `body_text` (framework error tokens); fix wrong test-name ref `:116` |
| `events_test.go`, `scale_thresholds_test.go`, `orphan_meta_cleanup_test.go` | **keep** | Live RFC3339/summary cohesion; live autoscaling endpoint; live prune-wiring seam |
| `update_test.go`, `orphan_cleanup_safety_test.go` | **move → untagged** *(Codex; Workflow noted as smell)* | Need no platform; currently never run in CI. Real fix — see Target arch |

### e2e — export / git-delivery / knowledge
| Item | Disposition | Why |
|---|---|---|
| `export_test.go` | **keep** (drop `DiscoverMode` log-only subtest, per Codex) | Raw `GetProjectExport`/`GetServiceStackExport` — different surface from §9 workflow export |
| `export_multi_test.go` | **keep** (canonical functional) | Deterministic IsInfrastructure + Mode=NON_HA + project-env-in-YAML vs known fixture |
| `git_delivery_fullchain_test.go` | **keep** (canonical chain) | push→Actions→container-replace→reconstruct, real CI replay (900s, extra gate) |
| `git_delivery_live_test.go` | **keep** *(Workflow)* / fold *(Codex)* | Credential-helper primitives in isolation (180s, non-mutating). **Divergence §8** |
| `knowledge_quality_test.go` | **rewrite (single-owner)** | DRIFT: `serviceNormalizer` dropped keydb+rabbitmq; test still lists them → `ServiceCardExists` FAILS next run. Derive `normalizerKeys` from `serviceNormalizer`, drop the 2 rows, fix `:53` count. Phase-2 live port/env check is the unique keep |
| `laravel_recipe_test.go` | **investigate** | Mostly `t.Logf` discovery; KEY FINDING (`${hostname_varName}` resolves at container level from service-level env) trapped in test header → migrate to recipe `.md`. Decide: worth 10-min provision? |
| `env_generate_test.go`, `api_error_meta_test.go` | **keep** | Sole live `EnvGenerateDotenv` recursive expander; full `mapAPIError`→MCP-JSON chain |

### Build tags — live / api / probe
| Item | Disposition | Why |
|---|---|---|
| `logfetcher_probe{,2,3}_test.go` | **delete (atomic trio)** + drop `probe` tag | Self-declared "not pinned behavior"; all-`t.Logf`; findings promoted to `logfetcher_build_contract_test.go`. **Both passes + skeptics agree.** Deleting one orphans shared helpers → delete all 3 together |
| `internal/auth/auth_api_test.go` | **delete** | Permanent-`t.Skip` stub, unreachable no-op body. **Both passes agree** |
| `project_admin_api_test.go` | **keep (HARD)** | Spec-pinned by name (P-LP-9/P-LP-13); live 3-region corePackage/location read-back |
| `zerops_api_test.go`, `logfetcher_api_test.go`, `logfetcher_build_contract_test.go`, `apitest/harness.go` | **keep** | SDK-decode/normalization contract; LogFetcher wrapper; the correct probe→contract promotion; shared harness (13+ consumers) |
| `live_export_test.go` + `live_test_harness.go` | **keep (rehome target TBD §8)** | Sole live `BuildBundle` composer vs real source; harness's ONLY remaining consumer is `live_export` — they delete together if ever folded |
| `live_audit_test.go` (schema), `recipes_live_audit_test.go` (knowledge) | **keep (rehome `live`→`api`/`catalog` §8)** | Live all-types/all-recipes round-trip vs catalog — surfaces a type absent from `managedServicePrefixes` that frozen coverage can't |
| `evalisms_lint_test.go` | **keep (NOT live-tagged — premise was wrong)** | Default-suite AST lint forbidding `ZCP_E2E_*` in agent-facing strings |

### Eval scenarios (behavioral)
| Item | Disposition | Why |
|---|---|---|
| 3× rewritten launch scenarios (existing-project-token, existing-with-webhook, new-project-push-mode) | **keep (confirmed clean)** | Both passes confirm: pipeline-first `startWithoutCode:true`, no `buildFromGit`, single `ZCP_LAUNCH_TOKEN`, no method prompt |
| `git-push-setup-with-cicd-method-prompt.md` | **rewrite or delete** | Grades vs NEVER-SHIPPED inline cicd-method-prompt (phantom `ZEROPS_TOKEN_STAGE`, phantom atom, premise inverts the shipped *separate* build-integration action). Vocab-guard misses it |
| `git-push-setup-then-actions.md` | **keep, fix stale mechanics** *(Codex catch)* | Correct flow, but still says `.netrc` + restart; current code: credential-helper not `.netrc`, no restart needed |
| `delivery-git-push-actions-setup.md` | **merge / dry-run variant** *(Codex)* | Overlaps `git-push-setup-then-actions` |
| `launch-with-existing-cicd.md` | **rewrite** | Pre-migration "cicd handoff" + expects prod build to start immediately (pipeline-first: first release IS first build); evades vocab-guard via prose |
| `launch-to-existing-prod-project.md` | **rewrite/delete** | Pre-migration lifecycle (expects "first build starts"); superseded by `launch-production-existing-project-token.md` |
| `launch-production-pipeline-not-configured.md` | **rewrite** | B5-drift: trains re-supply same launchKey; shipped resume is stage-first |
| `existing-simple-mode-add-endpoint.md` | **un-defer or delete** | Explicitly parked (DO-NOT-AUTO-LOAD); its blocking plan has landed → marker stale |
| `landing-page-static-simple.md` | **keep** *(revised from merge)* | Orthogonal MODE axis (simple-vs-dev-only, dev 502 footgun) vs `classic-static`'s runtime-CLASS axis — NOT redundant |
| `cadence-multiservice-spec-run2-replay.md` | **investigate** | Spec-only never deploys; `-build-` sibling subsumes it |
| ~45 other adopt/classic/recipe/develop/discover/export + local scenarios | **keep** | Current vocab, distinct surfaces (zsc-noop + 6-state confirmed shipped) |
| scenarios-local/ (greenfield, auto-adopt, recipe, pipeline-local) | **keep** | Genuinely local-only surface (auto-adopt, deploy_local schema, `.env` bridge) |
| `brownfield-existing-node-app.md` (local) | **keep exploratory, non-gating** | Theme-3 brownfield is design-only (`SourceBrownfieldImport` reserved, no handler) |

### Eval infra + Era-1/Era-2
| Item | Disposition |
|---|---|
| `eval/AGENT_PROMPT.md`, `eval/run.sh`, `eval/taxonomy.yaml`, `eval/functional/` (3 files) | **delete** (Era-1 closed-loop, atomic cluster — cross-refs) |
| `eval/scripts/{score,generate,extract-tool-calls,analyze_bash_latency}.py`, `{cleanup,version_metrics}.sh` | **delete** (Era-1) |
| `eval/scripts/{aggregate-weather-audit,autonomous-weather-sweep}.py`, `{run-multi-runtime-weather,run-batch3-followup,run-scenario}.sh` | **delete** (Era-2 weather sweep) |
| `eval/scripts/timeline.py` | **keep if still used for manual run analysis** (Codex) — else delete |
| `eval/scripts/build-deploy.sh` | **KEEP** — `flow-eval.sh:106` ships the binary before every run. *Never `rm -r eval/scripts/`* |
| `internal/eval/scenarios/*.md` + `RunScenario` + `cmd/zcp/eval.go` subcommands + `scenarios_test.go` | **coupled Era-2 grader decision §8** — `close-mode-git-push-setup.md` asserts dead `closeMode=git-push`/`.netrc` (Codex). Retire whole era OR keep wired-but-unused |
| `internal/workflow/scenarios_test.go`, `internal/content/eval_scenario_drift_test.go` | **keep** | Live C3 envelope→plan pins; the B2 vocab-guard |
| `eval/behavioral/wave1-analysis.json`, `audits/usersim-*.md` | **delete** (frozen one-off outputs; git preserves) |
| `eval/results/` (281 MB), `e2e/AGENTS.md` (REFLOG dump) | gitignored / delete-tracking — **no repo action beyond stop-tracking AGENTS.md** |

### Plans / backlog
| Item | Disposition | Why |
|---|---|---|
| `backlog/post-run-platform-verification-framework.md` | **close (git rm)** | ALREADY BUILT — `verification.go` + `verification:` frontmatter shipped (`acba7b59`) |
| `backlog/tier-2-token-injected-scenarios.md` | **close (git rm)** | ALREADY BUILT — `requiredEnvVars` + env-file + sentinel scan shipped, all 6 scenarios exist |
| `backlog/e2e-bootstrap-helper-two-phase-wiring.md` | **close (git rm)** | Part A done; named consumer gone |
| `backlog/m1-glc-safety-net-identity-reset.md` | **keep** | Tightening #2 genuinely open (`workflow_bootstrap.go:282` log-only) |
| `plans/{live-flow-tests,real-life-test-matrix,path-to-everything-tested}-2026-05-16.md` | **archive (git mv)** | Transient roadmaps, substantially delivered. Repoint `live_export_test.go:4` provenance |
| `plans/open-work-compact-2026-06-09.md` | **keep (prune)** | Stream B (AgentResponse delivery redesign) genuinely unshipped |

---

## Structural fixes (the root-cause work, beyond per-file cleanup)

1. **Semantic drift guard** — `vet-tags` is compile-only; stale literals pass. Extend
   `eval_scenario_drift_test.go` (+ a sibling for tagged tests) with regexes for known-dead
   tokens: `ZEROPS_TOKEN_STAGE`, `re-call .* same launchKey`, `closeMode=git-push`, `.netrc`,
   phantom atom names, and a generated list of dropped enum members.
2. **Single-owner `knowledge_quality`** — derive `normalizerKeys` from `knowledge.serviceNormalizer`
   (export a `Keys()` accessor) so the keydb/rabbitmq drift class is structurally impossible.
3. **Scenario manifest** (Codex's highest-value pick) — a per-scenario front-matter manifest:
   `canonical | owner | behavior | overlaps | gating | last-reviewed`. Makes dedup + curation to
   the founding plan's 12-15 matrix mechanical, and gives the drift lint an ownership anchor.

---

## Execution order (lowest-risk-first, each phase independently verifiable)

**Phase 1 — Pure deletions, zero coverage loss** (`go vet -tags e2e,api,live` + `go test ./...`).
Delete 3 e2e orphan helpers, `auth_api_test.go`, the 3 `logfetcher_probe*` (atomic) + drop the
`probe` vet-tag line + retitle `live/api/e2e/probe`→`live/api/e2e`. Move `update_test.go` +
`orphan_cleanup_safety_test.go` to untagged (they finally run in CI). *Lose: nothing.*

**Phase 2 — Eval museum amputation** (one cluster/commit; verify `flow-eval.sh` still builds+runs).
Era-1 cluster, then Era-2 cluster, then frozen outputs. Order: `eval/functional/` first removes
the last caller of `score.py`. *Keep-list: `build-deploy.sh` — never `rm -r eval/scripts/`.*

**Phase 3 — Plan/backlog hygiene** (no code). `git rm` the 3 shipped backlog entries; `git mv` the
3 roadmap plans to archive, repointing `live_export_test.go:4` in the same commit.

**Phase 4 — Drift rewrites** (RED first where a scenario/test is graded; verify drift-guard green).
`knowledge_quality` single-owner rewrite; `instructions_test.go` add `AdoptionBootstrapping`;
rewrite the 5 launch/git-delivery scenarios to shipped vocab + fix `.netrc`/restart in the keeper;
**extend the drift guard** (structural fix #1). *Lose: nothing if the Phase-2 live checks survive.*

**Phase 5 — Merge + relocate** (verify transplanted asserts present). Merge `build_logs_test.go`
into `failure-classification::BuildPhase` (transplant all asserts; update `Makefile:163` run
filter). Rewrite `launch_baseline` to enum-canary-only (relocate `defaultAPIHost()`).

**Phase 6 — Decisions (Karel owns, no auto-execute):** §8 divergences + the Era-2 grader
retire-or-keep + `laravel_recipe` value vs cost + scenario-manifest build + the live-tag→api fold.

---

## §8 — Where the two passes diverge (decision levers)

| Item | Workflow | Codex | My lean |
|---|---|---|---|
| `live` tag fate | keep narrowed (composer/catalog) | fold 3 audits into `api`/`catalog`, **delete `live`** | **Codex** — fewer mechanisms; but low-risk, do LAST after rehoming |
| `deploy_prepare_fail_test.go` | keep (unique fast-terminate) | merge into failure-classification | Keep the file OR merge — **non-negotiable: preserve the <5min fast-terminate assert** |
| `subdomain_test.go` | keep (MCP-tool contract pin) | merge→lifecycle, delete (auto-enable already in deploy) | **Slight Workflow** — the `action=enable`+GET-URL tool contract is a real distinct pin; cheap |
| `git_delivery_live_test.go` | keep (cheap 180s isolated primitives) | fold into fullchain | **Slight Workflow** — the cheap non-mutating layer earns its keep vs the 900s gated chain |
| `bootstrap_advanced` / `import_provenance` | keep | trim/split | **Workflow** — skeptic found unique coverage; trimming is optional polish |
| behavioral corpus size | curate toward manifest | curate to 12-15 matrix + manifest | **Both** — same root: add the manifest, then curate |

---

## Top risks

1. **Era-2 grader is coupled all-or-nothing.** `internal/eval/scenarios/*.md` + `RunScenario` +
   4 `cmd/zcp/eval.go` subcommands + `scenarios_test.go` stand or fall together; deleting scenarios
   while keeping the grader breaks the non-empty-glob pin. *Mitigation:* one explicit Karel
   decision; do NOT touch in Phase 2.
2. **Behavioral verification is warn-only** → evals can't justify deleting e2e coverage yet.
   *Mitigation:* keep all e2e canonical pins; revisit only after verification becomes a hard gate.
3. **Atomic-deletion failures from shared helpers.** Probe trio shares helpers; Era-1 has
   cross-refs; orphan e2e helpers pair. *Mitigation:* delete each cluster as a unit, `go vet -tags`
   after each.
4. **`build-deploy.sh` cascade-delete** — the only live edge in the otherwise-dead `eval/scripts/`.
   *Mitigation:* explicit keep-list; never sweep the dir.
5. **Merge-by-deletion loses live asserts if transplant skipped** (`build_logs` `mode=ssh` checks;
   the enum canary). *Mitigation:* gate the merge commit on the asserts' presence.
6. **The vet-tags guard does not catch semantic drift** — rewrites can silently re-rot.
   *Mitigation:* Phase 4 must EXTEND the guards (structural fix #1), not just fix the data.

---

## Provenance

- Workflow run `wf_4652f8e8-455` (10 areas, 58 agents) — full JSON in the run transcript.
- Codex review `/tmp/codex-out-1781640674-69517-25929.md` (gpt-5.5 xhigh).
- No code changed by this audit — proposal only; Karel picks the phase(s) to execute.
