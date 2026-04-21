# HANDOFF-to-I8-v37-prep.md — build analysis harness, land fix stack, commission v37

**For**: fresh Claude Code instance picking up zcprecipator2 after v36 analysis. Your job is to **build the analysis harness first**, **land the v36-surfaced fix stack**, then **commission v37** as the full end-to-end confirmation run.

**Reading order**: this doc first (~45 min), then [`runs/v36/CORRECTIONS.md`](runs/v36/CORRECTIONS.md), then [`runs/v36/verdict.md`](runs/v36/verdict.md) (revised). The original [`runs/v36/analysis.md`](runs/v36/analysis.md) is preserved but superseded — it documents the state but carries false claims that `CORRECTIONS.md` lists. Read analysis.md as context, not truth.

**Branch**: `main`. **Previous runs**: v35 (v8.105.0, stalled at deploy), v36 (v8.108.0+v8.108.1, reached finalize but produced broken deliverable). **Your target**: v37 against whichever tag the fix stack lands as (expected v8.109.0).

---

## 1. Slots to fill at start

```
FIX_STACK_TAG:            v8.109.0                             (local; push via `git push origin main v8.109.0`)
FIX_STACK_COMMITS:        8eb7c29  feat(zcprecipator2): mechanical analysis harness
                          8fa9d1b  Cx-ENVFOLDERS-WIRED          (F-9 close)
                          d5f0e02  Cx-MARKER-FORM-FIX           (F-12 close)
                          3fca235  Cx-STANDALONE-FILES-REMOVED  (F-13 close)
                          b301941  Cx-CLOSE-STEP-STAGING        (F-10 close)
                          e6c87c0  Cx-CLOSE-STEP-GATE-HARD      (F-8/F-11 close)
HARNESS_TAG:              v8.109.0 (bundled)
ANALYSIS_HARNESS_PATH:    cmd/zcp/analyze/ + internal/analyze/
V37_COMMISSION_DATE:      <unfilled — user commissions>
V37_SESSION_ID:           <unfilled — user commissions>
V37_OUTCOME:              <unfilled — post-run>
```

### Phase 1 + Phase 2 completion notes

- Cx-ATOM-TEMPLATE-LINT (planned as a separate Cx commit) was bundled with the harness commit (`8eb7c29`). The `tools/lint/atom_template_vars/` scan walks every atom under `internal/content/workflows/recipe/` and fires on any `{{.Field}}` reference naming a field outside `DefaultAllowedAtomFields`. Wired into `make lint-local` via the `lint-atom-template-vars` target and into CI via the `tools/lint/atom_template_vars/_test.go` suite.
- v36 retrospective against the harness (`zcp analyze recipe-run` against `/Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v36/`) mechanically surfaces F-9 (B-15 observed=6), F-10 (B-16 observed=0/6), F-12 (B-17 observed=18 via merged deliverable + session Edit evidence), F-13 (B-18 observed=6 via merged deliverable + session Write evidence) — every gate in the Phase-1 §8 success criterion passes.
- `make lint-local` (catalog-sync + recipe-atom + atom-template-vars + golangci) and `go test ./... -count=1 -race` are both green at HEAD (commit `e6c87c0`, tag `v8.109.0`).

If any slot is `<unknown>` because the upstream phase hasn't run yet, that's expected — this handoff covers three phases (harness build → fix stack → v37 commission → v37 analysis). Fill slots as phases complete.

---

## 2. The meta-lesson from v36 (most important)

The v36 analysis was commissioned against HANDOFF-to-I7. It shipped as commit `67221ba` with verdict "ACCEPT-WITH-FOLLOW-UP". **That verdict was wrong.** It missed at least 5 defect classes that sat in plain view in the deliverable tree and session JSONL. Every defect was surfaced only after the user pointed at specific evidence.

The failure mode was not "insufficient time" or "wrong bars". It was **the analysis process rewarded artifact-shape-as-proxy-for-depth**. The handoff told the analyst to produce 5 files in specific shapes; the analyst produced them with convincing surface structure; nothing in the process forced verification of the claims inside. "Unmeasured — close unreached" became a universal alibi for skipping on-disk files that could have been read.

**Your job is to not reproduce this.** The analysis harness you're building first is the structural mechanism that makes the shortcut impossible. Read §6 (Analysis discipline rules) before writing any verdict on v37.

---

## 3. Required reading (in order, ≈ 60 min)

