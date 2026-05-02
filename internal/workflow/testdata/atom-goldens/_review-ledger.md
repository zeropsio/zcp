# Master defect ledger — Phase 2 Step 4

**Status**: Awaiting human approval (Phase 2 Step 5 PAUSE POINT).
**Plan**: `plans/atom-corpus-verification-2026-05-02.md` §2.
**Method**: 6 parallel review sub-agents (A1-A6) covering all 30 canonical scenarios at full body depth, plus initial single-conversation pass that produced the L1-C1 baseline. Each agent prefixed defects with their ID (A1-, A2-, …, A6-). This ledger consolidates findings.

---

## Coverage map

| Agent | Scenarios reviewed | Atoms read | Findings |
|---|---|---|---|
| baseline | sample of 16 of 30 (initial single-conv pass) | sample | L1, L2, R1-R8, O1, O2, E1, C1 (12 entries) |
| A1 | 4 idle + bootstrap/recipe/{provision,close} | 11 distinct | 6 new + cross-confirm of R1, R2, R3, R7, O1 |
| A2 | bootstrap/{classic/discover-standard-dynamic, classic/provision-local, adopt/discover-existing-pair} | 11 distinct | 16 new + cross-confirm of L2, R3, O1, C1 |
| A3 | 4 heaviest develop scenarios (first-deploy variants, scope-narrow, failure-tier-3) | 30+ distinct | 11 new + 1 fixture drift + cross-confirm of R4, R5, O1, O2, E1, C1 |
| A4 | 4 develop pair scenarios (auto / git-push-{configured,unconfigured} / unset) | 25+ distinct | 14 new + cross-confirm of L2, R4, R5, O1, C1 |
| A5 | 2 develop steady (steady-dev-auto, mode-expansion) + 2 closed-* | 22+ distinct | 5 new + L1 explicit confirmation + R4, R5, O1 |
| A6 | strategy-setup (2) + export (7) | 11 distinct | 10 new + cross-confirm of R6, R8 |
| **Total** | **30 / 30** | **~80+ atoms** (corpus has 82) | **62 new + 12 baseline = 74 entries; many cross-confirmations** |

**Substring contracts** (Phase 0b migration test): all 7 export status contracts pass per A6's verification.

---

## Multi-agent consensus findings (TIER S — ship in Cycle 1)

These were independently surfaced by 3+ agents AND/OR are structurally critical (rendered output produces invalid commands or contradicts itself in the same render).

### S1 — `priority: 0` parser silent-coercion bug

- **Found by**: A1 (A1-1), A2 (A2-O3), A3 (A3-O1), A4 (A4-O1), A5 (A5-O1) — **5 of 6 agents**
- **Severity**: HIGH structural
- **Affected atoms**: `bootstrap-intro.md`, `develop-intro.md` (both declare `priority: 0`)
- **Root cause**: `internal/workflow/atom.go::atomPriority` (line 457-469) silently maps `n < 1 || n > 9` to default 5. Pinned by existing test `priority_below_one_defaults` (`atom_test.go:138`).
- **Effect**: Two "intro" atoms render at position 5 (mid-render or late) instead of position 1 (overview). Author intent contradicts runtime behavior. In `multi-service-scope-narrow` golden, `develop-intro` lands at position 18 of 22.
- **Affected goldens**: every bootstrap-active and develop-active scenario where these atoms fire (~16 of 30).
- **Proposed fix**: extend `atomPriority` valid range to `[0-9]`, treat 0 as highest priority. Update `validateAtomFrontmatter` to reject only out-of-range. Add a parser-level test pinning `priority: 0 → first`. Adjust the existing `priority_below_one_defaults` test (rename + redefine to "negative priority defaults"). NO atom file edits needed.

### S2 — `develop-closed-auto.md` lies about `closeReason`

- **Found by**: baseline (L1), A5 (explicit confirmation with verbatim quotes from both goldens)
- **Severity**: HIGH lie-class — canonical example the plan was designed to catch
- **Affected atom**: `develop-closed-auto.md:10` — body asserts `` `closeReason: auto-complete` are set ``
- **Root cause**: atom gated on `phases:[develop-closed-auto]` only; that phase covers BOTH `auto-complete` AND `iteration-cap`. Body hardcodes one value, lying for the other.
- **Affected goldens**: `develop/closed-auto-complete` (truthful) + `develop/closed-iteration-cap` (LIE — actual envelope `CloseReason=iteration-cap` per fixture line 362; rendered text claims `auto-complete`).
- **Proposed fix (preferred — option (a))**: rewrite body to inspect: "The envelope's `phase: develop-closed-auto` is set; the specific `closeReason` is in `workSession.closeReason` — see `develop-auto-close-semantics` for what each value means." Procedural-form principle directly applies; the synthesizer already exposes `WorkSessionSummary.CloseReason` to the rendered envelope JSON.
- **Optional companion fix**: A5-M1 — add iteration-cap recovery guidance ("inspect `workSession.deploys[].reason` before retrying — repeating the same approach hits the cap again"). Either as new sentence in `develop-auto-close-semantics` OR new `develop-closed-iteration-cap.md` atom (axis-split path).

