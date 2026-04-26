# Run 13 readiness — implementation plan

Run 12 (`nestjs-showcase`, 2026-04-25) was the first dogfood after the
post-run-12-readiness engine. All twelve workstreams (E / A / C / I / M
/ G / R / B / D / Y1 / Y2 / Y3) shipped; ten produced exactly the surface
behaviour the plan promised; aggregate content quality lifted from 6/10
to 7/10 vs reference. The env-pattern surface flipped from D to A
(own-key aliases ship correctly across all three apps-repo zerops.yamls;
code reads `process.env.DB_HOST` etc.); the published yaml comment
prefix went from disfigured (272 `# # ` lines) to clean; tier-5
Meilisearch flipped from contradictory `mode: HA` to correct `mode:
NON_HA`; finalize closed in two complete-phase rounds without hand-edits.

But run 12 surfaced three new defect classes that no run-12-readiness
analysis predicted:

1. **Engine template injects `zcli push` into every CLAUDE.md** —
   `internal/recipe/content/templates/codebase_claude.md.tmpl:11-15`
   carries a hardcoded `## Zerops dev loop` section with prose that
   explicitly violates the §C scaffold-brief teaching the agent
   correctly honored. Run-13 §1+§2 deleted the validator that would
   have caught it on §4 catalog-drift grounds; nothing replaces it now.

2. **Tier-prose ships factually wrong against engine emit at scale.**
   Tier 5 README claims *"every runtime carries at least three
   replicas"* (yaml emits `minContainers: 2`); claims *"Meilisearch
   keeps a backup"* (yaml emits `mode: NON_HA` after §Y3 downgrade);
   claims *"object-storage replicated"* (no replication field exists).
   Tier 5 import.yaml comments claim "three replicas" / "Meilisearch in
   HA mode" / "Object-storage bucket sized at 50 GB" — every one
   contradicts the field 6-10 lines below. Tier 4 storage comment
   claims "Bucket quota at 10 GB"; field is `objectStorageSize: 1`.
   Tier 2 `app` comment claims "Vite SPA built once and served as
   static files... Zerops' static-runtime is cheaper than keeping a
   Node process up"; the recipe emits `nodejs@22` runtime running `npx
   vite preview` — there IS a Node process. Engine has the truth
   (`tier.RuntimeMinContainers`, `managedServiceSupportsHA`, plan
   defaults); the brief composer doesn't push it into the agent's
   context, so the agent extrapolates and ships invented prose.

3. **§G's "right author sees violations" intent failed at the actor
   layer.** Validators do fire at scaffold complete-phase (§G + §3
   auto-stitch landed correctly), but the SCAFFOLD SUB-AGENT has
   already terminated when complete-phase runs in main's session. Main
   inherits 13 violations, fixes via 7 direct `Edit` calls on
   `apidev/zerops.yaml` + `workerdev/zerops.yaml` plus 3
   `record-fragment mode=replace`. The hand-edit pattern run 12 was
   supposed to eliminate just shifted upstream from finalize-close to
   scaffold-close.

Plus a structural gap that surfaced in user feedback after the run:

4. **Feature subagent has no showcase scenario specification.** The
   feature dispatch wrapper says queue-demo is *"already proven by the
   scaffold api sub-agent"* via curl. The SPA panels the feature
   subagent designed (Items, Status, Upload) have no queue/broker
   visualization — the agent inherited "broker is fine, no panel
   needed." A porter clicking the published recipe sees no
   demonstration of the broker pathway, despite the recipe shipping a
   broker + worker + indexer chain.

Run 13 ships the template fix (§Q), the tier-fact table that pushes
engine-resolved capability data into the brief composer (§T), the
post-stitch backstop validator that catches tier-prose-vs-emit
divergence (§V), the showcase scenario specification + queue-demo
panel mandate (§F), the per-codebase complete-phase scoping that
closes the §G actor gap (§G2), the secondary brief / atom corrections
that didn't make run 12 (§N init-commands decomposed-step,
§U alias-type resolution timing, §I-feature feature-side IG-scope rule,
§W finalize anti_patterns mode=replace allowance), the cosmetic Y2
dedupe (§Y2D), and the long-deferred R-18 wrapper-elimination via
`build-subagent-prompt` (§B2 stretch).

Reference material:

- [docs/zcprecipator3/runs/12/ANALYSIS.md](../runs/12/ANALYSIS.md) — run-12 verdict, R-12-1..R-12-12 defects, evidence trails.
- [docs/zcprecipator3/runs/12/CONTENT_COMPARISON.md](../runs/12/CONTENT_COMPARISON.md) — surface-by-surface vs `/Users/fxck/www/laravel-showcase-app/`. Honest aggregate **7/10**.
- [docs/zcprecipator3/runs/12/PROMPT_ANALYSIS.md](../runs/12/PROMPT_ANALYSIS.md) — timeline, dispatch sizing (28-38% wrapper share, criterion-15 missed), 12 named smell items.
- [docs/zcprecipator3/system.md](../system.md) §1 (audience model) + §4 (TEACH/DISCOVER line + verdict table).
- [docs/spec-content-surfaces.md](../../spec-content-surfaces.md) — surface contracts, per-surface tests, citation map. Authoritative for what content belongs on which surface.
- [/Users/fxck/www/laravel-showcase-app/](/Users/fxck/www/laravel-showcase-app/) — apps-repo reference. **Floor, not ceiling**: laravel CLAUDE.md still mentions zcp MCP for platform operations; we want CLAUDE.md zero-zcp. Laravel IG mixes platform mechanics with framework-config; we want IG = platform-mechanics-only.
- [docs/zcprecipator3/plans/run-12-readiness.md](run-12-readiness.md) — prior plan; E / A / C / I / M / G / R / B / D / Y1 / Y2 / Y3 all shipped + run-13 §1-§4 cleanup.
- [docs/zcprecipator3/CHANGELOG.md](../CHANGELOG.md) — top entries: "run-13 readiness: root-cause cleanup of run-12 catalog residue + plumbing fix" + "run-12 readiness: foundation + flow + cosmetic".

---

## 0. Preamble — context a fresh instance needs

### 0.1 What v3 is (one paragraph)

zcprecipator3 is the Go recipe-authoring engine at
[`internal/recipe/`](../../../internal/recipe/). Given a slug, it drives
a five-phase pipeline (research → provision → scaffold → feature →
finalize) producing a deployable Zerops recipe. The engine never
authors prose — it composes briefs from atoms + Plan, renders templates,
runs validators on stitched output, and classifies recorded facts.
Sub-agents (Claude Code `Agent` dispatch) author codebase-scoped
fragments at the moment they hold the densest context; the main agent
authors root + env fragments at finalize. Per-codebase apps-repo
content (README + CLAUDE.md + zerops.yaml + source) lives at
`<cb.SourceRoot>` = `/var/www/<hostname>dev/`. Recipe-repo content
(recipe-root README + 6 tier folders) lives at `<outputRoot>/`.

### 0.2 The audience boundary tightens further

System.md §1: every published surface's reader is a **porter**. Run-12
§C extended this to "agent-authored CLAUDE.md content" and the agent
honored it perfectly — zero `zerops_dev_server`/`zerops_deploy`/
`zerops_verify` invocations in any agent-authored fragment across all
three CLAUDE.md files in run 12.

Run-13 §Q extends the same boundary to **engine-stitched content**.
The CLAUDE.md template is engine-injected at stitch time and currently
violates the §C rule (carries `Iterate with `\``zcli push`\`` line).
The audience boundary is unconditional: porter-facing surfaces don't
mention authoring tools regardless of which engine layer wrote them.

### 0.3 Tier-fact correctness is structural, not stylistic

Run 12 ships a tier 5 README claiming "three replicas" + "Meilisearch
backup" + "object-storage replicated." A porter clicking tier 5 expecting
HA Meilisearch + 3-replica runtimes gets NON_HA Meilisearch + 2-replica
runtimes. The recipe is **falsely advertised** at tier 5.

