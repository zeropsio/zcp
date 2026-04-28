# Phase 4 tracker — Atom corpus restructure

Started: 2026-04-28 (immediately after Phase 3 EXIT `8352dfa2`)
Closed: TBD (pending Codex PER-EDIT + POST-WORK rounds + user go for Phase 5)

> Phase contract per `plans/export-buildfromgit-2026-04-28.md` §6 Phase 4.
> EXIT: 6 new atoms in corpus + 1 deleted, all scenario tests pass, atom
> lint passes, Codex PER-EDIT + POST-WORK APPROVE.
> Risk classification: HIGH (largest user-facing surface change).

## Plan reference

- Plan SHA at session start: `e6da4c1a`
- Plan file: `plans/export-buildfromgit-2026-04-28.md`
- Phase 0 amendments folded: §13
- Phase 2 amendments folded: §14
- Phase 4 references-fields gate satisfied: Phase 2 landed `ops.ExportBundle` with `ImportYAML`, `ZeropsYAML`, `Warnings`, `RepoURL` fields.

## Atoms landed

| atom | priority | size | references-fields | Codex PER-EDIT |
|---|---|---|---|---|
| `export-intro.md` | 1 | ~30 lines | (none) | not mandatory |
| `export-classify-envs.md` | 2 | ~100 lines | (none) | **mandatory** (load-bearing classification protocol) |
| `export-validate.md` | 3 | ~70 lines | `ops.ExportBundle.ImportYAML`, `ops.ExportBundle.ZeropsYAML`, `ops.ExportBundle.Warnings` | not mandatory |
| `export-publish.md` | 4 | ~70 lines | `ops.ExportBundle.ImportYAML`, `ops.ExportBundle.ZeropsYAML`, `ops.ExportBundle.RepoURL`, `ops.ExportBundle.Warnings` | not mandatory |
| `export-publish-needs-setup.md` | 5 | ~50 lines | (none) | **mandatory** (chain contract semantics) |
| `scaffold-zerops-yaml.md` | 6 | ~75 lines | (none) | not mandatory |

## Atoms deleted

- `internal/content/atoms/export.md` — 229-line legacy procedural prose, superseded by the six topic-scoped atoms above. Removed via `git rm`.

## Test surface updated

- `internal/workflow/scenarios_test.go::TestScenario_S12_ExportActiveEmptyPlan` — replaces single-atom exact-match (`export`) with the six-atom exact-match list.
- `internal/workflow/scenarios_test.go` (pin coverage closure at L915+) — six new atoms appended to the global pin list (closes the `corpus_pin_density_test.go` requirement).
- `internal/workflow/corpus_coverage_test.go` (export_active fixture at L766-779) — `MustContain` updated from `["buildFromGit", "zerops_export", "import.yaml"]` to `["buildFromGit", "zerops-project-import.yaml"]` per Q2 default + Phase 0 amendment 9.

## Iteration history (placeholder + axis fixes)

Several rounds of placeholder + axis-violation fixes during the initial atom write:

| issue | atom | fix |
|---|---|---|
| `{key:bucket,...}` unknown placeholder | export-intro.md, export-classify-envs.md | reword to "envClassifications map (key → bucket per env)" |
| `{...your-map...}` unknown placeholder | export-publish-needs-setup.md | reword to "<your map: each project env mapped to its bucket>" |
| 7-atom corpus over 28KB cap | n/a (legacy export.md still present alongside new atoms) | `git rm internal/content/atoms/export.md` brought corpus to 6 atoms, fits under cap |
| axis-k `local-only` candidate | export-intro.md, export-validate.md, export-publish-needs-setup.md | `<!-- axis-k-keep: signal-#3 -->` markers per spec §11.5 |
| axis-m `the container` drift | export-publish.md, export-validate.md | rewrote to `the runtime container's /var/www` (canonical prefix) |
| axis-m `the platform` drift | export-validate.md | rewrote to `Zerops` |
| axis-m `the agent` drift | export-publish.md, export-publish-needs-setup.md, export-validate.md | rewrote to `you` |
| handler-behavior verb (`automatically`) | export-publish-needs-setup.md | rewrote action-dispatcher prose to break the verb proximity |

## Verify gate (post-iteration)

| check | command | result |
|---|---|---|
| fast lint | `make lint-fast` | 0 issues |
| full lint + atom-tree gates | `make lint-local` | 0 issues; recipe_atom_lint 0 violations; atom-template-vars 0 unbound |
| atom authoring lint | `go test ./internal/content/ -run TestAtomAuthoringLint` | PASS |
| short suite | `go test ./... -short -count=1` | all packages PASS |

## Codex rounds

Per plan §7 Phase 4 has TWO MANDATORY PER-EDIT rounds + ONE POST-WORK round. Plan §7 calls out fan-out as available — running PER-EDIT pair in parallel.

| agent | scope | output target | status |
|---|---|---|---|
| PER-EDIT classify-envs | bucket descriptions, worked examples, grep recipes, mis-classification traps, axis hygiene | `codex-round-p4-peredit-classify-envs.md` | running |
| PER-EDIT publish-needs-setup | chain-contract correctness, two-step resolve flow, stale RemoteURL section, compose-only escape, axis hygiene | `codex-round-p4-peredit-publish-needs-setup.md` | running |
| POST-WORK | overall axis K/L/M/N hygiene + corpus_coverage fixture coverage + scenario test diff | `codex-round-p4-postwork-corpus.md` | not yet run |