### S3 — `develop-close-mode-auto-workflow-dev.md` "dev process is already running" lie

- **Found by**: A5 (A5-L1, primary), A3 (A3-L1, cross-confirmed)
- **Severity**: HIGH lie-class — direct contradiction in the same composed render
- **Affected atom**: `develop-close-mode-auto-workflow-dev.md:17` — body asserts "The dev process is already running"
- **Root cause**: atom gated on `deployStates:[deployed]` AND `closeDeployModes:[auto]` AND `modes:[dev]` — but a service can be `deployed=true` with the dev process NOT running (every redeploy drops it; first-time-after-redeploy state too). Co-firing atoms `develop-dynamic-runtime-start-container.md:15` ("no dev process is live until you start one") and `develop-dev-server-triage.md:60-61` ("After every redeploy the dev process is gone") explicitly contradict this assertion in the same render. Atom even self-references the contradiction parenthetically.
- **Affected goldens**: `develop/steady-dev-auto-container`, `develop/mode-expansion-source`, `develop/multi-service-scope-narrow` (per A3+A5 observations).
- **Proposed fix**: replace assertion with inspection: "Verify the dev process is up first — `zerops_dev_server action=status`; if `running: false`, run `action=start` (see `develop-dynamic-runtime-start-container`). Code-only edits never trigger `zerops_deploy` — deploy is for `zerops.yaml` changes only…"

### S4 — Pair-vs-single-service language lie

- **Found by**: A3 (A3-L4), A4 (A4-L4) — **2 agents independently**
- **Severity**: HIGH lie (mild — wrong noun)
- **Affected atoms**:
  - `develop-close-mode-auto.md:10` — "This pair is on `closeDeployMode=auto`" but axes are `closeDeployModes:[auto], deployStates:[deployed]` with NO `modes:` filter — fires for single-service ModeDev / ModeSimple too.
  - `develop-close-mode-git-push.md:14` — same shape with `modes:[standard, simple, local-stage, local-only]` (simple + local-only are single-slot).
  - `develop-close-mode-git-push-needs-setup.md:14` — same.
  - `develop-close-mode-git-push-needs-setup.md:22` — "every service in the pair completes setup" with same ambiguity (A4-C1).
- **Affected goldens**: any single-service deployed close-mode scenario (~5+ scenarios firing these atoms).
- **Proposed fix**: replace "This pair" → "This service"; replace "every service in the pair" → "the service / pair". Universal noun.

### S5 — Adopt-route atom-set fundamentally malformed

- **Found by**: baseline (L2 — single phrase), A2 (A2-L1, A2-L2, A2-L3, A2-L6 — expanded into systemic finding)
- **Severity**: HIGH lie-class (multi-atom; structural)
- **Affected atoms**:
  - `bootstrap-mode-prompt.md` — entire body is plan-submission-centric ("before submitting the plan", "Plan MUST set `stageHostname`", "A submission omitting `stageHostname` rejects with…", "The plan commits the mode when you submit it"). `routes:[classic, recipe, adopt]` lets it fire for adopt route which doesn't submit plans-with-modes.
  - `bootstrap-provision-rules.md` — `routes:[classic, adopt]` but body 100% covers import-yaml construction (hostname format, runtime properties, `project:` block dichotomy, `zerops_import` semantics). Adopt doesn't construct YAML.
  - `bootstrap-provision-local.md` — NO `routes:` axis (fires for all routes) but body assumes classic construction with stage-mode rules.
  - `bootstrap-env-var-discovery.md` — `routes:[classic, adopt]` but body says "After import…" — adopt has no import.
- **Effect**: agent on adopt route reads classic-construction guidance that doesn't apply.
- **Proposed fix (structural)**: tighten all four atoms to `routes:[classic]` only. Author missing adopt-route atoms (`adopt-provision`, `adopt-close`, `adopt-env-var-discovery`) as needed — `bootstrap-adopt-discover` already exists; adopt path needs siblings for provision and close steps.

### S6 — `develop-strategy-awareness` aggregate-render emits handler-rejected commands

