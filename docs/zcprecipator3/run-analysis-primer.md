# Run-analysis primer for a fresh instance

> **Read this top-to-bottom before the run completes.** It establishes
> the worldview, points at the canonical reference docs, describes the
> artifacts you'll be analyzing, gives the analysis methodology, and
> shows the simulation tooling you'll need if a fix is required.
> When the run finishes you should be ready to start reading artifacts
> immediately.

## 1. Mission

A zcprecipator3 dogfood run is about to complete (or just completed).
Your job: analyze the published artifacts against the golden-standard
reference recipes + the spec contract, identify what landed well and
what didn't, and recommend whether the recipe is publishable or needs
another reauthor pass. If it needs another pass, name the specific
class of issue and the engine-side closure (validator extension, atom
edit, refinement scope tweak — see §6 simulation infrastructure).

You are not authoring the recipe. You are reviewing it the same way an
editor reviews a draft against the house style guide.

## 2. Required reading (canonical docs, ~30 minutes)

Read in this order:

1. **`docs/zcprecipator3/system.md`** — engine north star. Top-to-
   bottom. Internalize: §1 (what v3 produces), §3 (5-phase sequence),
   §4 (TEACH/DISCOVER line — the discriminator for "is this a real
   engine bug or a one-off"), §5 (three knowledge channels).

2. **`docs/spec-content-surfaces.md`** — authoritative surface
   contract. Per-surface line/item caps, classification × surface
   compatibility table, anti-patterns. The §200-218 (Surface 5 KB),
   §283-302 (Surface 7 zerops.yaml), §770-790 (IG one-mechanism),
   §380 (self-inflicted), §216 (citation shape) sections come up
   constantly in analysis.

3. **`docs/zcprecipator3/content-research.md`** — empirical reference
   comparison. Tells you why laravel-showcase is the density floor and
   why laravel-jetstream is the voice floor. Skim §1 (length budgets),
   §3 (universals), §6 (worked routing examples).

4. **`docs/zcprecipator3/plans/run-18-prep.md`** — most recent
   readiness plan. §1.2 lists the run-17 leak classes; §3 lists the
   engine fixes; §5 describes the simulation loop. Reading this tells
   you exactly what to look for in the current run — these are the
   classes the run was supposed to close.

5. **`CHANGELOG.md`** — read only the most recent entry. Don't
   internalize older history; it's run-archive.

After reading, you should be able to answer (without re-grepping):

- What's a "porter" and why does it matter?
- What's the difference between a TEACH-side rule (engine emits by
  construction) and a DISCOVER-side rule (per-run agent learning)?
- What's the difference between Surface 4 (IG) and Surface 5 (KB)?
- What classification routes a fact to "discard"?

If those don't land, re-read system.md §4 + spec §classification.

## 3. Anatomy of a run

A completed run lands at `docs/zcprecipator3/runs/<N>/`:

```
runs/<N>/
├── README.md                          short run description
├── TIMELINE.md                        phase-by-phase narrative + wall time
├── <codebase>dev/                     one per codebase (e.g. apidev, appdev, workerdev)
│   ├── README.md                      apps-repo README — IG + KB (porter-facing)
│   ├── CLAUDE.md                      apps-repo CLAUDE.md — porter dev guide
│   ├── zerops.yaml                    runtime config + block comments
│   ├── package.json / composer.json   framework manifest
│   └── src/                           application code
└── environments/                      recipe-repo content (engine outputRoot)
    ├── plan.json                      Plan struct (codebases, services, tier, research)
    ├── facts.jsonl                    full FactsLog snapshot — every recorded fact
    ├── README.md                      root recipe README (navigation page)
    └── <N> — <name>/                  six tier folders (0..5)
        ├── import.yaml                click-deploy bytes
        └── README.md                  per-tier card description (~8 lines)
```

Plus session logs (only if you need to debug agent behavior — usually
the artifacts above are sufficient):

