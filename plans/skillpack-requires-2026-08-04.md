# Plan: skillpack-requires

## Run State
- `phase:` awaiting-retest
- `base:` 469e60514fd200315f885348e5a73e4f70e25b86
- `integration:` feat/skillpack-requires @ 3d12dfc8 (landed: 26577007..0a334c0f = S1 + W2 [S2 ba1b032c, S3 e5e1c2f5, S4 c301fd10] + transitive-violations reconciliation 0a334c0f; 3d12dfc8 = ASSEMBLE-checkpoint docs commit)
- `approved:` Rev-2, 2026-08-04 — owner approved register incl. Codex Rev-2 changes; spec contracts promoted (spec-skill-packs.md §3.1/§4.2/§7/§8, spec-welcome-mode.md §7 W-SKILLS)
- `codex:` changes-requested → all findings incorporated Rev-2 (review: /tmp/codex-out-1785828251-59778-28560.md; core model confirmed sound; blocking gap was FE migration healing — fixed via picker-open normalization)
- `verify:` ASSEMBLE battery green (fresh verifier, 2026-08-04) — race/lint-local/vet-tags/e2e-fast/JS-suite all passed; deploy-E2E + behavioral-eval not-run (out of scope); real-binary drive (pack-status/pack-set/pack-add against live github.com/mattpocock/skills) confirmed AC2/AC3/AC5 live; diff review found no contradiction of the spec
- `next:` OWNER GATE 2 — Karel runs the retest pack (plans/skillpack-requires-2026-08-04.retest.md)

### Owner decisions (FRAME checkpoint, 2026-08-04)
- Closure model: **picker computes, CLI refuses** — picker auto-includes deps
  visibly pre-Apply and posts the closed set; pack-set refuses a non-closed
  `--skills` with a new coded error (missing skills + requiring skills, zero
  writes). §3.1 stays literally true; CodeAtomicPartial idiom.
- Deselection: **cascade in picker** — unchecking a dependency visibly
  unchecks its dependents; pending summary shows it before Apply.
- Edge list: **hard edges as proposed in Frame** (15 edges across 8 skills;
  soft/"if needed" dropped).

## Frame
**Outcome**: A Matt-pack subset selection can never install a set missing a
declared dependency — the picker auto-includes required skills visibly before
Apply, and `zcp skills pack-set` refuses a non-closed `--skills` set with a
coded error naming the missing edges (zero writes).

| obs | evidence |
|---|---|
| CatalogSkill = {Name, SourcePath, Category, Description}; no dependency concept anywhere | internal/skillpacks/catalog.go:41-46 |
| §3.1: "the caller states the desired installed set and the implementation derives the additions and removals" — silent CLI expansion would falsify it | docs/spec-skill-packs.md:99-101 |
| §4.2 admission standard: "A dependency may become mandatory only through a reviewed catalog decision backed by an upstream contract or behavioral proof" | docs/spec-skill-packs.md:163-164 |
| §8 non-goal bundles "Superpowers subsets or inferred dependency closure" — needs rewrite, declared closure ≠ inferred | docs/spec-skill-packs.md:248 |
| Refuse-with-zero-writes is the package idiom: CodeAtomicPartial, CodeConflict, CodeUnknownSkill all refuse before any write | internal/skillpacks/set.go:145-182, 244-248 |
| pack-add installs the full reviewed catalog (filterDiscoveredToCatalog) — closure only matters for pack-set subsets | internal/skillpacks/add.go:138 |
| Selection validation (normalizeDesiredSkills) runs before lock/manifest; closure check slots naturally beside it | internal/skillpacks/set.go:145-165 |
| revision = f(pack id, generation, sorted skill names); closure changes desired pre-diff, not the hash function | internal/skillpacks/status.go:158-176 |
| Wire: catalogSkillJSON maps 4 fields BY HAND — a Go field alone never reaches the FE; additive field = extension, version stays 1 | cmd/zcp/skills.go:47-78 |
| FE host passes catalog through verbatim (no allowlist); picker renders only name+category today, state is a flat {name:bool} map with 3 mutation sites | vscode-bootstrap-welcome.js:789; vscode-bootstrap-welcome.html:933-988 |
| Warnings force per-row result text even on ok:true — existing surfacing hook for closure messages | vscode-bootstrap-welcome.html:826-837 |
| Superpowers/Karpathy structurally unreachable by subset code (CodeNotSkillLevel, CodeAtomicPartial gates) — Requires there would be dead data | internal/skillpacks/set.go:40-43, 173-182 |
| Stale spec bullet: §4.2 "initially selected … recommended" contradicts §7 welcome-mode "never pre-selects"; code+tests follow §7 | docs/spec-skill-packs.md:152-153 vs docs/spec-welcome-mode.md:449-451; pack_picker.test.js:156-164 |
| grill-me precedent solved a dependency problem by EXCLUSION (no standalone value); Requires reverses that for skills WITH standalone value — distinction must be written into §4 | docs/spec-skill-packs.md:140-144; commit 469e6051 |
| Migration template exists: out-of-catalog skill → status surfaces, pack-set force-detaches with warning, never deletes | internal/skillpacks/set_test.go:593-710 |
| Any vscode-bootstrap-* template edit ships only with BootstrapExtVersion bump (now 0.1.32) | internal/init/adapters/claude.go:32 |
| Installed pack skills never flow into AGENTS.md/rendered content — ZCP context budget invariant to closure size | grep internal/content (KB lane) |

