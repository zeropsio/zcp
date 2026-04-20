# HANDOFF-to-I3.md — zcprecipator2 implementation phase resume

**For**: fresh Claude Code instance picking up zcprecipator2 rollout from commit `dd1e008` (C-5 flip).

**Branch**: `main`. Last commit: `dd1e008 feat(zcprecipator2): C-5 flip — buildSubStepGuide atom-preferring resolution`.

**Repo state**: clean. Every commit landed green through `go test ./... -count=1 -short` + `make lint-local` (0 issues).

---

## Required reading (in order — ~30 min)

1. **`docs/zcprecipator2/implementation-notes.md`** — per-commit running notes. Read top-to-bottom; shows what each of commits C-0 through C-5 did, LoC deltas, test layer coverage, decisions made, and **what's deliberately NOT done yet** (end of C-5 flip section).
2. **`docs/zcprecipator2/RESUME.md`** — research-phase handoff. Skim §"Open questions — user-resolved (handoff)" (Q1–Q6 answers govern implementation decisions) and §"Open decisions" (6 user decisions from research kickoff). Everything else in RESUME.md can be deferred — the implementation-notes supersede it for execution state.
3. **`docs/zcprecipator2/06-migration/rollout-sequence.md`** — the 16-commit plan. Read **only C-6 onward** (C-0..C-5 are done). Critical: `rollout-sequence.md` is the authoritative scope per commit — named files, LoC estimates, test layers, ordering deps, "breaks alone" consequences.
4. **`docs/zcprecipator2/06-migration/rollback-criteria.md`** — v35 go/no-go gate regimen. Only needs full read when you reach the post-C-14 gate. Before that, skim §9 headline (the 6 bars).
5. **`CLAUDE.md`** (project root) — TDD discipline + conventions + "do NOT" list. Already auto-loaded in your context; re-read if you drift.

That's it. Everything else (atomic-layout.md, principles.md, check-rewrite.md, data-flow-*.md, spec-content-surfaces.md, the 44 step-4 verification files) is reference material you consult **on demand** when a specific commit needs it — not upfront.

---

## Current state — what's landed

8 commits on top of `main`:

| # | Hash | What | Test/lint |
|---|---|---|---|
| 0 | `37b0062` | Research dump — 81-file zcprecipator2/ tree | N/A (docs) |
| 1 | `b76b293` | C-0: substrate-audit + FactScope pins | green |
| 2 | `791a5a2` | C-1: SymbolContract type + 12 FixRecurrenceRules | green |
| 3 | `4c01538` | C-2: FactRecord.RouteTo + 9-value enum | green |
| 4 | `6df79dc` | C-3: atom_manifest.go (120 atoms declared) | green |
| 5 | `dfcb500` | C-4: 120 atom files under internal/content/workflows/recipe/ | green |
| 6 | `515a6b3` | C-5 foundation: embed.FS + loader + 6 stitchers + SymbolContract wiring + RouteTo filter | green |
| 7 | `dd1e008` | C-5 flip: buildSubStepGuide atom-preferring for 17 substeps | green |

**Agent now sees atom-based content at substep-advance time** for 17 substeps. Dispatch briefs for scaffold/feature/writer/code-review flow from the new stitchers with byte-identical SymbolContract JSON across parallel dispatches.

---

## Known deferred work (NOT blockers for C-6)

- **`resolveRecipeGuidance` (step-entry composition)** still reads `recipe.md` via `content.GetWorkflow` + `ExtractSection`. The step-entry flip lands with C-15 when the monolith is deleted.
- **`recipe_topic_registry.go` fallback path** in `buildSubStepGuide` still serves as graceful degradation. C-15 deletes it.

Neither blocks C-6..C-14. Both resolve at C-15 (final deletion).

---

## Next commit — C-6

**Goal**: add 5 new architecture-level checks per `docs/zcprecipator2/03-architecture/check-rewrite.md` §16.

Checks (all NEW — no predecessor in the current check suite):

1. `symbol_contract_env_var_consistency` (P3) — diffs env-var tokens across all scaffolded codebases' `src/**` + `.env.example` + `zerops.yaml` against `plan.SymbolContract.EnvVarsByKind`. Closes v34 DB_PASS / DB_PASSWORD cross-scaffold class.
2. `visual_style_ascii_only` (P8) — greps `{host}/zerops.yaml` for Unicode Box-Drawing codepoints `[\x{2500}-\x{257F}]`. Closes v33 Unicode box-drawing class.
3. `canonical_output_tree_only` (P8) — `find /var/www -maxdepth 2 -type d -name 'recipe-*'` must return empty. Closes v33 phantom-tree class via positive allow-list.
4. `manifest_route_to_populated` (P5) — every `.facts[].routed_to` in ZCP_CONTENT_MANIFEST.json is non-empty and matches the enum.
5. `no_version_anchors_in_published_content` (P6) — greps `{host}/README.md`, `{host}/CLAUDE.md`, `environments/*/README.md` for `v\d+(\.\d+)*`. Closes v33 version-anchor leakage class.

**What changes**:
- `internal/tools/workflow_checks_symbol_contract.go` (new, ~150 LoC)
- `internal/tools/workflow_checks_visual_style.go` (new, ~50 LoC)
- `internal/tools/workflow_checks_canonical_output_tree.go` (new, ~60 LoC)
- `internal/tools/workflow_checks_manifest_route_to.go` (new, ~50 LoC)
- `internal/tools/workflow_checks_no_version_anchors_in_published.go` (new, ~70 LoC)
- Per-check unit test files per CLAUDE.md seed pattern (~600 LoC total)
- Register each check in `internal/workflow/recipe_substeps.go` + checker dispatch at the appropriate substeps per `check-rewrite.md §16 "Added to" column`