```
runs/<N>/SESSION_LOGS/
├── main-session.jsonl                 main agent's full transcript
└── subagents/
    ├── agent-<id>.jsonl               sub-agent transcripts (scaffold, feature,
    │                                  codebase-content, env-content,
    │                                  claudemd-author, refinement)
    └── agent-<id>.meta.json           agent kind + description
```

**Important:** if `plan.json` is absent, the run pre-dates run-18 (the
WritePlan engine change that persists plan.json was added then). For
older runs, plan can be reconstructed from the `update-plan` event in
`main-session.jsonl` via `jq` (see `runs/17/environments/plan.json`
for the shape after a one-shot backfill).

## 4. The quality bar (golden standards)

Two reference recipes set the floors:

- **`/Users/fxck/www/laravel-showcase-app/`** + 
  **`/Users/fxck/www/recipes/laravel-showcase/`** — early-flow output.
  The **density floor**: every comment carries mechanism + reason, KB
  stems are symptom-first, IG one-mechanism-per-item.

- **`/Users/fxck/www/laravel-jetstream-app/`** + 
  **`/Users/fxck/www/recipes/laravel-jetstream/`** — human-authored.
  The **voice floor**: friendly authority, inline doc URLs (not slug
  citations), `> [!CAUTION]` callouts, honest `# FIXME` comments.

Per-surface caps you'll measure against (from spec):

| Surface | Line/char cap | Item cap | Voice |
|---|---|---|---|
| Root README | ≤35 lines | 6 tier links + footer | navigation only |
| Tier README extract | ≤350 chars | 1-2 sentences | card description |
| Tier README total | ≤10 lines | n/a | n/a |
| Tier import.yaml comments | **≤40 indented `#` lines per tier**, ≤8 per block | 3-5 lines per service block | mechanism + reason, declarative |
| Apps-repo README intro extract | ≤500 chars | 1-3 sentences | n/a |
| Apps-repo IG | n/a | **4-5 items including engine-emitted IG #1** | imperative heading, one mechanism per item |
| Apps-repo KB | n/a | **5-8 bullets** | symptom-first stem, 2-4 sentence body |
| CLAUDE.md | ≤80 lines | 2-4 H2 sections | porter dev guide, NO Zerops content |
| Apps-repo zerops.yaml comments | ≤6 lines per directive group | n/a | causal block comments |

## 5. Run-17 baseline anti-patterns to verify closed

The current run was preceded by run-17 which shipped these classes.
Your analysis confirms each is closed; if any survived, that's a
signal.

**This list is necessary, not sufficient.** Every run produces NEW
anti-patterns the prior plan didn't anticipate. Detecting "did the
old regex targets disappear?" misses the more interesting failure:
the agent obeyed the rules and the output is still bad (see §12).
Treat §5 as the floor for what to check, not the ceiling.

- **Tier yaml volume**: 100-135 indented `#` lines per tier (vs spec
  ≤40, reference ~22). Run-18's beefed-up `per_tier_authoring.md`
  atom + density target should close this.

- **Self-inflicted KB bullets**: "That's intentional", "this is
  correct", "Not a problem", "Self-deploy wipes /var/www" (the
  recipe's own deploy decision framed as a porter trap). The
  `validateAuthoringDiscipline` validator catches the lexical
  patterns at record-fragment time.

- **Recipe-internal scaffold references in KB**: UI panels/tabs/
  widgets in stems, `+server.js`/`+page.svelte` paths, `/api/[...path]`
  proxy noun. Validator catches.

- **Slug-as-noun citations**: `See: foo guide.`, `see \`foo\``,
  `cited in the \`foo\``, `Per \`foo\``. Validator catches.

- **IG fusion**: multiple managed services cited in one slot's body.
  Validator catches via plan-scoped hostname check.