- AC1: `CatalogSkill.Requires []string` exists; catalog lint proves every edge targets the same pack's catalog, the graph is acyclic, and only `SelectionSubset` packs declare edges — planned evidence: table-driven catalog lint test (unit).
- AC2: `pack-set` with a non-closed `--skills` set refuses with a new coded error naming missing skills + requiring skills, zero workspace/manifest writes (incl. no lock/state artifacts) — planned evidence: set_test mutation-absence proof à la TestPackSet_UnknownSkillName_RefusedWithoutMutation + cmd/zcp tool tier (no MCP surface ⇒ no integration/e2e tier, per Codex review).
- AC3: closed-set behavior unchanged: proof 3 (select-all installs 21) and proof 9 (stale revision → conflict, byte-identical workspace) re-proven under closure — planned evidence: existing tests green + revision-under-closure case.
- AC4: picker: checking a skill auto-checks its transitive Requires; deselection semantics per owner decision; pending summary reflects implied additions; Apply posts the closed set — planned evidence: pack_picker.test.js jsdom cases for all 3 mutation sites (individual, per-category, whole-pack).
- AC5: `pack-status --json` catalog entries carry `requires` on the wire; absent/empty edges omit cleanly; older webview unaffected (extension, `version` stays 1) — planned evidence: status_test JSON shape case + FE fixture.
- AC6: pre-Requires non-closed installed set (in-catalog skill, dep missing): `pack-status` surfaces a warning; next picker Apply heals it — planned evidence: migration test patterned on TestSkillPacks_GrillMeOutOfCatalog_StatusSetRemove.
- AC7: specs reconciled in the same change: §3.1 (+closure-validation clause), §4.2 (edge admission rule + exclusion-vs-edge distinction + stale "initially selected" bullet fixed), §8 non-goal rewritten, §7 proofs extended — planned evidence: spec diff + re-transcribed drift-guard tests.

**Non-goals**: Superpowers subsets or edges · runtime inference of deps from
skill prose · per-install provenance ("why installed") in manifest ·
description rendering in picker · Codex-side behavior · pack-add changes.
**Constraints**: wire change must be additive (extension, not break) ·
BootstrapExtVersion bump for any template edit · refuse = zero writes ·
`Requires` gets a Category-style doc warning (curation data only, never a
filesystem/execution input).

**Risk class**: medium — trigger: public wire-contract change (pack-status
JSON + pack-set semantics, CLI↔FE contract per spec-welcome-mode.md §7 W7).

**Assumptions**:
- [VERIFIED] all obs-table rows above (file:line cited per row).
- [ASSUMED] no third-party `pack-set --json` callers beyond the bundled FE
  (repo+FE ship together; `version` field exists for evolution) — strict
  refusal is agent-actionable regardless, so not load-bearing.
- [ASSUMED] upstream edge facts (mattpocock/skills @2ab9580, session analysis
  2026-08-04) hold at install time — mitigated by per-install commit pinning;
  catalog re-review owns drift, not this feature.
- No [PROBE] claims — no live-platform assumption in scope; PROVE will skip.

### Approved edge list (v1 — owner Q3, FRAME checkpoint)
15 edges across 8 skills (hard: workflow-required or contract-backed):
- grill-with-docs → grilling, domain-modeling (delegation shell; body composes both)
- implement → tdd, code-review (drives both as required steps)
- improve-codebase-architecture → codebase-design, grilling (required steps; domain-modeling mention is side-effect → dropped)
- wayfinder → grilling, domain-modeling, research, setup-matt-pocock-skills
- triage → grilling, setup-matt-pocock-skills
- code-review → setup-matt-pocock-skills
- to-spec → setup-matt-pocock-skills
- to-tickets → setup-matt-pocock-skills
(setup edges: upstream ADR 0001 hard config dependency)
Transitive depth 2 exists: implement → code-review → setup-matt-pocock-skills.
Dropped soft edges: diagnosing-bugs → improve-codebase-architecture (post-fix
handoff only); all "if needed"/side-effect mentions.

## Evidence Ledger
| claim | gates | surface | command | observed | verdict | promote |
|---|---|---|---|---|---|---|
| — no PROBE claims; PROVE skipped | — | — | — | — | — | — |