- **Found by**: A4 (A4-L1) — single agent but **structural correctness bug**
- **Severity**: HIGH structural — rendered output produces commands the handler EXPLICITLY rejects
- **Affected atom**: `develop-strategy-awareness.md:31-37` — `{services-list:…}` directive enumerates over all matching services and emits per-service `git-push-setup service="appstage" remoteUrl="..."` for the stage half.
- **Root cause**: `git-push-setup` handler (per `internal/tools/workflow_git_push_setup.go:87-92`) explicitly rejects stage-half targets (`PushSourceIsStageHalf` ⇒ `ErrInvalidParameter`). Atom directive doesn't filter.
- **Effect**: agent runs the rendered command on stage hostname → handler error.
- **Affected goldens**: every pair scenario firing this atom (`develop/standard-auto-pair`, `develop/git-push-configured-webhook`, `develop/git-push-unconfigured`).
- **Proposed fix**: filter directive to dev-half hostnames only for `git-push-setup` and `build-integration` actions (those are per-pair, write meta from dev half only). Either (a) atom-level: add a "dev-half-only directive" mechanism to `expandServicesListDirectives`; or (b) per-axis service filtering at directive expansion. Option (b) is the structural fix; (a) is a tactical workaround.

### S7 — `develop/failure-tier-3` golden is byte-identical to first-deploy (no session-aware atoms)

- **Found by**: A3 (A3-L5)
- **Severity**: HIGH missing-critical — single most impactful corpus gap caught by review
- **Affected goldens**: `develop/failure-tier-3` is byte-identical to `develop/first-deploy-dev-dynamic-container` despite WorkSession.Deploys["appdev"] carrying 3 failed attempts (build-failed, start-failed, verify-failed).
- **Root cause**: no atom-model axis exposes session attempt depth or recent-failure pattern. Atoms can't differentiate iteration 0 from iteration 3.
- **Effect**: agent at iteration 3 with 2 iterations left before forced cap-close gets the same "first attempt" framing. No "you've tried 3 times — change approach" signal.
- **Proposed fix (structural — Cycle 2 candidate)**: add new envelope axis exposing attempt tier (e.g. `attemptTier: [fresh, retry, near-cap]` derived from `len(WorkSession.Deploys[hostname])` vs maxIterations=5). Author new atom `develop-failure-recovery.md` keyed on retry/near-cap. Until then, the failure scenario fixture is best treated as a "demonstrate the gap" fixture rather than blessed for production guidance.

### S8 — `develop/steady-dev-auto-container` and `develop/mode-expansion-source` are byte-identical fixtures

- **Found by**: A5 (A5-S1)
- **Severity**: MED fixture-design — corpus has 30 scenarios but effective unique coverage = 29
- **Affected fixtures**: `scenarios_fixtures_test.go` — both call `fixSnapDeployedDevAuto("appdev","nodejs@22",topology.RuntimeDynamic)` + `fixSession("appdev")`. Verified byte-identical via `diff` (only id + description differ).
- **Effect**: zero unique fire-set, zero unique rendered text, two scenarios occupying corpus slot for one fixture.
- **Proposed fix (preferred — option (b) differentiate)**: change `mode-expansion-source` fixture to use `topology.ModeSimple` instead of `topology.ModeDev`. `develop-mode-expansion` atom has `modes:[dev, simple]` axis; currently only `dev` is exercised in the corpus. Differentiating to ModeSimple covers the simple-mode arm and makes mode-expansion-source the canonical "expand from simple" scenario. (Alternative: option (a) merge — drop mode-expansion-source — but loses the unique-name pointer.)

---

## Single-agent findings — Cycle 2 candidates

Grouped by severity. Each links to the agent that surfaced it.

### LIE-CLASS (HIGH severity)