- **Factual fabrication**: e.g. "NATS HA = JetStream-backed streams"
  (Zerops's HA broker is core NATS in HA, no JetStream). Validator
  CANNOT catch this — refinement is the closer.

- **Tier-promotion narratives**: "Promote to tier N+1", "Outgrow",
  "Upgrade from tier N to N+1". Spec §108 forbids; validator does
  NOT enforce; refinement should reshape.

- **Cross-tier / cross-service repetition**: same OOM teaching on
  every service at tier 3, same `dev/stage` framing on app at tiers
  0 and 1. Brief atom's cross-tier diff principle should reduce; if
  it didn't, refinement is the closer.

## 6. Analysis methodology

For each surface, answer the same five questions:

1. **Verdict** — pick one:
   - "Below run-17 floor" (regressed somehow)
   - "Matches run-17 floor" (no real lift)
   - "Matches reference density" (at the bar)
   - "Above the bar" (better than reference)

2. **Anti-pattern hits** — for each class in §5, file:line citations
   when found, or "0 instances" when clean.

3. **Gaps vs reference** — read the matching surface in laravel-
   showcase + laravel-jetstream. Where does the reference do
   something specific that this run misses? Cite both sides with
   file:line.

4. **What lifted vs run-17** — read the matching `runs/17/` surface.
   Concrete improvements with file:line citations on both sides.

5. **Ship/no-ship** — could this be published as-is, or does another
   reauthor pass close it? If no-ship, name the specific class.

Surfaces to cover (the Cartesian product of codebases × surface
plus shared surfaces):

- Root README, root intro extract
- Per-tier README (×6) + per-tier import.yaml comments (×6)
- Per-codebase README intro, IG slots #2-#5, KB, zerops-yaml-comments
  blocks, CLAUDE.md (×N codebases)

For deterministic anti-pattern checks, use the simulation tool's
`validate` subcommand:

```bash
go run ./cmd/zcp-recipe-sim validate -dir <sim-dir>
```

`<sim-dir>` is a simulation directory previously populated by `emit`
+ Agent dispatches. The validator runs `checkSlotShapeWithPlan` over
every authored fragment under `<sim-dir>/fragments-new/<host>/`
(filename → fragment id via `__` → `/`).

## 7. End-to-end simulation infrastructure

If your analysis identifies a residual issue that requires an engine
fix (validator regex extension, brief atom edit, etc.), use the
`zcp-recipe-sim` tool to test the fix against the frozen run corpus
before any re-dogfood. The tool runs three subcommands matching the
phases of the simulation loop:

```bash
# 1. Edit the atom / brief composer / validator (whatever your fix is)
# 2. Rebuild
go build ./...

# 3. Stage frozen scaffold output + emit dispatch prompts.
#    Reads:  runs/<N>/environments/{plan.json,facts.jsonl}
#            runs/<N>/<host>dev/zerops.yaml (comments stripped on copy)
#    Writes: simulations/<M>/environments/{plan.json,facts.jsonl}
#            simulations/<M>/<host>dev/zerops.yaml (bare)
#            simulations/<M>/briefs/{<host>,env}-prompt.md
#            simulations/<M>/fragments-new/{<host>,env}/  (empty)
#    The plan.json's SourceRoot fields are rewritten to point at the
#    staged simulation paths so stitch reads from the staged tree.
go run ./cmd/zcp-recipe-sim emit \
  -run docs/zcprecipator3/runs/<N> \
  -out docs/zcprecipator3/simulations/<M>

# 4. Dispatch one Agent per prompt, in parallel via the Agent tool.
#    Each agent reads simulations/<M>/<host>dev/zerops.yaml +
#    simulations/<M>/environments/{plan.json,facts.jsonl}, writes
#    fragments to simulations/<M>/fragments-new/<host>/ as markdown
#    files (filename = fragmentId with `/` → `__`).

# 5. Stitch fragments into the simulated recipe shape.
#    Calls the canonical engine assembles (AssembleRootREADME,
#    AssembleEnvREADME, AssembleCodebaseREADME, AssembleCodebaseClaudeMD,
#    EmitDeliverableYAML) and the new WriteCodebaseYAMLWithComments
#    that strips on-disk comments and re-injects the recorded
#    zerops-yaml-comments fragments. Output mirrors the runs/<N>/
#    layout so the simulation can be diffed against the baseline.
go run ./cmd/zcp-recipe-sim stitch -dir docs/zcprecipator3/simulations/<M>

# 6. Run slot-shape refusals over every fragment.
go run ./cmd/zcp-recipe-sim validate -dir docs/zcprecipator3/simulations/<M>

# 7. Diff against reference recipes for quality verdict; ship to
#    codex (codex:codex-rescue agent) for an independent read.
```

The dispatch prompts emitted by `emit` are **byte-identical** to what
the engine emits in production via
`zerops_recipe action=build-subagent-prompt`, plus a 20-line
`<replay-adapter>` prefix that redirects record-fragment to file-write.
**Atom, brief, and validator changes land identically in simulation
and production** — you can iterate quickly without running an A-to-Z
dogfood.

## 8. When to escalate to codex

Use the `codex:codex-rescue` subagent when:

- You have a borderline judgment call (e.g. "is this bullet self-
  inflicted, or is it a legit platform trap?") and want a second
  read
- You want to verify your verdict on a specific surface against
  someone who hasn't seen your analysis
- You're about to recommend an engine fix and want pressure-testing
  on the regex / atom edit

Hand codex:
- The artifacts (run dir or simulation output)
- The golden-standard references (laravel-showcase + laravel-
  jetstream paths)
- The spec section
- Your specific question

Codex's strength is independent reads on borderline cases. Don't ask
it to do the whole analysis — your read + codex's read together is
better calibrated than either alone.

## 9. Final report shape

Structure your report as:

```
# Run-<N> analysis

## Pipeline status
- Phases that ran cleanly
- Phases that surfaced violations
- Refinement: did it fire? Did it close residuals?

## Quality lift over run-17 (per anti-pattern class)
| Class | Run-17 instances | Run-<N> instances |
|---|---|---|
| Slug citations | ~16 | <count> |
| Self-inflicted KB bullets | 2 | <count> |
| ...

## Per-surface verdict
For each surface in §6 list — verdict + anti-pattern hits + gaps + lift
+ ship/no-ship.

## Residuals (if any)
For each class still present, file:line citations + recommended
closure (validator extension / brief atom edit / refinement scope
tweak / fact-corpus addition).

## Ship/no-ship verdict
The decisive question. If no-ship, three targeted fixes ranked by
porter impact.
```

## 10. Antipatterns in YOUR analysis

- **Don't optimize for line counts.** Tier 0 might legitimately ship
  more lines than tier 4 because it's the inheritance baseline.
  Volume is a side-effect of clarity-per-line, not the goal.
- **Don't trust the agent's self-report.** Sub-agents claim "all
  close criteria PASS" in their final messages. Verify by reading
  the actual output and running the validator.
- **Don't cite line counts as quality verdicts.** Cite specific
  prose that does or doesn't teach a non-obvious choice. Reference
  the matching reference-recipe block when arguing "matches density".
- **Don't drift into engineering refactors.** If a residual issue
  needs an engine fix, name it and recommend it; don't implement it
  unless asked. The user's call is whether to iterate or ship.
- **Don't extrapolate platform behavior.** If a comment claims
  "JetStream-backed streams" or "exact-once delivery", verify against
  the citation map / managed-service knowledge before validating it.
  Plausible-sounding claims that aren't fact-attested are exactly
  the residual class refinement targets.

---

## 11. First Pass Order

Start with the machine signals, then read like an editor. Run the
validator first and save the exact distinction between hard failures,
non-blocking notices, and clean phase closure. Treat `ok:true` as
"the engine allowed publication," not as "the prose is good." Read
`TIMELINE.md` next for validator rounds, refusals, skipped
refinement, and any place the agent held a notice. Then read
`plan.json` and `facts.jsonl` before the stitched surfaces. For each
fact, compare `candidateClass`, `candidateSurface`, `citationGuide`,
phase, and scope to the final README or yaml location. If a fact
marked discard, library metadata, scaffold decision, browser
verification, or recipe-internal behavior appears as KB, call that
out even if the bullet has a symptom-first stem and cites a guide.

After that, read the published prose surface-by-surface in this
order: root/tier READMEs, tier import comments, codebase README intro
and IG, KB, runtime `zerops.yaml`, then CLAUDE.md. Finish with
reference side-by-side. Use the references to judge density and voice
only after you have classified whether each item belongs on the
surface at all.

## 12. Rule-Following Bad Output

Disagree with the agent when the artifact copies the rule shape
without satisfying the reader test. A KB bullet can be symptom-first
and still be bad if the symptom comes from this recipe's panel,
route file, helper, package metadata, or authoring recovery. An IG
item can contain a useful diff and still be fused if one heading
teaches multiple mechanisms or multiple managed services. A yaml
comment can contain "because" and still be filler if it narrates
the field, hedges toward tier promotion, or invents platform
doctrine.

Watch for mid-authoring drift: surface bloat near the cap, "if your
workload…" over-hedging that becomes promotion advice, guide slugs
used as porter citations, and structure mimicry where every item has
the right wrapper but no portable Zerops lesson. When validator and
prose conflict, let the spec and human read win; record the
validator behavior as a separate engine issue.

## 13. Fact-provenance check + when to consult `zerops_knowledge`

Before judging any surface's prose, walk through `facts.jsonl` once
and build a mental index: which fact ended up where, and does the
landing surface match the fact's `candidateClass` × spec
compatibility table?

Key fields per fact (see `internal/recipe/facts.go::FactRecord`):

- `topic` — author-assigned key; should be unique-ish per fact
- `phase` — when it was recorded (scaffold / feature / refinement)
- `scope` — where in the codebase the fact applies (e.g.
  `api/code/main.ts`, `worker/runtime/zerops.yaml`)
- `candidateClass` — `platform-invariant` / `intersection` /
  `framework-quirk` / `library-metadata` / `scaffold-decision` /
  `operational` / `self-inflicted`
- `candidateSurface` — `CODEBASE_KB` / `CODEBASE_IG` / `CODEBASE_YAML` /
  `CLAUDE_MD` / `DISCARD`
- `candidateHeading` — proposed stem/heading for the surface
- `citationGuide` — the guide id the agent attached (must appear in
  the body if non-empty per spec §216)

The provenance check fires when:

- A `candidateClass=library-metadata` or `candidateClass=self-inflicted`
  fact appears as a KB bullet anyway. Discard means discard.
- A `candidateClass=scaffold-decision` fact lands as KB instead of as
  a zerops.yaml comment (Surface 7) or IG with diff.
- A fact's `citationGuide` is set but the corresponding surface body
  doesn't *name the guide in prose* (run-15 §F.5 cite-by-name rule).
- A KB body claims platform behavior that doesn't appear in any fact's
  `why` field AND isn't covered by an embedded knowledge atom. That's
  the fabrication class refinement targets.

When you spot a fabricated platform claim (e.g. "NATS HA = JetStream
streams", "exact-once delivery via queue groups", "VXLAN guarantees X"),
verify against the citation map's known guides. If the engine has the
guide embedded, the actual mechanism should be queryable via
`zerops_knowledge query=<topic>`. If you can't get a live MCP session,
read the embedded knowledge corpus directly under
`internal/knowledge/...` (or trace what `CitationMap` references in
`internal/recipe/citations.go`).

The fast tell: a comment that explains a NATS / Postgres / Valkey /
Meilisearch behavior in detail without an attesting fact in
`facts.jsonl`. Plausible-sounding extrapolation is the failure mode.

---

When the run completes, start with `runs/<N>/README.md` for the
narrative, then dive into one surface at a time using §6 methodology.
Keep the spec + reference recipes open in adjacent buffers; the
analysis is a side-by-side comparison, not a memorized rubric.