## Slice Register
| ID | Title | Depends | Files | Layers | Gate | State |
|---|---|---|---|---|---|---|
| S1 | Requires field + 15 edges + catalog lint + shared closure module + wire exposure (tracer) | — | internal/skillpacks/catalog.go, internal/skillpacks/catalog_test.go, internal/skillpacks/requirements.go, internal/skillpacks/requirements_test.go, internal/skillpacks/status_test.go, cmd/zcp/skills.go, cmd/zcp/skills_test.go | unit, tool | review | landed |
| S2 | pack-set closure refusal (CodeUnclosedSelection, zero writes) | S1 | internal/skillpacks/set.go, internal/skillpacks/errors.go, internal/skillpacks/set_test.go, cmd/zcp/skills_test.go | unit, tool | review | landed |
| S3 | pack-status warning: non-closed installed set (migration) | S1 | internal/skillpacks/status.go, internal/skillpacks/status_test.go | unit | autonomous | landed |
| S4 | FE picker closure UX: auto-include + cascade + closed-set Apply | S1 | internal/content/templates/vscode-bootstrap-welcome.html, internal/content/welcomejs/pack_picker.test.js, internal/content/welcomejs/diagnostics.test.js, internal/init/adapters/claude.go, internal/content/templates/vscode-bootstrap-package.json | unit (jsdom) | autonomous | landed |
Waves: W1 = S1 · W2 = S2 + S3 + S4 (disjoint write-sets; S2/S3 overlap S1's
test files → Depends edge, never same wave as S1).
Replay evidence: S1 RED=build-fail on new seam (`Requires undefined` at both
layers; assertion-level second RED in slice transcript: edges=0 want 15,
requires=[] want values) · GREEN exit 0 both layers (skillpacks 0.307s,
cmd/zcp 0.384s) · merged 5c549bf7, post-merge full pkg tests + lint-fast 0
issues.
S2: RED=missing-symbol CodeUnclosedSelection (unit) + assertion FAIL (tool);
transcript's second assertion RED: code="conflict" want "unclosed-selection"
+ unexpected .zcp state pre-fix · GREEN exit 0 both layers. S3: RED=assertion
(Warnings=[] want closure warning) · GREEN exit 0. S4: RED exit 1 both JS
files (assertion diffs, 9 new cases) · GREEN exit 0; full welcomejs suite +
init parity green. Post-merge reconciliation 0a334c0f (RED-first): one
transitiveViolations semantic in requirements.go consumed by set + status —
proof 16 identical-wording intent; status oracle tightened.
**Release coupling (Codex)**: S2 and S4 ship in the same assembled release —
never release the CLI refusal without the picker closure UX. An already-
running 0.1.32 extension host can still hit the refusal post-upgrade until
window reload; old FE falls back to the backend message for unknown codes
(vscode-bootstrap-welcome.html:771), so the refusal message must be
self-explanatory. No compatibility flag / grace expansion.