| ID | Agent | Scenario(s) | Atom | Claim | Why wrong | Fix |
|---|---|---|---|---|---|---|
| A3-L2 | A3 | first-deploy-recipe-implicit-standard | `develop-implicit-webserver.md:11` | "Apache or nginx is already running and serves disk contents." | Atom has no `deployStates:` axis but asserts current state. For never-deployed implicit-webserver services, no runtime container exists → web server NOT running. | Add `deployStates:[deployed]` axis OR rewrite as deploy-state-aware: "After deploy, Apache or nginx serves…". |
| A3-L3 / A4-L2 | A3+A4 | post-adopt-standard-unset | `develop-strategy-review.md:13` | "The first deploy landed and verified." | Atom axes only expose `deployStates:[deployed]` — verify-pass is NOT pinned. A service can be `deployed=true` with failing verify. Per proposed §11.x evidence-rule for verify-state assertions. | Drop "and verified": "The first deploy landed. Before iterating, declare the develop session's delivery pattern." |
| A4-L3 | A4 | post-adopt-standard-unset (any closeMode=unset/manual) | `develop-auto-close-semantics.md:11-18` | "Work sessions close automatically when either of two conditions hold…" | Auto-close is gated on `CloseDeployMode ∈ {auto, git-push}` (verified `internal/workflow/work_session_test.go:387` — `unset_blocks` and `manual_blocks` both `want: false`). Atom asserts auto-close mechanics universally; lies for unset/manual. | Tighten axes to `closeDeployModes:[auto, git-push]`, OR rewrite with the gate: "Once close-mode is auto or git-push, work sessions close automatically when…". |
| A2-L4 | A2 | bootstrap/classic/provision-local | `bootstrap-wait-active.md` | "Repeat until every service reports `status: ACTIVE`" | Per `internal/tools/workflow_checks.go::checkServiceRunning` (line 159-161), services are operational at status RUNNING OR ACTIVE. Sister atom `bootstrap-provision-rules` line 52 says "Dev/Simple → RUNNING, Stage → READY_TO_DEPLOY, Managed → RUNNING/ACTIVE". Direct contradiction in same render. | Rewrite to "Repeat until every service reports `RUNNING` or `ACTIVE` (READY_TO_DEPLOY is the awaiting-first-deploy state for stage services in standard mode)". Drop unbacked timing range. |
| A2-L5 | A2 | bootstrap/classic/discover-standard-dynamic, bootstrap/classic/provision-local | `develop-api-error-meta.md:33` | Body uses `{hostname}` placeholder in pedagogical example | When envelope has empty `Services`, `{hostname}` substitutes to empty string → broken example "(`.mode`, `build.base`, `parameter`)". Body itself uses `<host>` literal at line 41 — internally inconsistent. | Standardize on `<host>` literal throughout; drop `{hostname}` substitution from this atom. |
| A2-L7 | A2 | bootstrap/classic/discover-standard-dynamic | `bootstrap-classic-plan-dynamic.md` (and `bootstrap-classic-plan-static`) | Atoms have `runtimes:[dynamic]` (or `[static]`) AND `routes:[classic]` AND `steps:[discover]` — but at discover step the envelope has empty Services, so service-scoped axes can't match. **Atoms NEVER FIRE in any of the 30 scenarios** (confirmed via coverage map — 0 scenarios). | Service-scoped `runtimes:` axis can't fire pre-import; envelope has no services to satisfy it at classic-discover step. | Two options: (a) drop `runtimes:` axis entirely — atom fires universally at classic-discover, agent picks the relevant section; (b) introduce `plannedRuntimes:` axis (envelope-scoped, populated from BootstrapState.Plan). Option (a) is the lower-risk fix consistent with procedural-form. |
| A6-L3 | A6 | export/validation-failed | `export-validate.md` | Atom gated on `exportStatus:[validation-failed]` but body bulk discusses `bundle.warnings` (M2/M4/MYSTERY_VAR sections) — warnings are classify-prompt territory; errors (path/message) are validation-failed territory. Tail "When publish-ready fires, spot-check…" forward-references publish-ready state from inside validation-failed-gated atom. | Content axis mismatch — atom carries the wrong primary message for its gating axis. | Either (a) widen `exportStatus:[validation-failed, classify-prompt]` and rename atom; OR (b) move warning content into `export-classify-envs` body, restrict `export-validate` to errors-only content. |
| A6-L4 | A6 | strategy-setup/container-unconfigured | `setup-git-push-container.md:10` | Lede ordering: "Set the token, walk through a first push, then mark the capability configured" — but body procedure is token (1) → stamp configured (2) → commit + push (3). | Lede contradicts body. Misleads agents who skim. | Rewrite lede: "Set the token, mark the capability configured, then commit + push." |
| A3-L6 | A3 | multi-service-scope-narrow, mode-expansion-source | `develop-mode-expansion.md:13` | "The envelope reports your current service with `mode: dev`…" | Singular language. Atom has no `multiService:` axis. In multi-service envelopes (2+ deployed dev runtimes) the body reads as if there's only one service. | Plural-friendly rewrite OR add `multiService: aggregate` with `{services-list:…}` enumeration. |

### Missing-critical (HIGH severity)