### Round outcomes

| agent | scope | duration | verdict |
|---|---|---|---|
| PER-EDIT classify-envs | bucket descriptions, worked examples, grep recipes, mis-classification traps, axis hygiene | ~178s | NEEDS-REVISION (3 MUST-FIX + 3 NICE-TO-HAVE) |
| PER-EDIT publish-needs-setup | chain-contract correctness, two-step resolve flow, stale RemoteURL section, compose-only escape | ~207s | NEEDS-REVISION (3 primary issues + failure-mode gaps) |
| POST-WORK | corpus-level axis hygiene + compaction + cross-atom consistency + Phase 5/6 forward-compat | ~153s | NEEDS-REVISION (compaction over cap by 897B + import.yaml shorthand + "recipes" wording) |

**Convergent verdict**: NEEDS-REVISION across all three → 9 amendments folded in-place → effective APPROVE per §10.5 work-economics rule.

### Amendments applied (9 total)

**From PER-EDIT classify-envs** (Codex Agent A):
1. **Grep recipes rewritten paste-safe** — replaced shell-quoted `\|` alternations with `rg -nE '(a|b|c)'` extended-regex patterns. Dropped Ruby/Rails row + supporting paragraphs.
2. **M6 test-fixture handling added** — sentinel value list extended with `test_xxx`, `noop`, `null`, `false`, `off`; new common-traps row covers `TEST_API_KEY=test_xxx` consumed only by tests.
3. **Phase 5 forward-compat note added** — explicit "schema validation lands in Phase 5" caveat near the per-env review section.

**From PER-EDIT publish-needs-setup** (Codex Agent B):
4. **Local-mode routing claim corrected** — atom no longer claims the export chain branches on `mode`; clarifies that `git-push-setup`'s walkthrough atom is selected by current ZCP runtime environment (container vs local zcp invocation).
5. **"lands at publish-ready" softened** — explicit "SHOULD land at publish-ready if no other prereq changed; otherwise read the new status/nextSteps".
6. **Stale RemoteURL section rewrite** — "Phase 0 capture" wording removed; correct attribution to `git-push-setup` confirm mode; Phase 6 forward-compat note added.
7. **Compose-only escape strengthened** — explicit warning that the bundle is a moment-in-time snapshot; "always re-run export immediately before manual extraction; do not act on a stored copy".
8. **Invalid GIT_TOKEN partial coverage** — added paragraph explaining `git-push-setup` confirm mode validates URL format only, not auth — pushes can still surface `failureClassification.category=credential`.

**From POST-WORK** (Codex corpus-hygiene agent):
9. **Compaction trim** — corpus shrunk from 29,569B (897B over cap) to 28,636B (under 28,672B cap). Trims came from `export-classify-envs.md` (grep table compacted, paragraph dropped) and `export-validate.md` (table cell compactions).
10. **`import.yaml` → `zerops-project-import.yaml`** — Phase 5 forward-compat note now uses canonical filename.
11. **`grep recipes` → `grep commands`** — heading + intro paragraph reworded to avoid recipe-engine ambiguity.

### Coding deltas (Phase 4 EXIT)

- 6 new atoms: `internal/content/atoms/export-{intro,classify-envs,validate,publish,publish-needs-setup}.md` + `internal/content/atoms/scaffold-zerops-yaml.md` (28,636B total).
- 1 atom deleted: `internal/content/atoms/export.md`.
- `internal/workflow/scenarios_test.go` — S12 split + pin closure update (+33 LOC).
- `internal/workflow/corpus_coverage_test.go` — `MustContain` update (+10 LOC).
- `internal/workflow/router.go` — Hint string update (filename + multi-call narrowing).
- `internal/tools/workflow.go` — error-message hint update (workflow="export" semantics).

## Phase 4 EXIT

- [x] 6 new atoms in corpus (28,636 bytes total — under 28,672B soft cap).
- [x] 1 atom deleted (`export.md`).
- [x] All scenario tests pass.
- [x] Atom lint passes (axes K/L/M/N hygiene clean).
- [x] Verify gate green (lint-fast 0 issues; full short suite + race PASS).
- [x] Codex PER-EDIT × 2 + POST-WORK APPROVE (effective verdict after 11 in-place amendments per §10.5).
- [x] All three Codex round transcripts persisted.
- [x] `phase-4-tracker.md` finalized.
- [ ] Phase 4 EXIT commits (atoms + tests + tracker — see commit hashes once made).
- [ ] User explicit go to enter Phase 5.

## Notes for Phase 5 entry

1. Phase 5 is MEDIUM risk (per Phase 0 amendment §14 — was LOW-MEDIUM before). Vendor `github.com/santhosh-tekuri/jsonschema/v5` for full JSON Schema validation; refresh embedded `internal/schema/testdata/import_yml_schema.json` (currently 202B behind live).
2. After Phase 5 lands, `export-validate.md` needs an update to reflect actual schema-validation behavior (currently says "not yet client-side").
3. `export-publish-needs-setup.md` may need a Phase 6 update if `refreshRemoteURL` rewrites the cache semantics.
4. Session pause point: Phase 5 begins ONLY after explicit user go.