# plans/v37-fix-stack.md — six Cx-commits + harness build before v37 commission

**Status**: TRANSIENT (per CLAUDE.md §Source of Truth #4). Archive to `plans/archive/` after v37 commission lands.
**Prerequisites**: [`../HANDOFF-to-I8-v37-prep.md`](../HANDOFF-to-I8-v37-prep.md) §4 defect inventory, [`../spec-recipe-analysis-harness.md`](../spec-recipe-analysis-harness.md).
**Target tag**: `v8.109.0`
**Estimated effort**: 3–5 days focused work (harness: 1–2 days, fix-stack: 2–3 days).

This doc is the execution plan the fresh instance follows. Each Cx-commit has a scope, files-touched list, RED test name, and acceptance criterion. Phase-1 harness work runs in parallel with Phase-2 Cx-commits after the first two commits land (ENVFOLDERS-WIRED + ATOM-TEMPLATE-LINT are fast and unblock the rest).

---

## Phase 0 — Prerequisites check (≤ 30 min)

1. Read [`../HANDOFF-to-I8-v37-prep.md`](../HANDOFF-to-I8-v37-prep.md) end to end.
2. Read [`../runs/v36/CORRECTIONS.md`](../runs/v36/CORRECTIONS.md) end to end.
3. Read [`../spec-recipe-analysis-harness.md`](../spec-recipe-analysis-harness.md) end to end.
4. Verify clean working tree: `git status` → clean on `main`.
5. Verify test suite green: `go test ./... -count=1 -short`.
6. Verify lint green: `make lint-local`.

If any of 1–6 fails, stop and investigate before proceeding.

---

## Phase 1 — Analysis harness (1–2 days)

See [`../spec-recipe-analysis-harness.md`](../spec-recipe-analysis-harness.md) §6 for full implementation plan. Summary here:

### Files to create

- `cmd/zcp/analyze/analyze.go` — subcommand dispatch (`recipe-run`, `generate-checklist`)
- `cmd/zcp/analyze/recipe_run.go` — `zcp analyze recipe-run` CLI
- `cmd/zcp/analyze/generate_checklist.go` — checklist emitter
- `internal/analyze/report.go` — `MachineReport` schema
- `internal/analyze/structural.go` — B-15, B-16, B-17, B-18, B-22 bars
- `internal/analyze/session.go` — B-20, B-21, B-23, B-24 bars + JSONL parser
- `internal/analyze/writer_compliance.go` — per-file surface-contract tests
- `internal/analyze/dispatch_integrity.go` — dispatch-prompt byte-diff engine
- `internal/analyze/checklist.go` — Markdown worksheet generator
- `tools/lint/atom_template_vars.go` — build-time lint for unbound atom template fields
- `tools/hooks/verify_verdict` — Bash pre-commit hook
- `tools/hooks/verify_citation_rule.py` — Python helper for citation regex
- `internal/analyze/testdata/v36_expected_machine_report.json` — golden file

### Order of implementation

1. **Schema first** (`report.go`). Freeze JSON structure before implementing bars.
2. **Structural bars** (B-15, 16, 17, 18, 22) — filesystem walks + greps. 40 LoC each + 20 LoC test.
3. **Session bars** (B-20, 21, 23) — JSONL parser + event filters.
4. **Dispatch integrity** (B-24) — capture + diff. Hardest bar.
5. **Writer compliance** (per-file surface tests) — 20 LoC per test × 10 tests.
6. **CLI wiring** — compose bars into `recipe-run` command.
7. **Checklist generator** — Markdown formatter.
8. **Commit hook + citation rule** — Bash + Python.

### Validation

Run harness against v36 deliverable tree. Confirm:
- `B-15_ghost_env_dirs.observed == 6` (status: fail)
- `B-17_marker_exact_form.observed >= 3` (status: fail)
- `B-20_deploy_readmes_retry_rounds.observed == 4` (status: fail)
- `B-23_writer_first_pass_failures.observed == 9` (status: fail)
- `writer_compliance.{apidev,appdev,workerdev}/README.md.integration_guide_fragment.every_h3_has_fenced_code_block == false`

If any of these don't match, fix the bar implementation before moving to Phase 2.

### Acceptance criterion

- Go test suite green on `./cmd/zcp/analyze/... ./internal/analyze/...`.
- `make lint-local` green (includes new atom template var lint).
- Harness retroactively surfaces v36 defects mechanically — this is the gate that validates the harness works.
- Commit hook installed and verified: intentional verdict-without-citation fails `git commit`.

**Tag after Phase 1**: `v8.109.0-harness` (prerelease) OR just merge to main and tag at end of Phase 2.

---

## Phase 2 — Fix stack (2–3 days)

Six Cx-commits ordered by dependency. Each lands with its own RED-GREEN test, green lint, green test-race.

### Cx-ENVFOLDERS-WIRED (closes F-9 / row 16.9)

**Scope**: populate `.EnvFolders` template variable from canonical `envTiers[].Folder`. Route brief-atom rendering through a template pass so returned atom bodies have concrete paths.

**Files touched**:
- `internal/workflow/recipe_templates.go` — export `CanonicalEnvFolders() []string`.
- `internal/workflow/atom_loader.go` — add `LoadAtomBodyRendered(id string, plan *RecipePlan) (string, error)` that does template substitution after load.
- `internal/tools/workflow.go` (`handleDispatchBriefAtom`) — use `LoadAtomBodyRendered` when a plan is active; fall back to `LoadAtomBody` only for plan-free contexts.
- `internal/workflow/atom_loader_test.go` — add test for rendered path.

**RED test**: `TestDispatchBriefAtom_EnvFoldersResolved`
- Setup: active showcase plan.
- Action: fetch `briefs.writer.canonical-output-tree` via `handleDispatchBriefAtom`.
- Assert: response body contains `0 — AI Agent/README.md` AND does not contain `{{`.

**Green**: after implementation, the test passes.

**Acceptance on v37**: writer dispatch prompt (captured in `flow-dispatches/recipe-writer-sub-agent.md`) contains canonical numbered env paths. Zero ghost dirs in deliverable tree.

**Estimated**: 2–3 hours.

---

### Cx-ATOM-TEMPLATE-LINT (prevention for F-9 class)

**Scope**: build-time lint that catches unbound template variable references in atoms.

**Files touched**:
- `tools/lint/atom_template_vars.go` (new) — walker + extractor.
- `tools/lint/atom_template_vars_test.go` — fixture-based tests.
- `Makefile` — add to `lint-local` chain.

**Allowed fields**: `ProjectRoot`, `Hostnames`, `EnvFolders`, `Framework`, `Slug`, `Tier`. Source: Go code that actually populates these during atom render.

**RED test**: `TestAtomTemplateVarLint_CatchesUnboundField`
- Fixture atom with `{{.FakeField}}`.
- Assert: lint walker returns an error naming the atom + field.

**Green**: after implementation, the test passes. Add to CI.

**Acceptance**: `make lint-local` fails if any atom references an unknown template field. This prevents future F-9-class regressions from atom-text edits.

**Estimated**: 1–2 hours.

---

### Cx-MARKER-FORM-FIX (closes F-12 / row 16.11)

**Scope**: correct the marker form in writer atoms + tighten the check regex.

**Files touched**:
- `internal/content/workflows/recipe/briefs/writer/content-surface-contracts.md` — lines 71, 93, wherever marker is shown. Change `<!-- #ZEROPS_EXTRACT_START:integration-guide -->` to `<!-- #ZEROPS_EXTRACT_START:integration-guide# -->`.
- `internal/content/workflows/recipe/briefs/writer/self-review-per-surface.md` — lines 47, 108–110. Same correction.
- `internal/ops/checks/manifest.go:404,408,458,462` — replace `strings.Contains(line, "ZEROPS_EXTRACT_START:intro")` with `regexp.MustCompile(\`<!-- #ZEROPS_EXTRACT_(START|END):(intro|integration-guide|knowledge-base)# -->\`).MatchString(line)`. Add a NEW check `fragment_marker_exact_form` that fails when a README contains markers missing the trailing `#`.
- `internal/ops/checks/manifest_test.go` — new test `TestMarkerFormCheck_RejectsMissingTrailingHash`.

**RED test**: `TestMarkerFormCheck_RejectsMissingTrailingHash`
- Fixture README with `<!-- #ZEROPS_EXTRACT_START:intro -->` (missing `#`).
- Assert: check fails with detail naming file:line and correct form.

**Green**: after implementation, test passes; existing check tests also pass (atoms now use correct form internally).

**Acceptance on v37**: writer first-pass READMEs contain correctly-formed markers. Zero marker-fix Edit cycles in any sub-writer session.

**Estimated**: 1–2 hours.

---

### Cx-STANDALONE-FILES-REMOVED (closes F-13 / row 16.12)

**Scope**: delete standalone INTEGRATION-GUIDE.md + GOTCHAS.md prescription from writer atoms.

**Files touched**:
- `internal/content/workflows/recipe/briefs/writer/canonical-output-tree.md` — delete lines 11-12 (the two standalone file bullets).
- `internal/content/workflows/recipe/briefs/writer/content-surface-contracts.md` — remove `+ INTEGRATION-GUIDE.md` and `+ GOTCHAS.md` suffixes at lines 63 and 81; update surface-summary-table rows 140 and 141.
- `internal/content/workflows/recipe/briefs/writer/completion-shape.md` — remove any standalone-file references.
- `internal/content/workflows/recipe/briefs/writer/self-review-per-surface.md` — remove file-existence checks for standalones.

**RED test**: `TestWriterAtoms_NoStandaloneFilePrescription`
- Grep the 4 writer atoms listed above.
- Assert: zero occurrences of `INTEGRATION-GUIDE.md` or `GOTCHAS.md` as paths.

**Green**: after atom edits, the test passes.

**Acceptance on v37**: writer authors 2 files per codebase (README.md + CLAUDE.md), not 4. Zero standalone duplicates anywhere.

**Estimated**: 1 hour.

---

### Cx-CLOSE-STEP-STAGING (closes F-10 / row 16.10)

**Scope**: engine-side action that stages writer content into the recipe output tree during close-step.

**Files touched**:
- `internal/workflow/engine_recipe.go` — new method `stageWriterContent(state *WorkflowState) error`. For each codebase in `plan.Targets` (runtime, non-shared-codebase-worker), copy `/var/www/{codebase}/{README.md, CLAUDE.md}` into `/var/www/zcprecipator/{slug}/{codebase}/`. Create dest dir if absent. Fail with detailed error if any expected file is missing on source.
- `internal/workflow/engine_recipe.go:RecipeComplete(step="close")` — call `stageWriterContent` after close-phase checks pass, before marking step complete.
- `internal/ops/checks/manifest.go` — new check `writer_content_staged` — verifies the staged files are present in the recipe output tree.
- `internal/workflow/engine_recipe_test.go` — new test `TestCloseStep_StagesWriterContent`.

**RED test**: `TestCloseStep_StagesWriterContent`
- Setup: session with writer output on `/tmp/var/www/apidev/README.md` etc.
- Call `RecipeComplete(step="close")`.
- Assert: `/tmp/var/www/zcprecipator/testslug/apidev/README.md` exists; `writer_content_staged` check passes.

**Optional git-commit integration**: if the app-repo publish flow reads `git ls-files` (it does, at `export.go:355`), the staging must also `git add + git commit` the staged files. Consider bundling this with the copy.

**Green**: test passes.

**Acceptance on v37**: after `action=complete step=close` accepted, deliverable tree at `/var/www/zcprecipator/{slug}/{codebase}/` contains per-codebase markdown. Sessioned export `--session=<id>` produces a `.tar.gz` containing them.

**Estimated**: 4–6 hours (most complex Cx in the stack — touches engine state + filesystem + check surface).

---

### Cx-CLOSE-STEP-GATE-HARD (closes F-8 / F-11 / row 16.8)

**Scope**: sessionless export refuses to run when a live recipe session exists for the target directory.

**Files touched**:
- `internal/sync/export.go` — detect live session in `ExportRecipe` function. Walk session state dir (configurable via `SessionStateDir` opt), match any session whose `OutputDir` matches `recipeDir` OR whose `--app-dir` args are in scope. If match AND `--session` unset AND not `--force-export`, return error with exit code 2 + structured error message naming session ID + remediation commands (`--session=<id>` OR `zerops_workflow action=complete step=close`).
- `internal/sync/export_test.go` — new test.

**RED test**: `TestExport_RefusesSessionlessWhenLiveSessionExists`
- Setup: fake session state dir with a session whose OutputDir matches tempDir.
- Call `ExportRecipe(tempDir, ExportOpts{Session: ""})`.
- Assert: error with exit code 2 + error message naming session ID.

**Green**: after implementation.

**Acceptance on v37**: agent cannot `zcp sync recipe export <dir>` without `--session` when a live session exists. Agent sees error telling them to complete close-step first. `--force-export` stays as explicit escape.

**Estimated**: 2–3 hours.

---

## Phase 3 — Integration verification (≤ 2 hours)

Before tagging v8.109.0:

1. Full test suite green: `go test ./... -count=1 -race`.
2. Full lint green: `make lint-local`.
3. Run harness against v36 deliverable — **expected behavior**: B-15, B-16, B-17, B-20, B-23 still fail (v36 is historical — the fixes haven't been applied to that tree, but the harness should still measure the same values).
4. Build a test commission directory mimicking what v37 would produce (6 canonical env dirs with correct markers, per-codebase markdown present, tarball with correct contents). Run harness — all bars should PASS.
5. Test commit hook: draft a verdict.md without citation → commit should fail. Add citation → commit should succeed.

---

## Phase 4 — Release v8.109.0 (15 min)

After Phase 3 validation:

```bash
make release-patch V=v8.109.0
```

Or explicit version bump per Makefile convention. Confirm tag pushed, GH Actions building, artifact published.

---

## Phase 5 — Hand back to user for v37 commission

Create/append to [`../HANDOFF-to-I8-v37-prep.md`](../HANDOFF-to-I8-v37-prep.md) §1 (Slots) with:
- `FIX_STACK_TAG: v8.109.0`
- `FIX_STACK_COMMITS: <6 SHAs>`
- `HARNESS_TAG: v8.109.0` (same tag if merged together)

Notify user: fix stack + harness shipped. v37 commission can proceed. Do not commission v37 autonomously — user drives.

---

## Phase 6 — v37 analysis (after user commissions)

See [`../HANDOFF-to-I8-v37-prep.md`](../HANDOFF-to-I8-v37-prep.md) §5 Phase 4 + §6 (Analysis discipline rules). The analysis instance (you, or a fresh instance) uses the harness per the discipline rules. Do NOT reproduce the v36 failure pattern.

---

## Appendix — Cx-commit order rationale

| Order | Commit | Why this position |
|---|---|---|
| 1 | Cx-ENVFOLDERS-WIRED | Highest impact (F-9 is visibly broken); self-contained; validates template-render path works for other consumers |
| 2 | Cx-ATOM-TEMPLATE-LINT | Depends on #1's rendered-atom contract; adds safety net before more atom edits |
| 3 | Cx-MARKER-FORM-FIX | Independent of #1/#2; atom-text edits + check tightening |
| 4 | Cx-STANDALONE-FILES-REMOVED | Independent atom-text edits; no Go code changes |
| 5 | Cx-CLOSE-STEP-STAGING | Most complex; depends on #4 for correct expected-file list |
| 6 | Cx-CLOSE-STEP-GATE-HARD | Depends on #5 for the staging to actually produce content to export |

Parallel opportunity: #1/#2, #3, #4 are independent. Can be committed in any order or batch. #5 should follow #4. #6 should follow #5.

---

## Appendix — What NOT to fix in this stack

Explicitly out of scope for v8.109.0:
- **F-14/F-15/F-16** (writer compliance): observe on v37 before deciding. May be single-run anomaly; may be systemic. If systemic, separate fix stack (probably Cx-WRITER-BRIEF-DECOMPOSITION) after v37.
- **agent-browser reliability**: environmental on zcp; not recipe-workflow defect. Separate issue.
- **Session wall-time reconciliation** (my earlier note about 1:45 vs user's 4h): measurement-side issue; not recipe-workflow defect.
- **TodoWrite full-rewrite frequency**: agent-habit signal; no architectural fix.
- **Publish-pipeline polish** (actually pushing to zeropsio/recipes): post-v37 concern.
- **Minimal-tier validation**: independent; not blocking.
- **Framework diversity**: v37 uses nestjs for A/B comparability; other frameworks post-v37.

---

## Exit criteria for this plan

Plan is complete when:
- [ ] Phase 1 harness merged + validates against v36.
- [ ] Phase 2 all 6 Cx-commits merged with green tests.
- [ ] Phase 3 integration verification passes.
- [ ] Phase 4 v8.109.0 tagged + published.
- [ ] Phase 5 handoff slot block populated.
- [ ] User confirms ready to commission v37.

Archive this document to `plans/archive/v37-fix-stack.md` after v37 verdict ships.