## Verify Trace
| ACx | check | result | evidence |
|---|---|---|---|
| — | `make test-race` | passed | every package `ok`, no FAIL/DATA RACE (58 packages incl. `internal/skillpacks`, `cmd/zcp`) |
| — | `make lint-local` | passed | `./bin/golangci-lint run ./...` → `0 issues.` |
| — | `make vet-tags` | passed | `go vet -tags api ./...` + `go vet -tags e2e ./...` — no output, no errors |
| — | `make e2e-zcp-fast` | passed | all `--- PASS:` lines, final `PASS`, no FAIL |
| — | Deploy E2E | not-run | out of scope: no deploy/import/export/launch surface touched |
| — | Behavioral eval | not-run | out of scope: no agent-behavior-facing change (picker is FE-side); `make flow-eval-local` never run per instruction |
| — | `node --test internal/content/welcomejs/*.test.js` (full JS suite) | passed | `tests 344`, `pass 344`, `fail 0` |
| AC1 | `go test ./internal/skillpacks/ -run TestCatalog_RequiresEdges_ValidGraph -short -count=1 -v` (same-pack targets, acyclic, SelectionSubset-only) | passed | `--- PASS: TestCatalog_RequiresEdges_ValidGraph` (+ 3 subtests: matt-pocock-skills, superpowers, andrej-karpathy-skills) `ok github.com/zeropsio/zcp/internal/skillpacks 0.300s` |
| AC1 | `go test ./internal/skillpacks/ -run TestCatalog_MattRequiresEdges_ExactSet -short -count=1 -v` (independent-oracle 15-edge drift guard) | passed | `--- PASS: TestCatalog_MattRequiresEdges_ExactSet` `ok ... 0.138s` |
| AC1 | `go test ./internal/skillpacks/ -run TestRequirements_ClosureAndViolations -short -count=1 -v` (shared module: direct/transitive/multi-parent/empty) | passed | `--- PASS: TestRequirements_ClosureAndViolations` + 7 subtests `ok ... 0.141s` |
| AC2 | `go test ./internal/skillpacks/ -run TestPackSet_UnclosedSelection_RefusedWithoutMutation -short -count=1 -v` + `go test ./cmd/zcp/ -run TestSkillsPackSet_UnclosedSelection_JSONCode -short -count=1 -v` | passed | `--- PASS: TestPackSet_UnclosedSelection_RefusedWithoutMutation` + 6 subtests `ok ...skillpacks 0.148s`; `--- PASS: TestSkillsPackSet_UnclosedSelection_JSONCode` `ok ...cmd/zcp 0.369s` |
| AC2 | live drive: fresh temp dir, `pack-set matt-pocock-skills --skills implement --expected-revision anything --json` | passed | exit 1, `{"code":"unclosed-selection","message":"selection is not dependency-closed: missing code-review (required by implement), setup-matt-pocock-skills (required by code-review), tdd (required by implement)"}`; no `.zcp` dir, no skill files created (`find . -type f` → empty before/no new artifacts) |
| AC3 | `go test ./internal/skillpacks/ -run 'TestPackSet' -short -count=1 -v` (all TestPackSet* green incl. TestPackSet_ClosedSet_AppliesUnchanged + stale-revision-under-closure) | passed | `ok github.com/zeropsio/zcp/internal/skillpacks 0.423s`, all subtests `--- PASS` incl. `TestPackSet_ClosedSet_AppliesUnchanged/closed_expanded_set_with_a_stale_revision_still_conflicts_byte-identically` |
| AC3 | live drive: real `pack-add` (21 skills, commit `2ab958093e83e0ec752e6c1c5932da465bf23e0c`) → closed 3-skill subset apply → closed 4-skill (`implement` chain) subset apply | passed | `pack-add` → `skillCount:21`; `pack-set --skills "grilling,grill-with-docs,domain-modeling"` → `ok:true, selected:["domain-modeling","grill-with-docs","grilling"]`; `pack-set --skills "implement,tdd,code-review,setup-matt-pocock-skills"` → `ok:true, skillCount:4`; `pack-status` after → `warnings:[]` (no closure warning on a closed installed set) |
| AC4 | `node --test internal/content/welcomejs/pack_picker.test.js` — closure cases: individual/transitive check auto-include, cascade (direct + transitive + last-dependent-stays), category select-all cross-category pull, open normalization (legacy heal + out-of-catalog drop), omitted-requires robustness, refusal-inside-picker, Apply posts closed set with counted summary | passed | `tests 30`, `pass 30`, `fail 0`; incl. `✔ picker_closure_transitive...`, `✔ picker_cascade_transitive...`, `✔ picker_cascade_last_dependent...`, `✔ picker_open_normalization...` (×2), `✔ picker_omitted_requires...`, `✔ picker_refusal_visible...`, `✔ picker_apply_closed...` |
| AC5 | `go test ./cmd/zcp/ -run TestSkillsPackStatus_RequiresOnWire -short -count=1 -v` (requires array present; empty omitted; version stays 1) | passed | `--- PASS: TestSkillsPackStatus_RequiresOnWire` `ok github.com/zeropsio/zcp/cmd/zcp 0.266s` |
| AC5 | live drive: `pack-status matt-pocock-skills --json` on a fresh workspace (no install) | passed | `jq '.packs[0].catalog[] | select(.name=="implement").requires'` → `["tdd","code-review"]`; entries without edges (`ask-matt`, `diagnosing-bugs`, `setup-matt-pocock-skills`) carry no `requires` key; `.version == 1` |
| AC6 | `go test ./internal/skillpacks/ -run TestPackStatus_NonClosedInstalledSet_Warns -short -count=1 -v` | passed | `--- PASS: TestPackStatus_NonClosedInstalledSet_Warns` + 3 subtests (`implement without code-review warns`, `out-of-catalog skill keeps existing behavior`, `fully closed installed set has no closure warning`) `ok ... 0.175s` |
| AC6 | live sub-check: pre-Requires legacy manifest warning | not-run | needs a pre-Requires manifest fixture (out of reach from the CLI surface — Requires is now baked into the shipped catalog); fully covered by the unit test above (real fixture, seeded manifest) |
| AC7 | spec diff reviewed (this verifier pass): §3.1 closure clause, §4.2 edge table + admission rule + migration bucket, §7 proofs 13-16, §8 non-goal rewrite all present and match the implementation; `docs/spec-welcome-mode.md` §7 W-SKILLS carries the auto-include/cascade/normalization sentence | passed | manual diff review, no contradiction found (see report) |
| — | negative: `--skills ""` (explicit empty) stays valid — closure of ∅ is ∅ | passed | `TestPackSet_UnclosedSelection_RefusedWithoutMutation/empty_set_is_trivially_closed` `--- PASS` |
| — | negative: unknown-skill error precedes closure check (raw-list validation order unchanged) | passed | code review: `set.go` PackSet calls `normalizeDesiredSkills` (unknown/duplicate) → `validateSelectionGranularity` (atomic-partial) → `validateSelectionClosure` (unclosed-selection), in that order; `TestPackSet_UnknownSkillName_RefusedWithoutMutation` still green under `-run 'TestPackSet'` |
| — | negative: stale revision + unclosed set ⇒ `unclosed-selection`, NOT `conflict` (pure refusal pre-lock) | passed | `TestPackSet_StaleRevisionUnclosedSet_UnclosedWins` `--- PASS (0.03s)` |
| — | negative: closed expanded set losing a revision race ⇒ `conflict`, byte-identical workspace | passed | `TestPackSet_ClosedSet_AppliesUnchanged/closed_expanded_set_with_a_stale_revision_still_conflicts_byte-identically` `--- PASS (0.03s)` |
| — | regression: superpowers atomic full-set apply unaffected (no closure path reached) | passed | `TestPackSet_AtomicPack_PartialSelectionRefused` `--- PASS`; `TestCatalog_RequiresEdges_ValidGraph/superpowers` `--- PASS` (zero declared edges, atomic pack) |
| — | migration: picker open normalizes pending = transitiveClosure(selected ∩ catalog) — legacy non-closed set opens healed with Apply enabled; out-of-catalog entry (grill-me) excluded from pending so pack-set detaches it | passed | `picker_open_normalization: a legacy non-closed install opens healed, with Apply enabled and a migration note` + `picker_open_normalization: an out-of-catalog leftover is dropped from the opening pending set` both `✔` |
| — | FE: `unclosed-selection` refusal rendered INSIDE the open picker, not only on the obscured pack row | passed | `picker_refusal_visible: an unclosed-selection pack-set refusal renders its message inside the open picker` `✔`; diff review confirmed `applyPickerResult` sets `pickerState.refusalMessage` for any non-conflict error code and re-renders the open modal |
| — | FE robustness: catalog entry with omitted `requires` key ⇒ treated as no edges | passed | `picker_omitted_requires: a catalog entry with no requires key behaves as edge-free` `✔` |
| — | determinism: multi-parent missing skill (e.g. grilling required by wayfinder+triage) yields one sorted, stable message | passed | `TestPackSet_UnclosedSelection_RefusedWithoutMutation/multi-parent:_wayfinder+triage_share_the_missing_grilling` `--- PASS`; `TestRequirements_ClosureAndViolations/multi-parent_missing_dependency_collapses_to_one_violation` `--- PASS` |
| — | wire/no-second-traversal: `set.go` and `status.go` both consume `requirements.go`'s `transitiveViolations`/`FormatViolations` exclusively | passed | grep confirms the only non-test call sites of `transitiveViolations`/`Violations`/`Closure` are `requirements.go` itself, `status.go:127`, and `set.go:198` — no independent walk |
| — | FE/no-hard-coded-edges: `vscode-bootstrap-welcome.html` never names an edge skill (e.g. `grill-with-docs`, `setup-matt-pocock-skills`) outside the CLI-reported catalog data | passed | grep for edge skill-name literals in the template returns no hits |
| — | template version parity: `BootstrapExtVersion` 0.1.32 → 0.1.33 across `claude.go`, `vscode-bootstrap-package.json`, `diagnostics.test.js` pin | passed | `TestBootstrapExtVersion_ParityWithManifest` `--- PASS`; diff confirms all three sites bumped together |