This is structural — the engine has the truth in
[`tiers.go::Tiers()`](../../../internal/recipe/tiers.go) and
[`plan.go::managedServiceSupportsHA`](../../../internal/recipe/plan.go#L93-L103)
but doesn't push it into the brief composers. Agent extrapolates from
the fuzzy `tierAudienceLine()` ("production replicas" — no number),
authors prose against its mental model, engine emits actual fields,
prose ships factually wrong.

The fix-direction is system.md §4-aligned positive shape: engine emits
the resolved capability matrix into the brief at compose time. Agent
authors against truth. No catalog-shaped "ban these strings" backstop —
a structural relation validator (§V) catches the residual but is not
the load-bearing fix.

### 0.4 The §G acceptance criterion was specified at the wrong layer

Run-12-readiness §2.G acceptance: *"the right author (scaffold sub-agent)
gets the violation in their own session."* But [`handlers.go:228`](../../../internal/recipe/handlers.go#L228)
shows `complete-phase` doesn't accept a codebase parameter today, and the
v3 control flow (verified across 12 dogfood runs) is: main calls
complete-phase AFTER all sub-agents return. Sub-agents have terminated
by then; main inherits violations.

Run-13 §G2 closes this at the actor-orchestration layer:
- Engine extends `complete-phase` to accept optional `codebase=<host>`.
  When set, runs codebase gates scoped to that codebase only.
- Scaffold brief tells sub-agent: "Before terminating, call
  `complete-phase phase=scaffold codebase=<your-host>`. Fix in-session
  via `record-fragment mode=replace` for IG/KB/CLAUDE-fragment violations,
  or ssh-edit `<SourceRoot>/zerops.yaml` for yaml-comment / scaffold-
  filename violations. Re-call until ok:true, then terminate."
- Yaml stays file-based; the per-codebase `zerops.yaml` is NOT
  promoted to a fragment. Sub-agent ssh-edits when needed (matching
  how the agent already edits its own source files in-phase).

### 0.5 Showcase scenario is hardcoded + framework-agnostic

User feedback after run 12: *"feature subagent didn't get a proper
description of what it should do and how it should look ... didn't
demonstrate broker (which should've been queue more like maybe)."* The
feature dispatch wrapper said queue-demo is "already proven by the
scaffold api sub-agent" via curl, so the feature subagent designed
panels for Items / Status / Upload and skipped queue.

Run-13 §F adds a hardcoded showcase scenario specification to the
feature brief — framework-agnostic (a NestJS recipe, a Laravel recipe,
a Rails recipe all implement the same demo shape), a panel per managed-
service category, focus on demonstration components (NOT chrome /
layout / branding / typography effort).

### 0.6 Workstream legend

Each workstream maps to one named gap. Tranche structure sequences by
dependency.

| Letter | Scope | Tranche | Type |
|---|---|---|---|
| **Q** | Strip / rewrite `## Zerops dev loop` block in `codebase_claude.md.tmpl` | T1 | template (~5 lines) |
| **T** | Engine-composed tier capability matrix into scaffold + finalize briefs | T2 | engine + brief (~80 LoC) |
| **V** | Post-stitch validator: tier-prose-vs-emit divergence detection | T2 | engine validator (~50 LoC) |
| **F** | Feature brief: hardcoded showcase scenario spec + queue-demo panel mandate + per-panel browser-verification requirement | T2 | content (~40 lines) + brief composer change (~10 LoC) |
| **N** | `init-commands-model.md` adds "decomposed steps need distinct execOnce keys" paragraph | T2 | content (~6 lines) |
| **U** | `platform_principles.md` Alias-type contracts table adds resolution-timing footnote | T2 | content (~6 lines) |
| **I-feature** | `feature/content_extension.md` carries §I IG-scope rule explicitly | T2 | content (~7 lines) |
| **W** | `finalize/anti_patterns.md` rewrites "do not touch codebase ids" to allow `record-fragment mode=replace` | T2 | content (~5 lines) |
| **G2** | `complete-phase` accepts `codebase=<host>` scope; scaffold brief tells sub-agent to call it before terminating | T3 | engine (~30 LoC) + brief (~20 lines) |
| **Y2D** | Suppress duplicate Y2 fallback comment on dev-pair stage slot when dev slot already emitted the same string | T4 | engine (~6 LoC) |
| **B2** | New `build-subagent-prompt` action: engine emits the FULL dispatch prompt (recipe-level wrapper + brief body + close criteria) so main dispatches with `prompt=<response>` byte-identical | T5 stretch | engine (~80 LoC) |

T1 unblocks the porter-runnability template defect. T2 covers the
content-side foundations (factuality, scenario spec, brief gaps). T3
unblocks the actor mismatch on scaffold close. T4 polishes published
yaml. T5 is the wrapper-share elimination Run-11 R-18 deferred —
strictly recommended but not structurally blocking run-13.

---

## 1. Goals for run 13

A `nestjs-showcase` (or fresh slug) recipe run that, compared to
run 12 + the laravel-showcase-app reference:

1. **Apps-repo CLAUDE.md is engine-template-clean.** Zero
   `zcli push` / `zcli vpn` / `zerops_*` MCP tool name occurrences in
   any of the three published CLAUDE.md files, regardless of which
   engine layer composed the content. The §Q template fix removes the
   hardcoded `## Zerops dev loop` block; agent-authored Notes section
   carries the dev loop with framework-canonical commands.

2. **Tier 5 README + import.yaml comments match what tier 5 emits.**
   Tier 5 README does NOT claim "three replicas" (yaml has
   `minContainers: 2`), does NOT claim "Meilisearch backup" (yaml has
   `mode: NON_HA`), does NOT claim "object-storage replicated" (no
   such field). Tier 5 yaml comments match emitted fields:
   `mode: NON_HA` for Meilisearch is paired with single-node prose;
   `objectStorageSize: 1` is paired with 1-GB prose.

3. **Same correctness extends to all tiers.** Tier 4 storage comment
   doesn't claim "10 GB"; tier 2 `app` comment doesn't claim "static-
   runtime" while emitting `nodejs@22`; etc.

4. **Feature SPA carries a queue-demo panel.** A porter clicking the
   recipe sees a panel that visibly demonstrates: a publish trigger,
   the worker processing the message, the result landing in search.
   Each managed-service category (db, cache, queue, storage, search)
   has its own demonstration panel; modern design; chrome is minimal.

5. **Each demonstration panel records a browser-verification fact.**
   The feature brief acceptance criterion requires one
   `record-fact topic=*-{db,cache,queue,storage,search}-browser` per
   panel exercised, with concrete observables in the symptom field.

6. **Scaffold sub-agents fix violations in-session.** Zero main-agent
   `Edit` calls during scaffold-close. Sub-agents call
   `complete-phase phase=scaffold codebase=<host>` before terminating;
   if violations fire, fix via `record-fragment mode=replace` (for
   fragment-based content) or ssh-edit (for yaml-file content), re-
   call complete-phase, terminate when clean.

7. **Init-commands decomposed-step trap captured at the source.** No
   sub-agent rediscovers the `${appVersionId}` shared-key collision
   that scaffold-api hit in run 12 (per-step suffixed keys are taught
   in the atom).

8. **Cross-service alias resolution timing trap captured at the
   source.** No sub-agent rediscovers the SPA-built-before-target-
   deployed race that scaffold-app hit in run 12 (alias-type contracts
   table carries the resolution-timing footnote).

9. **Feature appends to IG don't break the §I IG-scope rule.** Feature
   subagent doesn't append cache-demo + liveness-probe prose
   subsections inside the IG extract markers; recipe-internal
   contracts route to KB or claude-md/notes per the same teaching
   scaffold honored.

10. **Tier 0/1 dev-pair runtime services don't carry duplicate
    comments.** Y2 fallback emits the bare-codebase comment above the
    dev slot; stage slot suppresses the duplicate.

11. **Stretch — engine-composed dispatch prompt.** Main agent
    dispatches sub-agents with `prompt=<build-subagent-prompt
    response>` byte-identical. Wrapper share drops from 28-38% to <
    10%. Acceptance criterion 15 from run-12-readiness finally met.

---

## 2. Workstreams

### 2.0 Guiding principles

Six invariants the implementation session must hold:

1. **No architectural work.** Each workstream is small (~5-100 LoC).
   None justifies redesigning state, renaming types, splitting
   packages, or reshaping the spec.

2. **Reference is a floor, not a ceiling.** Run-13 targets exceed
   reference on two axes: CLAUDE.md zero-zcp (template-side too); IG =
   platform-mechanics-only (feature side too).

3. **System.md §1 audience model holds at every engine layer.** When
   an atom, brief, validator, OR template injects content the porter
   cannot use (an MCP invocation, an authoring-time tool name), that
   content is mis-targeted regardless of which layer authored it.
   Run-13 tightens at the template layer specifically.

4. **TEACH side stays positive.** Per system.md §4, every fix expressed
   as "what the engine produces or requires by construction." §T
   pushes the resolved tier capability matrix into the brief; agent
   authors against truth, not "ban these claims." §F adds a
   hardcoded showcase scenario specification; agent designs panels
   against truth, not "discover what to demonstrate."

5. **Fail loud at engine boundaries.** Run-12 §G's silent actor
   mismatch (validators fire in main's session, not sub-agent's) is
   the structural cause of the hand-edit pattern persisting. §G2
   surfaces violations to the right actor at the right time.

6. **Catalog-shaped validators are last resort, not first.** §V is a
   structural-relation validator (compares numeric/categorical claims
   in prose against numeric/categorical fields in adjacent yaml) —
   NOT a phrase-ban list. The load-bearing fix is §T (push truth into
   brief); §V is the backstop that catches §T's residual.

### 2.Q — Strip `## Zerops dev loop` from CLAUDE.md template

**What run 12 showed**: all three published CLAUDE.md files
([apidev:19-22](../runs/12/apidev/CLAUDE.md#L19-L22),
[appdev:19-22](../runs/12/appdev/CLAUDE.md#L19-L22),
[workerdev:21-24](../runs/12/workerdev/CLAUDE.md#L21-L24)) carry the
identical block:

```
## Zerops dev loop

Dev container runs with SSHFS-mounted source. Iterate with `zcli push`, or
edit files over SSH and rerun in place.
```

This is engine-stitched, not agent-authored — the source is
[`internal/recipe/content/templates/codebase_claude.md.tmpl:11-15`](../../../internal/recipe/content/templates/codebase_claude.md.tmpl#L11-L15).
The §C scaffold-brief teaching at
[`content_authoring.md:144-145`](../../../internal/recipe/content/briefs/scaffold/content_authoring.md#L144-L145)
explicitly bans `zcli push` in CLAUDE.md, and the §C work added a
`claude-md-zcp-tool-leak` validator that would have caught the leak.
Run-13 §2 deleted that validator on §4 grounds (correct decision —
it was catalog-shaped), but the template defect was never inspected.

The agent honored the brief perfectly — zero `zerops_*` invocations or
`zcli` references in any agent-authored fragment. The leak is purely
template-side. Each published CLAUDE.md ends up with TWO competing
dev-loop authoritative claims:

- Template-injected `## Zerops dev loop`: *"Iterate with `zcli push`, or
  edit files over SSH and rerun in place."* (authoring-tooling voice)
- Agent's `## Notes` `Dev loop:` first bullet: *"SSH into the dev slot
  and run `npm run start:dev`"* (porter voice)

The two contradict. A porter scanning the file sees the template
version first, may stop reading, and never finds the porter-canonical
version below.

**Fix direction**:

(a) Rewrite the template block. Two options for the rewrite:

**Option A — Delete the section entirely.** The §C brief routes "Dev
loop / SSH / curl" content to `claude-md/notes`
([`content_authoring.md:157`](../../../internal/recipe/content/briefs/scaffold/content_authoring.md#L157)).
Notes section already carries the dev-loop bullet. The hardcoded
section duplicates AND contradicts. Delete it from the template;
agent-authored Notes carries the dev-loop content as the single source
of truth.

**Option B — Replace with a porter-canonical placeholder header
section.** Keep a `## Dev loop` heading but rewrite the body to a
minimal framework-agnostic line (e.g. *"See `## Notes` below for the
codebase-specific dev-loop command."*) so the section is structurally
present but defers to the agent-authored content.

**Recommended: Option A.** Simpler structure; matches the §C routing
rule directly; eliminates the duplicate-source-of-truth problem. Notes
is a known agent-authored section; carrying the dev-loop bullet there
is what §C teaches.

(b) New template after Option A:

```markdown
# CLAUDE.md — {HOSTNAME}

This file guides an AI agent (or human) working in this repo specifically. It
is not a deploy guide (see the commented `zerops.yaml`) and not a porting
guide (see the Integration Guide section of README.md).

## Zerops service facts

<!-- #ZEROPS_EXTRACT_START:service-facts# -->
<!-- #ZEROPS_EXTRACT_END:service-facts# -->

## Notes

<!-- #ZEROPS_EXTRACT_START:notes# -->
<!-- #ZEROPS_EXTRACT_END:notes# -->
```

Three sections (header + service-facts + notes), no dev-loop section
hardcoded. The agent's `claude-md/notes` fragment carries dev-loop +
runbooks + cross-codebase contracts as the single authoritative
location.

**Tests**:

```go
// internal/recipe/assemble_test.go
func TestAssembleCodebaseClaudeMD_NoTemplateInjectedDevLoop(t *testing.T) {
    plan := syntheticShowcasePlan()
    plan.Fragments = map[string]string{
        "codebase/api/claude-md/service-facts": "- Hostname: apidev\n",
        "codebase/api/claude-md/notes":         "- Dev loop: `npm run start:dev`\n",
    }
    body, _, err := AssembleCodebaseClaudeMD(plan, "api")
    if err != nil {
        t.Fatal(err)
    }
    forbidden := []string{
        "zcli push", "zcli vpn",
        "Iterate with",
        "## Zerops dev loop",
    }
    for _, s := range forbidden {
        if strings.Contains(body, s) {
            t.Errorf("template-injected forbidden string %q in CLAUDE.md output:\n%s", s, body)
        }
    }
    // Agent's Notes section is preserved.
    if !strings.Contains(body, "## Notes") {
        t.Errorf("Notes section missing")
    }
    if !strings.Contains(body, "npm run start:dev") {
        t.Errorf("agent-authored dev-loop bullet missing")
    }
}
```

**Acceptance**:
- Run-13 published CLAUDE.md files contain zero `zcli push` / `zcli vpn`
  / `zerops_*` MCP tool name occurrences across all three codebases.
- Each file's `## Notes` section carries the dev-loop bullet as the
  single authoritative location.

**Cost**: template (~5 lines deleted + 1 test). **Value**: highest
porter-perspective single-fix lever — kills R-12-1 + R-12-2 simultaneously,
removes the dual-source-of-truth ambiguity.

### 2.T — Tier capability matrix in scaffold + finalize briefs

**What run 12 showed**: tier 5 README + import.yaml comments ship
factually-wrong claims at scale. Evidence:

- [Tier 5 README:9](../runs/12/environments/5%20—%20Highly-available%20Production/README.md#L9):
  *"every runtime carries at least three replicas"* — yaml emits
  `minContainers: 2` (verified at `runs/12/environments/5 — Highly-available Production/import.yaml:28,45,62`).
- [Tier 5 README:11](../runs/12/environments/5%20—%20Highly-available%20Production/README.md#L11):
  *"Meilisearch keeps a backup"* — yaml emits `mode: NON_HA`
  (verified at line 136).
- [Tier 5 README:25](../runs/12/environments/5%20—%20Highly-available%20Production/README.md#L25):
  *"object-storage replicated"* — no replication field exists in any
  tier; storage emits `objectStorageSize: 1` + `objectStoragePolicy:
  private`.
- [Tier 5 import.yaml:122-128](../runs/12/environments/5%20—%20Highly-available%20Production/import.yaml#L122-L128):
  Meilisearch comment claims "in HA mode keeps a backup" — field is
  `NON_HA` six lines below.
- [Tier 4 import.yaml:107-113](../runs/12/environments/4%20—%20Small%20Production/import.yaml#L107-L113):
  Storage comment claims "Bucket quota at 10 GB" — field is
  `objectStorageSize: 1`.
- [Tier 5 import.yaml:114-124](../runs/12/environments/5%20—%20Highly-available%20Production/import.yaml#L114-L124):
  Storage comment claims "sized at 50 GB" — field is `1`.
- [Tier 2 import.yaml:31-36](../runs/12/environments/2%20—%20Local/import.yaml#L31-L36):
  app comment claims "Vite SPA built once and served as static
  files... Zerops' static-runtime is cheaper than keeping a Node
  process up." Yaml emits `nodejs@22` runtime running `npx vite
  preview` (per appdev/zerops.yaml prod setup) — there IS a Node
  process at this tier.

**Mechanism**:

The engine has truth at:
- [`tiers.go:34-70`](../../../internal/recipe/tiers.go#L34-L70) —
  per-tier `RuntimeMinContainers`, `ServiceMode`, `CPUMode`,
  `CorePackage`, `RunsDevContainer`, `MinFreeRAMGB`.
- [`plan.go:93-103`](../../../internal/recipe/plan.go#L93-L103) —
  `managedServiceSupportsHA` family table.
- [`yaml_emitter.go:283-318`](../../../internal/recipe/yaml_emitter.go#L283-L318) —
  per-service emit logic (storage stays at `objectStorageSize: 1`
  every tier; Meilisearch downgrades HA→NON_HA for non-HA-capable
  families).

The brief composers don't push this truth to the agent. Today's
`tierAudienceLine()` at
[`briefs.go:333-346`](../../../internal/recipe/briefs.go#L333-L346)
returns one fuzzy sentence per tier ("production replicas" — no
number; "managed services in HA mode" — but doesn't qualify the
downgrade). The agent extrapolates from this to:
- "production replicas" → assumes 3 (mental model: HA = at least 3
  for production-grade)
- "managed services in HA mode" → assumes ALL managed services
  uniformly HA, doesn't anticipate the per-family downgrade
- "object storage" → no field info, agent assumes production tiers
  bump quotas

The agent's prose ships against its mental model; engine emits the
literal struct values. They diverge.

**Fix direction**:

(a) Add `BuildTierFactTable(plan *Plan) string` to
[`briefs.go`](../../../internal/recipe/briefs.go). Returns a
deterministic markdown table emitted into the finalize brief AND
the scaffold brief (so the app subagent's tier-aware prose, like
[appdev/README.md IG #1](../runs/12/appdev/README.md#L17-L18), is
also grounded).

The table:

```markdown
## Tier capability matrix

The engine emits these field values per tier — your prose MUST match.

| Tier | RuntimeMinContainers | ServiceMode | CPUMode    | CorePackage | RunsDevContainer | MinFreeRAMGB |
|------|----------------------|-------------|------------|-------------|------------------|--------------|
| 0    | 1                    | NON_HA      | (shared)   | -           | yes (dev-pair)   | -            |
| 1    | 1                    | NON_HA      | (shared)   | -           | yes (dev-pair)   | -            |
| 2    | 1                    | NON_HA      | (shared)   | -           | no (single-slot) | -            |
| 3    | 1                    | NON_HA      | (shared)   | -           | no (single-slot) | 0.5          |
| 4    | 2                    | NON_HA      | (shared)   | -           | no (single-slot) | 0.25         |
| 5    | 2                    | HA          | DEDICATED  | SERIOUS     | no (single-slot) | 0.5          |

## Per-service capability adjustments

At tier 5 (`ServiceMode: HA`), the engine downgrades non-HA-capable
managed-service families to `NON_HA` at emit time. Your prose MUST
reflect the EMITTED mode, not the tier-baseline mode.

| Family | HA-capable | At tier 5 emits |
|--------|------------|-----------------|
| postgresql | yes | `mode: HA` |
| valkey | yes | `mode: HA` |
| nats | yes | `mode: HA` |
| redis | yes | `mode: HA` |
| rabbitmq | yes | `mode: HA` |
| elasticsearch | yes | `mode: HA` |
| meilisearch | NO | `mode: NON_HA` |
| kafka | NO | `mode: NON_HA` |
| (other / unknown) | NO (conservative default) | `mode: NON_HA` |

## Storage / quota fields the engine fixes

Object-storage emits `objectStorageSize: 1` + `objectStoragePolicy:
private` UNIFORMLY across all tiers. Do NOT claim larger quotas
("10 GB", "50 GB") or replication for storage at any tier — those
fields don't exist on `ServiceKindStorage` in the current emit.

## In your prose

When a tier README, env import-comment, or codebase IG yaml-block-
comment claims a number or category for any of these fields, the
claim MUST match the table. "Three replicas" prose paired with
`minContainers: 2` field is a defect — pick "two replicas" or
restructure prose to omit the number.
```

(b) Wire into brief composers:

- [`BuildFinalizeBrief`](../../../internal/recipe/briefs.go#L243):
  insert `BuildTierFactTable(plan)` after the existing tier map,
  before audience paths.
- [`BuildScaffoldBrief`](../../../internal/recipe/briefs.go#L97): add
  the table conditionally — only for codebases whose role is
  frontend (the SPA codebase ships tier-aware prose; api/worker
  rarely do). Emits after platform_principles atom.

(c) Brief size budgets:
- ScaffoldBriefCap raises 22 KB → 24 KB to fit ~1.5 KB table when
  frontend role triggers it.
- FinalizeBriefCap raises 12 KB → 14 KB for the table + the existing
  fragment list / math / anti-patterns.

**Tests**:

```go
// internal/recipe/briefs_test.go
func TestBuildTierFactTable_EmitsAllTiers(t *testing.T) {
    plan := syntheticShowcasePlan()
    table := BuildTierFactTable(plan)
    for i := 0; i < 6; i++ {
        if !strings.Contains(table, fmt.Sprintf("| %d ", i)) {
            t.Errorf("tier %d row missing in table:\n%s", i, table)
        }
    }
    // Tier 5 row pins `minContainers: 2` (NOT 3) — direct counter to
    // run-12 R-12-3.
    if !strings.Contains(table, "| 5    | 2 ") {
        t.Errorf("tier 5 RuntimeMinContainers value not 2:\n%s", table)
    }
    // Per-service capability adjustments name meilisearch as NON_HA at tier 5.
    if !strings.Contains(table, "meilisearch") || !strings.Contains(table, "`mode: NON_HA`") {
        t.Errorf("meilisearch downgrade row missing")
    }
    // Storage emits objectStorageSize: 1 uniformly.
    if !strings.Contains(table, "objectStorageSize: 1") {
        t.Errorf("storage uniform-size note missing")
    }
}

func TestBuildFinalizeBrief_IncludesTierFactTable(t *testing.T) {
    plan := syntheticShowcasePlan()
    brief, err := BuildFinalizeBrief(plan)
    if err != nil { t.Fatal(err) }
    mustContain(t, brief.Body, "## Tier capability matrix")
    mustContain(t, brief.Body, "## Per-service capability adjustments")
    mustContain(t, brief.Body, "meilisearch")
}

func TestBuildScaffoldBrief_FrontendIncludesTierFactTable(t *testing.T) {
    plan := syntheticShowcasePlan()
    var frontendCB Codebase
    for _, cb := range plan.Codebases {
        if cb.Role == RoleFrontend {
            frontendCB = cb
            break
        }
    }
    brief, err := BuildScaffoldBrief(plan, frontendCB, nil)
    if err != nil { t.Fatal(err) }
    mustContain(t, brief.Body, "## Tier capability matrix")
}

func TestBuildScaffoldBrief_APINotIncludeTierFactTable(t *testing.T) {
    // The api codebase doesn't author tier-aware prose; table omitted.
    plan := syntheticShowcasePlan()
    var apiCB Codebase
    for _, cb := range plan.Codebases {
        if cb.Role == RoleAPI {
            apiCB = cb
            break
        }
    }
    brief, err := BuildScaffoldBrief(plan, apiCB, nil)
    if err != nil { t.Fatal(err) }
    mustNotContain(t, brief.Body, "## Tier capability matrix")
}
```

**Acceptance**:
- Run-13 tier 5 README claims `minContainers: 2` (NOT 3); does NOT
  claim "Meilisearch keeps a backup"; does NOT claim "object-storage
  replicated".
- Run-13 tier yaml comments don't claim numeric quotas / replica
  counts that contradict the field 6-10 lines below.
- Tier 2 / 3 / 4 / 5 prose factually matches yaml emits.

**Cost**: engine (~80 LoC + 4 tests + ~50 lines of brief content the
engine emits at compose time). **Value**: highest factuality lever —
fixes R-12-3 across every tier where engine adjusts fields silently.

### 2.V — Post-stitch validator: tier-prose-vs-emit divergence

**What run 12 showed**: §T closes the source-of-truth gap (agent gets
the table, authors against truth), but agent extrapolation can still
produce divergent prose. §V is the structural-relation backstop.

**Fix direction**:

Add `validateTierProseVsEmit` in
[`validators_root_env.go`](../../../internal/recipe/validators_root_env.go),
registered against `SurfaceEnvImportComments` and
`SurfaceEnvREADME`. The validator scans tier-prose for numeric or
categorical claims and cross-checks them against the emitted yaml at
the same tier.

This is NOT a phrase-ban list — it's a structural relation between
two yaml elements (or a markdown claim and a yaml field). Per system.md
§4 verdict-table criteria, structural-relation validators are TEACH-
side defensible.

(a) Detected divergences:

| Pattern in prose | Cross-check against emit | Violation code |
|---|---|---|
| `\b(\d+)\s+replicas?\b` adjacent to a runtime block | `minContainers: <not-N>` | `tier-prose-replica-count-mismatch` |
| `\b(HA|high availability|backed-up|replicated)\b` adjacent to a managed service block | `mode: NON_HA` field | `tier-prose-ha-claim-vs-non-ha` |
| `\b(\d+)\s*(GB|MB)\b` adjacent to a storage block | `objectStorageSize: <not-N>` | `tier-prose-storage-size-mismatch` |
| `\bstatic[ -]runtime\b` claim adjacent to `app` runtime block | `type: nodejs@<X>` (NOT `static`) field | `tier-prose-runtime-type-mismatch` |
| `\bdedicated\s+CPU\b` claim adjacent to a runtime block | tier `CPUMode != "DEDICATED"` | `tier-prose-cpu-mode-mismatch` |

Each violation includes: prose excerpt (≤80 chars), field value,
hostname, tier index. Severity: `SeverityNotice` (DISCOVER side per
§4 — agent sees the lesson but publication doesn't block, since §T
is the load-bearing fix and §V is backstop).

(b) Algorithm sketch:

```go
func validateTierProseVsEmit(_ context.Context, path string, body []byte, inputs SurfaceInputs) ([]Violation, error) {
    if inputs.Plan == nil {
        return nil, nil
    }
    tierKey := tierKeyFromPath(path)
    if tierKey == "" {
        return nil, nil
    }
    tierIdx, _ := parseTierIndex(tierKey)
    tier, ok := TierAt(tierIdx)
    if !ok {
        return nil, nil
    }
    var vs []Violation
    s := string(body)
    // Walk service blocks in the yaml; for each block, check the
    // preceding comment paragraph + its prose claims.
    blocks := parseYAMLServiceBlocks(s) // new helper
    for _, blk := range blocks {
        comment := blk.precedingComment
        if comment == "" { continue }
        // Replica-count claim vs minContainers
        if m := replicaClaimRE.FindStringSubmatch(comment); m != nil {
            claimed, _ := strconv.Atoi(m[1])
            actual := blk.minContainers
            if actual == 0 { actual = 1 } // platform default
            if claimed != actual {
                vs = append(vs, notice("tier-prose-replica-count-mismatch", path,
                    fmt.Sprintf("tier %d / %s: prose claims %d replicas; field emits minContainers: %d",
                        tierIdx, blk.hostname, claimed, actual)))
            }
        }
        // HA claim vs mode
        if haClaimRE.MatchString(comment) && blk.mode == "NON_HA" {
            vs = append(vs, notice("tier-prose-ha-claim-vs-non-ha", path,
                fmt.Sprintf("tier %d / %s: prose claims HA / replicated / backed-up; field emits mode: NON_HA (see %s)",
                    tierIdx, blk.hostname, capabilityHint(blk.serviceType))))
        }
        // Storage quota claim
        if m := storageSizeRE.FindStringSubmatch(comment); m != nil {
            claimed, _ := strconv.Atoi(m[1])
            actual := blk.objectStorageSize
            if claimed != actual {
                vs = append(vs, notice("tier-prose-storage-size-mismatch", path,
                    fmt.Sprintf("tier %d / %s: prose claims %d %s; field emits objectStorageSize: %d",
                        tierIdx, blk.hostname, claimed, m[2], actual)))
            }
        }
        // ... runtime-type claim, cpu-mode claim
    }
    return vs, nil
}
```

(c) Where it runs:
- Wire into [`gates.go::EnvGates`](../../../internal/recipe/gates.go#L68)
  via `gateEnvSurfaceValidators` (the env-import-comments registration
  already exists; this validator extends behaviour for the same
  surface).
- Tier-README cross-checks live in `validateEnvREADME` extension — the
  README mentions tier-level capability claims ("every runtime carries
  three replicas") which the validator extracts and checks against
  the resolved tier struct (no per-block context needed for tier-README
  claims, since they're statements about the tier as a whole).

**Tests**:

```go
func TestValidateTierProseVsEmit_FlagsReplicaCountMismatch(t *testing.T) {
    plan := syntheticShowcasePlan()
    body := `
# Three replicas because production scale.
- hostname: api
  type: nodejs@22
  minContainers: 2
`
    vs, _ := validateTierProseVsEmit(ctx, "5/import.yaml", []byte(body), SurfaceInputs{Plan: plan})
    mustHaveCode(t, vs, "tier-prose-replica-count-mismatch")
}

func TestValidateTierProseVsEmit_FlagsHAClaimVsNonHA(t *testing.T) {
    body := `
# Meilisearch in HA mode keeps a backup of the index.
- hostname: search
  type: meilisearch@1.20
  mode: NON_HA
`
    vs, _ := validateTierProseVsEmit(ctx, "5/import.yaml", []byte(body), inputs)
    mustHaveCode(t, vs, "tier-prose-ha-claim-vs-non-ha")
}

func TestValidateTierProseVsEmit_AcceptsConsistentClaim(t *testing.T) {
    body := `
# Two replicas because rolling deploys need a sibling.
- hostname: api
  type: nodejs@22
  minContainers: 2
`
    vs, _ := validateTierProseVsEmit(ctx, "4/import.yaml", []byte(body), inputs)
    mustNotHaveCode(t, vs, "tier-prose-replica-count-mismatch")
}
```

**Acceptance**:
- Run-13 finalize complete-phase fires zero `tier-prose-*-mismatch`
  notices (the §T table eliminated them at the source).
- The validator is in place as a backstop for run-14+ regressions.

**Cost**: engine (~50 LoC + 4 tests). **Value**: medium — backstop for
§T. Independent value when §T's brief teaching doesn't fully land.

### 2.F — Showcase scenario specification + queue-demo panel

**What run 12 showed + user feedback**: The feature dispatch wrapper
([main-session.jsonl@feature-dispatch:line 60-104](../runs/12/SESSION_LOGS/main-session.jsonl))
told the feature subagent:

> Three scaffold sub-agents have ALREADY completed and committed.
> All scaffold fragments + facts are already recorded. The
> end-to-end queue-demo round-trip (api inserts row → publishes
> `items.indexed` → worker indexes into Meilisearch → search
> returns it) is already proven by the scaffold api sub-agent.

The wrapper DESCRIBES queue-demo as already-proven via scaffold-api's
curl. The work-to-do list (lines 1-6 of the wrapper) names cache-demo,
verification, browser-walks, redeploys, fragment extensions, commits.
**Nothing in the wrapper says the SPA must demonstrate queue-demo
visibly.** Result: appdev SPA panels are Items / Status / Upload —
zero queue/broker visualization. A porter clicking the recipe sees
no demonstration of the broker pathway.

User feedback: *"think of a simple way to describe the showcase app
somewhere in the subagent dispatch message I guess, the spec can be
hardcoded, its framework agnostic, demonstrate redis, nats, object
storage, db, modern design, focus on demonstration components, not the
overal chrome"*.

**Fix direction**:

(a) Add a "Showcase scenario specification" section to
[`briefs/feature/feature_kinds.md`](../../../internal/recipe/content/briefs/feature/feature_kinds.md).
The section is hardcoded, framework-agnostic, mandates one
demonstration panel per managed-service category.

```markdown
# Showcase scenario specification

A `tier=showcase` recipe MUST produce an SPA (in the frontend codebase)
that visibly demonstrates EVERY managed-service category the recipe
provisions. The reader is a porter clicking the published recipe to
see what each managed service can do — the SPA is the recipe's
demonstration surface.

## Mandate: one demonstration panel per managed-service category

The frontend codebase MUST render these panels:

| Panel | Proves | Mandatory observable |
|-------|--------|----------------------|
| **Items / DB** | crud through database | Form to create + list view; row count survives container restart. |
| **Cache** | cache-demo (read-through) | `X-Cache: HIT/MISS` header visible in the panel; trigger button + display. |
| **Queue / Broker** | queue-demo through worker | Trigger button publishes a job; live feed shows worker processed it (log tail or status indicator); resulting indexed document appears in the search panel within seconds. |
| **Storage** | storage-upload (object storage) | File picker + upload button; on success, signed URL displayed; clicking URL retrieves the same bytes. |
| **Search** | search-items (full-text) | Search box; query against the items list with ranked results. |
| **Status** (optional but recommended) | per-service liveness | One row per managed service (api, db, cache, broker, search, storage); per-service ping result `ok` / `down`. |

## Design priorities

- **Modern design.** Clean component shapes, sensible spacing, basic
  responsive layout. Tailwind utilities or framework defaults are
  fine; do not author a custom design system.
- **Demonstration components, not chrome.** Spend effort on what each
  panel demonstrates (the queue-demo's live feed, the cache-demo's
  HIT/MISS indicator, the search results ranking) — NOT on branding,
  hero sections, marketing copy, multi-column dashboards, or
  decorative iconography.
- **Reading order is panel-first.** A porter scanning the page sees
  panels in the order: Items, Cache, Queue, Storage, Search, Status.
  Headings explain what each panel proves.
- **Data is real.** Panels exercise the actual deployed managed
  services; no mock data, no client-only fixtures. The Items panel
  shows real Postgres rows; the Queue panel shows real worker output;
  the Search panel shows real Meilisearch hits.

## Per-panel browser-verification

After implementing the panels, run `zerops_browser` against the SPA
and exercise EACH panel. For each, record one fact:

```
zerops_recipe action=record-fact slug=<slug>
  topic=<frontend-cb>-<panel>-browser
  symptom="<what you saw + whether the demonstration signal was visible>"
  mechanism="zerops_browser"
  surfaceHint=browser-verification
  citation=none
  scope=<frontend-cb>/<panel>
  extra.console=<digest>
  extra.screenshot=<path or none-snapshot-only>
```

Mandatory facts:
- `<frontend-cb>-items-browser` — Items panel renders + create works
- `<frontend-cb>-cache-browser` — Cache panel shows `X-Cache: MISS`
  on first call, `HIT` on second
- `<frontend-cb>-queue-browser` — Queue panel: publish trigger fires;
  worker processes (visible somehow); indexed document appears
- `<frontend-cb>-storage-browser` — Upload + signed-URL retrieve
- `<frontend-cb>-search-browser` — Search query returns ranked hits

Status panel verification optional. Any browser walk that produces
console errors is a regression — fix before close.

## Panel scope, not feature-kind scope

The feature_kinds taxonomy (above) names the BACKEND endpoints each
demonstration requires. The PANELS are the frontend's responsibility.
A queue-demo backend that's never visualized fails this scenario spec
even if curl proves the round-trip works.
```

(b) Wire into the dispatch:

The feature brief composer already loads `feature_kinds.md` (per
[`briefs.go:189-194`](../../../internal/recipe/briefs.go#L189-L194)). The
new "Showcase scenario specification" section sits AFTER the existing
feature-kind catalog, BEFORE the symbol table. Engine appends it for
recipes whose `Plan.Tier == "showcase"`.

(c) Brief composer addition (briefs.go::BuildFeatureBrief):

```go
atoms := []string{
    "briefs/feature/feature_kinds.md",
    "briefs/feature/content_extension.md",
    "principles/mount-vs-container.md",
    "principles/yaml-comment-style.md",
}
if planDeclaresSeed(plan) {
    atoms = append(atoms, "principles/init-commands-model.md")
}
if plan.Tier == "showcase" {
    atoms = append(atoms, "briefs/feature/showcase_scenario.md")
}
```

The new file `briefs/feature/showcase_scenario.md` carries the section
content above. Conditional on tier (hello-world / minimal recipes
don't get the showcase mandate — they have fewer managed services).

**Tests**:

```go
func TestBuildFeatureBrief_ShowcaseTierIncludesScenarioSpec(t *testing.T) {
    plan := syntheticShowcasePlan()
    plan.Tier = "showcase"
    brief, _ := BuildFeatureBrief(plan)
    mustContain(t, brief.Body, "Showcase scenario specification")
    mustContain(t, brief.Body, "Queue / Broker")
    mustContain(t, brief.Body, "Per-panel browser-verification")
    mustContain(t, brief.Body, "queue-browser")
}

func TestBuildFeatureBrief_MinimalTierOmitsScenarioSpec(t *testing.T) {
    plan := syntheticShowcasePlan()
    plan.Tier = "minimal"
    brief, _ := BuildFeatureBrief(plan)
    mustNotContain(t, brief.Body, "Showcase scenario specification")
}
```

**Acceptance**:
- Run-13 SPA renders panels for items / cache / queue / storage /
  search (all five demonstration categories).
- Run-13 facts.jsonl contains 5+ browser-verification entries with
  `topic` matching `<host>-{items,cache,queue,storage,search}-browser`.
- Queue panel demonstrates publish → worker → search; concrete
  observables in the recorded fact's symptom field.
- Modern design, panel-first reading order; chrome minimal.

**Cost**: content (~40 lines new atom + ~10 LoC composer change + 2
tests). **Value**: highest scenario-coverage lever. Closes the user-
flagged gap directly.

### 2.N — Init-commands decomposed-step key collision atom extension

**What run 12 showed**: scaffold-api recorded
[`execOnce-key-collision-across-decomposed-steps`](../runs/12/environments/facts.jsonl#L2):

> Mechanism: `zsc execOnce` uses the first positional arg as the lock
> key. Two `initCommands` sharing the same `${appVersionId}` key are
> ONE lock — first runner wins and writes the success marker; second
> runner sees the marker and skips silently, even though the command-
> args differ. The decomposition rule (one execOnce per logical step)
> requires DISTINCT keys per step, e.g. `${appVersionId}-migrate` and
> `${appVersionId}-seed`.

The atom
[`principles/init-commands-model.md`](../../../internal/recipe/content/principles/init-commands-model.md)
covers decomposition (lines 37-42) — *"When one command does multiple
non-idempotent things, either gate all on one static key or split
into separate `initCommands` with shapes matching each operation's own
lifetime. Don't mix lifetimes under one key."* But it does NOT
explicitly call out the key-COLLISION trap when you decompose with the
same `${appVersionId}` baseline.

**Fix direction**:

Append a paragraph to the atom's `## Decomposition` section:

```markdown
**Distinct keys per step.** When you split work into multiple
`initCommands`, each step needs a DISTINCT lock key. Two commands
sharing the same `${appVersionId}` collapse to one lock — the first
runner wins and writes the success marker; the second sees the
marker and skips silently even though the command tail differs.

```yaml
# WRONG — both commands share the same ${appVersionId} lock; only one runs
initCommands:
  - zsc execOnce ${appVersionId} --retryUntilSuccessful -- node dist/migrate.js
  - zsc execOnce ${appVersionId} --retryUntilSuccessful -- node dist/seed.js

# RIGHT — each step gets its own distinct key under the same deploy version
initCommands:
  - zsc execOnce ${appVersionId}-migrate --retryUntilSuccessful -- node dist/migrate.js
  - zsc execOnce ${appVersionId}-seed --retryUntilSuccessful -- node dist/seed.js
```
```

**Tests**:

```go
func TestInitCommandsAtom_TeachesDecomposedStepKeyDistinction(t *testing.T) {
    atom, _ := readAtom("principles/init-commands-model.md")
    mustContain(t, atom, "Distinct keys per step")
    mustContain(t, atom, "${appVersionId}-migrate")
    mustContain(t, atom, "${appVersionId}-seed")
    mustContain(t, atom, "collapse to one lock")
}
```

**Acceptance**:
- Run-13 scaffold-api does NOT rediscover the key-collision trap (no
  fact recorded for `execOnce-key-collision-across-decomposed-steps`
  if a similar scenario surfaces).

**Cost**: content (~12 lines added to atom + 1 test). **Value**: low
(saves ~30s per recipe with decomposed init-commands).

### 2.U — Alias-type contracts: resolution timing footnote

**What run 12 showed**: scaffold-app recorded
[`cross-service-alias-resolution-timing`](../runs/12/environments/facts.jsonl#L3):

> Mechanism: `${<host>_zeropsSubdomain}` resolves to the platform-
> injected URL only after the target service's first deploy mints it.
> When the SPA's prod build runs before apistage has ever deployed,
> the build container reads the literal token and Vite's `define`
> plugin inlines that token verbatim into dist/. Parallel scaffold
> dispatch (engine policy) makes this race visible: app and api
> scaffold in parallel, and whichever finishes first publishes a
> stale build.

The §A alias-type contracts table at
[`platform_principles.md:64-94`](../../../internal/recipe/content/briefs/scaffold/platform_principles.md#L64-L94)
covers alias SHAPE but says nothing about resolution TIMING. SPA
recipes (Vite, Webpack) that bake URLs at build time are vulnerable.

**Fix direction**:

Append a "Resolution timing" footnote to the alias-type contracts
section (between the table and the existing `${zeropsSubdomainHost}`
paragraph):

```markdown
**Resolution timing.** `${<host>_zeropsSubdomain}` is a literal token
(`${...}` verbatim) until the target service's first deploy mints
the URL. Build-time-baked references (Vite `define`, Webpack
`DefinePlugin`, Astro/Next/SvelteKit static-site builds) must order
the dependency's first deploy BEFORE consuming the alias — otherwise
the build container reads the literal token and the bundle ships
with `${apistage_zeropsSubdomain}` baked in instead of the resolved
URL.

For runtime references (`process.env.APISTAGE_URL` read at request
time), the alias resolves on container start — no ordering concern.
The race only bites build-time consumers.

Recovery for build-time consumers: deploy the target service first,
verify the subdomain is minted, THEN trigger the consumer's build.
Parallel scaffold dispatch makes this race visible — an SPA build
running in parallel with the api's first deploy is the canonical
scenario.
```

**Tests**:

```go
func TestPlatformPrinciplesAtom_TeachesAliasResolutionTiming(t *testing.T) {
    atom, _ := readAtom("briefs/scaffold/platform_principles.md")
    mustContain(t, atom, "Resolution timing")
    mustContain(t, atom, "literal token")
    mustContain(t, atom, "Build-time-baked references")
    mustContain(t, atom, "no ordering concern")
}
```

**Acceptance**:
- Run-13 scaffold-app does NOT rediscover the alias-resolution-timing
  trap (no fact recorded if a similar scenario surfaces).

**Cost**: content (~12 lines + 1 test). **Value**: low-medium (saves
~3 min per SPA recipe rediscovering the race).

### 2.I-feature — Feature content_extension §I IG-scope rule

**What run 12 showed**: apidev IG carries 7 numbered items + 2
unnumbered prose subsections (`### Cache-demo wrapper`, `### Liveness
probe`) inside the integration-guide extract markers
([apidev/README.md:250-272](../runs/12/apidev/README.md#L250-L272)).
appdev has the same shape (`### Cache-demo form on the items panel`,
`### Status panel`).

The §I IG-scope rule lives in scaffold's
[`content_authoring.md:45-65`](../../../internal/recipe/content/briefs/scaffold/content_authoring.md#L45-L65).
The feature brief
[`content_extension.md:36`](../../../internal/recipe/content/briefs/feature/content_extension.md#L36)
says *"Typical scale: 1–2 KB bullets + 0–1 IG item per feature. Most
features change code, not topology."* — but doesn't explicitly carry
the IG-scope rule (what BELONGS in IG vs what doesn't).

Result: feature appended cache-demo + liveness-probe prose to
`codebase/<h>/integration-guide` fragments via default `mode=append`,
landing inside the IG markers. These ARE recipe-internal contracts
(cache-demo wrapper behaviour, liveness probe pattern) that §I would
have routed to KB or claude-md/notes.

**Fix direction**:

Add an "IG scope (extending scaffold's items)" subsection to
[`content_extension.md`](../../../internal/recipe/content/briefs/feature/content_extension.md):

```markdown
## IG scope (extending scaffold's items)

If your feature genuinely changes what a porter has to do to deploy
their NestJS / Laravel / SvelteKit app on Zerops (binding, trust-
proxy, env-aliasing, init-commands, deploy-files), `record-fragment
mode=append fragmentId=codebase/<h>/integration-guide` adds the new
item.

If your feature adds a recipe-internal CONTRACT (a new endpoint
shape, a cache-demo wrapper's TTL convention, a NATS subject naming
rule, a queue-group name), that goes to KB
(`record-fragment mode=append fragmentId=codebase/<h>/knowledge-base`)
or claude-md/notes
(`record-fragment mode=append fragmentId=codebase/<h>/claude-md/notes`)
— NOT to integration-guide. The IG audience is a porter bringing
their own code; recipe-internal contracts are not their concern.

Examples:
- Adding `forcePathStyle: true` for object storage → IG (porter
  needs to do this in their own code)
- Adding a `/items/:id` cache-demo wrapper with `X-Cache: HIT/MISS`
  → KB or claude-md/notes (recipe-internal endpoint shape)
- Adding `app.enableCors({ exposedHeaders: ['X-Cache'] })` → KB
  (platform × framework intersection: CORS expose-headers behaviour)
- Adding a `/status` aggregator endpoint → claude-md/notes
  (recipe-internal liveness pattern)

Aim for 0-1 IG appends per feature; 1-2 KB bullets is normal; 0-3
claude-md/notes additions is normal. If you find yourself adding 2+
IG items, check whether the additions are recipe-internal contracts
that belong elsewhere.
```

**Tests**:

```go
func TestFeatureContentExtensionAtom_TeachesIGScopeRule(t *testing.T) {
    atom, _ := readAtom("briefs/feature/content_extension.md")
    mustContain(t, atom, "IG scope (extending scaffold's items)")
    mustContain(t, atom, "recipe-internal CONTRACT")
    mustContain(t, atom, "Aim for 0-1 IG appends")
}
```

**Acceptance**:
- Run-13 feature subagent appends to `codebase/<h>/integration-guide`
  zero or one times per codebase. Recipe-internal contracts route to
  KB or claude-md/notes.

**Cost**: content (~25 lines + 1 test). **Value**: medium (sharpens
the IG audience contract; closes R-12-12).

### 2.W — Anti-patterns atom: allow `record-fragment mode=replace`

**What run 12 showed**:
[`anti_patterns.md:8-11`](../../../internal/recipe/content/briefs/finalize/anti_patterns.md#L8-L11)
says:

> Do NOT touch `codebase/<h>/{intro,integration-guide,knowledge-base,
> claude-md/*}` ids — scaffold + feature have already validated
> their content at their own complete-phase. By finalize, those
> surfaces are green.

But §R explicitly enabled `mode=replace` for these IDs so finalize
CAN touch them when needed. The atom contradicts §R; finalize sub-
agent reads "do not touch" and never sees the §R escape hatch.

**Fix direction**:

Rewrite the bullet:

```markdown
- By finalize, codebase fragments should be green (scaffold + feature
  validated them at their own complete-phase). If finalize complete-
  phase still flags a residual codebase fragment violation, use
  `record-fragment mode=replace` to correct it — the §R API was added
  exactly for this case. Default mode for codebase ids is append; pass
  `mode=replace` only when correcting an existing fragment.
```

**Tests**:

```go
func TestFinalizeAntiPatternsAtom_AllowsReplaceMode(t *testing.T) {
    atom, _ := readAtom("briefs/finalize/anti_patterns.md")
    mustContain(t, atom, "record-fragment mode=replace")
    mustContain(t, atom, "§R API was added exactly for this case")
}
```

**Acceptance**:
- If run-13 finalize complete-phase flags a residual codebase fragment
  violation, the sub-agent uses `mode=replace` to correct (no hand-
  edit fall-back).

**Cost**: content (4 lines + 1 test). **Value**: low (eliminates one
contradiction).

### 2.G2 — Per-codebase complete-phase scoping

**What run 12 showed**: §G's acceptance criterion was "the right author
(scaffold sub-agent) gets the violation in their own session." Actual
behaviour: sub-agents terminate at 20:18:21–20:22:26; main calls
`complete-phase scaffold` at 20:22:40 (post-termination); main
inherits 13 violations; main fixes via 7 direct `Edit` calls on
`apidev/zerops.yaml` + `workerdev/zerops.yaml` plus 3 `record-fragment
mode=replace`.

`complete-phase` doesn't accept a codebase parameter today
([handlers.go:228](../../../internal/recipe/handlers.go#L228)); the
control flow forces main to be the actor.

User selected option (b) for Q2: keep yaml as file; sub-agent calls
complete-phase before terminating; sub-agent ssh-edits zerops.yaml as
needed.

**Fix direction**:

(a) Engine: extend `complete-phase` to accept optional `codebase=<host>`.
When set, runs codebase gates scoped to that codebase's surface paths
only.

```go
// internal/recipe/handlers.go::RecipeInput
Codebase string `json:"codebase,omitempty" jsonschema:"For build-brief OR complete-phase: codebase hostname. complete-phase with codebase set scopes codebase gates to that codebase only — sub-agent's pre-termination self-validate path. Unset = full phase gates (main agent's phase-close path)."`
```

(b) Engine: dispatcher passes the codebase scope through to a new
`completePhaseScoped` helper.

```go
// internal/recipe/handlers.go::completePhase
func completePhase(sess *Session, in RecipeInput, r RecipeResult) RecipeResult {
    if sess.Current == PhaseScaffold || sess.Current == PhaseFeature {
        if err := stitchCodebases(sess); err != nil {
            r.Error = "complete-phase: pre-stitch codebases: " + err.Error()
            return r
        }
    }
    var blocking, notices []Violation
    var err error
    if in.Codebase != "" {
        // Sub-agent's pre-termination self-validate. Run codebase gates
        // scoped to this codebase only — the subagent doesn't have
        // jurisdiction over peer codebases.
        if err := validateCodebaseExists(sess.Plan, in.Codebase); err != nil {
            r.Error = err.Error()
            return r
        }
        blocking, notices, err = sess.CompletePhaseScoped(
            CodebaseGates(),
            in.Codebase,
        )
    } else {
        blocking, notices, err = sess.CompletePhase(gatesForPhase(sess.Current))
    }
    if err != nil {
        r.Error = err.Error()
        return r
    }
    snap := sess.Snapshot()
    r.Violations, r.Notices, r.Status = blocking, notices, &snap
    r.OK = len(blocking) == 0
    if r.OK && in.Codebase == "" {
        // Phase advance only on full close. Per-codebase scoped close is
        // a self-validate, not a phase-state mutation.
        if next, ok := nextPhase(sess.Current); ok {
            r.Guidance = "Next phase: " + string(next) + "\n\n" + loadPhaseEntry(next)
        }
    }
    return r
}
```

(c) Engine: new `Session.CompletePhaseScoped` method.

```go
// internal/recipe/workflow.go
func (s *Session) CompletePhaseScoped(gates []Gate, codebase string) (blocking, notices []Violation, err error) {
    s.mu.Lock()
    defer s.mu.Unlock()
    // Filter Plan.Codebases to just the named codebase, run gates
    // against that subset. Phase NOT marked complete — this is a
    // self-validate, not a transition.
    scopedPlan := *s.Plan
    scopedPlan.Codebases = nil
    for _, cb := range s.Plan.Codebases {
        if cb.Hostname == codebase {
            scopedPlan.Codebases = append(scopedPlan.Codebases, cb)
            break
        }
    }
    if len(scopedPlan.Codebases) == 0 {
        return nil, nil, fmt.Errorf("codebase %q not in plan", codebase)
    }
    ctx := GateContext{
        Plan:       &scopedPlan,
        OutputRoot: s.OutputRoot,
        FactsLog:   s.FactsLog,
        Parent:     s.Parent,
    }
    blocking, notices = PartitionBySeverity(RunGates(gates, ctx))
    return blocking, notices, nil
}
```

(d) Brief teaching: extend
[`phase_entry/scaffold.md`](../../../internal/recipe/content/phase_entry/scaffold.md)
"Complete-phase gate" section + scaffold brief
[`content_authoring.md`](../../../internal/recipe/content/briefs/scaffold/content_authoring.md):

```markdown
## Self-validate before terminating

Before you terminate, call:

    zerops_recipe action=complete-phase phase=scaffold codebase=<your-host>

This runs the codebase-scoped validators (IG / KB / CLAUDE / yaml-
comment / source-comment-voice) against your codebase's surfaces.

If `ok:true`: all your work passes the gate; safe to terminate.

If `ok:false` with violations:
- Violations on `codebase/<host>/{integration-guide,knowledge-base,
  claude-md/*}` ids → fix via `record-fragment mode=replace
  fragmentId=codebase/<host>/<name> fragment=<corrected body>`.
- Violations on `<SourceRoot>/zerops.yaml` (yaml-comment-missing-
  causal-word, IG-scaffold-filename, etc.) → ssh-edit the yaml file
  directly; it's not a fragment, it's the committed source. After
  ssh-edit, the engine's IG item-1 generator will re-read the yaml
  body on next stitch.
- Re-call `complete-phase phase=scaffold codebase=<your-host>` to
  verify the fix.
- Repeat until `ok:true`, then terminate.

The phase-level `complete-phase` (no codebase parameter) is the main
agent's responsibility after all sub-agents return — it advances the
phase state. Your job is just to ensure your own codebase's gate
passes before you exit.
```

(e) Same teaching added to
[`briefs/feature/content_extension.md`](../../../internal/recipe/content/briefs/feature/content_extension.md)
for feature subagents (single sub-agent, but same self-validate pattern
— call `complete-phase phase=feature codebase=<host>` per touched
codebase before terminating).

**Tests**:

```go
// internal/recipe/handlers_test.go
func TestCompletePhase_CodebaseScoped_OnlyValidatesNamedCodebase(t *testing.T) {
    sess := newTestSession(t, withTwoCodebases())
    // Record one fragment violation on api, none on app
    sess.RecordFragment("codebase/api/integration-guide", "1. plain ordered\n2. list shape\n", "")
    sess.RecordFragment("codebase/app/integration-guide", "### 1. Adding zerops.yaml\n### 2. Bind 0.0.0.0\n", "")
    sess.StitchCodebases()

    // Scoped to api: violation surfaces
    apiResult := completePhase(sess, RecipeInput{Codebase: "api"}, RecipeResult{})
    mustHaveCode(t, apiResult.Violations, "codebase-ig-plain-ordered-list")

    // Scoped to app: clean
    appResult := completePhase(sess, RecipeInput{Codebase: "app"}, RecipeResult{})
    mustNotHaveCode(t, appResult.Violations, "codebase-ig-plain-ordered-list")
    mustEqual(t, appResult.OK, true)
}

func TestCompletePhase_CodebaseScoped_DoesNotAdvancePhase(t *testing.T) {
    sess := newTestSession(t)
    sess.Current = PhaseScaffold
    completePhase(sess, RecipeInput{Codebase: "api"}, RecipeResult{})
    // Phase stays scaffold — self-validate doesn't transition.
    mustEqual(t, sess.Completed[PhaseScaffold], false)
}

func TestCompletePhase_NoCodebase_AdvancesPhase(t *testing.T) {
    sess := newTestSession(t)
    sess.Current = PhaseScaffold
    sess.RecordFragment(...) // valid content for all codebases
    completePhase(sess, RecipeInput{}, RecipeResult{})
    mustEqual(t, sess.Completed[PhaseScaffold], true)
}

func TestCompletePhase_UnknownCodebase_Errors(t *testing.T) {
    sess := newTestSession(t)
    r := completePhase(sess, RecipeInput{Codebase: "nonexistent"}, RecipeResult{})
    mustContain(t, r.Error, "codebase \"nonexistent\" not in plan")
}
```

**Acceptance**:
- Run-13 scaffold sub-agents call `complete-phase phase=scaffold
  codebase=<host>` before terminating. If violations fire, sub-agents
  fix in-session.
- Zero main-agent `Edit` calls during scaffold-close.
- Run-13 main agent's `complete-phase scaffold` (no codebase) fires
  zero codebase-scoped blocking violations (sub-agents already
  cleared them).

**Cost**: engine (~30 LoC + 4 tests + brief teaching ~25 lines).
**Value**: highest engine-flow lever — closes the §G actor mismatch
that persisted through run 12.

### 2.Y2D — Suppress Y2 fallback duplicate on dev-pair stage slot

**What run 12 showed**: Tier 0 + 1 dev-pair runtime services have
DUPLICATE comments above each slot. Y2's fallback (run-12 §Y2) reads
`comments[cb.Hostname + "dev"]` then `comments[cb.Hostname]`. Both
`writeRuntimeDev` and `writeRuntimeStage` hit the bare-codebase
fallback, both render the same comment.

[Tier 0 import.yaml:14-37](../runs/12/environments/0%20—%20AI%20Agent/import.yaml#L14-L37)
shows `apidev` and `apistage` carrying identical 6-line comment blocks.
Same pattern for `appdev`/`appstage` and `workerdev`/`workerstage`.

**Fix direction**:

Track the last-rendered-from-fallback comment in `writeDeliverableServices`
and suppress when stage's fallback resolves to the same text:

```go
// internal/recipe/yaml_emitter.go::writeRuntimeStage
func writeRuntimeStage(b *strings.Builder, plan *Plan, cb Codebase, tier Tier, comments map[string]string, devEmittedFallback string) {
    host := cb.Hostname + "stage"
    comment := comments[host]
    if comment == "" {
        comment = comments[cb.Hostname]
        // Suppress when dev slot already emitted the same fallback —
        // Y2 fallback duplicates would otherwise render the same prose
        // above both slots in dev-pair tiers. Run-13 §Y2D.
        if comment == devEmittedFallback {
            comment = ""
        }
    }
    writeComment(b, comment, "  ")
    ...
}
```

`writeDeliverableServices` tracks `devEmittedFallback` per codebase:

```go
func writeDeliverableServices(b *strings.Builder, plan *Plan, tier Tier) {
    b.WriteString("services:\n")
    comments := plan.EnvComments[envKey(tier)].Service
    for _, cb := range plan.Codebases {
        var devEmittedFallback string
        switch {
        case tier.RunsDevContainer && isRuntimeShared(cb, plan):
            writeRuntimeStage(b, plan, cb, tier, comments, "")
        case tier.RunsDevContainer:
            devEmittedFallback = ""
            if comments[cb.Hostname+"dev"] == "" && comments[cb.Hostname] != "" {
                devEmittedFallback = comments[cb.Hostname]
            }
            writeRuntimeDev(b, plan, cb, comments)
            writeRuntimeStage(b, plan, cb, tier, comments, devEmittedFallback)
        default:
            writeRuntimeSingle(b, plan, cb, tier, comments)
        }
    }
    ...
}
```

**Tests**:

```go
func TestWriteRuntimeStage_SuppressesFallbackDuplicate(t *testing.T) {
    plan := syntheticShowcasePlan()
    plan.EnvComments = map[string]EnvComments{
        "0": {Service: map[string]string{
            "api": "Two slots — apidev and apistage — share one source tree.",
        }},
    }
    got, _ := EmitImportYAML(plan, 0)
    // Comment renders ONCE (above apidev), not twice.
    occurrences := strings.Count(got, "share one source tree")
    if occurrences != 1 {
        t.Errorf("comment rendered %d times; expected 1 (Y2D dedup):\n%s", occurrences, got)
    }
}

func TestWriteRuntimeStage_DistinctSlotKeyComment_BothEmit(t *testing.T) {
    // When BOTH slot keys carry distinct comments, both render.
    plan := syntheticShowcasePlan()
    plan.EnvComments = map[string]EnvComments{
        "0": {Service: map[string]string{
            "apidev":   "Dev slot — hot iteration target.",
            "apistage": "Stage slot — stable demo target.",
        }},
    }
    got, _ := EmitImportYAML(plan, 0)
    mustContain(t, got, "Dev slot — hot iteration target")
    mustContain(t, got, "Stage slot — stable demo target")
}
```

**Acceptance**:
- Run-13 tier 0 + 1 yaml has each codebase's bare-name fallback
  comment rendered exactly once, above the dev slot. Stage slot
  rendered without comment.
- When agent records distinct slot-keyed comments (`apidev` /
  `apistage`), both render.

**Cost**: engine (~6 LoC + 2 tests). **Value**: low cosmetic.

### 2.B2 — Engine-composed dispatch prompt (stretch)

**What run 12 showed**: Run-12 §B landed engine-composed brief
envelopes (tier map / fragment list / hostname sets / anti-patterns
moved into BuildFinalizeBrief). But the DISPATCH envelope around the
brief is still 5-9 KB of hand-typed wrapper:

| Sub-agent | Brief size | Dispatch size | Wrapper share |
|---|---|---|---|
| scaffold-api | ~19.6 KB | 28,688 B | **~32%** |
| scaffold-app | ~19.6 KB | 27,407 B | **~28%** |
| scaffold-worker | ~18.0 KB | 27,196 B | **~34%** |
| feature | ~12.0 KB | 17,512 B | **~31%** |
| finalize | ~8.0 KB | 12,972 B | **~38%** |

Run-12-readiness acceptance criterion 15 said *"Finalize dispatch
prompt size ≈ engine brief size (within 10%)."* Actual: 152%. Wrapper
content (slug, framework, codebase identity, mount paths, fragment-id
list, close criteria) is ~70-95% Plan-derivable.

**Fix direction**:

Add a new action `build-subagent-prompt` that returns the FULL
dispatch prompt (engine-composed wrapper + brief body + close criteria)
given Plan + briefKind + codebase.

(a) `RecipeInput` already has fields for Plan / briefKind / codebase.
New action accepts the same shape:

```go
case "build-subagent-prompt":
    prompt, err := buildSubagentPrompt(sess, in)
    if err != nil {
        r.Error = err.Error()
        return r
    }
    r.Prompt, r.OK = prompt, true
```

(b) `buildSubagentPrompt(sess, in)` composes:

```
You are the {kind} sub-agent for the `{codebase}` codebase of the
{plan.slug} recipe. Read the engine brief below verbatim and follow
it; recipe-level context above and closing instructions below the
brief are wrapper notes from the engine.

## Recipe-level context

- Slug: `{plan.slug}`
- Framework family: {plan.framework}
- Tier: `{plan.tier}`
- Codebase shape: {plan.research.codebaseShape} ({list-codebase-hostnames})
- Project-level secret already set: `{plan.research.appSecretKey}`

### Sister codebases

{for each cb in plan.codebases except this one:}
- `{cb.hostname}` — role={cb.role}, runtime={cb.runtime}, worker={cb.isWorker}

### Managed services

{for each svc in plan.services:}
- `{svc.hostname}` ({svc.type}) — kind={svc.kind}

## Your codebase (`{codebase}`)

- `cb.Hostname`: `{codebase}`
- `cb.SourceRoot` / mount: `{cb.SourceRoot}`
- Dev slot: `{codebase}dev`
- Stage slot: `{codebase}stage`
- Runtime: `{cb.BaseRuntime}`
- Subdomain access: {cb.IsWorker ? "NO" : "yes"}

---

# Engine brief — {kind}

{brief.body}

---

## Closing notes from the engine

{kind-specific close criteria — scaffold's preship contract, feature's
deploy + browser-walk, finalize's stitch + complete-phase}

When you're ready to terminate: ensure all required fragments are
recorded, complete-phase passes (call
`complete-phase phase={kind} codebase={codebase}` to self-validate),
and any phase-specific commit shape is in place.
```

(c) Brief size impact: dispatch ≈ brief + ~1-2 KB engine wrapper (vs
today's 5-9 KB hand-typed). Wrapper share drops below 15% across all
sub-agent dispatches.

(d) Phase-entry teaching update: scaffold + feature phase-entry atoms
direct main to call `build-subagent-prompt` instead of `build-brief`,
then dispatch with `prompt=<response.prompt>` byte-identical.

**Tests**:

```go
func TestBuildSubagentPrompt_Scaffold_IncludesRecipeLevelContext(t *testing.T) {
    plan := syntheticShowcasePlan()
    sess := newTestSession(t, plan)
    prompt, err := buildSubagentPrompt(sess, RecipeInput{
        BriefKind: "scaffold",
        Codebase:  "api",
    })
    if err != nil { t.Fatal(err) }
    mustContain(t, prompt, "## Recipe-level context")
    mustContain(t, prompt, "Slug: `nestjs-showcase`")
    mustContain(t, prompt, "Sister codebases")
    // Engine brief body appears verbatim inside.
    brief, _ := BuildScaffoldBrief(plan, plan.Codebases[0], nil)
    mustContain(t, prompt, brief.Body)
}

func TestBuildSubagentPrompt_WrapperShareIsSmall(t *testing.T) {
    plan := syntheticShowcasePlan()
    sess := newTestSession(t, plan)
    prompt, _ := buildSubagentPrompt(sess, RecipeInput{
        BriefKind: "finalize",
    })
    brief, _ := BuildFinalizeBrief(plan)
    wrapperBytes := len(prompt) - len(brief.Body)
    if wrapperBytes > 2500 {
        t.Errorf("wrapper too large (%d bytes); criterion-15 target is < 2 KB", wrapperBytes)
    }
}
```

**Acceptance**:
- Run-13 main agent uses `build-subagent-prompt` for every dispatch.
- Wrapper share < 15% across all sub-agent dispatches.
- Run-12-readiness criterion 15 finally met (finalize wrapper < 10%).

**Cost**: engine (~80 LoC + 2 tests + phase-entry update). **Value**:
medium — long-term wrapper-drift elimination. Stretch — defer if
T1-T4 run hot.

---

## 3. Ordering + commits

### Tranche structure

**Tranche 1 — template fix** (Q):
- Smallest fix; eliminates the worst R-12-1 + R-12-2 regression.

**Tranche 2 — content factuality** (T, V, F, N, U, I-feature, W):
- Foundation for run-13 content quality lift.
- T (tier capability matrix) is the load-bearing engine work in
  this tranche.
- V (validator) backstops T.
- F (showcase scenario) closes user-flagged gap.
- N / U / I-feature / W are smaller content fixes.

**Tranche 3 — engine-flow** (G2):
- Closes the §G actor mismatch persisting from run 12.
- Cost ~30 LoC engine + brief.

**Tranche 4 — engine cosmetic** (Y2D):
- Smallest engine fix; cosmetic.

**Tranche 5 — wrapper-elimination** (B2):
- Run-11 R-18 + run-12 acceptance-criterion-15 carry-forward.
- Strongly recommended but not structurally blocking.

### Commit order

**Tranche 1**:

1. **commit 1** — Q template fix
   - Edit `internal/recipe/content/templates/codebase_claude.md.tmpl`
     (delete `## Zerops dev loop` block, lines 11-15).
   - Add `TestAssembleCodebaseClaudeMD_NoTemplateInjectedDevLoop` to
     `assemble_test.go`.

**Tranche 2**:

2. **commit 2** — T tier-fact table (engine)
   - New helpers in `briefs.go`: `BuildTierFactTable`,
     `tierCapabilityRow`, `serviceCapabilityFamilyRow`.
   - Wire into `BuildFinalizeBrief` (after tier map).
   - Wire into `BuildScaffoldBrief` (after platform_principles atom,
     conditional on `cb.Role == RoleFrontend`).
   - Raise ScaffoldBriefCap 22→24 KB; FinalizeBriefCap 12→14 KB.
   - Add 4 tests.

3. **commit 3** — V validator
   - New `validateTierProseVsEmit` in `validators_root_env.go` (extends
     env-import-comments + env-readme registration).
   - New helper `parseYAMLServiceBlocks` (or extend existing yaml
     comment-block parser).
   - Add 5 tests covering replica-count / HA-claim / storage-size /
     runtime-type / cpu-mode mismatches.
   - Wire as Notice severity (DISCOVER side).

4. **commit 4** — F showcase scenario spec
   - New atom `briefs/feature/showcase_scenario.md`.
   - Conditional load in `BuildFeatureBrief` when `plan.Tier == "showcase"`.
   - Add 2 tests (showcase tier includes; minimal tier omits).

5. **commit 5** — N init-commands decomposed-step extension
   - Edit `principles/init-commands-model.md` (append to `## Decomposition`
     section).
   - Add 1 test.

6. **commit 6** — U alias-type resolution timing footnote
   - Edit `briefs/scaffold/platform_principles.md` (append to
     `## Alias-type contracts` section).
   - Add 1 test.

7. **commit 7** — I-feature IG-scope rule
   - Edit `briefs/feature/content_extension.md` (new
     `## IG scope (extending scaffold's items)` subsection).
   - Add 1 test.

8. **commit 8** — W finalize anti_patterns rewrite
   - Edit `briefs/finalize/anti_patterns.md` (rewrite "do not touch"
     bullet to allow `mode=replace`).
   - Add 1 test.

**Tranche 3**:

9. **commit 9** — G2 per-codebase complete-phase scoping
   - Edit `internal/recipe/handlers.go::RecipeInput` (clarify
     `Codebase` field jsonschema for complete-phase usage).
   - Edit `handlers.go::completePhase` to dispatch on `in.Codebase`.
   - New `Session.CompletePhaseScoped` in `workflow.go`.
   - Edit `phase_entry/scaffold.md` (add `## Self-validate before
     terminating` section).
   - Edit `briefs/scaffold/content_authoring.md` (replace the existing
     "Correcting a fragment you authored" section with the broader
     self-validate teaching).
   - Edit `briefs/feature/content_extension.md` (add same self-validate
     teaching scoped to feature phase).
   - Add 4 tests.

**Tranche 4**:

10. **commit 10** — Y2D dedup
    - Edit `yaml_emitter.go::writeRuntimeStage` (accept
      `devEmittedFallback` parameter; suppress when matching).
    - Edit `writeDeliverableServices` (track + pass).
    - Add 2 tests.

**Tranche 5 (stretch)**:

11. **commit 11** — B2 build-subagent-prompt
    - Edit `handlers.go::dispatch` (new `case "build-subagent-prompt"`).
    - New `RecipeResult.Prompt` field.
    - New `buildSubagentPrompt(sess, in)` helper.
    - Edit `phase_entry/scaffold.md` + `feature.md` + `finalize.md` to
      direct main to use the new action.
    - Add 2 tests.

**Tranche 6 — CHANGELOG + system.md verdict table**:

12. **commit 12** — CHANGELOG + verdict-table sign-off
    - New CHANGELOG entry summarizing all 11 fixes.
    - Update system.md §4 verdict table:
      - `tier-prose-*-mismatch` validators (TEACH defensible — structural
        relation between yaml elements, not a phrase ban)
      - showcase scenario spec atom (TEACH — engine emits per-tier mandate)
      - codebase_claude.md.tmpl template strip (positive shape, no
        validator catalog needed)

### Fast-path

If time pressure forces a partial run-13 dogfood: **Tranche 1 + 2 alone**
is viable. The template fix + tier-fact table cascade through every
recipe immediately; a dogfood run against a Tranche-1+2 engine produces
a recipe that doesn't ship `zcli push` template leaks AND ships factually
correct tier prose. The §G actor mismatch persists (main hand-edits
during scaffold-close), but the recipe content is correct.

**Tranche 1 + 2 + 3** is the recommended must-ship.

**Tranche 4 + 5** are strongly recommended polish.

---

## 4. Acceptance criteria for run 13 green

### Inherited from run 12 (continue to hold)

1. All five phases close `ok:true`.
2. Three sub-agents in parallel for scaffold (single-message Agent
   dispatch).
3. Per-codebase apps-repo content lands at `<cb.SourceRoot>/`.
4. Per-codebase `.git/` initialized with at least one scaffold commit.
5. Recipe-root README templated; per-tier READMEs ≥ 40 lines.
6. Workspace yaml inline-imported; deliverable yamls written at
   `<outputRoot>/`.
7. V-5 abstract litmus holds (no run-10 anti-patterns reintroduced).
8. Run-11 + run-12 cleanup demotions hold (no validator regressed to
   blocking).
9. Apps-repo `zerops.yaml run.envVariables` declares own-key aliases;
   code reads `process.env.<OWN_KEY>` (run-12 §E held).
10. Zero `https://${<host>_zeropsSubdomain}` source occurrences (run-12
    §A held).
11. Apps-repo CLAUDE.md AGENT-AUTHORED content has zero `zerops_*` /
    `zcli` / `zcp` invocations (run-12 §C held).
12. Apps-repo IG focuses on platform mechanics; numbered items 4-7 per
    codebase (run-12 §I held — at the scaffold layer).
13. Finalize dispatch prompt is engine-composed (run-12 §B held);
    every dispatch verified pre-call (run-12 §D held).
14. Zero `# # ` doubled-prefix lines in tier yamls (run-12 §Y1 held).
15. Tier 5 yaml mode field matches platform capability (run-12 §Y3
    held — meilisearch NON_HA, postgresql/valkey/nats HA).

### New for run 13

16. **Apps-repo CLAUDE.md PUBLISHED content has zero `zcli push` /
    `zcli vpn` occurrences.** Includes engine-stitched template
    content. The `## Zerops dev loop` block is gone from the template;
    the `## Notes` section carries the dev-loop bullet as the single
    authoritative location.

17. **Tier 5 README claims minContainers=2** (NOT 3); does NOT claim
    "Meilisearch keeps a backup"; does NOT claim "object-storage
    replicated" (no such field).

18. **Tier yaml comments factually match emit fields.** Zero
    `tier-prose-*-mismatch` blocking violations; `tier-prose-*-mismatch`
    notice count < 5 (some tolerance for §V being a backstop).

19. **Showcase SPA carries one demonstration panel per managed-service
    category** — items, cache, queue, storage, search. Each panel has
    a concrete observable signal (X-Cache header, queue worker
    processing log, signed-URL retrieve, search result ranking).

20. **Each demonstration panel records a browser-verification fact.**
    `facts.jsonl` contains entries with `topic` matching
    `<frontend-host>-{items,cache,queue,storage,search}-browser`. The
    queue-browser fact's symptom field describes a concrete observable
    (e.g. "publish trigger fires; worker log shows item processed
    within 2s; search panel returns the new document").

21. **Scaffold sub-agents call `complete-phase phase=scaffold
    codebase=<host>` before terminating.** If violations fire, sub-
    agents fix in-session via `record-fragment mode=replace` (for
    fragment ids) or ssh-edit (for `<SourceRoot>/zerops.yaml`).

22. **Zero main-agent `Edit` calls during scaffold-close.** Main's
    `complete-phase scaffold` (no codebase) returns 0 blocking
    violations on first call.

23. **Init-commands decomposed-step trap NOT rediscovered.** No
    `execOnce-key-collision-across-decomposed-steps` fact in run-13
    facts.jsonl.

24. **Cross-service alias resolution timing trap NOT rediscovered.**
    No `cross-service-alias-resolution-timing` fact.

25. **Feature appends to IG scoped narrowly.** apidev IG has 0-1
    feature-appended IG items; no unnumbered prose subsections inside
    the IG extract markers.

26. **Tier 0 + 1 dev-pair runtime services have comments rendered
    once per codebase, not duplicated** above dev/stage slots.

### Stretch criteria

27. **Engine-composed dispatch prompts < 15% wrapper.** Main agent
    uses `build-subagent-prompt` for all 5 dispatches; wrapper share
    drops below 15% across all sub-agent kinds.

28. **Acceptance criterion 15 from run-12-readiness met.** Finalize
    dispatch prompt size ≈ engine brief size within 10%.

---

## 5. Non-goals for run 13

- **No re-design of the surface taxonomy.** spec-content-surfaces.md is
  authoritative; run-13 enforces it more thoroughly, doesn't modify it.
- **No new fragment ids.** zerops.yaml stays a file (not a fragment per
  Q2 option b). Sub-agents ssh-edit for yaml-comment fixes.
- **No re-promotion of run-11/12-cleanup-demoted validators.** They
  stay as notices.
- **No new spec PRs.** spec-content-surfaces.md and system.md are
  authoritative; run-13 implements against them.
- **No catalog-of-frameworks atom additions.** §T tier capability
  matrix is platform-invariant. §F showcase scenario is platform-
  invariant. §V validator is structural-relation, not phrase-ban.
- **No publish-path work.** `zcp sync recipe publish` stays out of
  scope.
- **No multi-codebase recipe-shape changes.** The 3-codebase showcase
  shape stays.
- **No re-architecture of complete-phase semantics beyond §G2 scoping.**
  Phase advance is still triggered by the no-codebase form; per-
  codebase form is self-validate only.

---

## 6. Risks + watches

### Risk: §T tier-fact table makes scaffold brief too large for non-frontend codebases

ScaffoldBriefCap raises 22 → 24 KB for the api/worker codebases.
Without §T, those agents don't need the table — they don't author
tier-aware prose. Adding ~1.5 KB unconditionally is wasted dispatch
size.

**Mitigation**: §T's brief composer wires the table conditionally —
loaded into BuildScaffoldBrief only when `cb.Role == RoleFrontend`.
api/worker scaffolds get the brief without the table; cap stays at
22 KB for them.

**Watch**: monitor scaffold dispatch sizes in run-13. If frontend
brief exceeds 24 KB, raise cap further or trim the table.

### Risk: §V validator fires high-noise notices on legitimate prose

Tier 4 prose says "two replicas" matches `minContainers: 2` — fine.
Tier 5 prose says "production replicas" with no number — should NOT
fire (no number to mismatch). Edge cases like "scales horizontally"
or "multi-replica" don't carry numbers and shouldn't trip the
detector.

**Mitigation**: §V regex requires explicit numeric or categorical
claim adjacent to a service block. Fuzzy claims ("production-grade",
"multi-replica") pass. Tests cover the boundary cases.

**Watch**: monitor §V notice count in run-13. If > 5 false-positives,
tighten regex; if 0 firings AND prose still divergent, broaden.

### Risk: §F showcase scenario forces SPA work that doesn't fit small-tier recipes

Hello-world / minimal-tier recipes might not have all five managed-
service categories. Mandating 5 panels for a 2-managed-service recipe
is wrong.

**Mitigation**: §F's atom is loaded conditionally on `plan.Tier ==
"showcase"`. The mandate scopes to the categories the recipe
provisions — a recipe without a queue/broker doesn't need a queue
panel. The atom's table says "the frontend codebase MUST render these
panels" with the implicit understanding that "these" = "the categories
the recipe ships."

**Watch**: monitor whether feature subagent in run-13 questions or
mis-applies the spec. If panel mandate is ambiguous, tighten the
atom's "applies when X is in Plan.Services" clause.

### Risk: §G2 self-validate forces multiple complete-phase calls per scaffold sub-agent

Each scaffold sub-agent calls `complete-phase codebase=<h>` at least
once. If violations fire, the sub-agent re-calls after each fix. With
3 sub-agents × ~1-2 iterations, that's 3-6 extra complete-phase calls
per recipe. Each is ~5-15s of platform interaction.

**Mitigation**: complete-phase scoped to a single codebase is faster
than the full phase gates (only codebase gates run, not env or default
gates). Total cost ~30s per recipe — well below the run-12 hand-edit
6-minute pattern §G2 replaces.

**Watch**: monitor sub-agent total wall time in run-13 vs run-12.
Expect modest increase (~1-3 min) from self-validate calls; net
savings from main not hand-editing.

### Risk: §B2 dispatch prompt eliminates the wrapper, but loses recipe-specific authoring decisions

Today's wrapper carries decisions like *"the SPA is **Svelte + Vite**
compiled to a static bundle. Don't reach for SvelteKit/Next/Nuxt"* —
this is a research-phase decision, not Plan-derivable. If §B2
mechanically composes the wrapper from Plan only, these decisions
get dropped.

**Mitigation**: §B2's wrapper composer reads
`plan.Research.Description` and any framework-specific notes captured
during research-phase. Research atom extension: research-phase agent
records framework-canonical pins (`Svelte+Vite static`,
`NestJS+TypeORM`, etc.) into a typed Plan field; §B2 composer reads
that field. Stretches the workstream slightly; deferrable if too big.

**Watch**: if run-13 §B2 lands, verify research-phase decisions reach
the dispatch prompt. If gaps, escalate research-phase atom changes.

### Risk: Tier-fact table contradicts existing Plan.Service overrides

The `Service` struct allows `ExtraFields` for per-service yaml
field overrides. If a recipe declares `Service{Type: "meilisearch",
SupportsHA: true}` (force-override), §T's table claims "meilisearch
NON_HA at tier 5" but emit will be HA — same divergence in reverse.

**Mitigation**: §T's table reads the actual Plan, not the
`managedServiceSupportsHA` family fallback. If a service has explicit
`SupportsHA: true`, the table reflects that. The table's source of
truth is `plan.Services[i].SupportsHA`; conservative default is the
family table's verdict.

**Watch**: verify run-13's table against tier-5 service modes. If a
recipe explicitly sets HA for a normally-non-HA family, the table's
output should match.

---

## 7. Open questions

1. **Should §V validator fire as Notice or Blocking?** Per system.md
   §4, structural-relation validators are TEACH-defensible and could
   block. But §T (the brief teaching) is the load-bearing fix; §V is
   backstop. Run-13 wires §V as Notice; if run-13 still produces
   prose-vs-emit divergence at scale, promote to Blocking in run-14.

2. **Should the showcase scenario panels be a typed Plan field?**
   Today's design loads `showcase_scenario.md` atom conditionally on
   `plan.Tier == "showcase"`. A more precise approach would type
   `Plan.ShowcasePanels []string` populated at research-phase
   ("items", "cache", "queue", "storage", "search"). Run-13 picks the
   atom-conditional approach for simplicity; if research-phase
   captures different scenario subsets per recipe, escalate to typed
   field in run-14.

3. **Should `complete-phase phase=feature codebase=<host>` be
   supported?** §G2's primary target is scaffold (where the actor
   mismatch bit hardest in run 12). Feature is a single sub-agent
   that touches multiple codebases; per-codebase scoping might not
   match how feature works. Run-13 implements both for symmetry but
   the feature sub-agent is encouraged to call without codebase scope
   when terminating (validates all touched codebases at once).

4. **Should §Q delete the template's `## Zerops dev loop` block (Option
   A) or replace with a porter-canonical placeholder (Option B)?**
   Run-13 picks Option A (delete entirely; `## Notes` carries
   dev-loop). Option B would keep section structure but defer to
   Notes. If run-13 dogfood shows the agent forgetting to include
   dev-loop in Notes, escalate to Option B.

5. **Should §B2 stretch ship in run-13 or run-14?** Run-12 acceptance
   criterion 15 missed; B2 is the carry-forward fix. Cost ~80 LoC
   engine. Run-13 includes it as Tranche 5 stretch — recommended but
   skippable. If T1-T4 run hot, defer to run-14.

---

## 8. After run 13 — what's next

If run 13 closes green on criteria 16-26:

- Run-13's content quality should be **8.5 / 10 vs reference** (run 12
  today: 7/10, run 11: 6/10). The template fix (§Q) recovers C-→B+ on
  the CLAUDE.md axis. The tier-fact table (§T) recovers D→A on the
  yaml-comment axis. The §F showcase scenario adds a missing surface
  (queue panel + per-panel browser facts) that run 12 didn't have.
  The §G2 actor scoping closes the hand-edit pattern.

- If criterion 27-28 (B2 stretch) ALSO holds: 9/10. The wrapper-share
  elimination is the last major engine-side carry-forward.

- Run-14 readiness focuses on remaining smell items the run-12 dogfood
  surfaced but run-13 deferred:
  - Recipe-root README cross-codebase runbook content (open question
    from run 11; nothing currently lands in recipe-root README beyond
    the engine-templated intro + tier links).
  - `Plan.ShowcasePanels` typed field for richer scenario shape.
  - V validator promotion to Blocking if dogfood still ships
    prose-vs-emit divergence.
  - Catalog-shaped validator residue from earlier runs (V-3, V-4, O-2,
    P-3, claudeMDForbiddenSubsections, kbTripleFormatRE, etc.) —
    audit for whether the underlying lessons still bite or whether
    brief teaching has fully replaced them; delete or re-promote
    accordingly.

If run 13 closes RED on any of 16-26:

- ANALYSIS will name the structural cause. Most likely places it goes
  wrong:
  - **§T partial** — agent's prose still diverges because the table
    is loaded but agent doesn't reference it. Brief teaching needs an
    explicit "MUST cross-check claims against the table" instruction.
  - **§F partial** — feature subagent designs panels but skips one
    category (most likely queue, given run-12 history). Atom mandate
    needs to be more explicit per category.
  - **§G2 partial** — sub-agent calls self-validate but doesn't fix
    flagged violations (treats Notice as ignorable). Brief teaching
    needs to clarify Blocking vs Notice severity.

The whole-engine path forward stays:
- Tighter audience boundary (porter vs authoring agent) per system.md §1.
- TEACH-side positive shapes per system.md §4.
- Engine pushes resolved truth into briefs; agent authors against
  truth, not mental models.
- Sub-agent self-validate before terminating; main only handles
  phase-state transitions.

---

## 9. Pre-flight verification checklist

Before run-13 dogfood:

- [ ] All 11 commits land cleanly (Tranche 1-5 + CHANGELOG sign-off).
- [ ] `make lint-local` passes (full lint, not lint-fast).
- [ ] `go test ./internal/recipe/... -count=1` passes.
- [ ] `go test ./internal/recipe/... -count=1 -race` passes.
- [ ] No `replace` directives in `go.mod`.
- [ ] CHANGELOG entry summarizes all workstreams with file:line for
      key changes.
- [ ] system.md §4 verdict table updated to reflect run-13 additions.
- [ ] Manual sanity check: `internal/recipe/content/templates/codebase_claude.md.tmpl`
      no longer contains `## Zerops dev loop` block.
- [ ] Manual sanity check: `BuildFinalizeBrief` and `BuildScaffoldBrief`
      include tier-fact table (run a synthetic plan, eyeball the brief).
- [ ] Manual sanity check: `briefs/feature/showcase_scenario.md` exists
      and is wired into `BuildFeatureBrief` for `tier=showcase`.
- [ ] Manual sanity check: `complete-phase phase=scaffold codebase=api`
      against a synthetic session fires codebase gates only.

When all green: dogfood `nestjs-showcase` (replay) — replay isolates
engine changes from research-phase variability.