1. **This document** — you're reading it.
2. **[`runs/v36/CORRECTIONS.md`](runs/v36/CORRECTIONS.md)** — specific defects the first v36 pass missed and why. ≤ 10 min.
3. **[`runs/v36/verdict.md`](runs/v36/verdict.md)** (revised) — the corrected v36 call: PAUSE pending fix stack. Replaces the original ACCEPT-WITH-FOLLOW-UP verdict.
4. **[`spec-recipe-analysis-harness.md`](spec-recipe-analysis-harness.md)** — harness Tier 1/2/3 design. The thing you're building.
5. **[`HANDOFF-to-I7-v36-analysis.md`](HANDOFF-to-I7-v36-analysis.md)** — prior handoff. Context for what v36 analysis tried to do.
6. **[`HANDOFF-to-I6.md`](HANDOFF-to-I6.md)** §Cx-BRIEF-OVERFLOW..§Cx-KNOWLEDGE-INDEX-MANIFEST — the fix stack v8.108.0 shipped (F-1..F-6 closures that DID work on v36).
7. **[`runs/v36/analysis.md`](runs/v36/analysis.md)** — context only; see §2 caveat.
8. **[`../../CLAUDE.md`](../../CLAUDE.md)** — auto-loaded; re-read for TDD/operating norms.
9. **Raw v36 artifacts** at `/Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v36/`.

---

## 4. v36 corrected defect inventory

Eight systemic defects surfaced on v36. F-1..F-6 closed by v8.108.0 (real). F-7 closed pre-run by v8.108.1 (real). F-8..F-16 are open or new.