## Promotion
- Contracts → docs/spec-skill-packs.md: §3.1 closure-validation clause (caller
  states the desired set; a non-closed set is refused `unclosed-selection`,
  zero writes; picker owns visible expansion) · §4.2 Requires admission rule +
  exclusion-vs-edge distinction (no standalone value → exclude, e.g. grill-me;
  standalone value → declared edge) + edge table + fix stale "initially
  selected … recommended" bullet · §7 new proofs (lint graph validity; refusal
  zero-writes; migration warning; picker closure UX) · §8 non-goal reworded
  ("dependency closure inferred from skill prose" — declared reviewed edges
  now in scope)
- Contracts → docs/spec-welcome-mode.md §7 W-SKILLS: auto-include + cascade
  sentence in the picker contract; the "opening selection mirrors what is
  installed and nothing else" sentence gains the declared-dependency /
  migration-normalization exception (pending = closure(selected ∩ catalog))
- §4.2 admission wording (per Codex): exclude a pure wrapper; declare an edge
  only when the target is proven mandatory; leave optional references
  unmodeled
- Invariants → TestCatalog_RequiresEdges_ValidGraph ·
  TestPackSet_UnclosedSelection_RefusedWithoutMutation ·
  TestPackStatus_NonClosedInstalledSet_Warns · picker closure cases in
  pack_picker.test.js
- CLAUDE.md trap line (≤1): none — spec §3.1/§4.2 + the refusal test carry it
- This plan → `plans/archive/` on LAND close

## Slice Briefs

### Slice brief: S1 — Requires field + edges + catalog lint + wire exposure

Self-contained: no other file is required to execute this. Spec §s cited are
pending GATE 1 promotion (flagged below).

**Outcome** (observable): `zcp skills pack-status --json` reports a
`requires` array on Matt-pack catalog entries that declare dependencies;
catalog lint proves the declared graph is valid.

**Allowed scope**
- Files: internal/skillpacks/catalog.go, internal/skillpacks/catalog_test.go,
  internal/skillpacks/requirements.go, internal/skillpacks/requirements_test.go,
  internal/skillpacks/status_test.go, cmd/zcp/skills.go, cmd/zcp/skills_test.go
- Explicitly excluded: set.go/errors.go (S2), status.go (S3), any FE template
  or welcomejs file (S4), docs/ (GATE 1 owns spec writes).

**Spec citations**: docs/spec-skill-packs.md §4.2 (Requires admission rule +
edge table — pending GATE 1), §3 (catalog membership = installability).

**Change spec**:
- `CatalogSkill` gains `Requires []string` with a Category-style doc warning:
  reviewed curation data only (per §4.2), never a filesystem path or
  execution input; targets must name skills in the SAME pack's catalog.
- Edge data (15 edges, 8 skills): grill-with-docs→[grilling,domain-modeling];
  implement→[tdd,code-review]; improve-codebase-architecture→[codebase-design,
  grilling]; wayfinder→[grilling,domain-modeling,research,
  setup-matt-pocock-skills]; triage→[grilling,setup-matt-pocock-skills];
  code-review→[setup-matt-pocock-skills]; to-spec→[setup-matt-pocock-skills];
  to-tickets→[setup-matt-pocock-skills]. Transitive depth 2 exists:
  implement→code-review→setup-matt-pocock-skills — deliberate, keep.