**Ordering deps**: C-1 (SymbolContract field for symbol-contract check), C-2 (RouteTo field for manifest-route-to check). Both ✓ landed.

**Rough LoC**: +980. **Test layers**: unit + tool + integration.

**Breaks alone**: new gates fire. Zero regression risk against prior runs — target is a clean v35 candidate.

---

## Operating rules (from user's task contract)

1. **TDD non-negotiable** — every behavior change has a failing test first (RED → GREEN → REFACTOR)
2. **After each commit**: run `go test ./... -count=1` + `make lint-local`. Do not advance to the next commit until both green.
3. **User-review gates (STOP and report — do not proceed without user approval)**:
   - BEFORE C-7.5 (NEW ROLE — editorial-review sub-agent dispatch; review composed briefs against step-4 goldens at `docs/zcprecipator2/04-verification/brief-editorial-review-*.md`)
   - BEFORE C-10 (BREAKING payload shape flip)
   - AFTER C-14 (STOP — user commissions v35 showcase run)
   - Note: pre-C-5 gate already cleared this session
4. **Each commit produces a summary** — append to `docs/zcprecipator2/implementation-notes.md` under a new `## C-N` section: LoC delta, what landed, verification, breaks-alone consequence, ordering deps. Gate summaries additionally document user-facing review items.
5. **Q1–Q6 resolutions are locked** (see RESUME.md):
   - Q1: top-level `plan.SymbolContract` ✓ implemented
   - Q2: `writer_manifest_honesty` runs at BOTH deploy.readmes complete AND close.code-review complete → implement in C-8
   - Q3: TodoWrite content-only discipline (no Go-layer refusal) → no commit needed; gated on v35 metric
   - Q4: minimal writer Path A main-inline for v35 ✓ implemented; editorial-review uses Path B dispatch default-on per refinement → C-7.5
   - Q5: commission v35.5 minimal AFTER v35 showcase passes → operational (post-C-15)
   - Q6: v35 locked to `claude-opus-4-7[1m]`; fallback = dual-cause flagging → baked into C-14

---

## Parallelization within remaining commits

Per `rollout-sequence.md §Parallelization opportunities`:
- C-6 and C-8 can be authored in parallel after C-5 (both are check additions; independent surfaces)
- C-7 could split into 4 sub-commits (16 checks rewritten; suggested split in the doc)
- C-7.5 can parallel-author with C-8 post-C-7 (different phases: close vs deploy.readmes)
- C-11, C-12, C-13 can parallel post-C-10

Serial-at-merge. But if subagents help on mechanical work (C-7's 16 check refactors), use them — proven pattern from C-4.

---

## Subagent usage pattern (proven in C-4)

When a commit has >5 files of independent mechanical work:

1. Main instance reads the scope doc (rollout-sequence.md entry + relevant check-rewrite.md / atomic-layout.md sections) and maps each file to a subagent assignment
2. Dispatch N `general-purpose` subagents in parallel via the Agent tool, each with:
   - Exact file paths + LoC budget
   - Reference files to read (max 3-4)
   - Invariants to preserve (P2/P6/P8 etc.)
   - Output contract (files written, summary to return)
3. Each subagent returns a compact summary (`{filename: line_count}` + notes)
4. Main instance verifies the output — greps for invariant violations, runs `go build`, runs relevant tests
5. Fix + commit

C-4's 9-subagent dispatch worked cleanly. C-7's 16-check split is the next natural fit.

---

## Expected pace

- C-6: ~45 min (5 new checks, small scope, all new files)
- C-7: ~2 hours if split into 4 subagent-parallel sub-commits; ~4 hours single-thread
- C-7.5 (after gate): ~1.5 hours (10 atoms — reuse C-4 pattern — + substep registration + 7 checks)
- C-8: ~30 min (extend one check's dimension iteration)
- C-9: ~15 min (delete one check + tests)
- C-10 (after gate): ~1 hour (payload shape + cascading test updates across 12 check files)
- C-11..C-14: ~2 hours combined (smaller surfaces each)
- v35 commission (user) — operational step
- T-1..T-12 application: ~30 min
- C-15: ~30 min (deletion + cascading reference cleanup)
- v35.5 commission (user) — operational step

Total remaining: ~10 focused hours of engineering work + user-gated pauses at the 3 remaining gates.

---

## If something is unclear, ask the user

The user resolved Q1–Q6 before implementation started. Anything that wasn't explicitly resolved there and requires a decision (e.g., if C-7's CLI shim surface has an ambiguity, or C-10's debug-log retention needs a judgment call) is fair to pause and ask. Don't invent structure — the research artifacts are the source of truth for what the system should look like.

---

## Starting action

1. Read the required docs (~30 min)
2. Verify the baseline is still green: `go test ./... -count=1 -short` + `make lint-local`
3. Begin C-6. First commit: add `symbol_contract_env_var_consistency` check (the one with the most-direct P3 closure for v34).
4. Report between commits — LoC delta, test result, lint result, breaks-alone consequence.
5. STOP at the 3 remaining user-review gates (pre-C-7.5, pre-C-10, post-C-14) — do not proceed without explicit user approval.

Good hunting.