| # | Defect | Origin | Type | Evidence |
|---|---|---|---|---|
| F-1 | Dispatch brief overflow | v35 | **CLOSED** by Cx-BRIEF-OVERFLOW (v8.108.0) | Max response 11.5 KB on v36; writer stitched correctly |
| F-2 | Check Detail Go notation | v35 | **CLOSED** by Cx-CHECK-WIRE-NOTATION | Zero Go struct.field in check details |
| F-3 | Iterate fake-pass | v35 | **UNREACHED** fix-shipped | No iterate calls on v36 |
| F-4 | Skip on mandatory step | v35 | **PASSED** gate held + recovery clean | 1 skip attempt at 15:09:38, engine refused |
| F-5 | Unknown/zero-byte guidance | v35 | **CLOSED** by Cx-GUIDANCE-TOPIC-REGISTRY | 0 unknown topics on v36 |
| F-6 | Knowledge misses manifest atoms | v35 | **UNREACHED** fix-shipped | No knowledge queries issued for wire-contract atoms |
| F-7 | Predicate-gated topics on nil plan | v36-attempt-1 | **CLOSED** by Cx-PLAN-NIL-GUIDANCE (v8.108.1, commit `c512757`) | Research-step tier-only lookups resolved on v36 |
| F-8 | Close-step gate bypassable via sessionless export | v36 | **OPEN** — addressed by Cx-CLOSE-STEP-GATE-HARD below | [flow-main.md row 204](runs/v36/flow-showcase-v36-main.md#L204): `zcp sync recipe export … --include-timeline` (no `--session`) → "skipping close-step gate" → .tar.gz produced. Editorial-review + code-review never ran. |
| **F-9** | Writer brief atom references un-populated `{{.EnvFolders}}` template variable; main agent invents slug names | v36 | **SYSTEMIC OPEN** — addressed by Cx-ENVFOLDERS-WIRED below | [canonical-output-tree.md:18](../../internal/content/workflows/recipe/briefs/writer/canonical-output-tree.md#L18) uses `{{index .EnvFolders i}}`; full-tree grep: `.EnvFolders` never populated in Go. Writer dispatch prompt at [recipe-writer-sub-agent.md:95](runs/v36/flow-showcase-v36-dispatches/recipe-writer-sub-agent.md#L95) shows invented slug paths `dev-and-stage-hypercde`, `local-validator`, `prod-ha`, `remote-cde-and-stage`, `small-prod`, `stage-only`. Writer created 6 ghost env dirs at [sub-writer-1.md row 40](runs/v36/flow-showcase-v36-sub-writer-1.md). Result: dual env tree (6 canonical + 6 ghost). Reproduces every showcase run. |
| **F-10** | Writer per-codebase markdown stranded — not committed to git, stripped by export | v36 | **SYSTEMIC OPEN** — addressed by Cx-CLOSE-STEP-STAGING below | Writer authored 12 files at `/var/www/{codebase}/{README,CLAUDE,GOTCHAS,INTEGRATION-GUIDE}.md`. Export via [export.go:355](../../internal/sync/export.go#L355) uses `git ls-files`; writer never committed; zero per-codebase markdown in tarball. |
| **F-11** | Close-step gate is advisory (same surface as F-8, different framing — the CLI explicitly documents the gap) | v36 | **SYSTEMIC OPEN** — folded into Cx-CLOSE-STEP-GATE-HARD | [export.go:110](../../internal/sync/export.go#L110): tool message literally says "exporting without close produces an incomplete deliverable (per-codebase READMEs + CLAUDE.md not staged, no code-review signals)" — it KNOWS but doesn't block. |
| **F-12** | Writer brief atom shows extract markers without trailing `#`; canonical form has trailing `#` | v36 | **SYSTEMIC OPEN** — addressed by Cx-MARKER-FORM-FIX below | [content-surface-contracts.md:71](../../internal/content/workflows/recipe/briefs/writer/content-surface-contracts.md#L71) documents marker as `<!-- #ZEROPS_EXTRACT_START:integration-guide -->` (missing `#`). Scaffold emits `<!-- #ZEROPS_EXTRACT_START:intro# -->` ([recipe_templates_app.go:21-48](../../internal/workflow/recipe_templates_app.go#L21)). Writer wrote wrong form; fix pass spent 20+ Edit calls correcting. |
| **F-13** | Writer atoms prescribe standalone `INTEGRATION-GUIDE.md` + `GOTCHAS.md` per codebase that duplicate fragment content; not consumed by publish pipeline | v36 | **SYSTEMIC OPEN** — addressed by Cx-STANDALONE-FILES-REMOVED below | [canonical-output-tree.md:11-12](../../internal/content/workflows/recipe/briefs/writer/canonical-output-tree.md#L11) lists them. [content-surface-contracts.md:63,81](../../internal/content/workflows/recipe/briefs/writer/content-surface-contracts.md#L63) says "README fragment **+** standalone". Writer authored 6 extras per showcase run. Zero consumers. User-confirmed: fragments only. |
| **F-14** | Writer-1 first-pass output: 3 READMEs had NO fragment markers at all | v36 | OPEN — writer-compliance class, may be single-run anomaly or systemic | [flow-main.md row 170](runs/v36/flow-showcase-v36-main.md#L170) check results: `fragment_intro / fragment_integration-guide / fragment_knowledge-base — missing fragment markers` × 3 codebases = 9 failing checks after writer-1. F-12 contributes but doesn't explain total absence. |
| **F-15** | Writer IG items skipped fenced code blocks | v36 | OPEN — writer-compliance class | Round-2 check at 15:52:35: `app_integration_guide_per_item_code — integration-guide H3 section(s) with no fenced code block`. Writer atom content-surface-contracts.md:71 REQUIRES fenced code blocks. Writer ignored. |
| **F-16** | Writer knowledge-base fragments missing `### Gotchas` H3 heading | v36 | OPEN — writer-compliance class | Round-2 check: `knowledge_base_gotchas — knowledge-base fragment must include a Gotchas section`. Atom content-surface-contracts.md:93 specifies. Writer ignored. |

**Structural vs behavioral**: F-8..F-13 are **source-level defects** that reproduce deterministically every run. Fixing them is bounded engineering. F-14..F-16 are **writer-behavior defects** — writer ignored explicit brief instructions in 9 distinct ways on v36's single writer-1 pass. Whether they're a single-run noise floor or a systemic "writer brief is too long for the writer to obey" signal is an open question v37 answers.

**Writer-fix retry history, corrected**:

My first v36 verdict said "1 cosmetic fix pass". That was wrong. The session actually did:
- Round 1 (15:48:41): 9 "missing fragment markers" failures after writer-1.
- Round 2 (15:52:35): 5+ new failures (comment_ratio 0%, blank_after_marker, knowledge_base_gotchas missing, intro_length, IG per-item-code missing).
- Round 3 (15:55:51): 1 remaining (worker comment_ratio 27%).
- Round 4 (15:56:20): green.

That's **4 check rounds + 2 sub-agent dispatches** (writer-1 + writer-2-fix) for the readmes substep alone. Under F-14..F-16 the writer ignored brief instructions at high frequency. v36's C-1 bar passed (≤ 2 deploy rounds) only because the deploy-step rolls readmes-substep checks up into one deploy-complete call — but the underlying check-round count was 4.

---

## 5. Operating order: harness FIRST, then fix stack, then v37

Rationale: v36's failure mode was analysis rigor, not code velocity. If we commission v37 without the harness in place, the analysis instance (even this fresh one) will hit the same shortcuts I did. The harness is the mitigation.

### Phase 1 — Build analysis harness (target: 1–2 days focused work)

**Spec**: [`spec-recipe-analysis-harness.md`](spec-recipe-analysis-harness.md). Read it in full before coding.

**Deliverable**:
- `cmd/zcp/analyze/analyze.go` — CLI entry point (`zcp analyze recipe-run <deliverable-dir> <sessions-logs-dir>`)
- Tier-1 structural bars (B-15 through B-24) implemented as measurement units
- Tier-2 checklist generator (`zcp analyze generate-checklist <machine-report.json>`)
- Tier-3 commit hook (`.githooks/verify-verdict`) that blocks verdict commits failing citation rules
- `tools/lint/atom_template_vars.go` — build-time catch for unbound template vars in atoms

**Validation test**: run the harness against v36's deliverable tree at `/Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v36/`. **Expected output**: harness mechanically surfaces F-9 (ghost dirs), F-10 (missing per-codebase markdown in tarball), F-12 (wrong marker form), F-13 (standalone files present). If it DOESN'T flag these, the bars aren't sharp — iterate before moving to Phase 2.

**Success criterion**: `zcp analyze recipe-run nestjs-showcase-v36/ SESSIONS_LOGS/` emits a JSON report where:
- `B-15_ghost_env_dirs.status == "fail"` with `observed: 6`
- `B-16_tarball_per_codebase_md.status == "fail"` with `observed: 0`
- `B-17_marker_exact_form.status == "fail"` with 3 file:line pointers
- `B-18_standalone_duplicate_files.status == "fail"` with count and paths
- `B-20_deploy_readmes_retry_rounds.observed == 4`
- `B-23_writer_first_pass_failures.observed == 9`

If the harness retrospectively catches v36's real defects, it will catch v37's.

### Phase 2 — Land fix stack (target: 2–3 days, in parallel with harness if resourced)

Six Cx-commits. Ordered by dependency. Each must land with green `make lint-local` + `make test-race` + its RED-GREEN tests. Tag as v8.109.0 after final commit.

#### Cx-ENVFOLDERS-WIRED (F-9)

**Scope**: populate `.EnvFolders` template variable from `recipe_templates.go envTiers[].Folder`. Route through brief-atom rendering path so the atom body returned via `handleDispatchBriefAtom` has concrete paths, not unresolved template syntax.

**Files touched**:
- `internal/workflow/atom_loader.go` — add template-render pass on atom load, passing plan context (include `CanonicalEnvFolders()` helper).
- `internal/workflow/recipe_templates.go` — export `CanonicalEnvFolders() []string { return extract(envTiers[].Folder) }`.
- `internal/content/workflows/recipe/briefs/writer/canonical-output-tree.md` — no text change; template interpolation will now resolve.
- Similar for `completion-shape.md`, `self-review-per-surface.md` if they use the variable.

**RED test**: `TestWriterAtom_EnvFoldersResolved` — fetch `briefs.writer.canonical-output-tree`, assert body contains `0 — AI Agent/README.md` AND does not contain `{{`.

**Acceptance on v37**: writer dispatch prompt (captured in `flow-dispatches/recipe-writer-sub-agent.md`) contains canonical numbered env paths. Writer writes `environments/0 — AI Agent/README.md`. Zero ghost dirs in deliverable.

#### Cx-ATOM-TEMPLATE-LINT (F-9 prevention)

**Scope**: build-time lint that walks every atom `*.md`, extracts every `{{...}}` template expression, asserts each field name is populated by at least one known render path.

**Files touched**:
- `tools/lint/atom_template_vars.go` (new) — walker + extractor + allowed-fields set
- `Makefile` — add to `lint-local` chain

**Allowed fields list** (from what Go code actually populates): `ProjectRoot`, `Hostnames`, `EnvFolders`, `Framework`, `Slug`, `Tier`. Any other `{{.X}}` is a lint failure.

**RED test**: fixture atom in `internal/content/workflows/recipe/briefs/writer/` with `{{.FakeField}}` → `make lint-local` fails.

**Acceptance**: prevents re-introduction of F-9-class defects via atom edits.

#### Cx-MARKER-FORM-FIX (F-12)

**Scope**: correct the marker form in every atom that shows it + tighten the check regex to enforce exact form.

**Files touched**:
- `internal/content/workflows/recipe/briefs/writer/content-surface-contracts.md` — update lines 71, 93, wherever marker is shown without trailing `#`.
- `internal/ops/checks/manifest.go:404+408+458+462` — change `strings.Contains(line, "ZEROPS_EXTRACT_START:intro")` to exact-form regex `regexp.MustCompile(\`<!-- #ZEROPS_EXTRACT_START:intro# -->\`)`.
- `internal/ops/checks/manifest_test.go` — add negative test: README with `:intro` (no trailing `#`) fails the check.

**RED test**: `TestMarkerFormCheck_RejectsMissingTrailingHash` — feed README with marker missing trailing `#`, assert check fails with clear message.

**Acceptance on v37**: writer first-pass output has correct markers. Zero marker-fix Edit cycles in sub-writer sessions.

#### Cx-STANDALONE-FILES-REMOVED (F-13)

**Scope**: delete the standalone-file requirement from writer atoms. Fragments in README remain; standalone INTEGRATION-GUIDE.md / GOTCHAS.md go away entirely.

**Files touched**:
- `internal/content/workflows/recipe/briefs/writer/canonical-output-tree.md` — delete lines 11-12 (the two standalone file bullets).
- `internal/content/workflows/recipe/briefs/writer/content-surface-contracts.md` — delete the `+ INTEGRATION-GUIDE.md` and `+ GOTCHAS.md` suffixes in lines 63 and 81 + the matching Surface-summary-table rows 140, 141.
- `internal/content/workflows/recipe/briefs/writer/completion-shape.md` — remove any references to standalone files.
- `internal/content/workflows/recipe/briefs/writer/self-review-per-surface.md` — remove file-existence checks for the standalones.

**RED test**: grep assertion — none of the writer atoms mention `INTEGRATION-GUIDE.md` or `GOTCHAS.md` as standalone paths.

**Acceptance on v37**: writer authors 2 files per codebase (README.md + CLAUDE.md), not 4. Zero standalone duplicates in deliverable.

#### Cx-CLOSE-STEP-STAGING (F-10)

**Scope**: close-step has an explicit engine-side action that stages writer output into the recipe output tree. This is the "staging" contract [export.go:110](../../internal/sync/export.go#L110) already describes but doesn't execute.

**Files touched**:
- `internal/workflow/engine_recipe.go` — new method `stageRecipeContent(state *WorkflowState)` called during `RecipeComplete(step="close")`. For each `{codebase}` in plan.Targets (runtime, non-shared-worker), copy `/var/www/{codebase}/{README,CLAUDE}.md` (post-F-13 list) into `/var/www/zcprecipator/{slug}/{codebase}/`. Fail close-step if any expected file missing.
- `internal/ops/checks/manifest.go` — new check `writer_content_staged` — verifies the staging happened.
- Optional Git stage: `git -C /var/www/{codebase} add README.md CLAUDE.md && git commit -m "recipe content: staged from writer sub-agent"`. Needed if the app-repo publish flow reads commits not working-tree.

**RED test**: `TestCloseStep_StagesWriterContent` — session with writer output on `/var/www/{codebase}/`, call `complete step=close`, assert files copied + check passes.

**Acceptance on v37**: deliverable tree under `/var/www/zcprecipator/{slug}/{codebase}/` contains README.md + CLAUDE.md after close-step complete. Tarball includes them.

#### Cx-CLOSE-STEP-GATE-HARD (F-8 / F-11)

**Scope**: `zcp sync recipe export` refuses sessionless invocation when a live recipe session exists for the target directory. Turns the advisory note into a hard block. `--force-export` stays as explicit escape.

**Files touched**:
- `internal/sync/export.go` — detect live session: walk session state dir, match any session whose `OutputDir` or any `--app-dir` argument matches. If match AND `--session` unset AND not `--force-export`, return error with exit code 2 + message naming session ID + remediation commands.
- `internal/sync/export_test.go` — negative test: simulate live session, invoke sessionless export, assert exit code 2 + error message.

**RED test**: `TestExport_RefusesSessionlessWhenLiveSessionExists`.

**Acceptance on v37**: agent cannot `zcp sync recipe export <dir>` without `--session` if a live session exists for `<dir>` or any `--app-dir`. Agent sees error telling them to complete close-step first.

### Phase 3 — v37 commission (1 session, ≈ 2 hours of active-work under good conditions)

Tag under test: v8.109.0 (fix-stack release).

**Commission spec** (see [`HANDOFF-to-I7-v36-analysis.md`](HANDOFF-to-I7-v36-analysis.md) §Slots-to-fill for format):

```
TIER:             showcase
SLUG:             nestjs-showcase  (same framework as v34-v36 for comparability)
FRAMEWORK:        nestjs
COMMISSIONED_BY:  user
AGENT_MODEL:      claude-opus-4-7[1m]
MUST_REACH:       close-step complete (action=complete step=close accepted)
MUST_DISPATCH:    editorial-review, code-review sub-agents
MUST_RUN:         close-browser-walk (soft-pass acceptable if agent-browser environmentally broken; hard-fail if skipped without attempt)
```

**During-run tripwires**: set on the main session JSONL (user + analyst monitor concurrently):
- Any `Write` to `environments/<dir>/` where `<dir>` not in `{"0 — AI Agent", "1 — Remote (CDE)", "2 — Local", "3 — Stage", "4 — Small Production", "5 — Highly-available Production"}` → **halt + alert**. F-9 regression.
- Any `zcp sync recipe export` tool call without `--session` → **halt + alert**. F-11 regression.
- Any `action=complete step=deploy` with `checkResult.passed==false` AND `len(failing_checks) > 3` on same substep → **halt + alert**. Writer-compliance regression (F-14/15/16 didn't close).
- Any `dispatch-brief-atom` response > 20 KB → **alert** (B-9 signal).
- Close-step `in_progress` for > 30 min without dispatching editorial-review → **alert**.

**Post-run artifacts required** (handed to analysis instance):
- Deliverable tree at `/Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v37/`
- SESSIONS_LOGS/ populated with main + sub-agent JSONLs
- User-authored TIMELINE.md describing what the user observed + AFK gaps
- Session-end state export: `action=status` response capturing close-step state

### Phase 4 — v37 analysis via harness

**DO NOT REPRODUCE v36'S ANALYSIS FAILURE.** The structural discipline is:

1. Run harness first: `zcp analyze recipe-run <v37-dir> <logs-dir> > runs/v37/machine-report.json`. Commit this before anything else.
2. Generate checklist: `zcp analyze generate-checklist runs/v37/machine-report.json > runs/v37/verification-checklist.md`.
3. Fill checklist cells:
   - Structural rows auto-populated from machine-report.
   - Content-quality rows REQUIRE analyst Read tool call on each file + row-level grade + file:line evidence. No bulk judgments.
   - Read-receipt timestamps enforce that the Read actually happened.
4. Draft verdict — every PASS/FAIL claim cites `[checklist X-Y]` or `[machine-report.structural_integrity.B-N]`.
5. Commit hook enforces citation rule + checklist completeness. Verdict without proper citations fails pre-commit.

**Read every writer-authored content file.** For v37, that's:
- 1 root README.md (if F-13 removed standalones, 1 is the count)
- 3 per-codebase README.md (apidev, appdev, workerdev)
- 3 per-codebase CLAUDE.md
- 6 env READMEs (canonical numbered names only if F-9 closed)
- 6 env import.yaml (comment blocks)
- ZCP_CONTENT_MANIFEST.json (every fact)
- TIMELINE.md

Total: ~20 files. Reading them is ~60 minutes of analyst time. Non-negotiable.

**Diff every dispatch prompt against Go source.** For each sub-agent dispatch captured in `flow-dispatches/`, diff byte-by-byte against `BuildXxxDispatchBrief(plan)` output. Flag any main-agent interpolation or paraphrasing. This catches F-9-class defects that the harness doesn't catch if the fix-stack regressed.

### Phase 5 — v37 verdict

**PROCEED gate** — all must hold:
- All 8 Cx-fix-stack targets close (F-8 through F-13) with zero regressions.
- F-14, F-15, F-16 either close or are accepted as single-run-anomaly (≤ 2 failures on first writer pass vs v36's 9).
- Every `[gate]` bar in `runs/v37/calibration-bars.md` passes.
- Close-step complete + editorial-review dispatched + code-review dispatched + close-browser-walk attempted.
- Deliverable tree structurally correct: no ghost dirs, correct markers, per-codebase markdown in tarball.
- Analysis harness report clean.

**ACCEPT-WITH-FOLLOW-UP** gate — if one signal-grade issue remains (e.g. agent-browser environmental, or one writer-compliance class still > threshold):
- Document it as a targeted patch class for v8.109.x.
- Commission v38 after the patch lands.

**PAUSE** gate — if any fix-stack target regressed OR if > 2 writer-compliance classes still fail (F-14/15/16-like):
- Write HANDOFF-to-I9 with the new defect stack.
- Do not commission v38 until the new fix stack lands.

**ROLLBACK-Cx** gate — if any fix-stack commit introduced a worse regression than the defect it was meant to close:
- Identify the specific commit via harness evidence.
- `git revert <sha>` + patch release v8.109.1 reverting only the bad commit.
- Commission v38 against v8.109.1.

**ROLLBACK** (full rollout) — highly unlikely on v37. Would require v8.109.0 to break something v8.108.x had working. Harness would catch this as a new bar regression vs v36 machine-report baseline.

---

## 6. Analysis discipline rules (enforced by harness)

These are the rules the v36 analysis broke. Your job on v37 analysis is to not break them.

### Rule 1 — No verdict without machine-report SHA

The verdict.md front matter includes:
```yaml
---
machine_report_sha: <sha256 of runs/v37/machine-report.json>
checklist_sha: <sha256 of runs/v37/verification-checklist.md>
---
```

The pre-commit hook validates the SHAs match the current files. If you edit the verdict without re-running the harness, the SHAs mismatch and the commit blocks.

### Rule 2 — No PASS claim without evidence citation

Every sentence asserting PASS/FAIL of a bar or defect class must include `[checklist X-Y]` or `[machine-report.<key>]` within 50 characters of the assertion. Regex-enforced by pre-commit. Example:

❌ "F-9 closed — ghost directories eliminated."
✅ "F-9 closed [machine-report.B-15_ghost_env_dirs=pass, observed=0]."

### Rule 3 — No "unmeasured" cells in the checklist

Every content-quality cell must be PASS or FAIL with Read-receipt timestamp + file:line evidence. "Unmeasured — phase unreached" is not a valid cell state UNLESS the phase genuinely didn't run AND the checklist's `phase_reached` metadata confirms it. Specifically:

- If close-step didn't complete, editorial-review bars can be UNMEASURED.
- If close-step did complete, editorial-review bars cannot be UNMEASURED.
- If the file is on disk (regardless of phase), content-quality for that file cannot be UNMEASURED.

### Rule 4 — Read every file before grading it

Each writer-authored file the checklist grades must have a corresponding Read tool call timestamp in the analyst's session log. The commit hook verifies (either via JSONL parse or explicit Read-receipt annotations the analyst adds).

No skimming. No "I saw the first 80 lines earlier so I'll grade the whole file". Row-by-row.

### Rule 5 — Diff dispatch prompts against Go source

For each Agent dispatch in `flow-dispatches/`, the analysis must include a diff verdict:
- `dispatch_vs_source_diff: clean` — byte-identical modulo plan-slot variance.
- `dispatch_vs_source_diff: divergent` — with specific divergences listed + root cause.

This catches F-9-class defects (main agent hallucinating template values). Put in machine-report as `dispatch_integrity.{role}.diff_status`.

### Rule 6 — Retry-cycle attribution

Every failing check round gets attributed to a defect class. The attribution is row-level in the checklist. Patterns:
- "writer-1 pass 1 fragment_intro missing" → F-14 attribution
- "writer-fix-pass comment_ratio 0%" → writer-compliance attribution
- "finalize env 4 factual_claims" → writer-compliance (env_comment authorship)

Retry cycles without attribution = analysis incomplete.

### Rule 7 — No verdict before checklist is 100% filled

Checklist cells have states: `pending | pass | fail | unmeasurable-valid | unmeasurable-invalid`. The commit hook counts `pending` cells; if > 0, blocks. `unmeasurable-invalid` is for cells the analyst tried to mark UNMEASURED but couldn't justify (e.g., file is on disk → UNMEASURABLE is invalid).

### Rule 8 — Tripwire on self-congratulatory language

If verdict contains "success" / "works" / "clean" / "PROCEED" without an evidence citation within 50 chars, pre-commit flags. Soft warning if once; hard block if > 3 occurrences. Forces the analyst to be specific.

---

## 7. What v37 is NOT testing

To keep v37 scoped, these are OUT of v37's gate:

- **Minimal-tier (v35.5)** — runs independently; not blocked by v37 and not gating v37.
- **Different framework** — v37 uses nestjs for A/B comparability with v34-v36. Framework variation is post-v37.
- **Multiple concurrent runs** — single commissioned run; no load testing.
- **CI/CD / cron / scheduled runs** — manual commission.
- **Recipe publishing flow end-to-end** — v37 produces a tarball; publishing it to `zeropsio/recipes` is out of scope.
- **Agent-browser reliability** — environmental; soft-pass acceptable.
- **Performance optimization** — wall-time is signal-grade, not gate.
- **Content cold-read for publish quality** — the v37 harness grades against surface-contract tests, not human "is this a good recipe" judgment. A separate publish-gate happens downstream.

---

## 8. Traps and operating rules (inherited from HANDOFF-to-I7)

Unchanged:
1. **Analysis is NOT implementation.** The v37 analysis phase does not write code. If you find new defects, document them; fix-stack comes from a separate commissioning with its own handoff.
2. **Cite evidence file:line or row:timestamp.** Never "probably". Trace every claim to a Read'able source.
3. **One commit at the end.** The full analysis + verdict + checklist + harness-report land as a single `docs(zcprecipator2): v37 run analysis + verdict` commit.
4. **Stop at verdict.** Do not start HANDOFF-to-I9 autonomously; hand back to user.

New for v37:
5. **Build harness first, always.** If you skip Phase 1, you cannot do Phase 4 properly — there's no mechanical layer to bind your claims to.
6. **If harness output and your intuition disagree, trust the harness.** If the harness says `B-15_ghost_env_dirs.status=pass` but you see what looks like a ghost dir, investigate the harness bar definition, not the deliverable. The harness is the contract.
7. **Meta-failure recovery**: if mid-analysis you realize you've been pattern-matching on "looks like success", STOP, re-read rule 2 + 3 + 4 + 8, re-start the checklist with fresh eyes. Don't ship.

---

## 9. Success definition (when is this handoff "done")

Phase 1 done: harness commits merged; `make lint-local` + tests green; validation test (run against v36 dir) surfaces F-9, F-10, F-12, F-13 mechanically.

Phase 2 done: 6 Cx-commits merged; tag v8.109.0 published; green test suite including all new RED tests.

Phase 3 done: v37 commissioned against v8.109.0; deliverable tree + session logs handed off.

Phase 4 done: `runs/v37/{machine-report.json, verification-checklist.md, verdict.md}` all present; pre-commit hooks pass; verdict is PROCEED / ACCEPT-WITH-FOLLOW-UP / PAUSE / ROLLBACK-Cx with binding evidence.

Phase 5: final commit lands; user review; decision.

If v37 verdict is PROCEED: zcprecipator2 is architecture-validated. The remaining work is publish-pipeline polish + framework diversity.

If v37 verdict is PAUSE or ROLLBACK-Cx: HANDOFF-to-I9 follows. The architecture has another layer of defects to surface. The thesis hasn't been invalidated but the fix-stack cadence isn't over.

---

## 10. Context from v36 — what you should feel reassured about

Not everything v36 showed was bad news. Validated positives:

- **Deploy works**. 5/5 features dev + stage; round-trip NATS→worker→DB UPDATE in <500ms. Platform integration is solid.
- **State machine + step gates work**. All 6 phases reachable; all 12 deploy substeps tractable; checker gates caught real issues (env 4 minContainers, version anchors) and agent fixed them.
- **Envelope-pattern dispatch works under load**. 15 dispatch-brief-atom calls in 9 seconds, max 11.5 KB per response. Cx-BRIEF-OVERFLOW earned its keep.
- **Multi-agent orchestration works**. 3 parallel scaffolders + 1 feature + 2 writers with coordinated SymbolContract.
- **F-1 through F-7** all verified closed under live conditions. That's 7 defect classes the fix stack actually fixed.
- **Platform invariants hold**. `0.0.0.0` bind, trust proxy, `zsc execOnce`, VXLAN routing — all correctly integrated.
- **Import.yaml content quality is production-grade**. Substantive comments teaching platform mechanisms, not boilerplate.

The failures are in the **integration glue** between layers (writer → staging → export) and in **atom text accuracy** (marker form, template variables, standalone file spec). Both are bounded engineering, not thesis-level failures.

**The architecture is salvageable**. The fix stack is small. v37 is the confirmation.

---

## 11. Starting action

1. Read all 9 items in §3 required reading.
2. Validate your understanding of the v36 corrected state by reading `runs/v36/CORRECTIONS.md` end-to-end.
3. Begin Phase 1: draft `cmd/zcp/analyze/analyze.go` against the spec in `spec-recipe-analysis-harness.md`.
4. Validate harness against v36 deliverable tree BEFORE writing any Cx-commits. If harness doesn't catch F-9/F-10/F-12/F-13 retrospectively, fix the harness first.
5. Land Cx-commits in order (ENVFOLDERS-WIRED → ATOM-TEMPLATE-LINT → MARKER-FORM-FIX → STANDALONE-FILES-REMOVED → CLOSE-STEP-STAGING → CLOSE-STEP-GATE-HARD).
6. Tag v8.109.0.
7. Hand back to user for v37 commission.
8. Post-commission, do analysis per Phase 4 discipline rules.
9. Ship v37 verdict.
10. Stop. Hand back. User decides next.

Good luck. v36 was the dress rehearsal. v37 is where zcprecipator2 proves itself — or doesn't.