- Wire: `catalogSkillJSON` (cmd/zcp/skills.go:47-55 maps fields BY HAND — a
  Go struct field alone never reaches the FE) gains
  `Requires []string \`json:"requires,omitempty"\``; populate in the same
  hand-mapping site. `version` stays 1 (additive extension).
- Superpowers/Karpathy entries declare NO edges.
- **Shared closure module** `internal/skillpacks/requirements.go` (Codex —
  S2 and S3 must NOT independently implement traversal/formatting): pure
  functions over a `Pack` — (a) `transitive closure` of a skill-name set;
  (b) `violations` of a set: structured list of {missing skill, sorted
  requiring skills}, deterministically sorted; message rendering lives here
  too so set/status wording cannot drift.

**RED test list**
- `TestCatalog_RequiresEdges_ValidGraph` — layer: unit. Table-driven over the
  live catalog: (a) every Requires target exists in the same pack's Skills;
  (b) graph acyclic (DFS with the real edges); (c) only packs with
  `Selection == SelectionSubset` declare any edge (Superpowers atomic ⇒ edges
  would be dead data, spec-skill-packs.md §1); (d) no duplicate entries
  within one skill's Requires list.
- `TestRequirements_ClosureAndViolations` — layer: unit. Table over the
  shared module: direct expansion, transitive depth-2
  (implement ⇒ +tdd +code-review +setup-matt-pocock-skills), closed set ⇒ no
  violations, empty set ⇒ empty closure, multi-parent missing skill ⇒ one
  deterministic sorted violation entry. Oracle: hand-computed sets from the
  approved edge table.
- `TestCatalog_MattRequiresEdges_ExactSet` — layer: unit. Independent oracle:
  hand-transcribe the 15 approved edges (from the spec §4.2 table once
  promoted), compare against the catalog literal — drift guard in the style
  of TestCatalog_MattSupportedSet_Exactly21Grouped.
- `TestSkillsPackStatus_RequiresOnWire` — layer: tool (cmd/zcp). pack-status
  --json for the Matt pack: entries with edges carry `requires` verbatim;
  entries without edges OMIT the key (omitempty); top-level `version` == 1.
- Tracer check (Codex — real CLI, not only package tests): `go build -o
  <scratch>/zcp ./cmd/zcp` then run `zcp skills pack-status
  matt-pocock-skills --json` in a temp workspace and assert
  `.packs[0].catalog[] | select(.name=="implement").requires` ==
  ["tdd","code-review"] via jq. Include command + output in the report.

**Protocol**: RED → GREEN → REFACTOR.
1. Write the named tests first; confirm each fails for the right reason
   (missing field = compile error on a new seam is acceptable RED for the
   struct; edge-set mismatch = assertion RED):
   `go test ./internal/skillpacks/ ./cmd/zcp/ -run 'Requires' -short -count=1 -v`
2. Implement until green; same command.
3. Refactor green; `make lint-fast`.

**Report contract** (all required — never summarize away a failure)
- RED output + exit code · GREEN output + exit code · exact files touched ·
  layer-matrix pass lines (unit: ./internal/skillpacks/; tool: ./cmd/zcp/) ·
  independent-oracle note (edge set hand-transcribed from the approved list
  above, never read back from catalog.go).

**Stop conditions**: scope drift · material unknown · AC change · repeated
unexplained failure → halt + handoff.

**Definition of Done**
- [ ] RED replay: fails at slice base SHA, passes at slice head
- [ ] Named tests pass with -count=1 -v
- [ ] make lint-fast clean
- [ ] No file outside Allowed scope touched
- [ ] Report contract filled in full

### Slice brief: S2 — pack-set closure refusal

Self-contained. Spec §s pending GATE 1.

**Outcome** (observable): `zcp skills pack-set matt-pocock-skills --skills
implement --expected-revision <rev> --json` (a non-closed set) exits 1 with
`code: "unclosed-selection"`, a message naming every missing skill AND which
selected skill requires it, and provably zero workspace/manifest writes.

**Allowed scope**
- Files: internal/skillpacks/set.go, internal/skillpacks/errors.go,
  internal/skillpacks/set_test.go, cmd/zcp/skills_test.go
- Explicitly excluded: catalog.go (S1 landed it), status.go (S3), FE files
  (S4), docs/ (GATE 1). Do NOT expand the desired set — refusal only.

**Spec citations**: docs/spec-skill-packs.md §3.1 (declarative apply: the
caller states the desired installed set — closure validation clause pending
GATE 1), §1 (webview never an installation authority).

**Change spec**:
- New `CodeUnclosedSelection = "unclosed-selection"` in errors.go with a
  comment citing §3.1, matching the existing kebab-case catalog
  (errors.go:13-31).
- Closure check CONSUMES S1's `internal/skillpacks/requirements.go` module
  (violations + message rendering) — never re-implement traversal or
  formatting here (Codex).
