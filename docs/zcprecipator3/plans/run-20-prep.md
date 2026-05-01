# Run-20 prep — fixes derived from run-19 root-cause analysis

**Source:** `docs/zcprecipator3/runs/19/ANALYSIS.md` + codex agentId
`a0cad39e93fe0e3cf` (root-cause verification of H1..H5).

**Operating principle (revised after codex Q1 review):** Phases 4-7
(codebase-content, env-content, finalize, refinement) are
deterministic functions of `(plan.json, facts.jsonl, bare
zerops.yaml per codebase, code in src/**, parent context,
mountRoot)`. **The current `zcp-recipe-sim` driver does NOT stage
all of these inputs:**

- `src/**` is not staged — `cmd/zcp-recipe-sim/emit.go:65-87` only
  creates `<host>dev/` directories and writes the stripped
  `zerops.yaml`. The codebase-content brief tells the replayed agent
  to glob `<SourceRoot>/src/**`
  ([briefs_content_phase.go:124-128](../../../internal/recipe/briefs_content_phase.go#L124-L128)),
  so replay can't read what isn't there.
- `Parent` and `MountRoot` are passed as nil (`emit.go:112,133`),
  but production sessions have them and they affect the brief
  ([briefs_content_phase.go:130,241](../../../internal/recipe/briefs_content_phase.go#L130)).
- Refinement isn't emitted — `emit.go:101,124` only emits
  codebase-content + env-content prompts.
- Plan SourceRoot fields are mutated by `emit` (rewritten to staged
  paths) — not "frozen verbatim."

**So sim is deterministic OVER STAGED INPUTS, but the staged inputs
are incomplete today.** Run-20 includes sim driver extensions
(prerequisite work, see S1+S3+S4 below) to close those gaps.

**Once those gaps close, the verification claim becomes:** every
phase 4-7 engine + content + brief fix is sim-verifiable. Scaffold-
phase brief changes still need prompt-eval or a real dogfood.
**CLAUDE.md is treated as run-20 debt, not closed** — it's flagged
explicitly rather than out-of-scope (the 4-retry slot-shape problem
on worker is real, see D1 below).

This doc lists every fix the run-19 analysis surfaced, the layer it
lands in, and the verification path.

---

## Verification matrix legend

| Tier | Meaning |
|---|---|
| **sim** | Verifiable in `zcp-recipe-sim` (engine, brief composer, phases 4-7 sub-agent output) |
| **unit** | Pure go test, no agent dispatch (validators, gate predicates, regex extensions) |
| **patched-sim** | Verifiable in sim with a manual patch to the staged input (e.g. SPA `base: static` rewrite of staged `appdev/zerops.yaml`) |
| **prompt-eval** | One-shot scaffold sub-agent dispatch against a synthesised minimal plan, asserts decision shape |
| **dogfood** | Real end-to-end run; only when nothing cheaper works |

---

## E1. Engine — `AssembleCodebaseREADME` double-injects yaml comments

**Symptom:** every codebase IG #1 inline yaml has block-level
repetition (`runs/19/apidev/README.md:35-46`, `:55-66`, `:165-174`;
`appdev/README.md:37-48`, `:52-63`; `workerdev/README.md:35-46`,
`:49-60`). On-disk `zerops.yaml` files are clean.

**Root cause:** [`internal/recipe/assemble.go:144`](../../../internal/recipe/assemble.go#L144)
calls `injectZeropsYamlComments` on the on-disk yaml without
stripping prior comments. `WriteCodebaseYAMLWithComments` does
strip-then-inject. Across multiple `stitchCodebases` calls
(`complete-phase=scaffold`, `complete-phase=feature`,
`stitch-content` at finalize), the on-disk yaml accumulates engine
`# #` blocks and `AssembleCodebaseREADME` re-injects them above the
same directives. `insertCommentAtBlock` has no dedupe — pure splice
above the matched directive every call.

**The strip itself is a band-aid for a violated contract.** The
intended contract is: scaffold writes bare yaml; codebase-content
records comment fragments; engine stamps comments in **once**. The
strip exists because (a) scaffold leaks comments (see E4) and (b)
the assembler reads from the same mutating disk file the writer
writes to.

**Fix (recommended — one-line strip, picked after codex Q2 review):**

```go
// internal/recipe/assemble.go:144 (current)
yamlBody = injectZeropsYamlComments(yamlBody, plan.Fragments, hostname)

// proposed
yamlBody = injectZeropsYamlComments(stripYAMLComments(yamlBody), plan.Fragments, hostname)
```

Matches the existing run-19 fix shape in `WriteCodebaseYAMLWithComments`
(strip-then-inject), keeps both consumers of the in-memory injected
yamlBody (IG #1 only — `assemble_run16_test.go:11,60,87` pins
injection behavior, not write ordering).

**Architectural alternative considered + REJECTED.** Reordering
`writeCodebaseSurfaces` to run `WriteCodebaseYAMLWithComments` before
`AssembleCodebaseREADME` and deleting the inject at assemble.go:144
would also work — but it ALSO requires reordering
[`cmd/zcp-recipe-sim/stitch.go:106-125`](../../../cmd/zcp-recipe-sim/stitch.go#L106-L125)
which has the same README-before-yaml ordering. If the production
reorder lands without the sim reorder, sim and prod diverge silently.
The one-line strip fallback dodges that risk entirely.

**Verification:** **sim** + **unit**.

- Unit test in `assemble_test.go`: read a yaml that already has
  engine `# #` blocks above directives → `AssembleCodebaseREADME`
  returns body with each block exactly once.
- Sim driver extension S1 below runs stitch twice and byte-diffs
  round 2 vs round 1. With the fix, identical. Without it, round 2
  has doubled blocks.

---

## E2. Engine — `injectZeropsYamlComments` is non-idempotent

**Status:** subsumed by E1's architectural fix. If E1 fallback path
is taken instead, add idempotence inside `insertCommentAtBlock`
(`internal/recipe/assemble.go:289-302`): before splicing the comment,
check whether the immediately-prior lines are the same comment block.
If they are, skip. Pin with the same unit test as E1.

**Verification:** **unit**.

---

## C1. Content — NATS atom not wired into env-content brief

**Symptom:** tier 0 (`environments/0 — AI Agent/import.yaml:100`)
fabricates "JetStream persists messages across restarts; in-memory
subjects don't"; tier 5 (`environments/5 — Highly-available
Production/import.yaml:83`) fabricates "NATS HA — three-node
JetStream cluster with quorum-replicated streams." This recipe
uses core NATS pub/sub with queue groups; no JetStream. Zero attesting
facts in `runs/19/environments/facts.jsonl`.

**Root cause:** the run-19 NATS atom rewrite landed in the
codebase-content brief composer (`briefs_content_phase.go`) but not
in the env-content brief composer. The env-content sub-agent did not
receive the atom that distinguishes core NATS pub/sub from JetStream
streams; it extrapolated from training-data NATS knowledge.

**Fix:**

1. Identify the env-content brief composer (likely
   `internal/recipe/briefs.go` or a sibling — search for the
   `env-content` phase brief assembly).
2. Embed the same NATS-themed atom that codebase-content gets, OR
   factor the NATS shape into a shared principle
   (e.g. `internal/recipe/content/principles/nats-shapes.md`) that
   both phases pull from.
3. Add a record-time refusal in the env-content slot-shape validator:
   tier import-comment fragments containing `JetStream` /
   `quorum-replicated streams` / `durable consumer` tokens refuse
   unless plan-scoped facts attest JetStream usage (e.g. a
   `framework_quirk` fact with `topic=nats-jetstream-enabled`).

**Verification:** **sim**.

- Re-run env-content sub-agent against the run-19 corpus + new brief.
- Assert zero `JetStream` / `quorum-replicated` tokens in
  `simulations/<M>/fragments-new/env/env__N__import-comments__broker.md`
  for every N.
- Assert refusal fires when a test fixture seeds the same fabrication.

---

## C2. Content — cross-service URL pattern (workspace dual-runtime) absent from scaffold brief

**Symptom:** appdev/zerops.yaml uses `base: nodejs@22` + build-time
bake of `${apistage_zeropsSubdomain}` + `npx serve` — wrong on three
axes. The agent deferred the appstage cross-deploy to dance around
the chicken-and-egg (TIMELINE.md:102-105). The scaffold sub-agent's
chain of reasoning was correct but it was never told the right tool.

**Root cause (corrected from initial pass):** the canonical
workspace dual-runtime pattern (`STAGE_API_URL: https://apistage-${zeropsSubdomainHost}-{port}.prg1.zerops.app`
in project envs) is documented in `internal/content/workflows/recipe.md:534-561`
— **but `recipe.md` is the legacy zcprecipator2 doc and zcprecipator3
never reads it.** The teaching is orphaned. Worse, the scaffold sub-
agent's brief at
[`internal/recipe/content/briefs/scaffold/platform_principles.md:98-102`](../../../internal/recipe/content/briefs/scaffold/platform_principles.md#L98-L102)
ACTIVELY FORBIDS the workspace use case ("`${zeropsSubdomainHost}` …
**Use only inside finalize-phase tier yaml templates**").

**Fix (5-layer):**

1. **New principle** at `internal/recipe/content/principles/cross-service-urls.md`
   teaching the workspace dual-runtime pattern: `${zeropsSubdomainHost}`
   in project envs (`zerops_env project=true action=set …`); typed
   own-key alias in build/run envVariables; deploy-time independence
   (no api-must-deploy-first race).
2. **Edit** `briefs/scaffold/platform_principles.md:98-102` to stop
   forbidding the workspace use case. Reframe the literal-only rule
   as: "stays literal in **deliverable** tier yaml templates only;
   workspace project envs resolve the value at provision time."
3. **Embed** the new principle in the scaffold brief
   (`internal/recipe/briefs.go:183-199`).
4. **New atom** at `internal/content/atoms/themes-frontend-static.md`
   teaching SPA on `base: static` (Nginx) + the `STAGE_API_URL` build
   bake from project envs.
5. **Record-time refusal** at `internal/recipe/validators_codebase.go`:
   refuse a scaffold-recorded zerops.yaml that has `base: nodejs@22`
   + `start:` containing `npx ... serve` + `deployFiles:` containing
   `dist`. Single-pattern; targets the SPA mis-shape specifically.
6. **decisions/choose-frontend-runtime.md** sync (or new file) so
   the agent's research-phase decision tree reflects static-vs-dynamic
   for SPAs.

**Verification:**

| Layer | Tier |
|---|---|
| Principle / atom content quality | sim — re-run codebase-content + env-content with new content available |
| Brief embeds new principle | sim — assert `build-subagent-prompt` for scaffold returns content containing the workspace dual-runtime block |
| Record-time refusal | unit — fixture yaml triggers refusal |
| Scaffold sub-agent **uses** the new pattern | **patched-sim** for engine handling + **prompt-eval** for behavior |

The patched-sim path: hand-edit `simulations/<M>/appdev/zerops.yaml`
to the correct shape (`base: static`, project-env URL constants), run
codebase-content + env-content + finalize, assert all surfaces
correctly describe the static SPA + project-env pattern. This proves
phases 4-7 handle the right shape correctly.

The prompt-eval path: dispatch one scaffold-app sub-agent against a
synthesised plan (Svelte SPA on Vite, api on stage, no other
codebases) with the new brief, assert the produced `zerops.yaml`
shape. ~5 min runtime, no full project provisioning needed.

---

## C3. Content — bare-yaml prohibition orphaned

**Symptom:** scaffold sub-agents write `zerops.yaml` with embedded
`#` causal comments. First Write to `/var/www/apidev/zerops.yaml` in
`runs/19/SESSION_LOGS/subagents/agent-ada4fac1...` is 188 lines with
embedded comments. Engine strips them later (the on-disk yaml is
clean) but the violation pollutes the inline yaml in IG #1 if the
strip path doesn't run before the assembler (see E1).

**Root cause:** the bare-yaml prohibition lives in
`internal/recipe/content/briefs/scaffold/content_authoring.md:21-30`
but that atom is **no longer included** in the active scaffold brief
at [`briefs.go:183-199`](../../../internal/recipe/briefs.go#L183-L199).
The active phase entry (`phase_entry/scaffold.md:152-156`) does say
"no zerops.yaml block comments during scaffold" — but it competes
against the embedded `principles/yaml-comment-style.md`
(`briefs.go:191-199`) which teaches comment shape. Goldens cited as
references DO have rich `#` comments. Imitation pressure beats the
weak rule.

**Fix:**

1. Re-include `briefs/scaffold/content_authoring.md` (or its bare-
   yaml clause as a new principle) in the scaffold brief at
   [`briefs.go:183-199`](../../../internal/recipe/briefs.go#L183-L199).
2. Add a scaffold-time validator
   (`internal/recipe/validators_codebase.go` or new file): on
   scaffold complete-phase, scan every committed `<SourceRoot>/zerops.yaml`
   for `^\s+# ` lines (single-hash, leading whitespace). If any
   present except the `#zeropsPreprocessor=on` shebang or trailing
   `data: # note` comments, refuse the gate with the exact violating
   lines named.

**Note (corrected from earlier draft after codex Q3 review):** the
strip in `WriteCodebaseYAMLWithComments` is **NOT** dead code after
C3 lands. It's the engine's own idempotence mechanism for repeated
stitch ([`stitch_yaml.go:18,29-30,56-57`](../../../internal/recipe/stitch_yaml.go#L18)),
pinned by [`stitch_yaml_test.go:140,179,183`](../../../internal/recipe/stitch_yaml_test.go#L140).
Even with bare scaffold yaml, the engine's own injected `# #`
comments accumulate across multiple stitch rounds and need the strip.
C3's validator only stops scaffold-leaked single-`#` lines; the
engine-side strip stays.

The same is true for E1's one-line strip — it stays. Both strips are
load-bearing for engine idempotence, not defensive scaffold cleanup.

**Verification:** **unit** for the scaffold validator; sim
unaffected by C3 (sim doesn't run scaffold).

---

## C4. Content — worker MCP `zerops_dev_server` carve-out missing from dev-loop principle

**Symptom:** scaffold-worker invoked MCP `zerops_dev_server` zero
times (api+app: 1 each). Worker behavior was attested only on
`workerstage` (compiled-entry start), never on `workerdev`.

**Root cause:** the dev-loop principle
(`internal/recipe/content/principles/dev-loop.md:21-23`) describes
the `zerops_dev_server` tool with required args `port` and
`healthPath` — both HTTP-shaped. A NestJS standalone-context worker
has no port and no health probe. The agent reasonably concluded the
tool didn't apply.

The carve-out exists elsewhere — `develop-dev-server-reason-codes.md:18-23`
references `port=0/healthPath=""` with `post_spawn_exit` reason — but
isn't lifted into `dev-loop.md`.

**Fix:**

1. Extend `internal/recipe/content/principles/dev-loop.md` with an
   explicit no-HTTP worker section: when the codebase has no `ports:`
   block in scaffolded yaml (or `isWorker=true` in plan), the
   canonical invocation is
   `zerops_dev_server action=start hostname=<h>dev command="<watch-cmd>" port=0 healthPath=""`.
2. Verification of running: tail logs (`zerops_logs`) for the
   framework's "started" message, then watch for the worker's first
   recorded heartbeat / consume / processing line.

**Validator (gate, corrected from earlier draft after codex Q4
review):** scaffold complete-phase gates only receive `Plan`,
`OutputRoot`, `FactsLog`, `Parent`
([`gates.go:22-28`](../../../internal/recipe/gates.go#L22)). They
have NO access to the sub-agent's tool history — `CompletePhase`
runs server-side; tool calls happen client-side in the Agent
dispatch loop. The earlier draft's "scan tool history for
`zerops_dev_server action=start` calls" predicate is unimplementable
as stated.

Use a fact-attestation path instead, accessible via `FactsLog`:

- Require a recorded fact `worker_dev_server_started: <timestamp>`
  on the worker codebase scope (or any dev codebase with
  `start: zsc noop --silent`). Refusal on missing fact at
  `complete-phase=scaffold`.
- Bypass: a recorded fact `worker_no_dev_server: <reason>` (already
  proposed in `ANALYSIS.md:289-292`) suppresses the requirement.
- Update the scaffold brief to instruct the agent to record the
  attestation fact immediately after the
  `zerops_dev_server action=start` call returns + after a
  successful log tail.

**Verification:**

- **unit** — fixture facts.jsonl missing the attestation fact triggers
  gate refusal; with the bypass fact, gate passes.
- **sim** — unaffected (sim doesn't run scaffold).
- **prompt-eval** OR **dogfood** for the brief change steering
  behavior. (Lower priority — the gate alone closes the worst case.)

---

## V1. Validator — slug-citation noun-phrase shapes

**Symptom:** 11 instances of "The Zerops `<slug>` reference covers..."
in `apidev/README.md` + 2 of "see `zerops_knowledge` guide
`<slug>`" in `workerdev/README.md`. The run-19 regex extension to
`\b(?:See|Cite|Per|Ref|cf):` caught zero of these (no colon).

**Fix:** extend the slug-citation validator in
`internal/recipe/validators.go` (or wherever the regex lives) with
the noun-phrase patterns:

```
\bThe Zerops `[a-z][a-z0-9-]+` reference\b
\b(?:see|per|cite) `zerops_knowledge` guide `[a-z][a-z0-9-]+`\b
\b(?:see|per|cite) guide `[a-z][a-z0-9-]+`\b
\b`[a-z][a-z0-9-]+` (?:reference|guide) (?:covers|documents|explains)\b
```

**Verification:** **unit** + **sim**.

- Unit: regex matches every noun-phrase fixture, refuses with
  redirect ("cite by mechanism, not by slug — e.g. 'see the
  rolling-deploys guide on Zerops docs' or inline `[link](...)`").
- Sim: re-run codebase-content sub-agent against run-19 facts +
  new validator; assert zero noun-phrase citations in the output.

---

## V2. Validator — classification routing not enforced at record-fragment

**Symptom:** 1 `library-metadata` + 3 `scaffold-decision` facts
landed on `CODEBASE_KB` despite being on the spec's discard / yaml-
comment / IG routing list. The schema requirement (run-19 fix #4) is
respected, but routing isn't.

**Fix:** at `record-fragment` time, when fragmentId starts with
`codebase/<h>/knowledge-base` or `codebase/<h>/integration-guide/<n>`,
look up the fact's classification and refuse if (classification ∈
{library-metadata, self-inflicted, scaffold-decision/recipe-internal})
AND surface == KB. Refusal message names the spec table row + offers
the right surface (DISCARD or zerops-yaml-comments or IG-with-diff).

**Verification:** **unit** + **sim**.

- Unit: fixture `library-metadata`+`CODEBASE_KB` pair triggers
  refusal.
- Sim: replay run-19 fragment recordings under the new validator;
  assert the four mis-routed facts now refuse.

---

## V3. Validator — SPA mis-shape (covered by C2 layer 5)

See C2.

---

## V4. Validator — scaffold yaml `^\s+# ` (covered by C3)

See C3.

---

## B1. Brief composer — comment density is fact-driven, no template floor

**Symptom:** sim-19 (clean dry-run, same engine code) produced 7
yaml-comment fragments for api covering deployFiles, httpSupport,
env aliases, S3_REGION, dev/prod start asymmetry, readinessCheck,
execOnce. Run-19 produced 3, covering only execOnce, env aliases,
healthCheck. Goldens cover every directive group + every env-var
family.

**Root cause (corrected from earlier draft after codex re-review):**
two layers cooperate to produce the sparse output:

1. **Brief filtering** — `BuildCodebaseContentBrief`
   ([`briefs_content_phase.go:108`](../../../internal/recipe/briefs_content_phase.go#L108))
   filters fact-stream to codebase scope.
2. **Fact-shape requirements** — `FactRecord.Validate`
   ([`facts.go:151`](../../../internal/recipe/facts.go#L151)) requires
   `fieldPath` and `why` for `field_rationale` records. Records that
   pass validation but lack populated `fieldPath` won't have a
   surface to attach to.

`synthesis_workflow.md:269-275` tells the sub-agent to author "for
each `field_rationale` fact" — fact-driven, no required template
list. Run-19's scaffold/feature recorded 7 such facts on api yaml,
some unclassified, some without `fieldPath`. Sub-agent emitted 3
fragments. The goldens cover 100% of directive groups because the
human authors didn't gate on facts.

**Fix (brief-side path — recommended; refined from earlier draft
after codex Q5 review):** extend the codebase-content brief to
enumerate expected comment slots from TWO structured sources, not a
prefix heuristic:

1. **Directive groups** — parsed from the on-disk yaml at
   brief-compose time using `gopkg.in/yaml.v3` (already a direct
   dependency, used at
   [`validators_import_yaml.go:8`](../../../internal/recipe/validators_import_yaml.go#L8)).
   One slot per top-level directive group: `build`, `deploy`,
   `run.envVariables`, `run.initCommands`, `run.ports`,
   `run.healthCheck`, `run.start`, `run.prepareCommands`, etc. For
   run-19's apidev yaml that's ~7 slots.

2. **Env-var families** — derived from `plan.Services` (managed
   services with structured hostname + type at
   [`plan.go:75-81`](../../../internal/recipe/plan.go#L75)) plus
   `field_rationale` facts whose `Service` or `Subject` field names
   the family. One slot per managed-service family the codebase
   consumes (db, cache, broker, storage, search, etc.) plus one per
   project-env family (app-urls from `plan.ProjectEnvVars`, shared
   secrets, etc.).

   **Avoid prefix-matching `${db_*}` / `${cache_*}`** — that
   heuristic misses custom app env names and over-matches non-
   service prefixes.

The sub-agent authors a comment block per slot. If no
`field_rationale` fact attests the rationale, the sub-agent records
one in-flight via `record-fact` while writing the comment. Slots
without facts + without comments fail the gate at
`complete-phase=codebase-content`.

**Optional fact-side complement:** add scaffold/feature-time prompts
to record `field_rationale` facts for every directive group the sub-
agent writes. This shifts cost left but doesn't eliminate the slot-
floor requirement (some directives are scaffold-template defaults
without an interesting rationale; the slot-floor brief makes the
sub-agent decide whether they need a comment, recording "no
rationale needed" as an explicit decision). Aligns with C5 below.

**Verification:** **sim** (assuming S3 staging gap closes — see
S3 below; without it, sim sub-agent can't read `src/**` and the
fragment density measure isn't representative).

- Re-run codebase-content sub-agent against run-19 facts + new
  brief.
- Assert per-codebase yaml-comment fragment count ≥ (directive group
  count + env-var family count) for the on-disk yaml. For run-19's
  apidev yaml: ~7 directive groups + ~7 env-var families = ~14
  expected blocks.
- Assert blocks cover every directive group present on disk.

---

## S1. Sim driver — multi-stitch idempotence assertion

**Symptom:** sim-19 ran a single linear pass and missed the inline-
yaml block-doubling regression because the bug only fires on
`stitchCodebases` round ≥2. Production runs `stitchCodebases` at
every phase boundary.

**Status:** **landed** by sub-agent A — commit `cb0c40b8` +
`stitch_test.go`. `-rounds=2` is the default; byte-diff fires at
the end. Self-verifying: with E1's strip in place, rounds are
identical; without it, round 2 has doubled blocks and the diff
prints them.

---

## S2. Sim driver — comment-density vs goldens assertion

Optional. Add a sim subcommand that, given a generated codebase
zerops.yaml, parses the directive tree and asserts coverage ≥ X% of
the corresponding golden recipe's directive coverage. Useful for
catching B1 regressions as the brief evolves.

---

## S3. Sim driver — stage code artifacts for replay

**Symptom (corrected from earlier draft after codex re-review):**
two different staging requirements depending on which sub-agent
replays:

- **Codebase-content** brief reads only `<SourceRoot>/zerops.yaml`
  (already staged) plus `<SourceRoot>/src/**` for code-grounded
  references
  ([`briefs_content_phase.go:124-128`](../../../internal/recipe/briefs_content_phase.go#L124-L128)).
  No package.json / tsconfig / framework manifests are referenced
  in that composer.
- **Claudemd-author** brief explicitly references `package.json`,
  `composer.json`, `src/**`, and Laravel `app/**`
  ([`briefs_content_phase.go:304`](../../../internal/recipe/briefs_content_phase.go#L304)).

`cmd/zcp-recipe-sim/emit.go:65-87` stages neither.

**Fix:** in `emit.go`, copy the union of the two requirement sets:
`<runDir>/<host>dev/src/**`, `package.json`, `composer.json`,
`<runDir>/<host>dev/app/**` (Laravel-specific), into
`simulations/<M>/<host>dev/` alongside the staged `zerops.yaml`.
Skip `node_modules`, `vendor`, `.git`. Match the union of the two
composers' expected reads — staying close to what's actually
referenced rather than over-staging "framework manifests" broadly.

**Status:** **landed** by sub-agent A — commit `77fd5e24`. Stages
the union (`src/**` + `package.json` + `composer.json` + `app/**`)
with skip-list (`node_modules`, `vendor`, `.git`).

---

## S4. Sim driver — pass `Parent` and `MountRoot` through

**Symptom:** `emit.go:112,133` sets `parent=nil` for emitted
prompts. Production sessions have `Parent` and `MountRoot` populated
(`internal/recipe/workflow.go:57,59,65`) and they affect the
codebase-content brief
([`briefs_content_phase.go:130,241`](../../../internal/recipe/briefs_content_phase.go#L130)) —
specifically parent-recipe dedup logic.

**Status:** **landed** by sub-agent A — commit `b2e3d008` +
`emit_parent_test.go`. Plan.json does NOT carry parent/mountRoot
fields (Plan struct at `internal/recipe/plan.go:16` is parent-less
by design — `Parent` lives on `Session`, not `Plan`). Implementation
loads parent via `recipe.ResolveChain(plan.Slug)` against
`-mount-root` (matching production session bootstrap), with
`-parent <slug>` as an explicit override. The earlier wording here
("read from saved Plan or session metadata if available")
overstated what the saved Plan provides — corrected.

---

## S5. Sim driver — emit refinement prompt + replay refinement phase

**Symptom:** `emit.go:101,124` only emits codebase-content +
env-content prompts. R1 below describes refinement as a sim-
verifiable closure — but the sim driver doesn't currently emit a
refinement prompt or replay the refinement phase.

**Status:** **landed** by sub-agent A — commit `6f2d30cc` +
`refine_test.go`. Implementation note: sim calls
`recipe.BuildRefinementBrief` directly rather than the public
`recipe.BuildSubagentPromptForReplay` wrapper, because the wrapper
hard-codes empty `runDir` for `BriefRefinement`
(`briefs_subagent_prompt.go:161`) and the refinement brief needs
that field populated to emit its "## Stitched output to refine"
pointer block. **Follow-up:** if a future engine revision exposes
`BuildSubagentPromptForReplayWithOutputRoot`, the sim driver should
switch to that wrapper to keep replay vs production prompt-shape
identical.

---

## S6. Sim driver — complete-phase analog for codebase-content

**Symptom (added after codex re-review):** the sim driver's replay
adapter at
[`cmd/zcp-recipe-sim/emit.go:169`](../../../cmd/zcp-recipe-sim/emit.go#L169)
explicitly tells the replayed agent NOT to use MCP tools — it
overrides `record-fragment` to write files. There's no sim analog
for `complete-phase` calls. **B1's proposed gate (refuse codebase-
content close when expected slots lack fragments) cannot be
verified in sim today** because sim doesn't run the gate path.

**Fix:** after the replayed sub-agent finishes writing fragments,
the sim driver's `stitch` step should additionally invoke the
codebase-content complete-phase gate logic against the staged
plan + facts + materialized fragments — mirroring what the
production engine does. Emit refusals as sim failures.

This is the missing piece for B1 verification: gate-running, not
just brief-emission.

**Status:** **landed** by sub-agent A — commit `f2906dd4` +
`gate_test.go`. Activated via `-gates codebase-content` flag on
the `stitch` subcommand; runs `DefaultGates+CodebaseContentGates`
against the staged plan/facts/fragments. Runs/exits as a sim
failure when refusals fire.

---

## S7. Sim driver — emit claudemd-author prompts (D1 prerequisite)

**Symptom:** `emit.go:101,124` emits codebase-content + env-content
only. D1's claudemd-author slot-shape debt isn't sim-verifiable
without expanding emit scope to include the claudemd-author sub-
agent's prompt.

**Fix:** add a fourth emit branch for `claudemd-author` per
codebase, building the prompt via the same composer the production
engine uses
([`briefs_content_phase.go:304`](../../../internal/recipe/briefs_content_phase.go#L304)).
Replay output goes into `simulations/<M>/fragments-new/<host>/codebase__<h>__claude-md.md`.

Optional — D1 doesn't block run-20. If staying tight on scope,
defer S7 until D1 actually surfaces in run-20 dogfood.

**Verification:** **unit** — fixture run-19 corpus + S7 emit;
assert claudemd-author prompt for `worker` codebase contains the
expected fact list and goldens.

---

## C5. Content — facts-integrity producer-side (scaffold + feature)

**Symptom:** run-19's `facts.jsonl` has 13 facts with no
classification and 7 `field_rationale` facts missing `fieldPath`.
B1's slot count depends on `field_rationale` facts being well-formed.

**Root cause:** `FactRecord.Validate`
([`facts.go:151-155`](../../../internal/recipe/facts.go#L151)) requires
`fieldPath` and `why` for `field_rationale` records, and
`gateFactsValid` runs that validation
([`gates.go:466-478`](../../../internal/recipe/gates.go#L466)) — but
**only on individual records**. There's no producer-side completeness
check ensuring scaffold/feature recorded a `field_rationale` fact for
every directive group the sub-agent wrote in the on-disk yaml.

**Fix:**

1. Add a new gate (e.g. `gateFactRationaleCompleteness`) at scaffold
   + feature complete-phase. The gate parses
   `<SourceRoot>/zerops.yaml` using `gopkg.in/yaml.v3` (same parser
   already in use at
   [`validators_import_yaml.go:8`](../../../internal/recipe/validators_import_yaml.go#L8)
   — directive-tree parsing does not exist in scaffold gates today
   per [`gates.go:59`](../../../internal/recipe/gates.go#L59), this
   adds it). Enumerate directive groups + env-var families derived
   from `plan.Services`. Refuse when any directive lacks an
   attesting `field_rationale` fact whose `FieldPath` matches.
2. Bypass mechanism: extend the bypass via the existing
   `FactKindFieldRationale` `why` field — a `why` containing
   `"intentionally skipped: <reason>"` (or an explicit
   `Decision="skip"` tag added as a new optional `FactRecord`
   field) suppresses the completeness requirement for that
   `FieldPath`.

   **Note:** the original draft proposed
   `field_rationale_skipped` as a new FactKind, but the existing
   FactKind constants
   ([`facts.go:84`](../../../internal/recipe/facts.go#L84)) are
   `porter_change`, `field_rationale`, `tier_decision`, `contract`
   — adding a new top-level kind for a bypass is heavyweight. The
   bypass-via-decoration approach reuses the existing kind.
3. Update the scaffold + feature briefs to instruct the agent to
   record one `field_rationale` per directive group at the moment
   they write or modify it.

**Note:** this is the **producer-side** complement to B1's
**consumer-side** slot floor. B1 alone can't fix the gap because
the consumer can't materialize comments without source rationale; C5
forces the rationale to exist before codebase-content runs.

**Verification:** **unit** — fixture facts.jsonl missing a
`run.healthCheck` rationale fact triggers gate refusal at
`complete-phase=scaffold`.

---

## C6. Content — preventive lint against framework version-pin drift

**Symptom:** run-19's TIMELINE.md:264 explicitly records "brief
said Svelte 4, scaffold shipped Svelte 5 (`svelte ^5.1.9`)". The
feature sub-agent adapted with Svelte 5 runes (`$state`). Code
self-corrected; brief stale.

**Status (corrected after codex re-review):** the active brief paths
do NOT currently contain Svelte 4 references — `grep -rin 'svelte
4\|svelte ^4\|svelte@4' internal/recipe/content/ internal/content/atoms/`
is empty. The drift the run-19 TIMELINE captured was either in the
ResearchData input from the main agent (out of scope of brief
content) or was already cleaned up between runs.

**Fix (downscoped from earlier draft):** add a preventive lint over
brief + atom content paths so future drift is caught before
landing — not a backfix for current content.

1. New lint test in `internal/recipe/briefs_test.go` (or new file)
   that walks `internal/recipe/content/briefs/**/*.md` +
   `internal/content/atoms/*.md` and refuses any line matching a
   denylist of pinned-major patterns: `\bsvelte ?\^?4\b`,
   `\b(?:next|nuxt|astro|sveltekit|laravel)@[0-9]+\.`, etc.
   Allowlist exceptions (e.g. specific atoms that legitimately
   reference a major) get inline `<!-- pin-version-keep: <reason> -->`
   markers, mirroring the existing axis-marker convention
   ([`atoms_lint.go`](../../../internal/content/atoms_lint.go)).
2. Brief authoring convention: prefer "the framework's current
   stable major" + `package.json`-as-source-of-truth language over
   pinned version numbers.

**Verification:** **unit** — the lint passes today (clean baseline);
add a fixture proving it catches a re-introduction.

---

## Companion docs

- **C7 diagnosis** — root cause + fix options for the subdomain
  auto-enable gap, traced from run-19 SESSION_LOGS:
  [`run-20-c7-diagnosis.md`](run-20-c7-diagnosis.md). Conclusion:
  empty `meta.Mode` shorts-circuits the eligibility predicate.
  Option B (one-line predicate relaxation) lands as the immediate
  fix; Option A (populate Mode at provision) is the longer-term
  cleanup.

- **Patch script** — `cmd/zcp-recipe-patch -profile run19-for-run20
  -from docs/zcprecipator3/runs/19 -to docs/zcprecipator3/runs/19-patched`
  produces a derivative corpus reflecting what scaffold + feature
  WOULD have produced if the run-20 brief changes (C2 SPA, C4 worker
  dev-server attestation, C5 producer-side facts integrity) had
  been live. Sim runs against the derivative to verify downstream
  engine + content + validator fixes (E1, V1, V2, C1, C3) against
  correct inputs without waiting for the brief changes to ship in
  a real dogfood. The patch is a TEST INPUT, not a test ORACLE.

## C7. Subdomain auto-enable — diagnose run-19 gap despite tested closure

**Symptom:** run-19 TIMELINE.md:128-129,259-260 records the
subdomain-auto-enable gap surviving on apidev/apistage/appstage —
manual `zerops_subdomain action=enable` was needed.

**Status (corrected file paths after codex re-review):** the auto-
enable code lives at
[`internal/tools/deploy_subdomain.go:46`](../../../internal/tools/deploy_subdomain.go#L46),
called after SSH deploy at
[`deploy_ssh.go:212`](../../../internal/tools/deploy_ssh.go#L212)
and by record-deploy at
[`workflow_record_deploy.go:158`](../../../internal/tools/workflow_record_deploy.go#L158).
The recipe-authoring eligibility case has direct test coverage:
`TestPlatformEligible_DetailSubdomainAccessFalse_HTTPSupportTrue_Eligible`
at
[`deploy_subdomain_test.go:484`](../../../internal/tools/deploy_subdomain_test.go#L484).

**The gap the plan must explain:** the predicate is implemented and
tested, the deploy paths invoke it — yet run-19 still required
manual subdomain enables. There's a mismatch somewhere: either (a)
the zcprecipator3 deploy invocation path doesn't go through
`deploy_ssh.go`/`workflow_record_deploy.go` (uses a different
deploy tool entry that bypasses the auto-enable), or (b) the
predicate signal is satisfied for these services but a downstream
step fails silently.

**Action (refined):**

1. Read run-19 SESSION_LOGS for `mcp__zerops__zerops_deploy` calls
   on apidev/apistage/appdev/appstage. Identify which deploy entry
   was used (full deploy via `zerops_deploy` MCP, or some other
   path).
2. Compare the deploy entry's call graph with `deploy_ssh.go:212`
   to find where the auto-enable hook is or isn't invoked.
3. If the gap is a missing hook in the zcprecipator3 deploy path,
   add it (likely small).
4. If the predicate fires but `zerops_subdomain` action returns a
   result the workflow doesn't surface to the agent, fix the
   surfacing.

**Verification:**

- **unit** — exercise the predicate against a recipe-authoring
  fixture (no `meta` field, dev mode, httpSupport=true). Likely
  passes — the test already exists.
- **integration** — synthetic deploy in a test harness that mirrors
  the zcprecipator3 deploy invocation; assert subdomain ends
  active.
- **dogfood** — confirm appdev/appstage auto-enable in run-20 final
  minimal-recipe deploy.

---

## D1. CLAUDE.md slot-shape — known debt (not closed, not blocking)

**Symptom:** run-19 TIMELINE.md:193-195,266 records 4 retries on
worker claudemd-author because the validator blocks bare hostname
tokens (`db`, `cache`, `broker`, `search`) — even when they're real
source-tree directory names (`src/db/`, `src/cache/`, etc.) or
generic phrases (`message broker`).

**Status:** correctly classified as DEBT, not out-of-scope.
Run-19's claudemd-author eventually produced a clean fragment but
burned tokens / wall-clock on the retry loop. The validator is
over-broad: it blocks legitimate uses of those terms in source
paths or generic prose.

**Fix path (deferred unless run-20 also exhibits high retry
counts):**

1. Refine the slot-shape regex to allow `src/<token>/` path
   contexts and known generic noun-phrase contexts ("message
   broker", "search index").
2. Or: relax the validator to a **notice** rather than a **refusal**
   for these contexts; let the sub-agent self-correct on first
   try.

**Verification:** **unit** — fixture claude-md fragments with
`src/db/` paths pass; bare `db.` hostname references in non-path
contexts still refuse.

---

## R1. Refinement — surface what's already there

The refinement sub-agent already exists
(`internal/recipe/briefs_refinement.go`) and is dispatched after
finalize. Its rubric and reference patterns
(`briefs/refinement/embedded_rubric.md`,
`reference_yaml_comments.md`, etc.) target exactly the problems run-
19 shipped: sparse yaml comments, slug citations, JetStream-style
fabrication.

Run-19 ran refinement (TIMELINE marks "Gate: complete-phase
phase=finalize → ok:true (advanced to optional refinement phase)")
but the residuals shipped anyway. Three possibilities:

1. Refinement ran but its replace-fragment refusals fired and the
   sub-agent didn't iterate.
2. Refinement ran and produced low-confidence verdicts that didn't
   meet the replace threshold.
3. Refinement's reference-pattern atoms don't actually carry the
   teaching to fix what's broken (sparse comments → no rubric for
   "every directive group").

Action: read the refinement sub-agent's session log
(`SESSION_LOGS/subagents/agent-*` with description=refinement, if
present in run-19) and trace what it did vs what it should have
done. Likely outcome: refinement is the right place to catch B1's
density problem post-hoc, but its rubric needs the same expected-
blocks list B1 adds at brief-compose time. Aligns the two.

**Verification:** **sim** — re-run refinement against run-19
corpus + updated rubric; assert it records replace-fragments for
the sparse comment surfaces.

---

## Run-20 verification matrix summary

| ID | Layer | sim | unit | patched-sim | prompt-eval | dogfood |
|---|---|---|---|---|---|---|
| E1 | engine (one-line strip) | ✓ | ✓ | | | |
| E2 | engine (idempotence inside insertCommentAtBlock) | | ✓ | | | |
| C1 | content (env-content brief) | ✓ (after S3+S4) | | | | |
| C2 | content (scaffold brief) | layers 1-3, 5 | layer 5 | layer 4 | layer 4 (alt) | last resort |
| C3 | content (scaffold brief + validator) | | ✓ (validator) | | (brief change) | |
| C4 | content (dev-loop + fact-attestation gate) | | ✓ (gate) | | (brief change) | |
| C5 | content (facts-integrity producer side) | | ✓ (gate) | | | |
| C6 | content (Svelte version drift in briefs) | | ✓ (lint) | | | |
| C7 | subdomain auto-enable closure | | ✓ (predicate) | | | confirm |
| V1 | validator (slug-citation noun-phrase) | ✓ (after S3) | ✓ | | | |
| V2 | validator (classification routing) | ✓ | ✓ | | | |
| B1 | brief composer (expected-blocks slot floor) | ✓ (after S3) | | | | |
| S1 | sim driver (multi-stitch idempotence) | self | | | | |
| S2 | sim driver (density vs goldens, optional) | self | | | | |
| S3 | sim driver (stage src/**) | | ✓ | | | |
| S4 | sim driver (Parent + MountRoot threading) | | ✓ | | | |
| S5 | sim driver (refinement emit + replay) | | ✓ | | | |
| R1 | refinement | ✓ (after S5) | | | | |
| D1 | claudemd-author slot-shape (debt) | | ✓ | | | |

| S6 | sim driver (complete-phase analog) | | ✓ | | | |
| S7 | sim driver (claudemd-author emit, optional for D1) | | ✓ | | | |

**Items that need NO live dogfood:** E1, E2, C1, C5, C6, V1, V2,
B1, S1, S2, S3, S4, S5, S6, S7, R1, D1 — 17 items. C2 layer 4
(SPA atom steering scaffold-app behavior) and C4 (dev-loop carve-
out brief text) need prompt-eval at minimum. C7 needs an
integration test + small dogfood deploy-confirmation.

**Important caveat:** several "sim" verifications above depend on
sim-driver groundwork landing first. The full dependency chain:

```
S3 (stage src/** + manifests) ─┬─→ B1 verification representative
                               ├─→ C1 verification representative
                               ├─→ V1 verification representative
                               └─→ R1 verification representative
S4 (parent + mountRoot) ─→ parent_recipe_dedup brief atom verification
S5 (refinement emit) ─→ R1 replay verification
S6 (complete-phase analog) ─→ B1 GATE verification (B1's slot-floor
                              refusal can't be tested without it)
S7 (claudemd-author emit) ─→ D1 verification (optional)
C5 (producer-side facts integrity) ─→ B1 SLOT-CONTENT verification
                                      (without C5, sim sub-agent has
                                      no facts to attach to slots)
```

**Two blockers for B1 specifically (corrected from earlier draft):**

1. S3 (sim staging) — without `src/**` staged, the sub-agent runs
   blind.
2. S6 (sim complete-phase) — without it, the gate-refusal half of
   B1's verification is unimplementable in sim.
3. C5 (producer-side facts) — without it, the sub-agent has no
   facts to attach comments to.

So B1 is gated on S3 + S6 + C5. That's an internal dependency the
earlier draft's matrix omitted.

**Order of operations:**

1. **Sim driver groundwork first.** Land S1 (multi-stitch), S3
   (stage `src/**` + manifests), S4 (Parent + MountRoot), S5
   (refinement emit), S6 (complete-phase analog). All
   `cmd/zcp-recipe-sim/{emit,stitch}.go` work, no engine changes.
   Without these, every other sim verification is unrepresentative
   or impossible. S7 (claudemd-author emit) can defer until D1
   surfaces.
2. Land E1 (one-line strip in `assemble.go:144`). Verify with S1
   multi-stitch byte-diff + new unit test.
3. Land C3's validator (`^\s+# ` refusal at scaffold complete-
   phase). Stops scaffold-comment leakage. **Engine-side strip in
   `WriteCodebaseYAMLWithComments` STAYS** (idempotence mechanism;
   see C3 note).
4. Land V1, V2 in parallel. Pure regex / refusal. Re-run sim's
   codebase-content + env-content phases; confirm zero noun-phrase
   slug citations and zero KB-routing of library-metadata facts.
5. Land C1. Re-run sim's env-content phase; confirm zero JetStream
   tokens.
6. Land C5 (producer-side facts-integrity gate at scaffold + feature
   complete-phase). Foundation for B1.
7. Land C6 (Svelte version-pin lint across briefs). Quick win.
8. Land B1 + R1 together (aligned expected-blocks + rubric). Re-run
   sim; confirm yaml-comment density approaches goldens.
9. Land C2 layers 1-3 + 5 (principle, brief edit, atom, refusal).
   Verify in sim + patched-sim.
10. Land C2 layer 4 (SPA `base: static` atom). Run prompt-eval. If
    green, defer the dogfood.
11. Land C4 (dev-loop carve-out + fact-attestation gate). Run
    prompt-eval.
12. C7 — confirm subdomain auto-enable closure. Unit test the
    predicate; if green, no further work.
13. D1 — defer unless run-20 dogfood reproduces high retry counts on
    claudemd-author worker.
14. Final gate: **full nestjs-showcase rerun** with all run-20
    fixes live. Same recipe as run-19 — same plan, same 5 managed
    services, same 3-codebase showcase shape — so every charter
    item gets exercised (NATS atom, worker dev-server, all 5
    managed-service comment families, cross-codebase IG, all 6
    tiers, full prose surfaces). The run-19 published artifacts
    are the A/B diff baseline. ~90 min wall-time, but
    minimal-recipe alternatives exercise less than half the
    charter — full rerun is the correct trade.

---

## Out of scope for run-20

- The deprecated `internal/content/workflows/recipe.md` doc — left
  in place with a deprecation banner so future agents don't
  accidentally read it as a zcprecipator3 source. Extraction of its
  still-relevant teachings (dual-runtime URLs) into zcprecipator3
  principles happens in C2.

**No longer out of scope (per codex Q6 review):**

- CLAUDE.md slot-shape: tracked as D1 (debt, not closed).
- Svelte version drift in briefs: tracked as C6.
- Facts-integrity producer-side: tracked as C5.
- Subdomain auto-enable closure confirmation: tracked as C7.
