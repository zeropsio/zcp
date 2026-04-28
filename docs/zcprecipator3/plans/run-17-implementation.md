# Run-17 Implementation Guide

**Status**: implementation-ready. Derived from [run-17-prep.md](run-17-prep.md) after triple-confirmation pass on 2026-04-28; folds in five derisking additions (Tranche -1 pre-flight harness; Tranche 0.5 distillation atoms + rubric; adversarial threshold tests; concrete rubric pinning; deploy-phase shape worked examples) that move first-dogfood-above-golden confidence from 60% to 80%.

**Predecessor chain**: [run-15-readiness.md](run-15-readiness.md) → [run-16-prep.md](run-16-prep.md) → [run-16-readiness.md](run-16-readiness.md) → run-16 dogfood ([runs/16/](../runs/16/)) → [run-16-post-dogfood.md](run-16-post-dogfood.md) → [run-17-prep.md](run-17-prep.md) → **run-17-implementation.md** (this doc).

**Authority**: this doc is the per-tranche implementation contract. Every claim with a file:line cite is verifiable in the current tree; every proposed change has an LoC estimate, exact edit text where mechanical, test names that pin the change, a verification command, and a gate criterion that must hold before the next tranche ships.

**Reading order for the implementer**:

1. §0 — confidence model + targets (what "above golden" means concretely)
2. §1 — corrections to fold into [run-17-prep.md](run-17-prep.md) before code lands
3. §2 — tranche overview (the 10 tranches in dispatch order)
4. §3-§13 — per-tranche implementation contract
5. §14 — per-tranche gate criteria (what must pass before next tranche ships)
6. §15 — open questions (Q1-Q5 from prep §10) resolved
7. §16 — risk register (updated, fragment-versioning + reference-embed risks resolved)
8. §17 — maintenance hooks (CLAUDE.md, spec, plan.md updates)

---

## §0. Confidence model + targets

### §0.1 What "above golden path" means

Run-15 honest grade: 7.5/10. Run-16: 8.0/10. Reference recipes (laravel-jetstream + laravel-showcase): ~8.5/10. "Above golden" = consistently ≥8.5 across the seven content surfaces enumerated in [docs/spec-content-surfaces.md](../../spec-content-surfaces.md).

Run-17 target: **8.5–9.0 across all surfaces, no surface below 8.0**. The ceiling is bounded by what the rubric (Tranche 0.5) can express; calibration of the rubric is itself part of run-17.

### §0.2 Confidence per tranche

| Tranche | Confidence (cleanly lands) | Confidence (closes its named miss) |
|---|---:|---:|
| -1 (pre-flight harness) | 95% | n/a — derisking tooling, not a quality lift |
| 0 (free quality wins) | 99% | 99% (R-17-C9) |
| 0.5 (distillation + rubric) | 90% (subjective: depends on my distillation quality) | 80% (sets the contract refinement and synthesis read against) |
| 1 (engine-emit retraction + brief embed) | 90% | 80% (R-17-C1/C2/C3/C4/C5/C6/C7 upstream closure) |
| 2 (KB symptom-first record-time refusal) | 95% | 90% (R-17-C1 backstop) |
| 3 (CodebaseGates split) | 95% | 99% (R-16-1) |
| 4 (refinement sub-agent) | 75% (lands cleanly) | 70% (above-golden lift on first dogfood) |
| 5 (refusal aggregation) | 95% | 99% (R-17-C10) |
| 6 (deployFiles narrowness validator) | 95% | 99% (R-17-C8) |
| 7 (v2 deletion) | 90% | n/a — eliminates dual-tree friction |

Aggregate first-dogfood-above-golden confidence: **80%**.

### §0.3 Decision: Tranche 4 ships even if pre-flight is mid

Pre-flight refinement against frozen run-16 fragments may show partial closure (some refinements correct, some misfire). The implementer ships Tranche 4 to dogfood IF the pre-flight shows ≥60% refinement-correct rate on a sample of 20 hand-graded refinements. Below 60%, Tranche 4 holds and the distillation atoms (Tranche 0.5) get a second pass. This avoids dogfooding a refinement primitive that's actively making content worse.

---

## §1. Corrections to fold into run-17-prep.md

These edits to [run-17-prep.md](run-17-prep.md) ship as a single commit BEFORE the implementation guide directs any code change. They keep the prep doc accurate as the implementation reference.

### §1.1 §3.1 fact breakdown counts

> "67 facts in [environments/facts.jsonl](../runs/16/environments/facts.jsonl) — 36 `porter_change`, 17 `field_rationale`, 8 `tier_decision`, 6 browser-verification + scaffolding observations."

Replace "17 `field_rationale`, 8 `tier_decision`" with "15 `field_rationale`, 10 `tier_decision`". Verified by `grep -c '"kind":"field_rationale"'` and `grep -c '"kind":"tier_decision"'`.

### §1.2 §9 tranche-numbering inconsistency

Prep §9 prologue: "Tranches 0-1 ship before run-17 dogfood. Tranches 2-6 ship in sequence. Tranche 7 is sign-off."

§9 detail block then lists Tranche 7 = v2 deletion, Tranche 8 = sign-off. Fix the prologue: "Tranches 2-7 ship in sequence. Tranche 8 is sign-off."

This implementation guide further inserts Tranche -1 (pre-flight) and Tranche 0.5 (distillation + rubric) between prep's Tranches 0 and 1, so the final ordering is `-1 → 0 → 0.5 → 1 → 2 → 3 → 4 → 5 → 6 → 7 → 8`.

### §1.3 §6.2 reference embed must be design-time distillation, not runtime filesystem reads

Prep §6.2 "Brief content (~200 KB)" bullet: "**Reference recipes verbatim** — laravel-jetstream + laravel-showcase, both apps repos + recipe trees. Every README, every import.yaml, every codebase zerops.yaml. ~80 KB."

This is wrong as written. The ZCP binary ships only the `embed.FS` corpus under `internal/content/...` and `internal/recipe/content/...`. `/Users/fxck/www/laravel-*` and `/Users/fxck/www/recipes/...` exist on the prep author's machine but NOT on production user machines. Any composer that Reads those paths at runtime fails for every user except the doc author.

Replace with:

> **Distilled reference shape atoms** — hand-extracted at design-time from laravel-jetstream + laravel-showcase, embedded under `internal/recipe/content/briefs/refinement/reference_*.md` (see §5 of run-17-implementation.md for atom inventory). ~25 KB after distillation. The composer reads only embedded atoms; no `/Users/fxck/www/...` paths at runtime.

Also add an explicit invariant to §7 ("What stays unchanged"):

> **Production agents read only the embedded atom corpus.** Any reference recipe / external example used to inform authoring must be distilled into an atom at design-time and embedded via `embed.FS`. The brief composer never opens local-filesystem paths outside `internal/recipe/content/...`. Pinned by `TestNoFilesystemReferenceLeak_RefinementBrief` (Tranche 4).

### §1.4 §8 Risk 3 fragment versioning aspirational claim

Prep §8 Risk 3: "post-refinement re-validation; fragment versioning rolls back failed refinements."

Verified: the engine has [`record-fragment` Mode=append/replace](../../../internal/recipe/handlers.go) — no fragment versioning primitive exists. Replace mitigation:

> **Mitigation**: refinement is wrapped in a transaction primitive — the engine snapshots the fragment body before refinement Replace, runs validators after Replace, and reverts to snapshot if any new violation surfaces. Implemented as `Session.SnapshotFragment(id) → string` + `Session.RestoreFragment(id, snapshot)`. ~30 LoC + tests in Tranche 4.

### §1.5 §11 v2 atom tree spec edits to mirror in v3

Prep §11: "edits landed in v2 atom tree; need re-doing in v3 — see Tranche 1."

Tranche 1's bullet list doesn't enumerate the specific files. Add to prep §11 (and incorporated into Tranche 1 below):

- `internal/content/workflows/recipe/briefs/editorial-review/single-question-tests.md` — "outgrow this tier" question deletion. v3 equivalent: NONE today (run-16 architecture has no single-question-tests atom in v3). Verify whether v3 needs the equivalent or whether the v3 brief composer already covers the territory.
- `internal/content/workflows/recipe/briefs/writer/content-surface-contracts.md` — showcase tier supplements section (queue-group + SIGTERM mandate, lines 102-109). v3 target: new atom `internal/recipe/content/briefs/codebase-content/showcase_tier_supplements.md` (Tranche 1, §6).

### §1.6 §4.6 / Tranche 7 surgery surface refinement

Prep §4.6 names `internal/analyze/structural.go` in the surgery surface, but verified: only `runner.go:21` imports the v2 path via `AtomRootDefault`. `structural.go` consumes `AtomRootDefault` indirectly. Tranche 7 should:

- Delete `runner.go::AtomRootDefault` constant + its only caller `CheckAtomTemplateVarsBound(AtomRootDefault, ...)`, OR
- Repoint at v3 (`internal/recipe/content/briefs`)

If repoint: `structural.go::CheckAtomTemplateVarsBound` continues working unchanged. If delete: delete callers transitively. Tranche 7 detail (§12) picks the path.

---

## §2. Tranche overview

10 tranches in this order. Each gate must close before the next ships.

| # | Name | Files | LoC | Gate to next |
|---|---|---|---:|---|
| -1 | Pre-flight harness | `tools/run17-preflight/` | +200 | Harness produces baseline + post-Tranche-1 brief diff for at least apidev codebase from runs/16 |
| 0 | Free quality wins | `briefs_content_phase.go` | -3 | `TestWriteFactSummary_PorterChange*` passes |
| 0.5 | Distillation atoms + rubric | `internal/recipe/content/briefs/refinement/`, `docs/spec-content-quality-rubric.md` | +700 | Distillation atoms hand-graded ≥8.5 against references; rubric pinned with 3 hand-scored examples per criterion |
| 1 | Engine-emit retraction + brief embed | `engine_emitted_facts.go`, `synthesis_workflow.md`, `briefs/scaffold/decision_recording.md`, `briefs/feature/decision_recording.md`, new `showcase_tier_supplements.md` | +50 net | Pre-flight harness shows lift on R-17-C1/C2/C3/C4/C5/C6/C7 fragments without refinement |
| 2 | KB symptom-first record-time refusal | `slot_shape.go` | +45 | `TestCheckCodebaseKB_SymptomFirst*` covers 8 fail + 6 pass cases |
| 3 | CodebaseGates split (R-16-1) | `gates.go`, `phase_entry/scaffold.md` | +30 | `TestCodebaseGates_ScaffoldFactQualityOnly` + 3 sibling tests pass |
| 4 | Refinement sub-agent | `briefs_refinement.go`, `phase_entry/refinement.md`, `briefs/refinement/synthesis_workflow.md`, `handlers.go` (snapshot/restore + dispatch wiring) | +600 | Pre-flight refinement-correct rate ≥60% on hand-graded sample of 20 |
| 5 | Refusal aggregation | `slot_shape.go`, `handlers.go` | +30 | `TestCheckSlotShape_AggregatesAllOffenders` passes |
| 6 | deployFiles narrowness validator | `validators_codebase.go` | +40 | `TestValidateDeployFiles_*` covers 4 scenarios |
| 7 | v2 atom tree deletion | `internal/content/workflows/recipe/`, `internal/workflow/recipe*.go`, `internal/workflow/atom_*`, `tools/lint/...`, `internal/analyze/runner.go`, `internal/server/` | -2,500 | `go build ./... && go test ./...` clean; `zerops_workflow` removed from MCP registration |
| 8 | Sign-off | `docs/zcprecipator3/runs/17{a,b}/`, plan.md, archive, GitHub release notes (no `CHANGELOG.md` — repo uses git tags + release notes; see §13.3) | +50 | All prior gates green; run-17 dogfood completed; CONTENT_COMPARISON.md + ANALYSIS.md filed |

Net LoC (recomputed):

- Atoms (Tranche 0.5 + Tranche 1 + Tranche 4): ~+1,680
- Engine code (Tranche 1 retraction -200 + Tranche 4 composer/snapshot +230 + Tranches 2/3/5/6 +145 + Tranche 0 -3): ~+170
- Tests (across all tranches): ~+620
- Pre-flight tooling (Tranche -1 +300, Tranche 8 -300): 0 net
- v2 deletion (Tranche 7): ~-2,500
- **Net: ~-30 LoC** (roughly neutral; the v2 deletion offsets the new architecture)

---

## §3. Tranche -1 — Pre-flight harness

**Goal**: build offline tooling that lets the implementer evaluate whether Tranche 1 (codebase-content brief embed) and Tranche 4 (refinement sub-agent) actually move the needle BEFORE the run-17 dogfood. Eliminates "we'll find out at dogfood" risk.

### §3.1 What it does

Two harnesses, both human-in-the-loop:

**(A) `cmd/zcp-preflight-codebase-content/`** — composes `BuildCodebaseContentBrief` for a chosen run-16 codebase using current code, dumps the brief to `runs/17-preflight/baseline/<codebase>/brief.md`. After Tranche 1 lands, re-runs and dumps to `runs/17-preflight/post-tranche-1/<codebase>/brief.md`. Caller manually dispatches the brief in a fresh Claude Code session, captures `record-fragment` outputs, files them to `runs/17-preflight/<phase>/<codebase>/fragments/`, and grades against the [Tranche 0.5 rubric](#§5-tranche-05--distillation-atoms--rubric).

**(B) `cmd/zcp-preflight-refinement/`** — for each fragment in `runs/16/<codebase>/`, composes `BuildRefinementBrief` (using post-Tranche-1 fragments + distillation atoms), dumps to `runs/17-preflight/refinement-input/<codebase>/<fragment-id>.md`. Caller dispatches against a fresh sub-agent and saves output to `runs/17-preflight/refinement-output/`. Diff input vs output; hand-grade refinement-correct or refinement-misfire.

### §3.2 Implementation

**New files**:

```
cmd/zcp-preflight-codebase-content/main.go    — ~80 LoC CLI
cmd/zcp-preflight-refinement/main.go          — ~80 LoC CLI
docs/zcprecipator3/runs/17-preflight/README.md  — manual-dispatch checklist
```

`cmd/zcp-preflight-codebase-content/main.go` skeleton:

```go
// zcp-preflight-codebase-content composes BuildCodebaseContentBrief
// for a run-16 codebase using the current engine code, dumping the
// brief + filtered facts to disk for human-in-the-loop evaluation.
//
// Usage: zcp-preflight-codebase-content -run runs/16 -codebase apidev \
//          -mount-root /path/to/recipes -out runs/17-preflight/baseline/apidev
//
// Plan struct has no CodebaseByHostname method today; the loader
// helper iterates plan.Codebases ([]Codebase). Parent is resolved
// via recipe.ResolveChain against the recipes mount root, not pulled
// from plan (Plan struct has no Parent field — that lives on Session).
package main

import (
    "flag"
    "log"
    "os"
    "path/filepath"
    "strings"

    "github.com/zeropsio/zcp/internal/preflight"
    "github.com/zeropsio/zcp/internal/recipe"
)

func main() {
    runRoot := flag.String("run", "", "path to runs/<N> root (e.g. docs/zcprecipator3/runs/16)")
    codebase := flag.String("codebase", "", "codebase hostname")
    mountRoot := flag.String("mount-root", "", "recipes mount root for parent resolution (optional)")
    outDir := flag.String("out", "", "output directory")
    flag.Parse()

    plan, err := preflight.LoadPlanFromSessionLog(*runRoot)
    if err != nil { log.Fatalf("load plan: %v", err) }
    facts, err := preflight.LoadFactsJSONL(*runRoot)
    if err != nil { log.Fatalf("load facts: %v", err) }

    var cb recipe.Codebase
    for _, c := range plan.Codebases {
        if c.Hostname == *codebase { cb = c; break }
    }
    if cb.Hostname == "" {
        log.Fatalf("codebase %q not in plan.Codebases", *codebase)
    }

    var parent *recipe.ParentRecipe
    if *mountRoot != "" {
        p, err := recipe.ResolveChain(recipe.Resolver{MountRoot: *mountRoot}, plan.Slug)
        if err == nil { parent = p }
    }

    brief, err := recipe.BuildCodebaseContentBrief(plan, cb, parent, facts)
    if err != nil { log.Fatalf("build brief: %v", err) }

    if err := os.MkdirAll(*outDir, 0o755); err != nil { log.Fatal(err) }
    os.WriteFile(filepath.Join(*outDir, "brief.md"), []byte(brief.Body), 0o644)
    os.WriteFile(filepath.Join(*outDir, "brief.parts.txt"),
        []byte(strings.Join(brief.Parts, "\n")), 0o644)
}
```

`cmd/zcp-preflight-refinement/main.go` follows the same shape but calls `BuildRefinementBrief` (lands in Tranche 4; this CLI is wired in Tranche 4 and lands as a stub now).

**Shared loader helpers** in a new package `internal/preflight/` (single file, deletable post-run-17):

```
internal/preflight/loader.go
  LoadPlanFromSessionLog(runRoot string) (*recipe.Plan, error)
  LoadFactsJSONL(runRoot string) ([]recipe.FactRecord, error)
  LoadFragments(runRoot string) (map[string]string, error)
```

**Important**: `runs/<N>/` does NOT persist a `plan.json` (verified: `find docs/zcprecipator3/runs/16 -name 'plan*'` returns nothing). The Plan only exists in-memory during a live run. `LoadPlanFromSessionLog` reconstructs the Plan by scanning `<runRoot>/SESSION_LOGS/main-session.jsonl` for the last `mcp__zerops__zerops_recipe` call with `action=update-plan` and unmarshalling its `plan` field — that JSON is byte-identical to the in-memory `recipe.Plan` (the dispatch already round-trips it). Fragments are reconstructed by reading the `record-fragment` calls in the same log.

### §3.3 Tests

```
internal/preflight/loader_test.go
  TestLoadPlanFromSessionLog_Run16  — extracts plan from main-session.jsonl
  TestLoadFactsJSONL_Run16          — fixtures from runs/16/environments/facts.jsonl
  TestLoadFragments_Run16           — every codebase has ≥1 fragment
```

### §3.4 Verification

```
go build ./cmd/zcp-preflight-codebase-content
./zcp-preflight-codebase-content -run docs/zcprecipator3/runs/16 -codebase apidev -out /tmp/preflight-baseline
ls /tmp/preflight-baseline/                # expect brief.md, brief.parts.txt
wc -l /tmp/preflight-baseline/brief.md     # expect ~80–150 lines
```

### §3.5 Gate to next tranche

- [ ] Both CLIs build cleanly.
- [ ] Loader tests pass.
- [ ] Manual run on apidev produces a brief that *opens correctly in a fresh Claude Code session* (no broken atom references, no truncation mid-instruction).
- [ ] Side-by-side diff between current-engine baseline brief and post-Tranche-1 brief is non-trivial and visibly contains the new embedded sections (classification table, voice patterns, KB fail/pass examples, IG one-mechanism examples). "Harness builds" without a meaningful brief delta is a Tranche -1 failure, not a pass.

### §3.6 LoC + cleanup

+~300 LoC (CLI + loader + tests + checklist). Both `cmd/zcp-preflight-*` directories and `internal/preflight/` get deleted in Tranche 8 sign-off — they're scaffolding for run-17, not permanent infrastructure.

---

## §4. Tranche 0 — Free quality wins

**Goal**: ship the 1-line truncation fix as a standalone commit. No risk; immediate quality lift visible in the codebase-content brief.

### §4.1 Change

**File**: [`internal/recipe/briefs_content_phase.go`](../../../internal/recipe/briefs_content_phase.go)

**Lines 326-336 (current `writeFactSummary` for porter_change + field_rationale)**:

```go
case FactKindPorterChange:
    fmt.Fprintf(b, "- porter_change | topic=%s | class=%s | surface=%s | %s\n",
        f.Topic, f.CandidateClass, f.CandidateSurface, truncate(f.Why, 120))
case FactKindFieldRationale:
    fmt.Fprintf(b, "- field_rationale | topic=%s | %s | %s\n",
        f.Topic, f.FieldPath, truncate(f.Why, 120))
```

**Replace with**:

```go
case FactKindPorterChange:
    // Why is bounded by recording (typically 5-10 facts/codebase, 250-500 chars
    // each); pass through verbatim — Tranche 0 closure of run-17 R-17-C9.
    fmt.Fprintf(b, "- porter_change | topic=%s | class=%s | surface=%s | %s\n",
        f.Topic, f.CandidateClass, f.CandidateSurface, f.Why)
case FactKindFieldRationale:
    // Same rationale — bounded count, pass through verbatim.
    fmt.Fprintf(b, "- field_rationale | topic=%s | %s | %s\n",
        f.Topic, f.FieldPath, f.Why)
```

Tier-decision branch (`truncate(f.TierContext, 120)` line 335) and platform-trap default (`truncate(f.Symptom, 100)`) stay as-is; tier_decision count grows with cross-tier diffs and platform-trap is bounded for a different reason. Shell summary (`truncate(f.Why, 100)` line 352) stays — shells are auto-generated, often long, and need bounding.

### §4.2 Tests

**File**: `internal/recipe/briefs_content_phase_test.go` (or new `_facts_test.go` if file exceeds 350 LoC)

```
TestWriteFactSummary_PorterChange_FullWhyVerbatim
  — pass FactRecord with 500-char Why
  — assert output contains the entire Why string
  — assert no "…" truncation marker

TestWriteFactSummary_FieldRationale_FullWhyVerbatim
  — same, for FactKindFieldRationale

TestWriteFactSummary_TierDecision_StillTruncates
  — pin the existing 120-char truncation for tier_decision (intentional)

TestWriteFactSummary_FactShellSummary_StillTruncates
  — pin the 100-char truncation for shells (intentional)
```

### §4.3 Verification

```
go test ./internal/recipe/... -run TestWriteFactSummary -count=1
grep -n "truncate(f.Why" internal/recipe/briefs_content_phase.go
# expect: 0 matches in lines 326-336 (porter_change + field_rationale branches)
# expect: 1 match at line ~352 (writeFactShellSummary, intentional)
```

### §4.4 Gate to next tranche

- [ ] All four tests pass.
- [ ] `go vet ./...` clean.
- [ ] No regressions in `go test ./internal/recipe/... -short`.

LoC delta: -3 (the comment lines net out the deleted `truncate(...)` calls).

---

## §5. Tranche 0.5 — Distillation atoms + rubric

**Goal**: hand-author the reference distillation atoms and the content-quality rubric BEFORE Tranche 1 embeds them. Both are content artifacts; quality of distillation gates everything downstream.

This is the load-bearing prep work. The implementer must NOT skip it under time pressure.

### §5.1 New atoms (refinement reference distillation)

Path: `internal/recipe/content/briefs/refinement/`

Each atom is hand-distilled from `/Users/fxck/www/laravel-jetstream-app/`, `/Users/fxck/www/laravel-showcase-app/`, `/Users/fxck/www/recipes/laravel-jetstream/`, `/Users/fxck/www/recipes/laravel-showcase/` AT DESIGN TIME. The atom carries verbatim excerpts + author annotations; the source paths NEVER appear in atom bodies.

```
internal/recipe/content/briefs/refinement/
  reference_kb_shapes.md           — symptom-first vs author-claim
                                     pairs + heuristic that distinguishes
  reference_ig_one_mechanism.md    — H3 fusion fail/pass examples
  reference_voice_patterns.md      — friendly-authority quotes with
                                     surface compatibility
  reference_yaml_comments.md       — tier yaml shapes (showcase
                                     field-restatement vs jetstream
                                     mechanism-first)
  reference_citations.md           — cite-by-name patterns
  reference_trade_offs.md          — two-sided trade-off examples
  refinement_thresholds.md         — the 100%-sure decision rules
                                     encoded as concrete patterns
```

**Each reference atom has the same shape**:

```markdown
# Reference: <topic>

## Why this matters

<2-3 sentences naming the failure mode this distillation fixes>

## Pass examples (drawn from references)

> **Verbatim quote from reference recipe**

**Why this works**: <annotation — the load-bearing teaching the
agent must internalize>

[Repeat for 2-4 examples per atom]

## Fail examples (drawn from run-16 misses)

> **Verbatim quote from run-16 output**

**Why this fails**: <annotation pointing at the specific shape miss>

**Refined to**: <the shape the refinement sub-agent should produce>

[Repeat for 2-4 examples per atom]

## The heuristic

<explicit rule the refinement sub-agent applies. E.g. for
reference_kb_shapes.md:

The stem text between `**...**` must contain at least one of:
- HTTP status code (e.g. `403`, `502`)
- Quoted error string (e.g. `"relation already exists"`)
- Verb-form failure phrase (`fails`, `crashes`, `corrupts`, `silently exits`)
- Observable wrong-state phrase (`empty body`, `null where X expected`)

If none match AND a symptom-first phrasing is derivable from the
fact's Why, refinement Replaces with the symptom-first stem.>

## When to HOLD (refinement does not act)

<explicit non-cases. The 100%-sure threshold lives here.>
```

**Sample distillation for `reference_kb_shapes.md`** (skeleton — implementer fills with actual references):

```markdown
# Reference: KB stem shape

## Why this matters

KB bullets exist for porters who hit a symptom and search for it.
Author-claim stems (`**Library X: setting Y**`) are unsearchable —
the porter doesn't know to search for the recipe's directive. Run-16
shipped 5/15 KB bullets in author-claim shape; the dominant Run-17
quality lift comes from reshaping these.

## Pass examples (drawn from references)

> **No `.env` file** — Zerops injects environment variables as OS
> env vars. Creating a `.env` file with empty values shadows the OS
> vars, causing env() to return null for every key…

**Why this works**: stem names the *thing porters do wrong* + the
*observable wrong state* (env() returns null). The porter searching
"env() returns null on Zerops" finds this.

> **Cache commands in `initCommands`, not `buildCommands`** —
> config:cache bakes absolute paths… The build container runs at
> /build/source/ while the runtime serves from /var/www/. Caching
> during build produces paths like /build/source/storage/... that
> crash at runtime with "directory not found."

**Why this works**: directive-tightly-mapped-to-symptom — the stem
IS the fix, but it names the file ("initCommands", "buildCommands")
the porter is editing AND the body carries the observable error
("directory not found").

## Fail examples (drawn from run-16)

> **TypeORM `synchronize: false` everywhere** — Auto-sync mutates
> the schema on every container start; with two or more containers
> booting in parallel, two simultaneous `ALTER TABLE` calls can
> corrupt the schema…

**Why this fails**: stem is recipe author's directive. Porter who
hit the symptom searches "schema corruption on deploy", "ALTER
TABLE deadlock", "relation already exists" — none of these match
the stem.

**Refined to**: `**ALTER TABLE deadlock under multi-container boot**
— TypeORM \`synchronize: true\` mutates the schema on every container
start; two replicas booting in parallel race the same DDL and the
deploy goes red intermittently. Pin \`synchronize: false\`…`

## The heuristic

The text between `**...**` must contain at least one of:
- HTTP status code matching `\b[1-5]\d{2}\b`
- Quoted error string in backticks or quotes
- Verb-form failure phrase (`fails`, `crashes`, `corrupts`,
  `deadlocks`, `silently exits`, `silently stops`, `returns null`,
  `breaks`, `drops`, `rejects`, `missing`)
- Observable wrong-state phrase (`empty body`, `wrong header`,
  `null where X expected`, `404 on X`)

## When to HOLD

- Stem is already symptom-first ✓ — refinement holds.
- Stem is directive-tightly-mapped AND body carries observable error
  string in first sentence ✓ — refinement holds (showcase reference
  shape).
- Body's facts don't support a symptom-first reshape (no observable
  failure mode named in the porter_change Why) — refinement holds
  AND records a notice for future fact-recording teaching.
```

**LoC per reference atom**: ~150 lines × 7 atoms = **~1,000 lines** of carefully distilled markdown.

**Distillation quality grading**: implementer hand-grades each atom against the actual reference recipe before the atom is checked in. The grade is a private note in the implementation thread, not part of the atom — the atom must read clean for an agent.

### §5.2 New rubric document

**Path**: [`docs/spec-content-quality-rubric.md`](../../spec-content-quality-rubric.md) (new file).

**Purpose**: convert "above golden path" from aspirational to measurable. The refinement sub-agent's brief carries the rubric; post-dogfood ANALYSIS.md grades against the rubric.

**Structure**:

```markdown
# Content quality rubric

The rubric grades each of the seven content surfaces on five
criteria. Each criterion has three hand-scored anchor examples:
7.0 (run-15 floor), 8.5 (reference floor), 9.0 (above golden).

## Criterion 1 — Stem shape (KB, IG)
  - 7.0 anchor: <author-claim stem from run-15>
  - 8.5 anchor: <symptom-first stem from showcase reference>
  - 9.0 anchor: <symptom-first stem with quoted error string + numeric symptom>
  - How to score: <explicit signals>

## Criterion 2 — Voice (Surface 7 zerops.yaml; Surface 3 tier yaml)
  - 7.0 anchor: zero friendly-authority phrasing; engineering-spec voice
  - 8.5 anchor: 1-2 friendly-authority phrasings per surface
  - 9.0 anchor: ≥3 friendly-authority phrasings, each tied to a real
    porter-adapt path
  - How to score: <count "feel free" / "configure this to" / "adapt"
    tokens; cross-check that each ties to an obvious porter-modify path>

## Criterion 3 — Citation prose-level
  - 7.0 anchor: zero inline guide refs across KB
  - 8.5 anchor: ≥50% of KB topics on Citation Map carry inline cite
  - 9.0 anchor: 100% of KB topics on Citation Map cite + cite-by-name
    pattern is natural ("The X guide covers Y; the application-specific
    corollary is …")
  - How to score: <topic match against CitationMap; manual read for naturalness>

## Criterion 4 — Trade-off two-sidedness
  - 7.0 anchor: every KB bullet names only chosen path
  - 8.5 anchor: ≥50% of KB bullets name rejected alternative
  - 9.0 anchor: 100% of KB bullets where a rejected alternative is
    namable do name it
  - How to score: <manual read; rejected alternative test>

## Criterion 5 — Classification × surface routing
  - 7.0 anchor: ≥1 misrouted item per codebase (recipe-preference in IG, etc.)
  - 8.5 anchor: zero misrouted items; every item passes spec
    Classification × surface compatibility table
  - 9.0 anchor: zero misrouted items + every routing decision is
    visibly intentional from facts.jsonl
  - How to score: <iterate every fragment; assert classification matches
    surface per spec table>
```

For each criterion, the rubric has 3 anchor examples drawn from real artifacts (run-15 / references / hand-crafted 9.0 ideal). **Total rubric: ~400 lines**.

**Pinned by**: a new test in `internal/recipe/rubric_test.go`:

```go
TestContentQualityRubric_AnchorsExist(t *testing.T)
  // Read the rubric markdown.
  // Assert 5 criteria sections.
  // Assert each criterion has 7.0 + 8.5 + 9.0 anchor blocks.
  // Assert "How to score" subsection per criterion.
```

### §5.3 Verification

```
ls internal/recipe/content/briefs/refinement/
# expect: 7 atoms

go test ./internal/recipe/... -run TestContentQualityRubric -count=1

# Manual: read each refinement atom; verify pass examples actually appear
# verbatim in the named reference recipe (this is the distillation-quality check)
for atom in internal/recipe/content/briefs/refinement/reference_*.md; do
    echo "=== $atom ==="
    grep -A2 "^> \*\*" "$atom" | head -20
done
```

### §5.4 Gate to next tranche

- [ ] All 7 reference atoms exist with the prescribed sections.
- [ ] Each pass-example quote is verbatim from a named reference (manual cross-check).
- [ ] Each fail-example quote is verbatim from run-16 output (mechanical: `grep` the run-16 fragments).
- [ ] Rubric has 5 criteria × 3 anchors × "How to score" each.
- [ ] `TestContentQualityRubric_AnchorsExist` passes.
- [ ] Implementer reads back the entire distillation atom set in one sitting and grades it ≥8.5 for self-consistency.

LoC delta: +1,000 atom + ~400 rubric + ~30 test = **+1,430 LoC** (all markdown + 1 small Go test).

---

## §6. Tranche 1 — Engine-emit retraction + brief embed

**Goal**: retract Class B/C/per-service engine-emit; embed reference shape teaching directly into the codebase-content brief; thread citation guides; add showcase-tier supplements atom; update deploy-phase decision-recording teaching with worked examples.

This is the upstream closure for R-17-C1/C2/C3/C4/C5/C6/C7. If Tranche 1 alone moves the pre-flight harness to ≥8.0 across surfaces, refinement (Tranche 4) becomes insurance, not the load-bearing fix.

### §6.1 Engine-emit retraction

**File**: [`internal/recipe/engine_emitted_facts.go`](../../../internal/recipe/engine_emitted_facts.go)

**Delete**: lines 33-148 — the comment block at 33-35, `classBFacts` (function header line 36, body extends to line 87), `classCUmbrellaFact` (89-105), `perServiceShells` (108-131), and `managedServicesConsumedBy` (134-148, no longer called once Class B/C/per-service are deleted).

**Keep**: lines 150-292 (`EmittedTierDecisionFacts` and tier helpers — verify exact start line by reading post-deletion).

**Update entry point** at lines 17-32 to no longer call the deleted helpers:

```go
// EmittedFactsForCodebase returns the engine-emitted fact shells
// for a single codebase. Run-17 §5.1 — Class B / C / per-service
// shells are retracted in favour of agent-recorded porter_change
// facts during deploy phases (worked examples in
// briefs/scaffold/decision_recording.md). Tier_decision and Class A
// (IG #1 from committed yaml at assemble.go::injectIGItem1) are the
// only retained engine-emit primitives.
func EmittedFactsForCodebase(plan *Plan, cb Codebase) []FactRecord {
    // No-op: returns empty. Agent-recorded facts for this codebase
    // arrive via FactsLog and are filtered separately.
    return nil
}
```

(Prefer keeping the function signature so the brief composer code at `briefs_content_phase.go:99-105` continues compiling. The "Engine-emitted fact shells" section in the brief renders empty, which is correct.)

**Tests** — update [`engine_emitted_facts_test.go`](../../../internal/recipe/engine_emitted_facts_test.go):

```
DELETE:
  TestClassBFacts_*
  TestClassCUmbrellaFact_*
  TestPerServiceShells_*
  TestEmittedFactsForCodebase_*ClassB*
  TestEmittedFactsForCodebase_*PerService*

KEEP (and re-verify):
  TestEmittedTierDecisionFacts_*

ADD:
  TestEmittedFactsForCodebase_ReturnsEmpty
    — pass any plan + codebase
    — assert []FactRecord{} returned (or nil)
    — pin the run-17 retraction
```

### §6.2 Brief embed: synthesis_workflow.md rewrite

**File**: [`internal/recipe/content/briefs/codebase-content/synthesis_workflow.md`](../../../internal/recipe/content/briefs/codebase-content/synthesis_workflow.md)

Currently 83 lines, mostly procedural. Rewrite to ~250 lines embedding:

1. **Classification × surface compatibility table** (verbatim from spec §349-362)
2. **Friendly-authority voice patterns** (verbatim from spec §305-330) — 4 reference quotes labeled by surface
3. **Citation map + cite-by-name pattern** — pattern + per-recipe guide list (composer threads `citationGuides()` — see §6.4 below)
4. **KB symptom-first fail-vs-pass example pair** — one annotated reference KB bullet from jetstream (symptom-first), one from showcase (directive-tightly-mapped), one annotated FAIL bullet from run-16 (`**TypeORM synchronize: false everywhere**`)
5. **IG one-mechanism-per-H3 worked example** — three reference H3s in sequence

**Important**: every quoted excerpt MUST be verbatim from the actual reference recipe. Implementer cross-checks each quote with `grep` before commit.

**Skeleton**:

```markdown
# Codebase content synthesis workflow

You are the codebase-content sub-agent. Your job is to author the
six surfaces this codebase ships: codebase intro, integration guide
(IG), knowledge base (KB), zerops.yaml block comments. (CLAUDE.md is
authored by a sibling claudemd-author sub-agent — do NOT touch.)

## Read order

1. The recorded facts (codebase scope) above this section.
2. The engine-emitted shells section above this section (Run-17 retracted to empty; section may be missing).
3. `[hostname]/zerops.yaml` on disk.
4. `[hostname]/src/**` for code-grounded references.
5. (If parent != nil) the parent recipe's published surfaces.

## Classification × surface compatibility (BINDING)

The engine refuses incompatible (classification, fragment) pairs at
record-fragment time. Use this table to route every recorded fact:

| Classification | Compatible surfaces | Refused with redirect |
|---|---|---|
| platform-invariant | KB, IG (if porter applies a diff) | CLAUDE.md (→ KB), zerops.yaml comments (→ IG/KB) |
| intersection | KB | All others |
| framework-quirk / library-metadata | none | All — content does not belong on any published surface |
| scaffold-decision (config) | zerops.yaml comments, IG (if porter copies the config) | KB, CLAUDE.md |
| scaffold-decision (code) | IG (with diff) | KB, CLAUDE.md |
| scaffold-decision (recipe-internal) | none | All — discard or move principle to IG |
| operational | CLAUDE.md (NOT YOUR SURFACE — sibling authors) | All others |
| self-inflicted | none | All — discard |

## Friendly-authority voice (Surface 7 + Surface 3)

Both reference recipes speak TO the porter, not AT them. Examples:

> *"Feel free to change this value to your own custom domain, after
> setting up the domain access."* — laravel-jetstream zerops.yaml

> *"Configure this to use real SMTP sinks in true production setups."*
> — laravel-jetstream zerops.yaml

> *"Replace with real SMTP credentials for production use."* —
> laravel-showcase

> *"Disabling the subdomain access is recommended, after you set up
> access through your own domain(s)."* — laravel-jetstream tier-4
> import.yaml

**Pattern**: declarative statement of fact + invitation to adapt.

**Where it applies**:
- zerops.yaml comments — primary site.
- Tier import.yaml comments — secondary site.
- IG prose — sparingly, where a config has multiple valid shapes.

**Where it does NOT apply**:
- KB — gotchas are imperative; "Feel free to" weakens the warning.
- CLAUDE.md — sibling sub-agent's surface.
- Root README — factual catalog.

## Citation map (BINDING for KB and IG)

When a topic appears on the Citation map AND in your KB/IG body, the
body MUST name the guide.

[engine inserts CitationMap + per-recipe citationGuides() here]

**Cite-by-name pattern**:

> *"The `init-commands` guide covers per-deploy key shape and the
> in-script-guard pitfall. The application-specific corollary here is
> two `execOnce` keys (`migrate` + `seed`) so a seed failure doesn't
> burn the migrate key."*

Not "see init-commands"; not "(per init-commands)"; the guide id is
named in prose.

## KB stem shape: fail-vs-pass

**FAIL** (run-16 apidev):

> **TypeORM `synchronize: false` everywhere** — Auto-sync mutates
> the schema on every container start…

The porter who hit this searches for "schema corruption on deploy",
"ALTER TABLE deadlock", "relation already exists", or "two
containers boot at once". None of those match the stem.

**PASS** (laravel-showcase, symptom-first):

> **No `.env` file** — Zerops injects environment variables as OS
> env vars. Creating a `.env` file with empty values shadows the OS
> vars, causing env() to return null for every key…

The stem names the *thing porters do wrong* + the *observable wrong
state* (env() returns null).

**PASS** (laravel-showcase, directive-tightly-mapped-to-symptom):

> **Cache commands in `initCommands`, not `buildCommands`** —
> config:cache bakes absolute paths… The build container runs at
> /build/source/ while the runtime serves from /var/www/. Caching
> during build produces paths like /build/source/storage/… that
> crash at runtime with "directory not found."

The stem is the fix, but the body's first sentence carries the
observable error.

## IG one mechanism per H3

Every H3 covers exactly one platform-forced change. Counter-example:

**FAIL** (run-16 apidev):

```
### 2. Bind 0.0.0.0, trust the proxy, drain on SIGTERM
```

Three independent platform mechanisms (HTTP routability, header
trust, rolling-deploy graceful exit) fused into one H3.

**PASS** (laravel-showcase, three sequential H3s):

```
### 2. Predis over phpredis
### 3. Cache + session in `initCommands`
### 4. Object storage with forcePathStyle
```

## Voice + stems live in the same document. Don't drop one when applying the other.

## Cap reminders

- IG: ≤5 numbered items per codebase.
- KB: 5-8 bullets per codebase.
- zerops.yaml comments: ≤6 lines per block.
- Codebase intro: ≤350 chars.

## What you do NOT author

- CLAUDE.md (sibling sub-agent)
- root/intro, env/<N>/intro, env/<N>/import-comments (env-content sub-agent at phase 6)
```

**LoC**: ~250 lines markdown.

### §6.3 New atom: showcase tier supplements

**File**: `internal/recipe/content/briefs/codebase-content/showcase_tier_supplements.md` (new)

Port from [v2 atom](../../../internal/content/workflows/recipe/briefs/writer/content-surface-contracts.md#L102) lines 102-109:

```markdown
# Showcase tier supplements

Conditionally appended to the codebase-content brief when
`plan.Tier == tierShowcase` AND this codebase is a separate worker
codebase (`cb.IsWorker`).

## Required worker KB gotchas

When the worker runs at tier ≥ small-production, two gotchas MUST
appear in the worker KB:

### Queue-group semantics under multi-replica

The worker has multiple replicas (minContainers ≥ 2). Without a
queue-group on the broker subscription, every replica receives every
message → duplicated work → corrupted state. The stem names the
broker, the term "queue group" (or library equivalent), and
"per-replica" or "exactly-once". The body shows the exact client
option that sets the group.

### Graceful SIGTERM drain

Rolling deploys send SIGTERM. Without explicit drain, in-flight
messages die mid-handler → poison-pill loops or lost work. The stem
names SIGTERM or "drain" or "graceful shutdown". The body carries a
fenced code block showing the catch → drain → exit sequence.

Both items cite the rolling-deploys platform topic.
```

LoC: ~30 lines.

### §6.4 Composer hook: thread citationGuides() into the codebase-content brief

**File**: [`internal/recipe/briefs_content_phase.go`](../../../internal/recipe/briefs_content_phase.go)

`citationGuides()` already exists at [`briefs.go:580-598`](../../../internal/recipe/briefs.go#L580). Plumb its output into `BuildCodebaseContentBrief` after the synthesis_workflow.md atom is loaded:

```go
// After the synthesis_workflow.md atom load (currently lines 48-52):
if atom, err := readAtom("briefs/codebase-content/synthesis_workflow.md"); err == nil {
    b.WriteString(atom)
    b.WriteString("\n\n")
    parts = append(parts, "briefs/codebase-content/synthesis_workflow.md")

    // Thread citation guides — synthesis_workflow.md references
    // the engine-injected list at the Citation Map section.
    // citationGuides() takes no arguments; it reads from CitationMap
    // package-global. See briefs.go:579.
    if guides := citationGuides(); len(guides) > 0 {
        b.WriteString("### Citation guides for this recipe\n\n")
        for _, g := range guides {
            fmt.Fprintf(&b, "- `%s`\n", g)
        }
        b.WriteString("\n")
        parts = append(parts, "citation-guides")
    }
}
```

### §6.5 Composer hook: showcase tier supplements

**File**: [`internal/recipe/briefs_content_phase.go`](../../../internal/recipe/briefs_content_phase.go)

After the synthesis_workflow.md atom + citation guides, add a conditional:

```go
// Showcase tier supplement — worker-only KB mandates.
if plan.Tier == tierShowcase && cb.IsWorker {
    if atom, err := readAtom("briefs/codebase-content/showcase_tier_supplements.md"); err == nil {
        b.WriteString(atom)
        b.WriteString("\n\n")
        parts = append(parts, "briefs/codebase-content/showcase_tier_supplements.md")
    }
}
```

(Verify `tierShowcase` constant + `cb.IsWorker` field — name them as they exist in the current codebase. If `cb.IsWorker` is not a field today, derive from `cb.Role == RoleWorker`.)

### §6.6 Decision-recording atoms — worked examples

**Files**:
- [`internal/recipe/content/briefs/scaffold/decision_recording.md`](../../../internal/recipe/content/briefs/scaffold/decision_recording.md)
- [`internal/recipe/content/briefs/feature/decision_recording.md`](../../../internal/recipe/content/briefs/feature/decision_recording.md)

Append a new section to each: **"Worked examples — what a good porter_change looks like"**.

Each example:
- Real-world platform-forced change shape (Class B from prep §3 — bind 0.0.0.0, SIGTERM drain, per-service connect, own-key-aliases)
- Verbatim porter_change Why drawn from a reference recipe's IG H3 body
- Annotation: "this Why captures the platform mechanism + the observable failure mode + the fix"

Pattern (skeleton):

```markdown
## Worked example: bind 0.0.0.0 + trust proxy

### What you'd see in the codebase

```
const app = await NestFactory.create(AppModule);
app.set('trust proxy', true);
await app.listen(parseInt(process.env.PORT, 10), '0.0.0.0');
```

### How to record it

```
record-fact kind=porter_change
  topic=api-bind-and-trust-proxy
  scope=api/code
  changeKind=code-addition
  classification=platform-invariant
  why=Default Node.js bindings to 127.0.0.1 are unreachable
       from the L7 balancer, which routes to the container's
       VXLAN IP. The X-Forwarded-* headers must be trusted so
       request.ip and request.protocol reflect the real caller,
       not the proxy. Both touches happen at app bootstrap.
  citationGuide=http-support
```

### Why this Why is good

Names the platform mechanism (L7 balancer routes to VXLAN IP), the
observable wrong state (unreachable / wrong request.ip), and the fix
(bind 0.0.0.0 + trust proxy). The codebase-content sub-agent at
phase 5 has everything it needs to author the IG H3 + the citation.
```

5 worked examples per atom × 2 atoms = 10 examples. ~400 LoC across both atoms.

### §6.7 v2 spec edits to mirror in v3

Per §1.5 above:

- `internal/content/workflows/recipe/briefs/writer/content-surface-contracts.md` — port the showcase tier supplements section to the new `internal/recipe/content/briefs/codebase-content/showcase_tier_supplements.md` (already done in §6.3 above).

- `internal/content/workflows/recipe/briefs/editorial-review/single-question-tests.md` — the "outgrow this tier" question deletion. v3 has no editorial-review atom today (run-16 architecture eliminated the editorial-review pass). Confirm the deletion is genuinely a v2-only cleanup (no v3 atom needs the equivalent) by checking that v3's codebase-content + env-content briefs cover the territory; if any v3 atom DOES inherit "outgrow this tier" question phrasing, edit it out.

### §6.8 Tests

```
internal/recipe/engine_emitted_facts_test.go
  TestEmittedFactsForCodebase_ReturnsEmpty
    — Tranche 1 retraction pin
  TestEmittedTierDecisionFacts_StillFires
    — re-verify tier_decision unaffected

internal/recipe/briefs_content_phase_test.go
  TestBuildCodebaseContentBrief_EmbedsClassificationTable
    — assert brief.Body contains "## Classification × surface compatibility"
    — assert all 8 classification rows present
  TestBuildCodebaseContentBrief_EmbedsVoicePatterns
    — assert brief.Body contains "Feel free to change this value"
    — assert brief.Body contains "Configure this to use real SMTP"
  TestBuildCodebaseContentBrief_EmbedsKBStemFailVsPass
    — assert brief.Body contains "TypeORM `synchronize: false` everywhere"
    — assert brief.Body contains "No `.env` file"
  TestBuildCodebaseContentBrief_ThreadsCitationGuides
    — pass plan with 3 citation guides
    — assert brief.Body contains "### Citation guides for this recipe"
    — assert brief.Body contains all 3 guide ids
  TestBuildCodebaseContentBrief_ShowcaseWorkerInjectsSupplement
    — pass plan.Tier=tierShowcase + cb.IsWorker=true
    — assert brief.Body contains "Required worker KB gotchas"
  TestBuildCodebaseContentBrief_NonShowcaseSkipsSupplement
    — pass plan.Tier=tierSmall
    — assert brief.Body does NOT contain "Required worker KB gotchas"

internal/recipe/content_test.go (or atoms_lint_test.go extension)
  TestRefinementAtoms_AllSevenPresent
    — assert 7 atoms exist under briefs/refinement/
  TestRefinementAtoms_PassExamplesVerbatim
    — for each atom, extract `> **...**` lines
    — grep references in laravel-jetstream + laravel-showcase
    — assert at least N quotes are real (not paraphrased)
  TestSynthesisWorkflowAtom_HasReferencedQuotes
    — assert verbatim "Feel free to change this value" present
    — etc.

internal/recipe/decision_recording_atoms_test.go (new)
  TestScaffoldDecisionRecording_HasWorkedExamples
    — assert at least 5 "## Worked example:" blocks
  TestFeatureDecisionRecording_HasWorkedExamples
    — same for feature atom
```

### §6.9 Verification

```
# Engine retraction
go test ./internal/recipe/... -run TestEmittedFacts -count=1

# Brief embed
go test ./internal/recipe/... -run TestBuildCodebaseContentBrief -count=1

# Atom lint (the existing TestAtomReferenceFieldIntegrity et al. should
# stay green — new atoms must follow the existing axis lints)
go test ./internal/recipe/... -run TestAtomAuthoringLint -count=1

# Pre-flight harness, post-Tranche-1 baseline
./zcp-preflight-codebase-content -run docs/zcprecipator3/runs/16 -codebase apidev -out /tmp/preflight-tranche1
diff /tmp/preflight-baseline/brief.md /tmp/preflight-tranche1/brief.md | wc -l
# expect: large diff — new sections added

# Manual: dispatch /tmp/preflight-tranche1/brief.md in a fresh Claude Code
# session against runs/16 facts; capture record-fragment outputs; grade
# against the rubric
```

### §6.10 Gate to next tranche (THIS IS THE LOAD-BEARING GATE)

- [ ] All Tranche 1 tests pass.
- [ ] Pre-flight harness: dispatch the new brief against run-16 frozen facts in a fresh Claude Code session; produce fragments for at least apidev codebase; **hand-grade against the rubric — average ≥8.0 across 5 criteria**, no criterion below 7.5.
- [ ] **If grading falls below the bar**: Tranches 2, 3, 5, 6 (small, mechanical) MAY still ship in parallel — they don't depend on distillation quality. Tranche 4 (refinement) MUST hold until distillation atoms (Tranche 0.5) get a second pass and the pre-flight clears. The refinement sub-agent depends on the same distillation atoms; shipping refinement on bad distillation compounds the failure.

LoC delta: -200 engine + 250 synthesis_workflow + 30 showcase_tier + 400 decision_recording + ~120 tests = **~+600 LoC net**.

---

## §7. Tranche 2 — KB symptom-first record-time refusal

**Goal**: prevent author-claim KB stems from landing in fragments, even if the codebase-content sub-agent's brief teaching slips. Backstop for R-17-C1.

### §7.1 Change

**File**: [`internal/recipe/slot_shape.go`](../../../internal/recipe/slot_shape.go)

Extend `checkCodebaseKB` (line 130-151) with a semantic stem-shape check on each `**...**` opener:

```go
var (
    kbStemHTTPCodeRE       = regexp.MustCompile(`\b[1-5]\d{2}\b`)
    kbStemQuotedErrorRE    = regexp.MustCompile("`[^`]+`|\"[^\"]+\"")
    kbStemFailureVerbRE    = regexp.MustCompile(
        `\b(fails|crashes|corrupts|deadlocks|silently exits|silently stops|returns null|breaks|drops|rejects|missing|hangs|times out|panics)\b`)
    kbStemObservableRE     = regexp.MustCompile(
        `\b(empty body|wrong header|null where|404 on|502 on|empty response|stale data|zero rows|no rows|unbound|undefined)\b`)
    kbStemBoldRE           = regexp.MustCompile(`\*\*([^*]+)\*\*`)
)

func checkCodebaseKB(body string) string {
    bulletCount := 0
    for line := range strings.SplitSeq(body, "\n") {
        trimmed := strings.TrimLeft(line, " \t")
        if !strings.HasPrefix(trimmed, "- ") {
            continue
        }
        bulletCount++
        rest := strings.TrimPrefix(trimmed, "- ")
        if !strings.HasPrefix(rest, "**") {
            return "codebase/<h>/knowledge-base bullets must follow `- **Topic** — 2-4 sentences` shape (no leading `**` found). See spec §Surface 5."
        }
        // Run-17 §7 — symptom-first stem heuristic.
        m := kbStemBoldRE.FindStringSubmatch(rest)
        if len(m) >= 2 {
            stem := m[1]
            if !kbStemMatchesSymptomFirst(stem) {
                return fmt.Sprintf(
                    "codebase/<h>/knowledge-base stem %q is author-claim shape; KB stems are symptom-first or directive-tightly-mapped-to-observable-error. Reshape: name the HTTP status code, quoted error string, failure verb, or observable wrong-state phrase the porter would search for. See refinement atom `briefs/refinement/reference_kb_shapes.md`.",
                    stem)
            }
        }
    }
    if bulletCount > 8 {
        return fmt.Sprintf("codebase/<h>/knowledge-base ≤ 8 bullets; got %d. See spec §Surface 5.", bulletCount)
    }
    return ""
}

func kbStemMatchesSymptomFirst(stem string) bool {
    return kbStemHTTPCodeRE.MatchString(stem) ||
        kbStemQuotedErrorRE.MatchString(stem) ||
        kbStemFailureVerbRE.MatchString(stem) ||
        kbStemObservableRE.MatchString(stem)
}
```

### §7.2 Tests

```
internal/recipe/slot_shape_test.go
  TestCheckCodebaseKB_SymptomFirst_HTTPCode_OK
    — stem "**403 on every cross-origin request**" passes
  TestCheckCodebaseKB_SymptomFirst_QuotedError_OK
    — stem "**`relation already exists` on second container**" passes
  TestCheckCodebaseKB_SymptomFirst_FailureVerb_OK
    — stem "**Subject typo silently stops delivery**" passes
  TestCheckCodebaseKB_SymptomFirst_Observable_OK
    — stem "**Empty body on cross-origin custom headers**" passes
  TestCheckCodebaseKB_AuthorClaim_TypeORM_Refused
    — stem "**TypeORM `synchronize: false` everywhere**" returns refusal
    — refusal message names the spec atom
    — NOTE: this stem does contain a backtick-quoted token; tweak the
      stem regex to NOT match backtick-quoted *config keys*, only
      backtick-quoted *error strings*. Implementation note: distinguish
      via context — if the quoted string is a known yaml/code identifier
      (synchronize, true, false, foo: bar), don't count it as a quoted
      error. Keep this list short and seeded; tune across run-17.
  TestCheckCodebaseKB_AuthorClaim_DecomposeExecOnce_Refused
    — stem "**Decompose execOnce keys into migrate + seed**" returns refusal
  TestCheckCodebaseKB_DirectiveMapped_OK
    — stem "**Cache commands in `initCommands`, not `buildCommands`**"
      with body opening "config:cache bakes absolute paths…" passes
      (the body has the observable; note this requires checking body
      content, not just stem — if too complex, restrict to stem-only
      and accept the false-positive rate)
  TestCheckCodebaseKB_StemAggregateNoOverflow
    — 5 valid + 1 invalid = single refusal naming the offender
```

**Implementation note on the synchronize-false case**: the simplest first cut is stem-only check; if `synchronize: false` matches `kbStemQuotedErrorRE`, the test will pin the false-positive. Two options:

(A) Accept and document the false-positive — the agent reshapes to add a verb-form failure, which is the desired refinement.

(B) Add a config-identifier exclusion list (`synchronize`, `buildCommands`, etc.) — more code but fewer false-positives.

Recommend (A) for run-17; tune to (B) if dogfood evidence shows the false-positive cost is high.

### §7.3 Verification

```
go test ./internal/recipe/... -run TestCheckCodebaseKB -count=1
```

### §7.4 Gate to next tranche

- [ ] All 8 stem-shape tests pass.
- [ ] No regression in existing slot_shape tests.

LoC delta: ~+45.

---

## §8. Tranche 3 — CodebaseGates split (R-16-1 closure)

**Goal**: split `CodebaseGates` so scaffold complete-phase runs only fact-quality gates. Codebase-content sub-agent at phase 5 owns content-surface validation. Closes the load-bearing run-16 finding.

### §8.1 Change

**File**: [`internal/recipe/gates.go`](../../../internal/recipe/gates.go)

Today (line 59-69):

```go
func CodebaseGates() []Gate {
    return []Gate{
        {Name: "codebase-surface-validators", Run: gateCodebaseSurfaceValidators},
        {Name: "source-comment-voice", Run: gateSourceCommentVoice},
        {Name: "cross-surface-duplication", Run: gateCrossSurfaceDuplication},
        {Name: "cross-recipe-duplication", Run: gateCrossRecipeDuplication},
    }
}
```

Replace with two functions:

```go
// CodebaseScaffoldGates runs at scaffold + feature complete-phase.
// Run-17 §8 — content-surface validators are NOT included here; they
// run at codebase-content complete-phase via CodebaseContentGates.
// The scaffold/feature sub-agent records facts only; authoring is
// strictly the codebase-content sub-agent's job. Pinned by R-16-1.
func CodebaseScaffoldGates() []Gate {
    return []Gate{
        {Name: "facts-recorded", Run: gateFactsRecorded},
        {Name: "engine-shells-filled", Run: gateEngineShellsFilled}, // see §8.2
        {Name: "source-comment-voice", Run: gateSourceCommentVoice},
    }
}

// CodebaseContentGates runs at codebase-content complete-phase.
// Owns content-surface validation now that codebase-content is the
// sole content-authoring phase for codebase-scoped surfaces.
func CodebaseContentGates() []Gate {
    return []Gate{
        {Name: "codebase-surface-validators", Run: gateCodebaseSurfaceValidators},
        {Name: "cross-surface-duplication", Run: gateCrossSurfaceDuplication},
        {Name: "cross-recipe-duplication", Run: gateCrossRecipeDuplication},
    }
}

// CodebaseGates is retained for callers that pre-date the split.
// New code calls CodebaseScaffoldGates / CodebaseContentGates
// directly. Deleted in run-18 cleanup.
func CodebaseGates() []Gate {
    out := CodebaseScaffoldGates()
    out = append(out, CodebaseContentGates()...)
    return out
}
```

**File**: every caller of `CodebaseGates()` — find via `grep -rn "CodebaseGates()" .`. Update each to call the appropriate variant per phase. Likely callers:

```
internal/recipe/session.go         — scaffold + feature complete-phase → CodebaseScaffoldGates
                                    codebase-content complete-phase → CodebaseContentGates
```

`gateFactsRecorded` and `gateEngineShellsFilled` may need to be added (verify whether they exist):

```go
// gateFactsRecorded — every codebase has at least one porter_change
// or field_rationale fact in scope. Catches scaffold-skip-to-finalize.
//
// FactsLog.Read() is the verified accessor (facts.go:240). Scope
// format is "<hostname>/<area>" per engine_emitted_facts.go (e.g.
// "api/code"); a small helper extracts the hostname prefix.
func gateFactsRecorded(ctx GateContext) []Violation {
    if ctx.FactsLog == nil || ctx.Plan == nil { return nil }
    records, err := ctx.FactsLog.Read()
    if err != nil { return nil }
    seen := map[string]bool{}
    for _, f := range records {
        if f.Kind == FactKindPorterChange || f.Kind == FactKindFieldRationale {
            if i := strings.IndexByte(f.Scope, '/'); i > 0 {
                seen[f.Scope[:i]] = true
            } else if f.Scope != "" {
                seen[f.Scope] = true
            }
        }
    }
    var vs []Violation
    for _, cb := range ctx.Plan.Codebases {
        if !seen[cb.Hostname] {
            vs = append(vs, notice("codebase-no-facts-recorded", cb.Hostname,
                fmt.Sprintf("codebase/%s recorded no porter_change or field_rationale facts during scaffold/feature; codebase-content sub-agent will have no fact stream to synthesize from", cb.Hostname)))
        }
    }
    return vs
}

// gateEngineShellsFilled — Run-17 retraction means engine no longer
// pre-emits Class B/C shells. Gate becomes vestigial; keep as no-op
// returning nil so existing wiring stays intact. Deleted in run-18.
func gateEngineShellsFilled(ctx GateContext) []Violation {
    return nil
}
```

### §8.2 Atom updates

**File**: [`internal/recipe/content/phase_entry/scaffold.md`](../../../internal/recipe/content/phase_entry/scaffold.md)

Lines 152-160 already say "do NOT author documentation surfaces". Tighten the language now that the gate aligns:

Add after the existing "do NOT author" paragraph:

```markdown
The gate set running at scaffold complete-phase
(`CodebaseScaffoldGates`) checks fact-recording quality only — it
does NOT check IG / KB / CLAUDE.md / zerops.yaml comment fragments.
Recording a fragment to "clear the gate" is wrong: the gate is
already satisfied by your fact-recording work. Fragment authoring
runs at codebase-content phase 5 with a different sub-agent.
```

### §8.3 Tests

```
internal/recipe/gates_test.go
  TestCodebaseScaffoldGates_OnlyFactQuality
    — assert returned gate set names: facts-recorded, engine-shells-filled, source-comment-voice
    — assert "codebase-surface-validators" NOT present
  TestCodebaseContentGates_OwnsContentSurfaces
    — assert codebase-surface-validators present
    — assert cross-surface + cross-recipe duplication present
  TestCodebaseScaffoldGates_FactsRecorded_AllCodebasesPass_NoViolation
  TestCodebaseScaffoldGates_FactsRecorded_MissingCodebase_RaisesNotice
  TestCodebaseGates_BackCompat_UnionsBoth
    — pin the back-compat helper
```

### §8.4 Verification

```
go test ./internal/recipe/... -run TestCodebaseScaffoldGates -count=1
go test ./internal/recipe/... -run TestCodebaseContentGates -count=1
go test ./internal/recipe/... -run TestCodebaseGates_BackCompat -count=1

# Sanity: scaffold sub-agent's brief no longer references content-authoring
grep -n "codebase-surface-validators\|integration-guide\|knowledge-base" \
    internal/recipe/content/phase_entry/scaffold.md
# expect: 0 matches in the "do NOT author" body section
```

### §8.5 Gate to next tranche

- [ ] All gate-split tests pass.
- [ ] Scaffold atom updated; lint passes.
- [ ] `CodebaseGates()` back-compat shim tested.

LoC delta: +30 (split + new gates + atom clarification + tests).

---

## §9. Tranche 4 — Refinement sub-agent

**Goal**: ship the post-finalize refinement primitive. The architectural addition that closes voice / stem / classification drift before sign-off. Highest-DoF tranche.

### §9.1 Phase + brief composer

**New**: `internal/recipe/briefs_refinement.go` (peer to `briefs_content_phase.go`).

```go
// BuildRefinementBrief composes the brief for the single refinement
// sub-agent dispatched at phase 8 (post-finalize). The sub-agent
// reads stitched output + reference distillation atoms + facts +
// the rubric; it Replaces fragments where the 100%-sure threshold
// holds.
//
// runDir is the recipe output root on disk. The composer assembles
// the brief from embedded atoms ONLY; runDir is read for
// (a) stitched-output paths the sub-agent will Read at dispatch time
// (the brief lists them) and (b) facts.jsonl path. No
// /Users/fxck/www/... paths leak into the brief — pinned by
// TestNoFilesystemReferenceLeak_RefinementBrief.
//
// Signature mirrors BuildCodebaseContentBrief — parent threaded so
// the sub-agent can read parent's published surfaces and skip
// refinements that would re-author parent material (Q5 resolution).
func BuildRefinementBrief(plan *Plan, parent *ParentRecipe, runDir string, facts []FactRecord) (Brief, error) {
    if plan == nil {
        return Brief{}, errors.New("nil plan")
    }
    parts := []string{}
    var b strings.Builder

    // Phase entry — voice + dispatch shape.
    if atom, err := readAtom("phase_entry/refinement.md"); err == nil {
        b.WriteString(atom)
        b.WriteString("\n\n")
        parts = append(parts, "phase_entry/refinement.md")
    }

    // Synthesis workflow — explicit refinement actions.
    if atom, err := readAtom("briefs/refinement/synthesis_workflow.md"); err == nil {
        b.WriteString(atom)
        b.WriteString("\n\n")
        parts = append(parts, "briefs/refinement/synthesis_workflow.md")
    }

    // The 7 reference distillation atoms.
    referenceAtoms := []string{
        "briefs/refinement/reference_kb_shapes.md",
        "briefs/refinement/reference_ig_one_mechanism.md",
        "briefs/refinement/reference_voice_patterns.md",
        "briefs/refinement/reference_yaml_comments.md",
        "briefs/refinement/reference_citations.md",
        "briefs/refinement/reference_trade_offs.md",
        "briefs/refinement/refinement_thresholds.md",
    }
    for _, p := range referenceAtoms {
        atom, err := readAtom(p)
        if err != nil {
            return Brief{}, fmt.Errorf("refinement reference atom %s: %w", p, err)
        }
        b.WriteString(atom)
        b.WriteString("\n\n")
        parts = append(parts, p)
    }

    // Quality rubric — embedded inline rather than via Read.
    if rubric, err := readAtom("briefs/refinement/embedded_rubric.md"); err == nil {
        b.WriteString(rubric)
        b.WriteString("\n\n")
        parts = append(parts, "briefs/refinement/embedded_rubric.md")
    }

    // Per-recipe context: pointer block to stitched output on disk.
    // Tier directories use Tier.Folder (e.g. "0 — AI Agent",
    // "4 — Small Production") — verified at internal/recipe/tiers.go:17.
    b.WriteString("## Stitched output to refine\n\n")
    b.WriteString("Read each path in order; refine fragments where the 100%-sure threshold holds.\n\n")
    fmt.Fprintf(&b, "1. `%s/README.md` — root README\n", runDir)
    for _, t := range Tiers() {
        fmt.Fprintf(&b, "2. `%s/environments/%s/README.md` + `import.yaml`\n", runDir, t.Folder)
    }
    for _, cb := range plan.Codebases {
        fmt.Fprintf(&b, "3. `%s/%s/README.md` + `zerops.yaml` + `CLAUDE.md`\n", runDir, cb.Hostname)
    }
    if parent != nil && parent.SourceRoot != "" {
        fmt.Fprintf(&b, "4. `%s/...` — parent recipe (`%s`). Refinement HOLDS on any fragment whose body would re-author parent material.\n", parent.SourceRoot, parent.Slug)
    }
    parts = append(parts, "stitched-output-pointer-block")

    // Facts log — full snapshot, no truncation.
    b.WriteString("\n## Recorded facts (run-wide)\n\n")
    for _, f := range facts {
        writeFactSummary(&b, f)
    }
    parts = append(parts, "filtered-facts")

    return Brief{
        Kind:  BriefRefinement,
        Body:  b.String(),
        Bytes: b.Len(),
        Parts: parts,
    }, nil
}
```

(`BriefRefinement` constant added to existing `BriefKind` enum.)

### §9.2 Phase entry atom

**New**: `internal/recipe/content/phase_entry/refinement.md`

```markdown
# Refinement phase

You are the refinement sub-agent. The recipe has finished phase 7
(finalize stitch + validate). Every fragment is structurally valid;
every cap is satisfied; every classification routing is internally
consistent.

Your job: read the entire stitched output and refine where the
100%-sure threshold holds. Below the threshold, you do not act.

## What you can do

- Replace fragment bodies via `record-fragment mode=replace`.
- Update fact bodies via `replace-by-topic`.
- Read any file under the run output directory.
- Call `zerops_knowledge` for citation lookups.

## What you cannot do

- Author NEW content (no new IG items, no new KB bullets except the
  showcase tier supplement explicit case in the workerdev KB).
- Change a fragment's surface (keep the same fragment id).
- Change a fragment's classification.
- Loop on refusal: per-fragment edit cap is 1 attempt.

## How you make decisions

You apply the rubric (5 criteria × 3 anchors each). For every
fragment:

1. Read fragment body.
2. Score against each rubric criterion.
3. If a criterion lands below 8.5 AND the fix is unambiguous from the
   reference distillation atoms, refine.
4. If a criterion lands below 8.5 but the fix requires judgment
   (multiple reasonable refinements), HOLD.
5. If a criterion lands ≥8.5, HOLD.

## The 100%-sure threshold

If you would hesitate to argue this change in a code review, you are
not 100% sure. Hold.

## Output

A series of `record-fragment mode=replace` and `replace-by-topic`
calls. End with `complete-phase phase=refinement`.
```

### §9.3 Synthesis workflow atom

**New**: `internal/recipe/content/briefs/refinement/synthesis_workflow.md`

Maps the 8 refinement types from prep §6.2 table to specific actions, citing the reference distillation atoms. ~150 LoC.

### §9.4 Embedded rubric atom

**New**: `internal/recipe/content/briefs/refinement/embedded_rubric.md`

Verbatim copy of `docs/spec-content-quality-rubric.md` (Tranche 0.5) — embedded so the sub-agent reads it inline rather than via Read. ~400 LoC.

**Sync mechanism**: a `go:generate` directive in `internal/recipe/content/briefs/refinement/embedded_rubric.go` (new tiny stub file) drives a small generator under `tools/sync_rubric/main.go` that copies `docs/spec-content-quality-rubric.md` → `internal/recipe/content/briefs/refinement/embedded_rubric.md` byte-for-byte. The generator runs in CI via `make lint-local` (extend the Makefile target). A test pins drift:

```go
// File: internal/recipe/embedded_rubric_test.go
func TestEmbeddedRubric_MatchesSpec(t *testing.T) {
    rubric, err := readAtom("briefs/refinement/embedded_rubric.md")
    if err != nil { t.Fatalf("read embedded rubric: %v", err) }
    // Spec lives at repo root; tests run from package dir, so go up.
    spec, err := os.ReadFile(filepath.Join("..", "..", "docs", "spec-content-quality-rubric.md"))
    if err != nil { t.Fatalf("read spec: %v", err) }
    if rubric != string(spec) {
        t.Errorf("embedded rubric drifted from spec; run `go generate ./internal/recipe/content/briefs/refinement/...`")
    }
}
```

### §9.5 Snapshot/restore primitive

**File**: [`internal/recipe/snapshot.go`](../../../internal/recipe/snapshot.go) (new). Session struct lives at [`internal/recipe/workflow.go:49`](../../../internal/recipe/workflow.go#L49); fragments are stored on `Plan.Fragments map[string]string` (verified at [`plan.go:26`](../../../internal/recipe/plan.go#L26) and [`handlers_fragments.go:52-66`](../../../internal/recipe/handlers_fragments.go#L52)). Session uses `sync.Mutex` (not RWMutex).

```go
// SnapshotFragment returns the current body of a fragment-id, or
// "" if not recorded yet. Used by refinement to preserve original
// content before Replace, so post-Replace validators can revert
// when the refinement degrades quality.
func (s *Session) SnapshotFragment(id string) string {
    s.mu.Lock()
    defer s.mu.Unlock()
    if s.Plan == nil || s.Plan.Fragments == nil {
        return ""
    }
    return s.Plan.Fragments[id]
}

// RestoreFragment writes body back as the fragment's recorded body,
// bypassing slot_shape + classification refusal. Used only by the
// refinement validator-revert path. Pinned by
// TestRestoreFragment_BypassesValidators.
func (s *Session) RestoreFragment(id, body string) {
    s.mu.Lock()
    defer s.mu.Unlock()
    if s.Plan == nil {
        return
    }
    if s.Plan.Fragments == nil {
        s.Plan.Fragments = map[string]string{}
    }
    s.Plan.Fragments[id] = body
}
```

Wire into `handleRecordFragment` ([`handlers.go:400`](../../../internal/recipe/handlers.go#L400)) so that on refinement-phase Replace (`in.Phase == "refinement" && in.Mode == "replace"`):

1. Snapshot prior body.
2. Apply Replace.
3. Run codebase-content validators.
4. If new violations surface, RestoreFragment + log notice.

Behavior gated on `RecipeInput.Phase == "refinement"` (verified `RecipeInput.Phase` field exists at [`handlers.go:111`](../../../internal/recipe/handlers.go#L111); jsonschema doc string needs updating to add `refinement` to the Phase enum) — only refinement-phase Replace gets snapshot/restore treatment.

### §9.6 Filesystem-leak pin

**File**: `internal/recipe/briefs_refinement_test.go` (new)

```go
TestNoFilesystemReferenceLeak_RefinementBrief(t *testing.T)
  // Build the brief for any plan + runDir.
  brief, _ := BuildRefinementBrief(plan, "/some/path", facts)
  forbiddenPrefixes := []string{
      "/Users/",
      "/home/",
      "/var/www/laravel-",
      "/var/www/recipes/",
  }
  for _, p := range forbiddenPrefixes {
      if strings.Contains(brief.Body, p) {
          t.Errorf("brief leaks filesystem path %q", p)
      }
  }

TestRefinementBrief_OnlyEmbeddedAtoms(t *testing.T)
  brief, _ := BuildRefinementBrief(plan, "/run/dir", facts)
  // Every Parts entry must resolve via readAtom.
  for _, p := range brief.Parts {
      if strings.HasPrefix(p, "stitched-output") || strings.HasPrefix(p, "filtered-facts") {
          continue
      }
      _, err := readAtom(p)
      if err != nil {
          t.Errorf("brief part %q is not an embedded atom: %v", p, err)
      }
  }
```

### §9.7 Adversarial threshold tests

**File**: `internal/recipe/refinement_threshold_test.go` (new)

8-12 synthetic test cases stretching the 100%-sure threshold. Each test:

- Constructs a fragment body
- Calls a `decideRefinement(body, atom_corpus)` helper (extracted from synthesis_workflow.md logic so the heuristic can be unit-tested)
- Asserts ACT or HOLD per case

```
Cases that should ACT:
  TestDecideRefinement_KBStem_AuthorClaim_TypeORM_Acts
  TestDecideRefinement_IG_FusedThreeMechanisms_Acts
  TestDecideRefinement_VoiceAbsent_TierYaml_Acts
  TestDecideRefinement_RecipePreference_InIG_RoutesToYamlComment_Acts
  TestDecideRefinement_TradeOffOneSided_Acts
  TestDecideRefinement_ShowcaseWorkerMissingQueueGroup_AddsKB_Acts

Cases that should HOLD:
  TestDecideRefinement_KBStem_Symptom_NoChange_Holds
  TestDecideRefinement_IG_OneMechanismPerH3_Holds
  TestDecideRefinement_VoicePresent_Holds
  TestDecideRefinement_RoutingCorrect_Holds
  TestDecideRefinement_TradeOffTwoSided_Holds
  TestDecideRefinement_AmbiguousReshape_NoUnambiguousFix_Holds
```

This unit-tests the heuristic without dispatching a sub-agent.

### §9.8 Dispatch wiring

**File**: `internal/recipe/session.go` (or wherever phase advancement is wired)

After `complete-phase phase=finalize` succeeds, add a phase=refinement step. Single sub-agent dispatch, single message. After complete-phase phase=refinement, the run signs off.

Phase enum extension. Existing constants (verified via test references in `handlers_test.go`, `phase_entry.go`, `workflow_test.go`): `PhaseResearch`, `PhaseProvision`, `PhaseScaffold`, `PhaseFeature`, `PhaseCodebaseContent`, `PhaseEnvContent`, `PhaseFinalize`. Add:

```go
const (
    // ... existing constants unchanged ...
    PhaseRefinement = Phase("refinement")  // Run-17 §9
)
```

Add to `phaseIndex` ([`workflow.go`](../../../internal/recipe/workflow.go), search for the `phases` slice — phaseIndex is invoked at line ~95 in EnterPhase) so the adjacent-forward transition from `PhaseFinalize` → `PhaseRefinement` succeeds. Update `gatesForPhase` ([`phase_entry.go`](../../../internal/recipe/phase_entry.go)) to dispatch the refinement-specific gate set (none today; refinement has its own validator pipeline post-Replace).

Update `RecipeInput.Phase` jsonschema doc string at [`handlers.go:111`](../../../internal/recipe/handlers.go#L111) to add `refinement` to the enumerated values.

### §9.9 Tests

In addition to threshold tests + filesystem-leak tests above:

```
internal/recipe/briefs_refinement_test.go
  TestBuildRefinementBrief_AssemblesAllAtoms
    — assert all 7 reference atoms present in brief.Parts
    — assert phase_entry/refinement.md present
    — assert briefs/refinement/synthesis_workflow.md present
  TestBuildRefinementBrief_EmbedsRubric
    — assert "## Criterion 1 — Stem shape" present
  TestBuildRefinementBrief_ListsStitchedPaths
    — pass plan with 3 codebases, 6 environments
    — assert 3 codebase paths + 6 env paths in pointer block
  TestBuildRefinementBrief_FactsLogPresent
    — pass 5 facts, assert 5 fact lines in brief

internal/recipe/snapshot_test.go
  TestSnapshotFragment_EmptyWhenAbsent
  TestSnapshotFragment_ReturnsRecordedBody
  TestRestoreFragment_BypassesValidators
  TestRefinementReplace_ValidatorsViolate_FragmentReverts
  TestRefinementReplace_ValidatorsPass_FragmentChanged
```

### §9.10 Verification

```
go test ./internal/recipe/... -run TestBuildRefinementBrief -count=1
go test ./internal/recipe/... -run TestDecideRefinement -count=1
go test ./internal/recipe/... -run TestNoFilesystemReferenceLeak -count=1
go test ./internal/recipe/... -run TestSnapshotFragment -count=1
go test ./internal/recipe/... -run TestRestoreFragment -count=1
go test ./internal/recipe/... -run TestEmbeddedRubric -count=1
```

### §9.11 Gate to next tranche

- [ ] All Tranche 4 tests pass (~20 new tests).
- [ ] Pre-flight refinement harness: dispatch refinement on Tranche-1-output frozen fragments. Hand-grade 20 randomly-selected refinement attempts. **Refinement-correct rate ≥60%**.
- [ ] If <60%: distillation atoms get a second pass, threshold heuristic re-tuned. Tranche 4 holds at HEAD until pre-flight clears.

LoC delta: ~+200 composer + 250 atoms + 30 snapshot + 80 threshold tests + 60 leak tests + 80 brief tests = **~+700 LoC**.

---

## §10. Tranche 5 — Slot-shape refusal aggregation

**Goal**: aggregate all offenders in a single fragment scan into one refusal response. Closes R-17-C10 efficiency cost.

### §10.1 Change

**File**: [`internal/recipe/slot_shape.go`](../../../internal/recipe/slot_shape.go)

Today `checkSlotShape` returns the first violation as a single string. Switch to returning all violations:

```go
// checkSlotShape now returns a slice of violation messages. An empty
// slice means the body passes; a non-empty slice carries every
// detected offender so the agent can re-author against the full
// list in one round-trip. Run-17 §10 — aggregation closure for
// R-17-C10 (run-16 evidence: scaffold-api hit 8 successive
// CLAUDE.md refusals naming one hostname each).
func checkSlotShape(fragmentID, body string) []string {
    switch {
    case fragmentID == fragmentIDRoot:
        return single(checkRootIntro(body))
    case envIntroRe.MatchString(fragmentID):
        return single(checkEnvIntro(body))
    case envImportCommentsRe.MatchString(fragmentID):
        return single(checkEnvImportComments(body))
    case codebaseIntroRe.MatchString(fragmentID):
        return single(checkCodebaseIntro(body))
    case slottedIGRe.MatchString(fragmentID):
        return single(checkSlottedIG(body))
    case codebaseKBRe.MatchString(fragmentID):
        return checkCodebaseKBAggregate(body) // returns []string
    case zeropsYamlCommentsRe.MatchString(fragmentID):
        return single(checkZeropsYamlComments(body))
    case singleSlotClaudeMDRe.MatchString(fragmentID):
        return checkClaudeMDAggregate(body)   // returns []string
    }
    return nil
}

func single(s string) []string {
    if s == "" {
        return nil
    }
    return []string{s}
}
```

`checkCodebaseKBAggregate` walks every bullet collecting refusals; returns all in order. Same for `checkClaudeMDAggregate` (the run-16 8-refusal cluster lived here).

**File**: [`internal/recipe/handlers.go`](../../../internal/recipe/handlers.go) line ~425-430

```go
// Run-17 §10 — slot-shape refusal aggregation.
if violations := checkSlotShape(in.FragmentID, in.Fragment); len(violations) > 0 {
    if len(violations) == 1 {
        r.Error = "record-fragment: " + violations[0]
    } else {
        r.Error = "record-fragment: " + strconv.Itoa(len(violations)) + " offenders\n" +
            "  - " + strings.Join(violations, "\n  - ")
    }
    return r
}
```

### §10.2 Tests

```
internal/recipe/slot_shape_test.go
  TestCheckSlotShape_AggregatesAllOffenders_KBMultipleAuthorClaim
    — body with 3 author-claim stems
    — assert 3 violations returned
  TestCheckSlotShape_AggregatesAllOffenders_ClaudeMDMultipleZeropsLeaks
    — body with 4 zerops-tool leaks
    — assert 4 violations returned
  TestCheckSlotShape_SingleOffender_StillSingleViolation
    — back-compat: 1 offender → 1 violation
  TestCheckSlotShape_NoOffender_EmptySlice
    — back-compat: clean body → empty slice / nil
```

### §10.3 Verification

```
go test ./internal/recipe/... -run TestCheckSlotShape_Aggregates -count=1
```

### §10.4 Gate to next tranche

- [ ] All aggregation tests pass.
- [ ] Existing slot-shape callers compile (return type change ripples).

LoC delta: ~+30.

---

## §11. Tranche 6 — deployFiles narrowness validator

**Goal**: catch the `src/scripts` shipped to prod-deploy where `dist/scripts/migrate.js` runs at runtime — the R-17-C8 path mismatch.

### §11.1 Change

**File**: [`internal/recipe/validators_codebase.go`](../../../internal/recipe/validators_codebase.go)

New validator: every prod `deployFiles` entry must be referenced by a `run.start` / `run.initCommands` / `build.deployFiles` field, OR carry a `field_rationale` fact explaining its presence.

**Implementation note**: the codebase uses `gopkg.in/yaml.v3` directly via `yaml.Unmarshal` (see [`validators_import_yaml.go:88`](../../../internal/recipe/validators_import_yaml.go#L88)). No `parseZeropsYaml` helper exists; the implementer either adds one or inlines the unmarshal. Follow the existing validator pattern in `validators_import_yaml.go` for consistency.

Algorithm (the implementer translates to the actual zerops.yaml AST shape — `setup.build.deployFiles`, `setup.run.{start,initCommands,envVariables}` per the [zerops.yaml schema](https://api.app-prg1.zerops.io/api/rest/public/settings/zerops-yml-json-schema.json)):

```go
// validateDeployFilesNarrowness — Run-17 §11. Each deployFiles
// entry in a prod setup must be reachable from runtime config
// (run.start, run.initCommands, run.envVariables) or be backed by
// a field_rationale fact justifying its presence. Unreferenced
// entries are dead weight (never read at runtime) or path mismatches
// (referenced from a different relative path than ships).
//
// Closes R-17-C8 (run-16 apidev shipped src/scripts while initCommands
// invoked dist/scripts/migrate.js — either dead weight or implicit
// ts-node fallback).
func validateDeployFilesNarrowness(_ context.Context, path string, body []byte, inputs SurfaceInputs) ([]Violation, error) {
    // 1. yaml.Unmarshal body into the zerops.yaml AST type used
    //    elsewhere in this package (mirror validators_import_yaml.go).
    // 2. Iterate each setup; skip dev setups.
    // 3. For each deployFiles entry in a prod setup:
    //    a. Substring-search the entry across run.start +
    //       run.initCommands joined + run.envVariables values; if
    //       found, continue.
    //    b. Iterate inputs.FactsLog records; if any
    //       FactKindFieldRationale carries the entry path in its
    //       FieldPath or Why, continue.
    //    c. Otherwise record a violation.
    //
    // The substring check is sufficient — deployFiles entries are
    // project-relative and don't normalize-compare across forms.
    var vs []Violation
    // ... (concrete unmarshal + iteration follows the existing
    //      validators_import_yaml.go shape; ~40 LoC inline)
    return vs, nil
}
```

The fact-side helper accesses `FactsLog.Read()` (verified at [`facts.go:240`](../../../internal/recipe/facts.go#L240)), not a `Records()` method:

```go
func hasFieldRationale(facts *FactsLog, entry string) bool {
    if facts == nil { return false }
    records, err := facts.Read()
    if err != nil { return false }
    for _, f := range records {
        if f.Kind == FactKindFieldRationale &&
            (strings.Contains(f.FieldPath, entry) || strings.Contains(f.Why, entry)) {
            return true
        }
    }
    return false
}
```

Register in `CodebaseContentGates` (Tranche 3):

```go
func CodebaseContentGates() []Gate {
    return []Gate{
        {Name: "codebase-surface-validators", Run: gateCodebaseSurfaceValidators},
        {Name: "deploy-files-narrowness", Run: gateDeployFilesNarrowness}, // §11
        {Name: "cross-surface-duplication", Run: gateCrossSurfaceDuplication},
        {Name: "cross-recipe-duplication", Run: gateCrossRecipeDuplication},
    }
}
```

### §11.2 Tests

```
internal/recipe/validators_codebase_test.go
  TestValidateDeployFilesNarrowness_Referenced_NoViolation
    — deployFiles: [dist], run.start: "node dist/main.js" → 0 violations
  TestValidateDeployFilesNarrowness_Unreferenced_Violation
    — deployFiles: [src/scripts], run.start: "node dist/main.js",
      no field_rationale → 1 violation
  TestValidateDeployFilesNarrowness_FieldRationale_NoViolation
    — deployFiles: [src/scripts], field_rationale fact mentions "src/scripts"
      → 0 violations
  TestValidateDeployFilesNarrowness_DevSetup_Skipped
    — deployFiles: [.] in dev setup → 0 violations (dev not gated)
```

### §11.3 Verification

```
go test ./internal/recipe/... -run TestValidateDeployFilesNarrowness -count=1
```

### §11.4 Gate to next tranche

- [ ] All 4 validator tests pass.
- [ ] Run-16 frozen apidev/zerops.yaml triggers the violation when fed to the validator (regression case).

LoC delta: ~+40.

---

## §12. Tranche 7 — v2 atom tree deletion

**Goal**: delete the parallel-live v2 atom tree + recipe engine. Eliminates dual-tree maintenance friction. ~-2,500 LoC net.

### §12.1 Decision: repoint or delete the analyzer pipeline

`internal/analyze/runner.go::AtomRootDefault = "internal/content/workflows/recipe/briefs"` is the only direct v2 import in the analyzer pipeline. Two paths:

**Path A — Repoint at v3**: `AtomRootDefault = "internal/recipe/content/briefs"`. Analyzer continues working against the v3 tree. `structural.go::CheckAtomTemplateVarsBound` ports cleanly.

**Path B — Delete the analyzer**: if v3 has its own equivalent lints (`internal/recipe/atoms_lint.go` exists per CLAUDE.md), delete the analyzer pipeline entirely.

**Recommendation**: Path A. The analyzer's `agent_summary.go` / `dispatch_integrity.go` etc. are post-run analysis tools, not v2-recipe-specific. Repointing keeps run-17 dogfood analysis tooling alive.

If Path B chosen, `internal/analyze/` is fully deleted; check for non-test consumers via `grep -rn "internal/analyze" --include="*.go"`.

### §12.2 Surgery surface

Delete:

```
internal/content/workflows/recipe/                  — full subtree (155+ atoms)
internal/content/workflows/recipe.md                — monolith
internal/content/workflows/recipe-template.md       — if exists
internal/workflow/recipe_*.go                        — v2 engine (every file with recipe_ prefix)
internal/workflow/atom_manifest*.go                  — atom_manifest.go + atom_manifest_briefs.go + atom_manifest_phases.go + atom_manifest_principles.go
internal/workflow/atom_stitcher.go
internal/workflow/recipe_templates_test.go
```

Update:

```
internal/content/content.go     — drop v2 paths from embed.FS
internal/server/                — remove zerops_workflow tool registration
tools/lint/recipe_atom_lint.go  — repoint atomRoot OR delete
tools/lint/atom_template_vars/main.go  — repoint atomRoot
internal/analyze/runner.go      — Path A or Path B (above)
```

Test cleanup:

```
grep -rln "workflow.RecipeEngine\|workflow.AtomManifest\|workflow.AtomStitcher" \
    --include="*.go"
# every match needs the test deleted (v2-specific) or rewritten (rare)
```

### §12.3 Step-by-step (sequence matters)

1. **Verify run-17 dogfood quality bar** (Tranche 8 sign-off green). Don't delete v2 until v3 has demonstrably shipped a recipe at the quality bar.
2. **Remove `zerops_workflow` from MCP server registration**. Verify no callers in CI / docs.
3. **Delete v2 engine code**: `internal/workflow/recipe_*.go`, `atom_manifest*.go`, `atom_stitcher.go`. Run `go build ./...` after each delete; fix compile errors by deleting downstream consumers.
4. **Delete v2 atom tree**: `internal/content/workflows/recipe/`. Update `embed.FS` declaration in `content.go` to drop the path.
5. **Repoint analyzer**: `internal/analyze/runner.go::AtomRootDefault` → v3 path. Run `go test ./internal/analyze/...`.
6. **Repoint lint tools**: `tools/lint/recipe_atom_lint.go` + `tools/lint/atom_template_vars/main.go`. Run `make lint-local`.
7. **Run full test suite**: `go test ./... -short`. Expect all green.
8. **Run linter**: `make lint-local`. Expect all green.
9. **Run build**: `go build ./...` on all platforms (the binary embeds atoms).

### §12.4 Tests

This tranche removes tests, doesn't add them. Add one regression pin:

```
internal/recipe/v2_deletion_pin_test.go (new, deletable later)
  TestNoV2AtomTreeReferences
    matches, _ := exec.Command("grep", "-rln",
        "internal/content/workflows/recipe", "--include=*.go").Output()
    if len(matches) > 0 {
        t.Errorf("v2 atom tree references survived: %s", matches)
    }
  TestZeropsWorkflowToolRemoved
    — assert internal/server/ does NOT register zerops_workflow
```

### §12.5 Verification

```
go test ./... -short
go build ./...
make lint-local

# Sanity: v2 paths gone
grep -rln "internal/content/workflows" --include="*.go" .
# expect: 0 matches

# Sanity: zerops_workflow not registered
grep -rn "zerops_workflow" internal/server/ --include="*.go"
# expect: 0 matches OR only in deletion-pin tests

# CHANGELOG bump
cat CHANGELOG.md | head -20
# expect: Run-17 entry naming v2 deprecation
```

### §12.6 Gate to next tranche

- [ ] All tests + lint + build green post-deletion.
- [ ] CHANGELOG entry filed.
- [ ] Migration note for any external `zerops_workflow` consumers (none expected; document in CHANGELOG anyway).

LoC delta: **~-2,500**. Single biggest reduction in run-17.

---

## §13. Tranche 8 — Sign-off

**Goal**: declare run-17 done. Archive plan docs. Open run-18 readiness.

### §13.1 Run-17 dogfood

**Pre-condition**: Tranches -1 through 7 all green.

**Dogfood candidates** (in this order):

1. **Small-shape calibration recipe**: `analog-static-hello-world` or similar single-codebase, no-managed-services recipe. Calibrates refinement sub-agent on simple output. Target: ≥8.5 across all surfaces.
2. **nestjs-showcase** (run-16's recipe): direct comparison to run-16 baseline. Target: ≥8.5 across all surfaces with refinement actually moving the needle on R-17-C1/C2/C3/C4/C5/C6/C7/C8.

Each dogfood saves output to `docs/zcprecipator3/runs/17a/` (small-shape) and `docs/zcprecipator3/runs/17b/` (showcase-shape). ANALYSIS.md + CONTENT_COMPARISON.md + TIMELINE.md per run, matching run-16 shape.

### §13.2 ANALYSIS.md template

For each dogfood:

```markdown
# Run-17[a/b] analysis

## Honest grade

- Run-15 honest: 7.5
- Run-16 honest: 8.0
- Run-17[a/b] honest: <X.X>
- Lift vs run-16: <±X.X>

## Per-surface grade (rubric criteria)

| Surface | Stem | Voice | Citation | Trade-offs | Routing | Aggregate |
|---|---:|---:|---:|---:|---:|---:|
| Root README | … | n/a | n/a | n/a | … | … |
| Tier README extracts | … | … | n/a | n/a | … | … |
| Tier import.yaml | n/a | … | n/a | … | … | … |
| Codebase intro | … | … | n/a | n/a | … | … |
| Codebase IG | … | … | … | … | … | … |
| Codebase KB | … | n/a | … | … | … | … |
| zerops.yaml comments | n/a | … | n/a | n/a | … | … |
| CLAUDE.md | … | n/a | … | n/a | … | … |

## Tranche closure status

- R-16-1 (load-bearing run-16 finding): … (PASS / PARTIAL / FAIL)
- R-17-C1 (KB stem): … 
- R-17-C2 (IG fusion): …
- R-17-C3 (recipe-preference in IG): …
- R-17-C4 (voice): …
- R-17-C5 (citation): …
- R-17-C6 (trade-offs): …
- R-17-C7 (showcase supplements): …
- R-17-C8 (deployFiles): …
- R-17-C9 (Why truncation): … (mechanical — should always PASS)
- R-17-C10 (refusal aggregation): … (mechanical — should always PASS)

## Refinement-phase audit

- Refinement actions taken: <count>
- Refinement-correct (manual grade): <count>
- Refinement-revert via snapshot: <count>
- Refinement-correct rate: <%>

## Anomalies

<list>

## Run-18 readiness signals

<list>
```

### §13.3 Release notes

The repo has **no `CHANGELOG.md`** (verified `ls CHANGELOG* docs/CHANGELOG*` returns no matches as of 2026-04-28). Releases ship as git tags (`v9.5.x`, `v9.6.x`, …, `v9.9.0` is the latest tag at the time of writing). Run-17 follows the existing convention — release notes go in the GitHub release body, not a CHANGELOG file.

**Action**: at run-17 ship, the implementer (or release engineer) creates a `gh release create v9.X` with the body below. Optionally, run-17 can introduce `CHANGELOG.md` as a new convention — if so, also add a `TestChangelogPresent` pin and document the format in CLAUDE.md. Default recommendation: stay with the existing tag-only mechanism unless the user explicitly asks for CHANGELOG.md.

**Release notes body** (passed to `gh release create v9.X --notes-file release-v9.X.md`):

```markdown
## v9.X — run-17 ship

### Added
- Refinement sub-agent at phase 8 (post-finalize quality refinement)
- Content quality rubric (`docs/spec-content-quality-rubric.md`)
- 7 reference distillation atoms under `internal/recipe/content/briefs/refinement/`
- Snapshot/restore primitive for fragment refinement (Session.SnapshotFragment / RestoreFragment)
- deployFiles narrowness validator
- Slot-shape refusal aggregation

### Changed
- Engine-emit retracted: Class B / C / per-managed-service shells removed; only Class A (IG #1) and tier_decision remain
- Codebase-content brief embeds classification × surface table, voice patterns, citation map, KB symptom-first fail-vs-pass, IG one-mechanism-per-H3 worked examples
- CodebaseGates split into CodebaseScaffoldGates (fact-quality only) + CodebaseContentGates (content-surface validators)
- Decision-recording atoms (scaffold + feature) carry worked examples drawn from reference recipes
- KB stem semantic check at record-time (symptom-first heuristic)
- porter_change.Why no longer truncated to 120 chars in the brief

### Removed
- v2 atom tree (`internal/content/workflows/recipe/`) + v2 recipe engine + `zerops_workflow` MCP tool
- Engine-emit Class B / C / per-managed-service shells (replaced by agent-recorded porter_change facts during deploy phases)

### Migration notes
- External callers of `zerops_workflow` MCP tool: migrate to `zerops_recipe`. Recipe-authoring is the v3 engine's surface.
```

### §13.4 Plan archive

```
git mv docs/zcprecipator3/plans/run-17-prep.md docs/zcprecipator3/plans/archive/
git mv docs/zcprecipator3/plans/run-17-implementation.md docs/zcprecipator3/plans/archive/
git mv docs/zcprecipator3/plans/run-16-prep.md docs/zcprecipator3/plans/archive/  # if not already
git mv docs/zcprecipator3/plans/run-16-readiness.md docs/zcprecipator3/plans/archive/
git mv docs/zcprecipator3/plans/run-16-post-dogfood.md docs/zcprecipator3/plans/archive/
```

Open `run-18-readiness.md` (skeleton: rubric refinement based on run-17 dogfood findings; refinement-phase tuning; v3 atom tree organization review).

### §13.5 Pre-flight tooling cleanup

```
git rm cmd/zcp-preflight-codebase-content/
git rm cmd/zcp-preflight-refinement/
git rm internal/preflight/
```

(These were Tranche -1 scaffolding for run-17; not permanent.)

### §13.6 Gate to ship

- [ ] Both dogfood ANALYSIS.md files saved.
- [ ] Aggregate honest grade ≥8.5 on at least one of the two dogfoods (calibration run can land lower if showcase clears the bar).
- [ ] All R-17-C miss closure statuses recorded.
- [ ] CHANGELOG entry committed.
- [ ] Plans archived.
- [ ] Pre-flight tooling deleted.
- [ ] Run-18 readiness opened.

LoC delta: +~50 (CHANGELOG entry + archive moves + run-18 skeleton; pre-flight tooling deletion contributes -~300 to net).

---

## §14. Per-tranche gate criteria (consolidated)

Each tranche's gate criterion in one place for the implementer's checklist:

| Tranche | Gate criterion |
|---|---|
| -1 | Both pre-flight CLIs build; loader tests pass; manual run on apidev produces brief that opens cleanly in fresh Claude Code session |
| 0 | `TestWriteFactSummary_PorterChange_FullWhyVerbatim` passes |
| 0.5 | All 7 reference atoms exist; pass examples verbatim from references; fail examples verbatim from run-16; rubric has 5×3 anchors; `TestContentQualityRubric_AnchorsExist` passes; implementer self-grades distillation ≥8.5 |
| 1 | All Tranche 1 tests pass; pre-flight harness shows aggregate ≥8.0 on apidev frozen-fact replay; if <8.0, distillation atoms get a second pass before Tranche 2 |
| 2 | All 8 stem-shape tests pass |
| 3 | All gate-split tests pass; scaffold atom updated |
| 4 | All Tranche 4 tests pass; pre-flight refinement on Tranche-1-output sample of 20 fragments shows ≥60% refinement-correct rate |
| 5 | All aggregation tests pass |
| 6 | All 4 deployFiles validator tests pass; run-16 frozen apidev/zerops.yaml triggers the violation |
| 7 | `go test ./... -short` clean post-deletion; `go build ./...` clean; `make lint-local` clean; `zerops_workflow` removed from MCP registration |
| 8 | Both dogfood ANALYSIS.md filed; aggregate honest ≥8.5 on at least one; CHANGELOG committed; plans archived |

---

## §15. Open questions resolved (Q1-Q5 from prep §10)

### Q1 — Refinement sub-agent's edit primitive

**Resolved**: Option A (`record-fragment mode=replace` only) + snapshot/restore primitive. The transactional wrapping (snapshot before, restore on validator-violation) handles the rollback concern that Option C would address structurally. Option C (`refine-fragment` with diff metadata) deferred to run-18 if instrumentation shows refinements changing surface/classification accidentally. See §9.5.

### Q2 — Should claudemd-author also get refinement?

**Resolved**: NO for run-17. claudemd-author is `claude /init`-shape; reference shape is straightforward; refinement target is the codebase-content output (IG / KB / yaml comments) where reference comparison adds the most value. Re-evaluate at run-18 if dogfood shows CLAUDE.md drift.

### Q3 — Should refinement see env-content output too?

**Resolved**: YES. Refinement scope spans all stitched output — root README, 6 tier intros, 6 import.yamls, 3 codebase READMEs, 3 zerops.yaml, 3 CLAUDE.md. Tier intro voice + tier import.yaml comment voice (Surface 3 friendly-authority) is one of the explicit refinement actions. Pointer block in `BuildRefinementBrief` (§9.1) lists every stitched path.

**Pre-flight implication**: Tranche -1's harness should ALSO compose the env-content brief (a parallel CLI `cmd/zcp-preflight-env-content/` or extend the codebase-content CLI with a `-kind env-content` flag). The Tranche 6.10 gate currently only validates codebase-content brief lift; add a sibling gate that validates env-content brief lift on the run-16 facts at the same threshold (≥8.0 across rubric criteria). Without env-content pre-flight, refinement-time edits to tier yaml comments and tier intros would be the first time we observe env-content brief output post-embed.

### Q4 — Refinement-time citation enforcement: heuristic or strict?

**Resolved**: Heuristic for run-17. The rubric (Tranche 0.5) Criterion 3 scores ≥8.5 at "≥50% of KB topics on Citation Map carry inline cite" — refinement acts when below 50%, holds above. Strict (100%) reserved for run-18 if heuristic leaves persistent gaps.

### Q5 — How does refinement interact with parent recipe context?

**Resolved**: refinement reads the parent's published surfaces (path threaded into the brief's pointer block when `parent != nil`). Refinement HOLDS on any fragment whose body would re-author parent material. Encoded as a 100%-sure threshold rule in `briefs/refinement/refinement_thresholds.md` (Tranche 0.5).

---

## §16. Risk register (updated from prep §8)

**Risk 1 — Engine-emit retraction shifts load to deploy-phase fact-recording.** Mitigation: §6.6 worked examples in scaffold + feature decision_recording atoms. Verified by run-16 evidence (36/36 facts have rich Why content). LOW.

**Risk 2 — Refinement sub-agent diverges from reference shape.** Mitigation: distilled reference atoms in brief; 100%-sure threshold; per-fragment edit cap; rubric grades refinement post-action. MEDIUM.

**Risk 3 — Refinement sub-agent introduces new defects.** Mitigation: snapshot/restore primitive (§9.5) rolls back any refinement that triggers a new validator violation. LOW.

**Risk 4 — Token cost of refinement brief.** User-explicit: token-inefficient is fine for above-golden quality. Brief is ~30 KB after distillation (down from prep's "~200 KB" estimate) — well within Opus 4.7 1M-context. Run-17 readiness tracks refinement-phase token cost as a baseline. LOW.

**Risk 5 — Refinement sub-agent caught in correction loops.** Mitigation: per-fragment edit cap (1 attempt). Fall-through on refusal. LOW.

**Risk 6 — KB symptom-first heuristic too permissive (false negatives) or too strict (false positives).** Mitigation: heuristic ORed across multiple shape signals; same-context recovery via slot-shape refusal; tune across run-17 + run-18. MEDIUM.

**Risk 7 — v2 atom tree deletion is multi-package surgery.** Mitigation: surgery sequenced at Tranche 7 (after all v3-side work lands). Step-by-step plan in §12.3. Run-16 dogfood at 8.0 honest is the trigger event for v2 deletion. LOW.

**Risk 8 — Refinement sub-agent + slot-shape refusal interaction.** Mitigation: refinement brief teaches the slot-shape refusal patterns explicitly; refinement falls through gracefully on refusal; aggregation (Tranche 5) gives full feedback in one round-trip. LOW.

**Risk 9 (NEW from §1.3) — Refinement brief leaks filesystem-local references.** Mitigation: `TestNoFilesystemReferenceLeak_RefinementBrief` (§9.6) pins the invariant; refinement composer reads only embedded atoms. LOW.

**Risk 10 (NEW) — Distillation atoms miss the load-bearing patterns.** Mitigation: implementer hand-grades distillation atoms ≥8.5 against actual references before Tranche 0.5 closes; Tranche 1 pre-flight harness validates the embed actually transfers shape; if <8.0, distillation atoms get a second pass before Tranche 4. MEDIUM-HIGH (this is the dominant first-dogfood risk; addressing it pre-flight is the 60→80 lift).

**Risk 11 (NEW) — Pre-flight harness becomes load-bearing infrastructure.** The CLIs in `cmd/zcp-preflight-*/` are run-17-specific. Mitigation: deletion in Tranche 8 (§13.5). LOW.

---

## §17. Maintenance hooks

### §17.1 CLAUDE.md updates

After Tranche 8 ships, add the following invariants to project CLAUDE.md:

```markdown
- **Production agents read only the embedded atom corpus.** Any
  reference recipe / external example used to inform authoring must
  be distilled into an atom at design-time and embedded via
  `embed.FS`. Brief composers never open local-filesystem paths
  outside `internal/recipe/content/...`. Pinned by
  `TestNoFilesystemReferenceLeak_RefinementBrief`.
- **Refinement is post-finalize, single-pass, transactional.** The
  refinement sub-agent at phase 8 reshapes existing fragments via
  `record-fragment mode=replace`; the engine snapshots before
  Replace and reverts on post-Replace validator violation. Per-
  fragment edit cap = 1. Pinned by `TestRefinementReplace_*`.
- **CodebaseScaffoldGates / CodebaseContentGates split.** Scaffold
  + feature complete-phase runs only fact-quality gates; content-
  surface validators run at codebase-content complete-phase.
  Recording fragments to clear a scaffold gate is a code smell —
  the scaffold/feature sub-agent records facts only. Pinned by
  `TestCodebaseScaffoldGates_OnlyFactQuality`.
- **Engine-emit is Class A + tier_decision only.** Class B
  (universal-for-role), Class C umbrella (own-key-aliases), and
  per-managed-service connect shells are retracted; agents record
  porter_change facts during deploy phases per
  `briefs/scaffold/decision_recording.md` worked examples. Pinned
  by `TestEmittedFactsForCodebase_ReturnsEmpty`.
```

### §17.2 Spec amendments

After Tranche 8:

```
docs/spec-content-surfaces.md         — add cross-ref to spec-content-quality-rubric.md
docs/spec-content-quality-rubric.md   — created in Tranche 0.5; canonicalized at Tranche 8
docs/spec-architecture.md             — add §X. Refinement phase
docs/zcprecipator3/plan.md            — mark Phase 5 (v2 deletion) DONE; open Phase 6 (run-18)
```

### §17.3 .claude/settings.json — none

No automation hooks needed for run-17. Quality bar is human-graded against the rubric; harness automation lands in Tranche -1 as deletable scaffolding.

---

## §18. Implementation timeline (rough)

Quality, not wall-time, is the bar — but rough estimate for the implementer planning capacity:

| Tranche | Effort (days, single implementer) |
|---|---:|
| -1 | 1 |
| 0 | 0.25 |
| 0.5 | **3-4** (the dominant time sink — careful distillation) |
| 1 | 2 |
| 2 | 0.5 |
| 3 | 0.5 |
| 4 | 3 |
| 5 | 0.25 |
| 6 | 0.5 |
| 7 | 1 |
| 8 (incl. dogfood × 2 + analysis) | 2 |
| **Total** | **~14 days** |

Prep work (Tranche -1 + 0.5) is ~5 days alone. This is the calibration cost. If skipped, dogfood-time risk goes back to 60%.

---

## §19. What this guide deliberately does NOT cover

- Run-18 readiness — derived from run-17 dogfood findings; opens at Tranche 8.
- Future refinement-phase tuning (Q1 Option C primitive, Q4 strict citation enforcement) — track in run-18 readiness.
- v3 atom tree organizational review — `internal/recipe/content/` could be reorganized for clarity post-deletion. Out of run-17 scope.
- New surface additions (e.g. integration-tests surface, dashboard-onboarding surface) — out of scope; spec-content-surfaces.md remains seven surfaces.
- Multi-recipe parent/child cross-refinement — refinement reads parent surfaces but does not refine them; scope deferred.

---

## §20. Sign-off

This guide is implementation-ready when:

1. The five §1 corrections to `run-17-prep.md` are applied.
2. The implementer reads §0-§16 in order.
3. The implementer agrees the gate criteria (§14) are achievable.

The 80% confidence number assumes:
- All gate criteria are honored (Tranche 1 pre-flight ≥8.0; Tranche 4 pre-flight ≥60% refinement-correct).
- Distillation atoms are hand-graded ≥8.5 before Tranche 0.5 closes.
- Tranches ship in the specified order (no skipping the pre-flight).

If any of these conditions slip, confidence decays. The guide is honest about that — it's a contract, not a sales pitch.