| ID | Agent | Scenario | Observation | Fix |
|---|---|---|---|---|
| A4-M1 | A4 | post-adopt-standard-unset | Render lacks dev-half self-deploy command sequence. `develop-standard-unset-promote-stage` only gives the cross-deploy template; `develop-close-mode-auto-standard` (which has the dev-iteration template) is gated `closeDeployModes:[auto]` and doesn't fire for `unset` envelopes. Agent gets half-instructions: pick close-mode + cross-deploy, but no template for the dev side. | Add sibling atom `develop-standard-unset-iterate.md` (modes:[standard], closeDeployModes:[unset], deployStates:[deployed], environments:[container]) with the dev iteration sequence. |
| A5-M1 | A5 | develop/closed-iteration-cap | 2-atom render gives "Next actions: start a new task or close" + auto-complete/iteration-cap descriptions. **No failure-recovery guidance for iteration-cap** — agent lands at exhausted-budget state with no instruction to inspect deploy history. | Either add closeReason-conditional branch in `develop-closed-auto`, OR new sibling atom `develop-closed-iteration-cap.md` (axis-split path matches S2's "option (b)"), OR extend `develop-auto-close-semantics` with "After iteration-cap close, inspect `workSession.deploys[].reason` and decide whether to retry or escalate". |

### Redundancy (MED severity)

| ID | Agent | Scenarios | Atoms | Observation | Fix |
|---|---|---|---|---|---|
| A3-R1 | A3 | multi-service-scope-narrow + 2 others | `develop-close-mode-auto` (P3) + `develop-close-mode-auto-deploy-container` (P2) | Both fire for deployed dev/simple+auto+container; both explain closeMode=auto semantics. Two atoms covering same ground. | Merge or split: deploy-container atom keeps env-specific mechanics, close-mode-auto carries high-level framing. |
| A3-R2 | A3 | first-deploy-dev-dynamic, failure-tier-3 | `develop-first-deploy-env-vars` | Atom carries managed-service env-var catalog table irrelevant to envelopes with zero managed services. | Move catalog behind a conditional or split atom. |
| A3-R3 | A3 | 3 first-deploy scenarios | `develop-first-deploy-intro` | Body unconditionally instructs `zerops_workflow action="close-mode"` even when fixture already has closeMode=auto. | Add `closeDeployModes:[unset]` filter OR make instruction conditional. |
| A3-R4 | A3 | multi-service-scope-narrow, steady-dev-auto-container | `develop-dev-server-triage` (P2) + `develop-dynamic-runtime-start-container` (P3) | Both cover `zerops_dev_server` lifecycle. Triage = decision tree; runtime-start = action reference. Repeated "after every redeploy…" + workers-without-HTTP. | Triage retains decision flow; runtime-start drops redundant paragraphs. |
| A4-R1 | A4 | git-push-configured-webhook | `develop-close-mode-git-push` + `develop-build-observe` | Heavy content overlap on async-build narrative + record-deploy. "returns as soon as the push transmits" appears 2× in golden; record-deploy 6×. Both atoms target same axes. | Trim build-observe to failure-class triage + log-fetch only; let close-mode-git-push own push command. |
| A4-R2 | A4 | standard-auto-pair (and auto + git-push scenarios) | `develop-strategy-awareness` + `develop-close-mode-auto` | Both describe close-mode switch command. Reader sees same "switch with this command" twice. | Trim close-mode-auto's "When you might switch" subsection; let strategy-awareness own deploy-config axis reference. |
| A2-R1 | A2 | bootstrap/classic/provision-local | `bootstrap-provision-local` step 1 + `bootstrap-env-var-discovery` | Two atoms instructing the same `zerops_discover includeEnvs=true`. | Drop env-var-discovery for env=local; or shrink provision-local to a pointer. |
| A2-R2 | A2 | bootstrap/classic/discover-standard-dynamic | `bootstrap-mode-prompt` + `bootstrap-runtime-classes` | Two parallel taxonomies render back-to-back; no atom integrates "modes ⊥ runtime-classes". | Add lead-in cross-reference between them. |
| A2-R3 | A2 | bootstrap/adopt/discover-existing-pair | `bootstrap-adopt-discover` + `bootstrap-mode-prompt` | adopt-discover says "modes inherited"; mode-prompt says "confirm modes for plan submission". Contradictory imperatives. | Resolved by S5 fix (drop adopt from mode-prompt's `routes:` axis). |
| A6-R9 | A6 | strategy-setup/configured-build-integration | `setup-build-integration-actions` + `setup-build-integration-webhook` | Both atoms open with **identical** `## 1. Confirm git-push setup landed` heading + identical paragraph. Sharpens R8. | Drop the prereq-confirm section from BOTH atoms — `gitPushStates:[configured]` axis already gates them. |
| A6-R10 | A6 | export/validation-failed + export/classify-prompt | `export-validate` + `export-classify-envs` | Both describe `bundle.errors` JSON-Schema validation mechanic. Same explanation in two atoms. | Move schema-validation into `export-validate` only; classify-envs cross-references. |
| A6-R11 | A6 | all 7 export scenarios | `export-intro` "What the next calls do" table | Table re-summarizes content reachable in next firing atom (per-env classification, importYaml/zeropsYaml). | Trim table to call shape (inputs + status name only); leave content explanation to status-specific atoms. |

### Stale-firing (MED severity)

| ID | Agent | Scenarios | Atom | Observation | Fix |
|---|---|---|---|---|---|
| A4-S1 | A4 | git-push-{configured-webhook, unconfigured} | `develop-deploy-files-self-deploy` | Atom fires universally for develop-active. In git-push pair scenarios delivery is cross-deploy via git-push; self-deploy invariant is irrelevant reference. | Add `closeDeployModes:[auto, manual, unset]` (drop git-push). |
| A4-S2 | A4 | git-push-{configured-webhook, unconfigured} | `develop-dynamic-runtime-start-container` | Atom matches dev-half. For git-push delivery, runtime restarts only on actual deploy — manual dev_server cycling not needed. "After every redeploy, re-run action=start" is auto/zcli-push-specific. | Add `closeDeployModes:[auto, manual, unset]` filter. |
| A3-S1 | A3 | first-deploy-recipe-implicit-standard | `develop-first-deploy-asset-pipeline-container` | Atom fires for all never-deployed implicit-webserver+container. Body assumes Laravel/Vite or Symfony+Encore. Wrong for backend-only PHP API. | Make body framework-conditional OR split into framework-specific guidance referenced by recipe markdown (per `feedback_atoms_vs_recipes_split` memory). |
| A6-S1 | A6 | export/classify-prompt | `export-classify-envs` "Schema validation" tail | Body discusses publish-ready/validation-failed response shape from inside classify-prompt-gated atom. Forward-status content claim. | Strip the cross-status forward-reference. |

### Order/priority (LOW severity)

| ID | Agent | Scenarios | Observation | Fix |
|---|---|---|---|---|
| A2-O2 | A2 | bootstrap/classic/provision-local | `bootstrap-wait-active` (priority 3) renders AFTER `bootstrap-provision-local` step list (priority 2). Provision-local says "After services reach RUNNING:…" — but wait condition belongs BEFORE the post-running steps. | Lower wait-active priority to 1, OR restructure provision-local to drop post-running steps. |
| A4-O2 / O2 (baseline) | A4+baseline | first-deploy scenarios + post-adopt-standard-unset | `develop-auto-close-semantics` renders BEFORE `develop-first-deploy-execute` (and BEFORE `develop-standard-unset-promote-stage`). Reader sees "session closes when deploy + verify pass" before being told to actually run deploy. | Lower auto-close-semantics priority so it renders after execute + verify. |

### Evidence-required (LOW severity, per proposed plan §11.x evidence rule)

| ID | Agent | Atom | Claim | Fix |
|---|---|---|---|---|
| E1 (baseline) / A3-E1 | baseline+A3 | `develop-first-deploy-execute.md:444` | "expect 30–90 seconds for dynamic runtimes and longer for `php-nginx` / `php-apache`" | Drop timing OR cite `references-fields` OR live-eval pointer. |
| A2-E1 | A2 | `bootstrap-wait-active.md` | "Production services take 30–90 seconds…" | Drop OR cite. |
| A2-E2 | A2 | `bootstrap-adopt-discover.md` | "After adopt close, the envelope reports each adopted hostname with `bootstrapped: true`; close-mode + git-push capability are left empty (develop configures them on first use)" | The "left empty (develop configures them on first use)" assertion is forward-looking platform-mechanic. Add comment pointer to develop-close-mode-* atoms or rephrase. |
| A3-E1 | A3 | `develop-close-mode-auto-deploy-container.md:17` | "`zerops_deploy` SSHes using ZCP's runtime container internal key." | Implementation-detail claim. Add `references-fields:` cite OR drop OR add live-eval pointer. |
| A6-E2 | A6 | `export-publish-needs-setup.md:45` | "(Phase 6 of the export-buildFromGit plan adds an automatic refresh on every export pass — once that lands, the manual re-run is reserved for…)" | Forward-reference to ARCHIVED plan. Same rot-vector class as the dropped §3.x cites. **Strip the parenthetical entirely.** |
| A6-E3 | A6 | `export-validate.md` warning code blocks | Sample warning strings embed `(plan §3.4 M2)`, `(plan §3.4 M4)`, `(plan §3.4)` | Atom CHOSE these particular samples — cite leaks into rendered guidance. | Substitute samples with stripped versions. |
| A6-E4 | A6 | `export-publish-needs-setup`, `export-validate` (also classify-envs) | Body uses "Phase A / Phase B / Phase C" terminology with no glossary | Replace with semantic names ("scope-/variant-prompt step" / "classify step" / "publish step") OR introduce one-sentence glossary in `export-intro`. |

### Cross-ref hygiene (MED — NEW class proposed by A1)

| ID | Agent | Atom | Observation | Fix |
|---|---|---|---|---|
| A1-2 | A1 | `bootstrap-recipe-close.md:19`, `idle-develop-entry.md:20`, `bootstrap-close.md:23` (corpus-wide pattern) | Forward atom-id references in agent-facing body prose: "develop-strategy-review surfaces…", "Auto-close semantics: develop-auto-close-semantics", "develop-first-deploy-intro fires on entry". Agent has NO atom-lookup mechanism. References to atoms not firing in current envelope are dead-ends. | Treat atom-IDs in body prose as a `TestAtomAuthoringLint` candidate forbidden pattern (mirroring §11.2 ban on plan-doc paths). For each ref: either inline the actual instruction the referenced atom would give, or drop. **Phase 4 lint candidate.** |

### Path-leak (LOW)

| ID | Agent | Atom | Observation | Fix |
|---|---|---|---|---|
| A1-4 | A1 | `bootstrap-resume.md:32` | Body mentions `.zcp/state/services/<hostname>.json` — env-shaped path with no `environments:` axis. Per memory `feedback_atoms_layered_knowledge.md`, env-shaped paths belong in boot shims. | Drop path detail; reference boot shim. |

### Cosmetic / over-claim (LOW)

| ID | Agent | Atom | Observation | Fix |
|---|---|---|---|---|
| A1-3 | A1 | `bootstrap-route-options` + `idle-adopt-entry` | Same start imperative repeated (same-class as R1). | Tighten route-options to TABLE only; let entry atoms own imperative. |
| A1-5 | A1 | `bootstrap-route-options.md:20-21` | "Pick one option, then call `start` again with its route plus required `recipeSlug` / `sessionId`" — adopt and classic need neither. Over-claim. | Trim to "…with the route-specific args from the table below". |
| A1-6 | A1 | `bootstrap-recipe-close.md:19` | "every service the recipe provisioned appears with `bootstrapped: true` and `closeMode: unset`" — partial-failure case + closeMode hardcode. | Soften OR replace with procedural inspection. |
| A2-C1 | A2 | `develop-api-error-meta.md` | Internal placeholder inconsistency: line 33 uses `{hostname}.mode`, line 41 uses `<host>.mode`. | Standardize on `<host>` literal. |
| A2-C2 | A2 | `bootstrap-adopt-discover.md:11` | Singular "Adoption attaches ZCP tracking to an existing runtime service" but fixture has 2 services. | Pluralize OR use neutral "one or more". |
| A4-C1 | A4 | `develop-close-mode-git-push-needs-setup.md:22` | "Once setup completes for every service in the pair, develop-close-mode-git-push takes over" — `git-push-setup` is per-PAIR (one call writes meta for both). | Replace "every service in the pair" → "the pair completes setup". |
| A4-C2 | A4 | `develop-close-mode-auto.md:33` + `develop-strategy-awareness.md:34-37` | Per-service close-mode commands emit as separate calls; action accepts a multi-service map. | Coalesce: `closeMode={"appdev":"auto", "appstage":"auto"}`. |
| A5-C1 | A5 | `develop-closed-auto.md` | Filename + ID is `develop-closed-auto` but close-mode `auto` and auto-CLOSE mechanism are different concepts — confusing double meaning. | Defer; revisit if A5-M1 path (b) ships sibling `develop-closed-iteration-cap` atom. |
| A6-C2 | A6 | `scaffold-zerops-yaml.md` (audit-test drift) | Audit promises `"minimal valid zerops.yaml"`, test loosened to `"minimal"`, golden has `"minimal valid yaml"`. Three sources disagree. | Align audit + test + golden on one phrasing. |
| C1 (baseline) | baseline+A1+A2+A3+A4 | `develop-api-error-meta` | Atom name suggests develop-only; fires across phases. | Rename to `general-api-error-meta` OR document. |

---

## Fixture-reality drift (Step 0 spot-check follow-up)

| ID | Scenario | Drift | Fix |
|---|---|---|---|
| A3-F1 | `develop/first-deploy-recipe-implicit-standard` | Fixture sets `WorkSession: fixSession("appdev")` (single hostname). Production session-start auto-expands a standard-pair scope to BOTH halves so `appstage` lands in scope (`internal/tools/workflow_develop.go:301-323`; pinned by `workflow_develop_test.go:341-361`). | Change fixture to `fixSession("appdev", "appstage")`. Cascade: any per-service-axis atom matching ModeStage will newly fire on appstage; affected goldens regenerate. |

---

## Cross-cutting systemic findings

These aren't single atom defects but corpus-level patterns surfaced by multiple agents:

1. **Adopt route is a second-class citizen** (S5 — A2 expanded baseline L2): four atoms blanket-fire for adopt route despite body content being classic-construction-only. Adopt path is missing dedicated provision/close atoms.

2. **Service-state assertions without axis backing** (S3, S4, A3-L2, A3-L3, A4-L2, A4-L3): atoms hardcode current-state ("dev process is already running", "Apache is already running", "first deploy landed and verified", "auto-close fires automatically") without the axes that would universally guarantee those states. Procedural-form principle (plan §11.1 bullet 7) is the spec-level fix; this ledger surfaces 6+ concrete instances.

3. **Pair-language without modes-axis backing** (S4): three atoms say "pair" but have no `modes:` filter narrowing to pair modes only.

4. **"After every redeploy" content scattered**: ~5 atoms repeat the dev-process-dies-on-redeploy mechanic. Could consolidate into one atom referenced by others (per A1-2 atom-id-cross-ref idea — but with body prose, not just frontmatter).

5. **Plan/amendment cite rot** (A6-E2, A6-E3, A1-2 partial): forward references to ARCHIVED plans, sample warning strings embedding plan cites, atom-id references to atoms not firing — all are rot vectors. Phase 4 lint candidates.

6. **RUNNING vs ACTIVE corpus inconsistency** (A2-L4 + adjacent): multiple atoms specify "ACTIVE" when handler accepts RUNNING OR ACTIVE. Worth a corpus sweep.

7. **Priority 0 silently coerced** (S1): two intro atoms render LATE instead of FIRST due to parser silently mapping out-of-range priorities. Single-fix high-impact.

8. **Forward-references to non-firing atoms in body prose** (A1-2): atom-id mentions in agent-visible text are dead-ends. Lint candidate.

---

## Recommended Cycle 1 scope (Tier S only — high-impact, multi-agent consensus)

These should ship in Cycle 1 to maximize impact-per-fix:

1. **S1** — `priority: 0` parser fix. Single edit, fixes ordering for ~16 of 30 scenarios (every scenario firing intro atoms).
2. **S2** — `develop-closed-auto.md` body rewrite + companion fix for closed-iteration-cap missing-critical (A5-M1). Removes the canonical lie L1.
3. **S3** — `develop-close-mode-auto-workflow-dev.md` "dev process is already running" → inspection. Removes most-explicit lie that contradicts co-firing atom.
4. **S4** — pair-language fix (3 atoms: `develop-close-mode-auto`, `develop-close-mode-git-push`, `develop-close-mode-git-push-needs-setup`). Universal "service" replacement.
5. **S5** — adopt-route atom-set tightening (4 atoms: `bootstrap-mode-prompt`, `bootstrap-provision-rules`, `bootstrap-provision-local`, `bootstrap-env-var-discovery`). Drop adopt from `routes:` axes; author missing adopt-route atoms in Cycle 2.
6. **S6** — `develop-strategy-awareness` aggregate-render structural fix (filter directive to dev-half hostnames for git-push-setup / build-integration commands). Atom-model-level change; removes invalid commands from rendered output.
7. **S8** — `develop/mode-expansion-source` fixture differentiation (change to ModeSimple). Restores unique scenario coverage.
8. **A3-F1** — `develop/first-deploy-recipe-implicit-standard` fixture standard-pair scope expansion.

That's **8 Cycle 1 fixes**. Goldens regenerate for ~20 of 30 affected goldens. Re-review pass focuses on the regenerated text.

**Cycle 2 candidates**:
- All HIGH single-agent lies (A3-L2, A3-L3, A3-L6, A4-L2, A4-L3, A2-L4, A2-L5, A2-L7, A6-L3, A6-L4)
- Missing-critical (A4-M1)
- Stale-firing (A4-S1, A4-S2, A3-S1, A6-S1)
- Substantial redundancy (A4-R1, A4-R2, A6-R9, A6-R10, A6-R11, etc.)
- Order (A2-O2, O2/A4-O2)

**Cycle 3 candidates** (or accept as documented):
- Evidence-required (A2-E1, A2-E2, A3-E1, A6-E2, A6-E3, A6-E4, baseline E1)
- Cosmetic
- Path-leak (A1-4)
- Cross-ref hygiene (A1-2 — Phase 4 lint candidate)

**Out-of-cycle (structural — needs new axis)**:
- **S7 (A3-L5)** — failure-tier session awareness. Needs new `attemptTier:` envelope axis. Plan §11.x scope creep; defer to follow-up plan.
- Recipe-specific framework knowledge (A3-S1, related) — atom vs recipe boundary issue per `feedback_atoms_vs_recipes_split` memory.

---

## Notes on review depth

- Total agent runtime: ~10-15 minutes per agent per scenario × 30 scenarios distributed across 6 parallel agents.
- Each agent independently read the plan, spec §11, atom corpus excerpts (~80 atoms total reads), and source code (`internal/workflow/atom.go`, `synthesize.go`, `compute_envelope.go`, `work_session.go`, `workflow_develop.go`, `workflow_git_push_setup.go`, etc.) to verify claims against handler behavior.
- **Cross-agent consensus is the strongest signal**: priority-0 (5/6 agents), L1 lie (A5 verbatim + A3 source-read), pair-language lie (A3+A4), verify-state lie (A3+A4).
- Single-agent findings have the agent's reasoning in their per-agent output (referenced above by ID).

## Status

**PAUSE POINT 2 active**. Awaiting human approval/amendment of:
1. **Cycle 1 scope** (S1-S6 + S8 + A3-F1, 8 fixes) — recommended.
2. Any single-agent findings to PROMOTE into Cycle 1.
3. Any Tier S findings to DEMOTE (deferred).
4. **Edits to this ledger** (additional defects, rebalanced severities, removed ones).

After approval, the Cycle 1 batch fix proceeds: edit affected atoms / parser / fixtures, regenerate goldens via `ZCP_UPDATE_ATOM_GOLDENS=1`, re-review the regenerated text, then either land Cycle 2 or finalize (strip UNREVIEWED markers, enable `ZCP_GOLDEN_COMPARE` default-on).