- **Pinned precedence** (Codex): `unknown/duplicate → atomic-partial →
  unclosed-selection → lock/busy/filesystem → conflict`. The closure check is
  a pure function of desired + catalog: it runs with the pre-lock request-
  shape validations, BEFORE lock acquisition and the revision compare — an
  unclosed set is invalid input, not stale state, so
  `stale revision + unclosed set ⇒ CodeUnclosedSelection` (never conflict).
  Empty selection (`--skills ""`) is trivially closed. Check is TRANSITIVE
  (implement alone misses tdd, code-review, and — via code-review —
  setup-matt-pocock-skills).
- Error message format comes from the shared module: deterministic sorted
  order, each missing skill paired with its sorted requiring skill(s), e.g.
  `selection is not dependency-closed: missing code-review (required by implement), setup-matt-pocock-skills (required by code-review), tdd (required by implement)`.
  Message must be self-explanatory standalone — an old (0.1.32) FE renders
  it verbatim via the unknown-code fallback (vscode-bootstrap-welcome.html:771).

**RED test list**
- `TestPackSet_UnclosedSelection_RefusedWithoutMutation` — layer: unit.
  Pattern on TestPackSet_UnknownSkillName_RefusedWithoutMutation
  (set_test.go:150-168): non-closed desired set → CodedError
  CodeUnclosedSelection, State stays absent/unchanged, ZERO writes —
  byte-identical workspace INCLUDING no lock/state artifacts (Codex). Table
  rows: direct miss (implement w/o tdd), transitive miss
  (implement+tdd+code-review w/o setup-matt-pocock-skills), multi-parent
  deterministic message (grilling required by wayfinder+triage), closed set
  passes, empty set passes.
- `TestPackSet_StaleRevisionUnclosedSet_UnclosedWins` — layer: unit. Stale
  `--expected-revision` + unclosed set ⇒ CodeUnclosedSelection, not
  CodeConflict (precedence pin; pure refusal never reads workspace state).
- `TestPackSet_ClosedSet_AppliesUnchanged` — layer: unit. A fully closed
  subset applies exactly as before (diff/revision/manifest identical
  semantics); a CLOSED expanded set losing a revision race still returns
  CodeConflict byte-identically (re-proof of §7 proof 9 under closure).
- `TestSkillsPackSet_UnclosedSelection_JSONCode` — layer: tool (cmd/zcp).
  mutationJSON envelope: ok=false, code="unclosed-selection", exit 1.

**Protocol**: RED → GREEN → REFACTOR (commands per S1, -run 'Unclosed|ClosedSet').

**Report contract**: as S1 (RED/GREEN outputs + exit codes, files, layer
matrix, independent-oracle note — expected missing-lists hand-derived from
the §4.2 edge table, not recomputed via the implementation's own closure).

**Stop conditions**: standard four → halt + handoff.

**Definition of Done**: RED replay · named tests -count=1 -v · lint clean ·
scope respected · full report.

### Slice brief: S3 — pack-status migration warning

Self-contained. Spec §s pending GATE 1.

**Outcome** (observable): `zcp skills pack-status --json` on a project whose
INSTALLED Matt-pack selection predates Requires and is non-closed (e.g.
implement installed, tdd absent) reports a warning naming the missing
dependencies; a closed installed set reports no such warning.

**Allowed scope**
- Files: internal/skillpacks/status.go, internal/skillpacks/status_test.go
- Explicitly excluded: set.go/errors.go (S2), catalog.go, FE files, docs/.
  Status stays READ-ONLY — never mutates, never auto-heals.

**Spec citations**: docs/spec-skill-packs.md §3.1 migration clause (pending
GATE 1 extension: in-catalog skill with missing dependency = warning, not
detachment — distinct from the out-of-catalog third bucket).

**Change spec**: status computes violations of the manifest's installed
reviewed skills via S1's `internal/skillpacks/requirements.go` module (never
re-implement traversal/formatting — Codex); violations append to the
existing `Warnings []string` (defaulted `[]`, never null —
cmd/zcp/skills.go:375-382 relies on that). Same rendered pairing format as
S2's error by construction (shared module). Healing happens naturally on the
next picker Apply — S4's picker-open normalization builds a closed pending
set — status only reports. No new structured field (Codex: FE derives its
migration banner from `catalog[].requires` + `selected[]`, never parses
warning strings).

**RED test list**
- `TestPackStatus_NonClosedInstalledSet_Warns` — layer: unit. Seed a manifest
  with a non-closed installed set (pattern: fixture_test.go seeding used by
  TestSkillPacks_GrillMeOutOfCatalog_StatusSetRemove, set_test.go:593-710);
  status carries the warning; closed-set row asserts NO closure warning;
  out-of-catalog skill case still behaves per the existing test (no
  regression — run it).

**Protocol / Report contract / Stop conditions / DoD**: per S1 (commands with
-run 'NonClosedInstalled').

### Slice brief: S4 — FE picker closure UX

Self-contained. Spec §s pending GATE 1.

**Outcome** (observable): in the Customize picker (jsdom-tested), checking a
skill auto-checks its transitive Requires; unchecking a dependency cascades
to uncheck every skill whose closure needs it; the pending "N to add, M to
remove" summary counts implied changes; Apply posts the full closed set.

**Allowed scope**
- Files: internal/content/templates/vscode-bootstrap-welcome.html,
  internal/content/welcomejs/pack_picker.test.js,
  internal/content/welcomejs/diagnostics.test.js,
  internal/init/adapters/claude.go,
  internal/content/templates/vscode-bootstrap-package.json
