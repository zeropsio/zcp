# Run-17 architecture prep — engine-emit retraction, brief embed, post-finalize refinement

**Status**: pre-implementation design doc. Triple-verifiable: every file:line, every spec section, every run-16 artifact reference is cite-checked against the actual artifact at the time of writing. The §12 triple-confirmation checklist is the verification protocol for a fresh instance reading this doc cold.

**Predecessor chain**:
[run-15-readiness.md](archive/) → [run-16-prep.md](run-16-prep.md) → [run-16-readiness.md](run-16-readiness.md) → run-16 dogfood ([runs/16/](../runs/16/)) → [run-16-post-dogfood.md](run-16-post-dogfood.md) → **run-17-prep.md** (this doc).

**Scope**: a content-quality-and-architecture-completion pass on top of the run-16 architecture pivot. Run-16 shipped the structural reshape (deploy phases record facts; content phases synthesize); run-17 closes the half-landed pieces (engine-emit retraction, embedded brief delivery, post-finalize refinement sub-agent, CodebaseGates split) so the engine can produce above-golden-standard content. Tokens are explicitly traded for quality — the recipe authoring flow doesn't optimize for wall-time, it optimizes for output above the laravel-jetstream + laravel-showcase reference floor.

**Reading order for a fresh instance**:

1. §1 — the shift in three sentences
2. §2 — what run-16-prep was attempting
3. §3 — what run-16 dogfood actually shipped (artifact-grounded, every claim cite-checked)
4. §4 — root causes (the corrections that anchor this design)
5. §5 — engine improvements by file
6. §6 — dispatch improvements (the post-finalize refinement sub-agent + slot-shape aggregation)
7. §7 — what stays unchanged (so run-17 doesn't relitigate run-16's working pieces)
8. §8 — risk register
9. §9 — tranche ordering
10. §10 — open questions to resolve in implementation prep
11. §11 — retired noise (already cleaned during run-16 post-dogfood thread)
12. §12 — triple-confirmation checklist

---

## §1. The shift in three sentences

Run-16's architecture pivot half-landed: the engine pre-emits content shells (Class B universal-for-role + Class C umbrella + per-managed-service connect) under framework assumptions that don't hold, the codebase-content brief delivers contracts via Read-pointers instead of embedding the load-bearing sections, and there's no post-finalize quality pass to catch the residual content drift before sign-off. Run-17 retracts engine-emit to Class A only (IG #1 from committed yaml) + tier_decision (real diff data), embeds the spec sections + reference excerpts + worked examples directly into the codebase-content brief, and adds a token-rich **post-finalize refinement sub-agent** that reads the stitched output against verbatim golden references and applies 100%-sure quality refinements before the run closes. The 6-agent phase-5 dispatch shape stays unchanged — it's working as designed.

---

## §2. What run-16-prep was attempting

Per [run-16-prep.md §1 + §4](run-16-prep.md):

> Today every content surface is authored mid-deploy by the agent that's also wrestling the codebase to boot — content quality slips because the agent is wearing two hats. Tomorrow the deploy phases (scaffold + feature) write only deploy-critical fields and code, plus structured `porter_change` / `field_rationale` / `tier_decision` facts capturing every non-obvious choice at densest context; two new content phases (codebase content, env content) read those facts plus on-disk canonical content (spec, source, zerops.yaml) and synthesize all documentation surfaces with single-author / cross-surface-aware authoring. CLAUDE.md is authored by a peer `claudemd-author` sub-agent at phase 5 with a strictly Zerops-free brief.

The deliverable was 7 phases (5 today + 2 new):

```
1 Research          | main agent           | plan, contracts            | (no fragments)
2 Provision         | main agent           | workspace yaml + import    | (no fragments)
3 Codebase deploy   | sub × N (parallel)   | code + zerops.yaml fields  | + porter_change + field_rationale facts
4 Feature deploy    | sub × 1              | feature code + yaml fields | + porter_change + field_rationale facts
5 Codebase content  | sub × 2N (parallel)  | (no code changes)          | codebase/<h>/intro + IG + KB + zerops-yaml-comments + claude-md
6 Env content       | sub × 1              | (no code changes)          | root/intro + env/<N>/intro × 6 + import-comments × 54
7 Finalize          | main                 | stitch                     | (validator iterations only)
```