- Explicitly excluded: vscode-bootstrap-welcome.js (host passes catalog
  through verbatim at collectPacksState — verify, don't edit), all Go files,
  docs/.

**Spec citations**: docs/spec-welcome-mode.md §7 W-SKILLS (picker rendered
from CLI-reported catalog, never a second hard-coded list; auto-include +
cascade sentence pending GATE 1); docs/spec-skill-packs.md §3.1 (Apply posts
the full desired set).

**Change spec**:
- Closure logic reads `s.requires` from the CLI-reported catalog objects —
  never a JS-side hard-coded edge list (W-SKILLS rule). An entry with an
  OMITTED `requires` key = no edges (robustness for old/edge-free entries).
- **One pure selection reducer** (Codex — never three copies of graph
  traversal): a single function (catalog, pending, mutation) → new pending,
  used by ALL of: individual checkbox (~html:973-988), per-category
  select-all (~html:963-966), whole-pack toggle (~html:933-936), AND
  picker-open initialization.
  Rules: check X ⇒ pending[dep]=true for X's transitive closure;
  uncheck X ⇒ pending[Y]=false for every selected Y whose closure contains X
  (cascade — owner-decided) — dependents cascade OFF, dependencies STAY
  checked (flat state cannot distinguish auto-added from user-wanted; orphan
  cleanup would remove explicit intent — Codex confirmed); never silently
  re-add.
- **Picker-open normalization** (Codex blocking-gap fix):
  `pending = transitiveClosure(selected ∩ catalog)` instead of copying
  installed names verbatim (~html:879). A legacy non-closed install (e.g.
  {implement}) opens with deps visibly added and Apply ENABLED (the diff is
  real); an out-of-catalog leftover (grill-me) drops out of pending so Apply
  lets pack-set force-detach it instead of reposting it as unknown-skill.
  Render a small migration note derived from `catalog[].requires` +
  `selected[]` (structured facts already on the wire — never parse warning
  strings; no new Go field).
- Pending diff/summary derives from pending vs installed as today
  (html:994-996) — implied changes flow through automatically; verify counts.
- Apply unchanged: posts Object.keys(pending).filter(...) — now always a
  closed set by construction (html:1078-1083).
- `unclosed-selection` pack-set result must render INSIDE the open picker
  (new-FE safety net; today pack-result text lands on the pack row the modal
  obscures — html:826-837).
- BootstrapExtVersion 0.1.32 → 0.1.33 in internal/init/adapters/claude.go +
  "version" in vscode-bootstrap-package.json + the pin in
  diagnostics.test.js (CLAUDE.md trap: an unbumped template edit never
  reaches a running fleet).
- The recommendation-badge slot stays empty (pack_picker.test.js:163 asserts
  [data-picker-skill-badge] absent — do not introduce a badge; auto-checking
  IS the affordance, per owner decision).

**RED test list** (node --test, fixtures hand-authored per the file's
independent-oracle header — extend MATT_CATALOG with `requires` mirroring the
approved §4.2 table, never generated from Go source)
- picker_closure: individual check auto-includes (grill-with-docs ⇒ grilling
  + domain-modeling checked)
- picker_closure_transitive: implement ⇒ tdd, code-review,
  setup-matt-pocock-skills all checked
- picker_cascade exact cases (Codex): (a) implement selected, uncheck
  code-review ⇒ implement off, setup-matt-pocock-skills STAYS on; (b)
  uncheck setup-matt-pocock-skills ⇒ code-review AND implement off,
  unrelated/shared deps stay on; (c) removing the LAST dependent leaves the
  former dependency selected (no orphan cleanup)
- picker_category_selectall: ENGINEERING select-all pulls `grilling` from
  Productivity (Productivity declares no edges — the cross-category
  direction is Engineering→Productivity; Codex corrected the earlier
  inverted case)
- picker_open_normalization: legacy installed {implement} ⇒ picker opens
  with tdd/code-review/setup checked, Apply enabled, migration note shown;
  legacy installed {grill-me, tdd} ⇒ grill-me absent from pending
- picker_omitted_requires: catalog entry without `requires` key behaves as
  edge-free (no crash, no closure effect)
- picker_refusal_visible: a pack-set reply code="unclosed-selection" renders
  its message inside the open picker
- picker_apply_closed: Apply argv --skills csv is dependency-closed and the
  pending summary counted implied additions/removals
- diagnostics version pin 0.1.33

**Protocol**: RED first (`node --test internal/content/welcomejs/pack_picker.test.js`
fails on new cases), GREEN, then the FULL welcomejs suite + `make lint-fast`.

**Report contract / Stop conditions / DoD**: per S1; layer matrix = the
welcomejs node --test suite (all files) + go test ./internal/init/... (version
parity test).

## Context (transient; FRAME input)
Prior phase: grill-me curation landed as 469e6051 (catalog 22→21, migration
behavior pinned by TestSkillPacks_GrillMeOutOfCatalog_StatusSetRemove).
Upstream analysis session 2026-08-04: mattpocock/skills @2ab9580 == plugin
1.2.0; edges sourced from skill bodies + upstream ADR 0001 + .agents/invocation.md.