Engine-emit decisions per [run-16-prep.md §3](run-16-prep.md#L78):
- **Class A (IG #1, "Adding `zerops.yaml`")**: engine-emit YES (committed yaml verbatim).
- **Class B (universal-for-role, e.g. bind 0.0.0.0 + trust proxy + SIGTERM)**: engine pre-emits structured fact + agent fills framework-specific diff slot.
- **Class C (universal-for-recipe, per managed service)**: engine pre-emits umbrella fact ("Read managed-service credentials from own-key aliases") + per-service shells ("Connect to <svc>"); agent fills connection idiom slot.
- **Class D (framework × scenario)**: agent records during feature phase.

Slot-shape refusal at `record-fragment` time replaces post-hoc structural validators. Validator narrowing per [run-16-prep.md §6.8](run-16-prep.md#L508).

CLAUDE.md authored by dedicated `claudemd-author` peer dispatched in parallel with codebase-content; brief is **strictly Zerops-free** so platform context cannot bleed into the codebase-operating guide.

---

## §3. What run-16 dogfood actually shipped

`nestjs-showcase` · 2026-04-28 · zcprecipator3 v9.X (post-run-16-readiness tranches 0-5). Artifact root: [runs/16/](../runs/16/). Honest grade vs reference recipes: **8.0 / 10** (run-15 was 7.5; lift +0.5 — see [runs/16/CONTENT_COMPARISON.md](../runs/16/CONTENT_COMPARISON.md)).

### §3.1 What worked

Verified by walking the artifacts, not by reading the readiness doc's promises.

- **7 phases reachable in adjacent-forward order.** Per [runs/16/ANALYSIS.md §1 timeline](../runs/16/ANALYSIS.md#L173): research (~30s) → provision (~2m) → scaffold (~25m) → feature (~20m) → codebase-content (~8m) → env-content (~11m) → finalize (~5m). Total ~1h22m, ~6 minutes shorter than run-15.
- **Phase-5 parallel dispatch held.** 6 sub-agents launched in a single assistant message at [main-session.jsonl:145,148,150,152,154,156](../runs/16/SESSION_LOGS/main-session.jsonl) with shared `requestId=req_011CaVwAfVRxvQM5D3eCfCu9` + identical `output_tokens=12714`. Phase-3 scaffold dispatch held the same shape.
- **Engine-emitted shells fill via `fill-fact-slot`.** 67 facts in [environments/facts.jsonl](../runs/16/environments/facts.jsonl) — 36 `porter_change`, 17 `field_rationale`, 8 `tier_decision`, 6 browser-verification + scaffolding observations. Every shell topic populated post-fill (EngineEmitted=false, Why non-empty, CandidateHeading non-empty except worker no-HTTP per design).
- **`porter_change.Why` field is rich.** 36/36 facts carry 250–500 char Why paragraphs — recording is working. (See §4.5 for the truncation render bug that obscures this in the codebase-content brief.)
- **Cross-surface duplication dropped to 0.** Run-15 baseline was 2 dups (apidev X-Cache, appdev duplex:'half'); run-16 has zero across all three codebases.
- **CLAUDE.md is Zerops-free in published form.** All three codebases ship `claude /init`-shape sections (`## Build & run`, `## Architecture`, optional 3rd section) with zero `## Zerops` headings, zero `zsc` / `zerops_*` / `zcp` tool name leaks.
- **Tier README extracts settled at 1-sentence cards.** 308–342 chars across all six tiers. Run-14's 35-line ladder regression is gone. R-15-3 closure HELD.
- **Per-codebase IG ships exactly 5 numbered items inside extract markers.** 5/5/5 across apidev / appdev / workerdev. R-15-5 closure HELD.
- **Slot-shape refusal works at record-time.** 42 refusals across the run; each refused fragment recovered within 1–8 turns same-context. Cluster pattern: scaffold-api hit 8 successive CLAUDE.md refusals at [agent-a6f709bdfbc83894f.jsonl:220-238](../runs/16/SESSION_LOGS/subagents/agent-a6f709bdfbc83894f.jsonl) naming one offending hostname per round-trip — works, but inefficient (see §6.3).

### §3.2 What didn't work — the load-bearing finding

**R-16-1 (HIGH)** — scaffold sub-agents authored 38 IG/KB/CLAUDE.md/intro fragments despite the pivot teaching "deploy phases record facts only". The forcing function is engine-side, not authorial: [`internal/recipe/gates.go::CodebaseGates`](../../../internal/recipe/gates.go) runs `codebase-surface-validators` at scaffold complete-phase, so the sub-agent must record fragments to clear the gate and terminate. [`phase_entry/scaffold.md`](../../../internal/recipe/content/phase_entry/scaffold.md) lines 152-157 teach "do NOT author"; the gate enforces the opposite; the gate wins. Closure is a 3-line `gates.go` edit + atom updates + 4 tests (§5.5).

### §3.3 Content-quality misses

Honest content read against [/Users/fxck/www/laravel-jetstream-app/](../../../../laravel-jetstream-app/) and [/Users/fxck/www/laravel-showcase-app/](../../../../laravel-showcase-app/). Full per-surface walk in [runs/16/CONTENT_COMPARISON.md](../runs/16/CONTENT_COMPARISON.md). The non-misses (previous-pass complaints that didn't survive reference grounding) are documented in CONTENT_COMPARISON §9; this section names only verified misses.

- **R-17-C1** — KB stems are author-claims, not symptom-first. Run-16: `**TypeORM synchronize: false everywhere**` ([apidev/README.md:252](../runs/16/apidev/README.md)). The reference shape from showcase + jetstream is observable-state-first or directive-tightly-mapped-to-symptom. A porter searching "schema corruption on deploy" or "ALTER TABLE deadlock" cannot find run-16's bullet.
- **R-17-C2** — IG #2 fuses three platform-forced changes. apidev IG #2: `### 2. Bind 0.0.0.0, trust the proxy, drain on SIGTERM`. Reference recipes give each its own H3 — three independent mechanisms, three failure modes. Caused by engine-emit cap pressure (§4.1).
- **R-17-C3** — IG #3 (apidev) is recipe preference, not platform-forced. `### 3. Alias platform env refs to your own names` is the recipe's choice; per spec [Classification × surface compatibility](../../spec-content-surfaces.md), recipe preference routes to zerops.yaml comment, not IG. Caused by engine pre-emitting `own-key-aliases` shell with `CandidateSurface=CODEBASE_IG`.
- **R-17-C4** — voice is engineering-spec, not friendly-authority. Zero "Feel free to change this", "Configure this to use real SMTP" tokens across all checked surfaces (apidev/appdev/workerdev README + tier-4 import.yaml). Reference recipes use these consistently. Caused by voice patterns living in spec only — never threaded into the brief.
- **R-17-C5** — citations under-applied at prose level. 3/15 KB bullets carry inline guide refs; 12 topics on the Citation Map ship without inline cite. The completion gate refuses missing `citations[]` payload (manifest-level), but the published prose can omit the guide reference and still pass.
- **R-17-C6** — trade-offs one-sided. Most KB / IG items name only the chosen path. Reference recipes consistently name the rejected alternative (`predis over phpredis`; `embedding NATS credentials in URL → double-auth`). One run-16 bullet (workerdev KB #1) does this correctly; the rest don't.
- **R-17-C7** — showcase tier supplements absent. workerdev KB lacks the queue-group + SIGTERM gotchas the writer-spec mandates for tier=showcase + separate worker. The teaching atom exists in the v2 atom tree at [internal/content/workflows/recipe/briefs/writer/content-surface-contracts.md:102-109](../../../internal/content/workflows/recipe/briefs/writer/content-surface-contracts.md) but was never ported to the v3 codebase-content brief tree.
- **R-17-C8** — `deployFiles` narrowness slip. apidev/zerops.yaml prod ships `src/scripts` while initCommands invokes `dist/scripts/migrate.js`. Either ts-node fallback (say so in comment) or dead weight (drop). No validator catches the path mismatch.
- **R-17-C9** — `porter_change.Why` is truncated to 120 chars in the brief render. facts.jsonl carries 250–500 char Why paragraphs; the codebase-content brief sees the first 120 chars cut mid-clause. Free quality win — 1-line fix at [briefs_content_phase.go:330](../../../internal/recipe/briefs_content_phase.go).
- **R-17-C10** — slot-shape refusal whittles per-token. 8 successive refusals naming one hostname each (efficiency, not correctness — recovery worked).
- **R-17-C11** — yaml-comment field-restatement preface. tier-4 service blocks open `# api in zeropsSetup: prod, 0.5 GB shared CPU, minContainers: 2`. Showcase reference does this too (partial non-miss); jetstream is tighter. Stretch goal, not a current-shape miss.

---

## §4. Root causes — the corrections that anchor this design

Each correction is a specific run-16 finding traced to its engine-side cause. Run-17 design follows from these.

### §4.1 Engine-emit was over-applied

[`internal/recipe/engine_emitted_facts.go`](../../../internal/recipe/engine_emitted_facts.go) hardcodes content shells under framework + role assumptions:

```go
// Lines 39-87: Class B (universal-for-role)
//   <host>-bind-and-trust-proxy   — Why hardcoded; CandidateHeading="Bind 0.0.0.0 and trust the L7 proxy"
//   <host>-sigterm-drain          — Why hardcoded; CandidateHeading="Drain in-flight requests on SIGTERM"
//   <host>-no-http-surface        — Why hardcoded; CandidateHeading agent-filled
//
// Lines 89-105: Class C umbrella
//   <host>-own-key-aliases        — Why hardcoded; CandidateHeading="Read managed-service credentials from own-key aliases"
//
// Lines 108-131: per-service shells
//   <host>-connect-<svc>          — Why empty (agent fills), CandidateHeading empty (agent fills), shell exists
```

**For apidev (RoleAPI, nodejs, 5 managed services)**: 1 (bind) + 1 (sigterm) + 1 (own-key-aliases) + 5 (per-service connect) = **8 engine-emitted IG shells**. Spec cap is **5**. The architecture forced the agent to fuse — which is exactly R-17-C2 (apidev IG #2 bundled bind+trust+SIGTERM) and R-17-C3 (own-key-aliases shipped as IG #3 because the engine pre-emitted that shell with `CandidateSurface=CODEBASE_IG` and the agent had nowhere else to put it).

**The framework assumptions are also shaky**:
- Bind 0.0.0.0 isn't universally needed (modern Node frameworks default to it; Vite already binds 0.0.0.0).
- Trust proxy is framework-specific in syntax AND in necessity.
- SIGTERM drain — modern frameworks handle it.
- `own-key-aliases` is recipe *preference*, not platform-forced — porters can read `${db_hostname}` directly.

**The prep doc was internally inconsistent.** [run-16-prep.md §3.5](run-16-prep.md#L156) acknowledged the cap problem ("multi-managed-service codebase → too many; spec forces routing decisions") but the engine shipped pre-emitting all of them anyway. The "spec forces routing" mitigation never landed in code.

**Conclusion**: Class B + Class C umbrella + per-service connect engine-emit is the wrong primitive. Only Class A (IG #1 from committed yaml) is genuinely structural. Class B/C/D should be **agent-recorded `porter_change` facts during deploy phases**, classified + routed by the codebase-content sub-agent at phase 5 per spec compatibility table. tier_decision stays engine-emit (real diff data, not framework assumptions).

### §4.2 Brief delivery is pointer-based, not embedded

The codebase-content brief delivers contracts via Read-pointers, not embedded text:

- [`internal/recipe/briefs_content_phase.go::BuildCodebaseContentBrief`](../../../internal/recipe/briefs_content_phase.go) (line 28-131): atoms include `phase_entry/codebase-content.md`, `briefs/codebase-content/synthesis_workflow.md`, `briefs/scaffold/platform_principles.md`. Per-codebase facts threaded via `FilterByCodebase`. Engine-emitted shells via `EmittedFactsForCodebase`.
- **NO reference excerpts.** No verbatim KB/IG/yaml-comment samples from laravel-jetstream / laravel-showcase.
- **NO worked examples.** No fail-vs-pass example pairs anywhere.
- **NO spec verbatim.** The pointer block (line 106-117) tells the agent to `Read /Users/fxck/www/zcp/docs/spec-content-surfaces.md` on demand.

Spec sections that DICTATE shape (voice patterns, citation requirement, classification × surface table, showcase-tier supplements) sit behind a Read instead of being embedded verbatim. Sub-agents under context pressure don't re-read 490-line specs at every authoring decision. **Rules without examples don't transfer.**

This is the dominant cause of R-17-C1 (stem shape), R-17-C4 (voice), R-17-C5 (citations), R-17-C6 (trade-offs), R-17-C7 (showcase supplements). The teaching exists; it just doesn't reach the sub-agent at decision time.

### §4.3 Validator stack is gameable on stem semantics

Three KB validators run; none check stem semantics:

- [`internal/recipe/slot_shape.go::checkCodebaseKB`](../../../internal/recipe/slot_shape.go) lines 130-151: record-time refusal. Checks `**` prefix + bullet count cap. **`**TypeORM synchronize: false everywhere**` passes both.**
- [`internal/recipe/validators_codebase.go::validateCodebaseKB`](../../../internal/recipe/validators_codebase.go) lines 107-145: finalize-time. Checks marker presence + bold-bullet density + count cap; bans the `**symptom**:` triple. **No symptom-first / observable-state / HTTP-status / error-string check.**
- [`internal/recipe/validators_kb_quality.go`](../../../internal/recipe/validators_kb_quality.go): V-2 paraphrase containment, V-3 platform-mention, V-4 self-inflicted voice. Author-claim declarative stems pass V-4 (third-person prescriptive is not self-inflicted).

Writer-spec teaches symptom-first stems but the validator stack only refuses the inverse pattern (`**symptom**:` triple) and self-inflicted voice. Author-claim declarative stems sail through. **Run-17 fix lands at `slot_shape.go:130` record-time refusal** (consistent with run-16's same-context-recovery doctrine), paired with a symptom-first authoring atom.

### §4.4 `CodebaseGates` contradicts the pivot

[`internal/recipe/gates.go::CodebaseGates`](../../../internal/recipe/gates.go) runs `codebase-surface-validators` at scaffold complete-phase. Scaffold sub-agent must record content fragments to clear the gate and terminate. [`phase_entry/scaffold.md`](../../../internal/recipe/content/phase_entry/scaffold.md) lines 152-157 teach "do NOT author"; the gate enforces the opposite; the gate wins. 38 fragments authored under duress; they degrade context for downstream phases.

### §4.5 `porter_change.Why` truncation render bug

[`briefs_content_phase.go:330`](../../../internal/recipe/briefs_content_phase.go) `writeFactSummary` calls `truncate(f.Why, 120)`. facts.jsonl Why values are 250–500 chars. The codebase-content brief sees the first 120 chars cut mid-clause. The deploy-phase recording is working; the synthesis-phase brief just isn't seeing the full Why content.

### §4.6 v2 atom tree is parallel-live, not migrated

Two atom trees coexist:

- **v2 tree**: [`/Users/fxck/www/zcp/internal/content/workflows/recipe/`](../../../internal/content/workflows/recipe/) — feeds the older workflow engine via `internal/workflow/atom_manifest*.go`, `atom_stitcher.go`, `internal/analyze/runner.go`, `tools/lint/atom_template_vars`, `tools/lint/recipe_atom_lint.go`, `internal/content/content.go` embed.FS. **13 Go references, multi-package surgery to delete.**
- **v3 tree**: [`/Users/fxck/www/zcp/internal/recipe/content/`](../../../internal/recipe/content/) — feeds the recipe engine (the run-16 architecture pivot). Confirmed via diagnostic that `synthesis_workflow.md` lands in the run-16 brief.

Zero v3-side references to v2. Recipe engine work belongs entirely in v3. v2 deletion is its own tranche on its own dependency chain.

### §4.7 No post-finalize quality pass

After phase 7 (finalize stitch + validate), the run is done. Slot-shape refusal catches structural drift at record-time; finalize validators check structural caps. Neither catches voice drift, stem-shape drift, citation drift, classification routing drift — the very gaps that produced R-17-C1 / C4 / C5 / C6. The current architecture has nowhere for "compare against golden references and apply 100%-sure refinements" to live.

---

## §5. Engine improvements by file

### §5.1 Engine-emit retraction — `internal/recipe/engine_emitted_facts.go`

**Delete**: `classBFacts` (bind-and-trust-proxy, sigterm-drain, no-http-surface), `classCUmbrellaFact` (own-key-aliases), `perServiceShells` (connect-<svc>). ~150 LoC removed.

**Keep**: `EmittedTierDecisionFacts` (lines 199-265). This one's based on real `Diff` between adjacent tiers — engine knows the diff because the diff exists; it's not a framework assumption. tier_decision facts feed env import.yaml comments and are doing genuine work.

**Keep**: IG #1 engine-emit at [`internal/recipe/assemble.go::injectIGItem1`](../../../internal/recipe/assemble.go) (line 170-195). This is Class A from committed yaml — structurally certain.

**Replace with**: agent-recorded `porter_change` facts during deploy phases. The recording teaching already exists in `briefs/scaffold/decision_recording.md` + `briefs/feature/decision_recording.md` (verified Q2 — recording works; 36/36 facts have rich Why content). Strengthen the teaching with worked examples drawn from reference recipes' actual IG items (one per Class B/C class shape) so the agents learn the recording pattern from concrete cases.

**Tests**: delete `engine_emitted_facts_test.go` Class B/C tests; keep tier_decision tests; update brief composer tests that assert engine-emit shell presence.

**Estimate**: -200 LoC engine + ~40 LoC atom (worked examples) + ~30 LoC test reshape. Net **-130 LoC**.

### §5.2 Brief delivery embed — `internal/recipe/content/briefs/codebase-content/synthesis_workflow.md` + composer hook

**Embed verbatim into `synthesis_workflow.md`**:

1. Classification × surface compatibility table from [spec §349-362](../../spec-content-surfaces.md). Sub-agent reads it at decision time, not via Read-pointer.
2. Friendly-authority voice patterns — 4 verbatim spec excerpts ([§312-317](../../spec-content-surfaces.md#L312)) labeled "use here" / "don't use here" per-surface. *"Feel free to change this value to your own custom domain"* and the three other reference patterns.
3. Citation map + cite-by-name pattern. Pattern: *"The `<guide-id>` guide covers <basic mechanism>; the application-specific corollary is …"*. Plus the per-recipe guide list (engine threads `citationGuides()` from [briefs.go:580-598](../../../internal/recipe/briefs.go) into the brief).
4. KB symptom-first fail-vs-pass example pair — one annotated reference KB bullet from jetstream (symptom-first), one from showcase (directive-tightly-mapped), one annotated FAIL bullet (`**TypeORM synchronize: false everywhere**` labeled "author-claim — porter searches for symptom").
5. IG one-mechanism-per-H3 worked example — three reference H3s in sequence showing one platform-forced change per H3.

**Add new atom**: `internal/recipe/content/briefs/codebase-content/showcase_tier_supplements.md` — port the queue-group + SIGTERM gotcha mandate from v2 tree (the existing `/Users/fxck/www/zcp/internal/content/workflows/recipe/briefs/writer/content-surface-contracts.md:102-109`). Conditionally include in `BuildCodebaseContentBrief` when `plan.Tier == tierShowcase && cb.IsWorker`.

**Composer hook**: thread `citationGuides()` (already computed at [briefs.go:580-598](../../../internal/recipe/briefs.go)) into [`briefs_content_phase.go:48-52`](../../../internal/recipe/briefs_content_phase.go).

**Estimate**: ~+200 LoC atom + ~10 LoC composer + ~30 LoC tests. Closes R-17-C1 (KB stem), R-17-C4 (voice), R-17-C5 (citations), R-17-C6 (trade-offs), R-17-C7 (showcase supplements).

### §5.3 KB symptom-first slot-shape refusal — `internal/recipe/slot_shape.go`

**Add to** [`checkCodebaseKB`](../../../internal/recipe/slot_shape.go) (line 130-151): semantic check on stem text. Heuristic — the text between `**...**` must contain at least one of:

- HTTP status code (`\b[1-5]\d{2}\b`)
- A quoted error string (`"..."`)
- Verb-form failure phrase (`fails`, `returns`, `rejects`, `drops`, `missing`, `breaks`, `crashes`, `corrupts`, `silently exits`)
- Observable wrong-state phrase (`empty body`, `wrong header`, `null where X expected`, etc.)

Author-claim declarative stems (`**Library X: setting Y**`, `**Decompose execOnce keys into migrate + seed**`) get refused at record-fragment time with a re-author hint. Same-context recovery per run-16 doctrine.

**Pair with** the §5.2 atom edit so the brief teaches the stem shape AND the validator enforces it.

**Estimate**: ~+30 LoC slot_shape + ~15 LoC tests. Closes R-17-C1 backstop.

### §5.4 `porter_change.Why` truncation fix — `internal/recipe/briefs_content_phase.go:330`

**Change**: `truncate(f.Why, 120)` → `truncate(f.Why, 400)` for porter_change facts (or drop truncation entirely for porter_change since count is bounded — typically 5-10 per codebase).

**Estimate**: 1 line + 1 test. Closes R-17-C9. **Free quality win**, ship in tranche 0.

### §5.5 `CodebaseGates` split — `internal/recipe/gates.go`

**Change**: split [`CodebaseGates()`](../../../internal/recipe/gates.go) so scaffold complete-phase runs ONLY fact-quality gates (`facts-recorded`, `engine-shells-filled`); content-surface validators run at codebase-content complete-phase.

Scaffold no longer authors fragments to clear the gate; codebase-content sub-agent owns content authoring per the pivot's design intent. Atoms + briefs + gate align.

**Estimate**: ~30 LoC + 4 tests + atom updates in `phase_entry/scaffold.md` to remove the contradictory teaching. Closes R-16-1 (the load-bearing run-16 finding).

### §5.6 deployFiles narrowness validator — `internal/recipe/validators_codebase.go`

**Add validator**: every prod `deployFiles` entry must be referenced by a `run.start` / `run.initCommands` / `build.deployFiles` field, OR carry a `field_rationale` fact explaining its presence. Otherwise fail with structured explanation.

**Estimate**: ~+40 LoC + 4 tests. Closes R-17-C8.

---

## §6. Dispatch improvements

### §6.1 6-agent phase-5 shape stays unchanged

3 codebase-content + 3 claudemd-author per codebase, dispatched in parallel in a single message. Confirmed working in run-16 (shared `requestId` + identical `output_tokens` evidence). Document explicitly in run-17 readiness so the question doesn't get relitigated.

### §6.2 NEW: Phase 8 — post-finalize refinement sub-agent

The major architectural addition for run-17. Solves a class of quality gaps the existing pipeline cannot reach.

**Position**: after phase 7 (finalize stitch + validate), before run sign-off.

**Dispatch**: single sub-agent, single message. Brief is **token-rich** — the recipe authoring flow trades tokens for output above the golden-standard floor.

**Brief content** (~200 KB):

- **All stitched output verbatim** — root README, 6 tier intros, 6 import.yamls, 3 codebase READMEs (with IG + KB extracts), 3 CLAUDE.md, 3 zerops.yaml.
- **Reference recipes verbatim** — laravel-jetstream + laravel-showcase, both apps repos + recipe trees. Every README, every import.yaml, every codebase zerops.yaml. ~80 KB.
- **Spec sections verbatim** — [docs/spec-content-surfaces.md](../../spec-content-surfaces.md) in full. Surface contracts, classification taxonomy, citation map, friendly-authority voice section. ~30 KB.
- **All facts** — porter_change + field_rationale + tier_decision from [environments/facts.jsonl](../runs/16/environments/facts.jsonl). ~50 KB.
- **The 100%-sure refinement scope** (atom-side teaching).

**Refinement scope** — only when the sub-agent is 100% sure the change matches reference shape:

| Refinement | Trigger | Action |
|---|---|---|
| KB stem reshape | Stem matches author-claim pattern AND a symptom-first phrasing is derivable from facts | `record-fragment` overwrite with symptom-first stem; preserve body |
| yaml-comment field-restatement tightening | Comment opens with field-echo preamble AND a mechanism-first version is shorter | `record-fragment` overwrite |
| IG H3 fusion split | H3 bundles 2+ independent platform-forced changes AND total IG count would still ≤ 5 after split | `record-fragment` per split item |
| Voice insertion | Surface allows friendly-authority (Surface 7 zerops.yaml comments primarily; Surface 3 tier yaml secondarily) AND adapt-for-your-needs follow-through fits | `record-fragment` overwrite with friendly-authority pattern |
| Trade-off two-sided expansion | KB body names only chosen path AND rejected alternative is namable from facts or zerops_knowledge | `record-fragment` overwrite |
| Citation prose-level enforcement | Topic on Citation Map AND body lacks guide reference | `record-fragment` overwrite with cite-by-name pattern |
| IG-recipe-preference correction | IG item is recipe choice (not platform-forced) AND zerops.yaml comment slot is empty | Move content to zerops.yaml comment fragment; remove IG item |
| Showcase tier supplement injection | Tier=showcase + separate worker AND queue-group OR SIGTERM gotchas missing from worker KB | Add KB bullets per writer-spec mandate |

**Constraints**:

- **100%-sure threshold**: if the refinement could be wrong, skip. Brief teaches: *"if you'd hesitate to argue this change in a code review, leave it alone."*
- **Per-fragment edit cap**: one refinement attempt per fragment. If slot-shape refusal blocks the refinement, accept the original.
- **No new content**: only reshape existing content. The refinement sub-agent doesn't author new IG items, doesn't add new KB bullets except for the explicit showcase-tier supplement case.
- **No correctness regressions**: after refinement, finalize re-runs validators. If a refinement introduces a new defect, the engine reverts to pre-refinement fragment via fragment versioning.
- **Refusal-aware**: slot-shape refusal at record-fragment is fall-through, not retry-loop.

**Tools available**: full Read / Glob / Grep, `record-fragment` (overwrite), `replace-by-topic` (fact body update), `zerops_knowledge` (citation lookup).

**Output**: refined fragments. Engine re-stitches. Run signs off when refinement completes (or skips cleanly).

**Why this works**: the refinement sub-agent has resources the codebase-content + claudemd-author sub-agents don't have:

- Full reference-recipe corpus in its brief (vs. zero references in codebase-content brief).
- Time + tokens to compare every surface against reference shape (vs. authoring under context pressure).
- No scope conflict (it's not authoring fresh content; it's refining existing).
- One pass over the entire output (vs. per-codebase fragmentation).

The refinement sub-agent codifies the **fresh-evaluator pattern** the user has applied manually after every dogfood since run-14. Run-17 ships the analyze-step inline so it catches all system + content problems before sign-off.

**Estimate**: new `BuildRefinementBrief(plan, runDir)` composer + `phase_entry/refinement.md` atom + `briefs/refinement/synthesis_workflow.md` atom + dispatch wiring. ~+200 LoC composer + ~+250 LoC atoms + ~50 LoC tests. ~+500 LoC total. Closes R-17-C1, C2, C3, C4, C5, C6, C7 backstop (alongside §5.2 atom embed which is the upstream closure).

### §6.3 Slot-shape refusal aggregation

[`record-fragment`](../../../internal/recipe/handlers.go) currently rejects on first-encountered offender. Run-16 evidence: scaffold-api hit 8 successive CLAUDE.md refusals naming one hostname each. Recovery worked but inefficient.

**Change**: aggregate all offenders detected in a single fragment scan into one refusal response. Sub-agent re-authors against the full offender list in one pass.

**Estimate**: ~+30 LoC. Closes R-17-C10 efficiency cost.

---

## §7. What stays unchanged

So run-17 doesn't relitigate run-16's working pieces:

- **6-sub-agent phase-5 dispatch shape** (3 codebase-content + 3 claudemd-author, parallel, single message).
- **Class A engine-emit** (IG #1 from committed yaml at `assemble.go::injectIGItem1`).
- **tier_decision engine-emit** (real diff data, not framework assumptions).
- **claudemd-author Zerops-free brief** (working as designed; CLAUDE.md is Zerops-free in published form).
- **Slot-shape refusal at record-fragment** as the structural-cap enforcer (run-16 doctrine).
- **`docs/spec-content-surfaces.md`** as the canonical content-spec source (referenced by atoms; embedded verbatim per §5.2).
- **`claude /init`-shape CLAUDE.md contract** (Surface 6 in spec, post-cleanup).
- **Validator narrowing** (post-hoc → record-time) per run-16's same-context-recovery doctrine.

---

## §8. Risk register

### Risk 1 — Engine-emit retraction shifts load to deploy-phase fact-recording

**Mitigation**: Q2 verified that 36/36 porter_change facts in run-16 carry rich Why content. Recording works. The shift is safe — deploy-phase teaching is sufficient. Strengthen with worked examples from reference recipes (§5.1).

### Risk 2 — Refinement sub-agent diverges from reference shape

**Mitigation**: brief carries references verbatim. 100%-sure threshold. Per-fragment edit cap. Re-validation after refinement.

### Risk 3 — Refinement sub-agent introduces new defects

**Mitigation**: post-refinement re-validation; fragment versioning rolls back failed refinements.

### Risk 4 — Token cost of refinement brief

User-explicit: token-inefficient is fine for above-golden-standard quality. Brief is ~200 KB; well within Opus 4.7 1M-context window. Run-17 readiness should track refinement-phase token cost so future tuning has a baseline.

### Risk 5 — Refinement sub-agent caught in correction loops

**Mitigation**: per-fragment edit cap (1 attempt). Fall-through on refusal. No retry-loop.

### Risk 6 — KB symptom-first heuristic is too permissive (false negatives) or too strict (false positives)

**Mitigation**: heuristic is ORed across multiple shape signals (status / quoted error / verb-form / observable phrase). Same-context recovery means false positives cost a re-author cycle, not a run failure. Tune the heuristic across run-17 + run-18 dogfoods.

### Risk 7 — v2 atom tree deletion is multi-package surgery

Per [zcprecipator3/plan.md §Phase 5](../plan.md), v2 deletion **triggers on first quality showcase via v3**. Run-16 is that trigger event (8.0/10 honest grade, single HIGH defect with clear closure). v2 deletion lands in Tranche 7 (§9).

v2 contents are pure recipe-authoring legacy: phase atoms (`close`/`deploy`/`finalize`/`generate`/`provision`/`research`) + brief atoms (`code-review`/`editorial-review`/`feature`/`scaffold`/`writer`). No non-recipe consumers — the MCP `zerops_workflow` tool is v2's only surface and gets removed from server registration as part of the deletion.

**Surgery surface** (8 files / dirs):
- `internal/content/workflows/recipe/` atom tree (delete)
- `internal/content/workflows/recipe*.md` recipe.md monolith (delete)
- `internal/workflow/recipe*.go` v2 recipe engine code (delete)
- `internal/content/content.go` embed.FS — drop v2 paths
- `internal/workflow/atom_manifest*.go` + `atom_stitcher.go` — delete (v2-only consumers)
- `internal/analyze/runner.go::AtomRootDefault` + `internal/analyze/structural.go` — repoint at v3 OR delete analyzer pipeline if it's v2-only audit
- `tools/lint/recipe_atom_lint.go` + `tools/lint/atom_template_vars/main.go` — repoint `atomRoot` at v3
- `internal/server/` — drop `zerops_workflow` registration

**Mitigation**: surgery sequenced as the second-to-last tranche (Tranche 7) so all v3-side content-quality work (Tranches 1-6) lands first. v2 deletion is mechanical once v3 is the only path; the prep work is mapping every consumer call site (already done in §4.6 + this risk's surgery surface above).

### Risk 8 — Refinement sub-agent + slot-shape refusal interaction

The refinement sub-agent issues `record-fragment` overwrites. Slot-shape refusal applies. If a refinement is correct in spirit but the refusal heuristic rejects (false positive at refinement time), refinement fails.

**Mitigation**: refinement brief teaches the slot-shape refusal patterns explicitly. Refinement falls through gracefully (per-fragment edit cap). Acceptable cost; favours refusal-as-truth.

---

## §9. Tranche ordering

Tranches 0-1 ship before run-17 dogfood. Tranches 2-6 ship in sequence. Tranche 7 is sign-off.

### Tranche 0 — Free quality wins (~10 LoC, hours)

- §5.4 `porter_change.Why` truncation fix at [briefs_content_phase.go:330](../../../internal/recipe/briefs_content_phase.go).
- (Already done this thread: subdomain gate removed; tier-promotion clauses removed; CLAUDE.md spec rewritten to `claude /init` shape.)

### Tranche 1 — Engine-emit retraction + brief embed (~+200 LoC atom, -150 LoC engine)

- §5.1 delete `classBFacts` / `classCUmbrellaFact` / `perServiceShells` from `engine_emitted_facts.go`. Update tests.
- §5.2 embed classification table + voice patterns + citation map + KB symptom-first example pair + IG one-mechanism-per-H3 example into `synthesis_workflow.md`. Add `showcase_tier_supplements.md` atom + composer conditional.
- §5.2 thread `citationGuides()` into codebase-content brief.
- Update `briefs/scaffold/decision_recording.md` + `briefs/feature/decision_recording.md` with worked examples drawn from reference recipes' Class B/C/D items.

**Net**: net +50 LoC, closes R-17-C1/C2/C3/C4/C5/C6/C7 at the upstream level.

### Tranche 2 — KB symptom-first slot-shape refusal (~+45 LoC)

- §5.3 add semantic stem-shape check at `slot_shape.go::checkCodebaseKB`.
- Update [`slot_shape_test.go`](../../../internal/recipe/slot_shape_test.go).

### Tranche 3 — `CodebaseGates` split (R-16-1 closure) (~+30 LoC)

- §5.5 split `CodebaseGates()` so scaffold runs only fact-quality gates.
- Update `phase_entry/scaffold.md` atom to remove the contradictory authoring teaching.
- 4 new tests.

### Tranche 4 — Phase 8 refinement sub-agent (~+500 LoC)

- §6.2 new `BuildRefinementBrief` composer + `phase_entry/refinement.md` atom + `briefs/refinement/synthesis_workflow.md` atom + dispatch wiring.
- New phase enum value (or extend Phase 7).
- Tests pinning refinement scope (each refinement type has a fail-vs-pass test).

### Tranche 5 — Slot-shape refusal aggregation (~+30 LoC)

- §6.3 aggregate offenders in `record-fragment` refusal scan.

### Tranche 6 — deployFiles narrowness validator (~+40 LoC)

- §5.6 new validator at `validators_codebase.go`.

### Tranche 7 — v2 atom tree deletion + workflow-engine recipe-path deprecation (~-2,500 LoC net)

Per [zcprecipator3/plan.md §Phase 5](../plan.md). Triggered by run-17 dogfood quality bar.

- Delete `internal/content/workflows/recipe/` atom tree (~155 atom files: 65 phase + 39 brief + 16 principle + writer/editorial-review/code-review subdirs).
- Delete `internal/content/workflows/recipe*.md` (recipe.md monolith).
- Delete `internal/workflow/recipe*.go` (v2 recipe engine code).
- Delete `internal/workflow/atom_manifest*.go` + `internal/workflow/atom_stitcher.go` (v2-only consumers).
- Update `internal/content/content.go` embed.FS — drop v2 paths.
- Repoint `tools/lint/recipe_atom_lint.go::atomRoot` + `tools/lint/atom_template_vars/main.go::atomRoot` at v3 (`internal/recipe/content/`), OR delete if v3 has its own equivalent lints.
- Repoint `internal/analyze/runner.go::AtomRootDefault` + `internal/analyze/structural.go::CheckAtomTemplateVarsBound` at v3, OR delete the analyzer pipeline if it's purely v2-audit infrastructure.
- Remove `zerops_workflow` MCP tool from `internal/server/` registration. v3 `zerops_recipe` becomes the sole recipe-authoring tool surface.
- Tests: remove every test that reads from `internal/content/workflows/recipe/`. Add migration notes to CHANGELOG explaining `zerops_workflow` removal.

**Net**: ~-2,500 LoC (atom tree + engine code + consumer infrastructure). Single biggest LoC reduction in run-17. Eliminates dual-tree maintenance friction.

### Tranche 8 — Sign-off

- CHANGELOG entry naming run-17's deliverables (engine-emit retraction; brief embed; KB symptom-first slot refusal; CodebaseGates split; refinement sub-agent; deployFiles validator; v2 deprecation).
- Spec amendments if anything drifted during implementation.
- Archive run-16 + run-17 plan docs.
- Run-18 readiness opens for the next quality lift cycle.

---

## §10. Open questions to resolve in implementation prep

These need answers before tranche 4 (refinement sub-agent) lands. Tranche 1-3 are well-scoped from this prep doc; tranche 4 is the architectural addition with most degrees of freedom.

### Q1 — Refinement sub-agent's edit primitive

Three options:

- **(A) `record-fragment` overwrite only** — refinement sub-agent re-issues record-fragment for any fragment it's overwriting. Engine handles versioning.
- **(B) `replace-by-topic` for fact body + `record-fragment` for surface fragments** — finer-grained but adds a second tool surface.
- **(C) New `refine-fragment` tool** — explicit refinement primitive that carries diff metadata; engine validates that the refinement is structurally compatible with the original (no surface change, no classification change, no fragment-id change).

(C) is most defensive; (A) is simplest. Recommendation: start with (A), add (C) if instrumentation shows refinements are accidentally changing surface/classification.

### Q2 — Should claudemd-author also get refinement?

claudemd-author brief is Zerops-free by construction. CLAUDE.md is `claude /init`-shape. Reference shape is straightforward. Probably skip — refinement target is the codebase-content output (IG / KB / yaml comments) where reference comparison adds the most value.

### Q3 — Should refinement see env-content output too?

Tier intros + import.yaml comments are env-content outputs. The fresh-evaluator pass typically grades these; reference shape is well-defined per spec Surface 2 + Surface 3. Yes — refinement scope should span all stitched output, not just codebase content.

### Q4 — Refinement-time citation enforcement: heuristic or strict?

Citation Map is a known list of topics × guide IDs. Refinement could either:

- **Heuristic**: scan body for any topic-shaped phrase that matches Citation Map; if matched and inline cite missing, refine.
- **Strict**: require every body sentence whose topic is in Citation Map to name the guide.

Strict is more invasive (might rewrite well-written prose just to insert a guide name). Heuristic is more conservative (only refines when the topic is unambiguous). Start heuristic; tune.

### Q5 — How does refinement interact with parent recipe context?

When `parent != nil`, refinement should know the parent's surface content so it doesn't duplicate. Cross-recipe duplication is a separate validator (R-15-N era). Refinement should READ the parent's published content + skip refinements that would re-author parent material. Slot for tranche-4 implementation.

---

## §11. Retired noise (already cleaned during run-16 post-dogfood thread)

Documenting so run-17 doesn't relitigate or reintroduce.

- **Subdomain manual-enable gate** (`verify-dogfood-no-manual-subdomain-enable`). The Makefile target + script were deleted. Source code at [`internal/tools/deploy_subdomain.go:13-17`](../../../internal/tools/deploy_subdomain.go) explicitly names manual `zerops_subdomain action=enable` as a "valid recovery path". The gate codified an invariant the source disclaimed. R-15-1 framing is retired across the run-16 ANALYSIS / CONTENT_COMPARISON.
- **`verify-claudemd-zerops-free` gate**. Also deleted. claudemd-author brief is Zerops-free by construction; the gate was a downstream symptom-detector for an upstream defect (R-16-1 — scaffold misdirected to author CLAUDE.md fragments under gate pressure). Once R-16-1 closes (§5.5), the gate's failure mode no longer exists.
- **Tier-promotion narration in tier yaml comments**. Neither reference recipe writes "promote to tier N when…" sentences. The writer-spec line + `editorial-review/single-question-tests.md` "outgrow this tier" question were edited out. [docs/spec-content-surfaces.md](../../spec-content-surfaces.md) Surface 3 + Surface 2 cleaned. (Note: edits landed in v2 atom tree; need re-doing in v3 — see Tranche 1.)
- **CLAUDE.md fixed-template** (3-section Zerops-flavoured). Replaced with `claude /init`-shape contract per [run-16-readiness §15](run-16-readiness.md#L1770). Spec section 6 rewritten. Run-16 actually shipped to the new contract; the readiness doc's Tranche 7 commit 1 spec rewrite landed during the post-dogfood cleanup thread.

---

## §12. Triple-confirmation checklist for the fresh instance

For the fresh Opus instance reading this doc to verify accuracy. Each item names the verification command + the expected match.

### §12.1 What run-16 dogfood actually shipped (§3)

- [ ] `ls /Users/fxck/www/zcp/docs/zcprecipator3/runs/16/` — confirm `apidev/`, `appdev/`, `workerdev/`, `environments/`, `SESSION_LOGS/`, `ANALYSIS.md`, `CONTENT_COMPARISON.md`, `TIMELINE.md`, `README.md`.
- [ ] `wc -l docs/zcprecipator3/runs/16/environments/facts.jsonl` — confirm 67 facts (or close — count varies slightly by what gets recorded mid-run).
- [ ] `grep -c '"kind":"porter_change"' docs/zcprecipator3/runs/16/environments/facts.jsonl` — confirm 36 porter_change facts.
- [ ] `grep -c '"why":""' docs/zcprecipator3/runs/16/environments/facts.jsonl` — confirm 0 (every porter_change has rich Why content).
- [ ] Read [docs/zcprecipator3/runs/16/ANALYSIS.md](../runs/16/ANALYSIS.md) — confirm honest grade 8.0/10, R-16-1 as load-bearing finding.
- [ ] Read [docs/zcprecipator3/runs/16/CONTENT_COMPARISON.md](../runs/16/CONTENT_COMPARISON.md) — confirm per-surface grades + non-misses §9.

### §12.2 Engine-emit hardcoding (§4.1)

- [ ] Read [internal/recipe/engine_emitted_facts.go](../../../internal/recipe/engine_emitted_facts.go) — confirm `classBFacts`, `classCUmbrellaFact`, `perServiceShells` exist with hardcoded Why prose + CandidateHeading.
- [ ] `grep -n "bind-and-trust-proxy\|sigterm-drain\|own-key-aliases" internal/recipe/engine_emitted_facts.go` — confirm topic IDs at lines 41, 55, 95.
- [ ] Confirm apidev's IG cap-vs-shells math: 1 (Class A from yaml) + 1 (bind) + 1 (sigterm) + 1 (own-key-aliases) + 5 (per-managed-service) = 8 shells. IG cap is 5. (Run-15 + run-16 prep doc both name the cap; verify in [internal/recipe/surfaces.go::surfaceContracts](../../../internal/recipe/surfaces.go).)

### §12.3 Brief delivery is pointer-based (§4.2)

- [ ] Read [internal/recipe/briefs_content_phase.go::BuildCodebaseContentBrief](../../../internal/recipe/briefs_content_phase.go) — confirm composer atom inclusion list. Look for any `embedSpec()` or `referenceExcerpts()` call. Expected: none.
- [ ] Read [internal/recipe/content/briefs/codebase-content/synthesis_workflow.md](../../../internal/recipe/content/briefs/codebase-content/synthesis_workflow.md) — confirm zero verbatim KB/IG excerpts from laravel-jetstream / laravel-showcase.
- [ ] `grep -rn "Read /Users/fxck/www/zcp/docs/spec-content-surfaces.md\|spec-content-surfaces.md" internal/recipe/content/briefs/codebase-content/` — expected: presence (the pointer pattern).

### §12.4 Validator stack (§4.3)

- [ ] Read [internal/recipe/slot_shape.go::checkCodebaseKB](../../../internal/recipe/slot_shape.go) — confirm structural-only check (no semantic stem-shape).
- [ ] Read [internal/recipe/validators_codebase.go::validateCodebaseKB](../../../internal/recipe/validators_codebase.go) — confirm bans `**symptom**:` triple but no positive symptom-first heuristic.
- [ ] Read [internal/recipe/validators_kb_quality.go](../../../internal/recipe/validators_kb_quality.go) — confirm V-2/V-3/V-4 are the only quality validators registered.

### §12.5 `CodebaseGates` contradiction (§4.4)

- [ ] Read [internal/recipe/gates.go::CodebaseGates](../../../internal/recipe/gates.go) — confirm `codebase-surface-validators` runs at scaffold complete-phase.
- [ ] Read [internal/recipe/content/phase_entry/scaffold.md](../../../internal/recipe/content/phase_entry/scaffold.md) lines 152-157 — confirm "do NOT author" teaching.
- [ ] Confirm contradiction: gate demands content; atom teaches no-content.

### §12.6 `porter_change.Why` truncation (§4.5)

- [ ] `grep -n "truncate(f.Why\|truncate(.*Why" internal/recipe/briefs_content_phase.go` — confirm `truncate(f.Why, 120)` call at ~line 330.
- [ ] Sample one run-16 porter_change Why length: `head -1 docs/zcprecipator3/runs/16/environments/facts.jsonl | jq -r '.why | length'`. Expected: 250+ chars.

### §12.7 v2 atom tree references (§4.6)

- [ ] `grep -rn "internal/content/workflows" --include="*.go"` — confirm 13 references across `internal/workflow/`, `internal/analyze/`, `tools/lint/`, `internal/content/`.
- [ ] `grep -rn "internal/content/workflows" internal/recipe/` — confirm 0 references (recipe engine is v3-only).

### §12.8 Reference recipes (§3.3)

- [ ] `ls /Users/fxck/www/laravel-jetstream-app/ /Users/fxck/www/laravel-showcase-app/` — confirm both apps repos exist.
- [ ] `ls /Users/fxck/www/recipes/laravel-jetstream/ /Users/fxck/www/recipes/laravel-showcase/` — confirm both recipe trees exist.
- [ ] Read [/Users/fxck/www/laravel-showcase-app/README.md:347](../../../../laravel-showcase-app/README.md) — sample 7 KB bullets in `### Gotchas` section.

### §12.9 Retired noise verification (§11)

- [ ] `grep -n "verify-dogfood-no-manual-subdomain-enable\|verify-claudemd-zerops-free" Makefile` — confirm zero matches (gates removed).
- [ ] `ls scripts/verify-dogfood-subdomain.sh scripts/verify-claudemd-zerops-free.sh 2>&1` — confirm both files don't exist.
- [ ] Read [docs/spec-content-surfaces.md](../../spec-content-surfaces.md) §Surface 6 — confirm `claude /init`-shape contract (not the old 3-section Zerops-flavoured template).
- [ ] Read [docs/spec-content-surfaces.md](../../spec-content-surfaces.md) §Surface 3 "Belongs here" — confirm no "Cross-tier promotion context" bullet (cleaned up).

### §12.10 Triple-verification result format

The fresh instance reports back with:

```
## Triple-verification report

§12.1 Run-16 dogfood: PASS / FAIL with notes
§12.2 Engine-emit hardcoding: PASS / FAIL with notes
§12.3 Brief delivery pointer-based: PASS / FAIL with notes
§12.4 Validator stack: PASS / FAIL with notes
§12.5 CodebaseGates contradiction: PASS / FAIL with notes
§12.6 Why truncation: PASS / FAIL with notes
§12.7 v2 atom tree: PASS / FAIL with notes
§12.8 Reference recipes: PASS / FAIL with notes
§12.9 Retired noise: PASS / FAIL with notes

## Anomalies found
<list of any discrepancies between this guide and the actual artifacts>

## Recommended corrections to the guide
<list of changes the guide needs before implementation starts>

## Confidence level for run-17 implementation
<HIGH / MEDIUM / LOW with reasoning>
```

If all PASS and no anomalies surface, the guide is implementation-ready. If anomalies surface, this guide gets corrected before tranche 0 ships.

---

## §13. What this guide deliberately does NOT cover

- Run-17 readiness plan with tranche dates / commit titles — that's the next document, derived from this prep.
- Refinement sub-agent's exact `BuildRefinementBrief` LoC — implementation determines structure; risks flagged in §8 but not benchmarked.
- Per-recipe variation in refinement scope (e.g. small-shape recipe with no managed services may not need showcase-tier-supplement injection) — implementation atom encodes the conditionals.
- Whether the analyzer pipeline (`internal/analyze/runner.go`) survives v2 deletion or gets replaced by a v3-aware analyzer — open question for Tranche 7 implementation prep.

---

## §14. How a fresh instance uses this doc

Read order: §1 (orient) → §2 (predecessor intent) → §3 (artifact-grounded current state) → §4 (root causes) → §5/§6 (proposed shape) → §12 (verify state vs claims).

The doc is **verifiable, not aspirational**. Every claim about run-16 has a file:line cite or an artifact reference. Every proposed change has an LoC estimate + tranche placement.

When investigating a new defect (run-17 dogfood or beyond):

- The §4 root-cause taxonomy (engine-emit assumption, pointer-based delivery, gameable validator, `CodebaseGates` contradiction, truncation render bug, v2/v3 split, no post-finalize quality pass) is the diagnostic framework.
- The §3.1 / §3.2 / §3.3 split (worked / didn't / quality misses) is the artifact-walk template.
- The §6.2 refinement sub-agent design is the architectural primitive for "the engine should catch this kind of drift before sign-off."

---

## §15. Next steps after triple-confirmation

1. Fresh instance reports back per §12.10.
2. Address all anomalies / corrections (this doc gets edited before code lands).
3. Write `run-17-readiness.md` with tranche dates, exact commit titles, per-tranche risk-mitigation checkpoints.
4. Tranche 0 ships first (free quality wins; can land independent of the rest).
5. Tranches 1-6 ship in order; tranche 4 (refinement sub-agent) has the most degrees of freedom — implementation prep should resolve §10 open questions before code lands.
6. Tranche 7 sign-off on run-17 dogfood (small-shape first, nestjs-showcase showcase-shape after).
