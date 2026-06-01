# Codebase-content phase — parallel sub-agent dispatch per codebase

**Next call:** `zerops_recipe action=enter-phase slug=<slug> phase=codebase-content` (advances the pointer + mints fact shells + retires the scaffold gate — REQUIRED before any dispatch). THEN for each codebase, `zerops_recipe action=build-subagent-prompt slug=<slug> briefKind=codebase-content codebase=<hostname>` AND again `briefKind=claudemd-author codebase=<hostname>`; dispatch all briefs in parallel via `Agent`, then `zerops_recipe action=complete-phase slug=<slug> phase=codebase-content` → `action=enter-phase slug=<slug> phase=env-content`.

After scaffold + feature complete, every codebase gets two sub-agents
dispatched in parallel:

1. **`codebase-content`** — Zerops-aware. Authors `codebase/<h>/intro`,
   `codebase/<h>/integration-guide/<n>` (slotted; engine pre-stamps
   n=1, agent authors n=2 through 5),
   `codebase/<h>/knowledge-base`, and the whole commented zerops.yaml
   as one fragment `codebase/<h>/zerops-yaml`. Reads the recorded fact
   stream (porter_change + field_rationale + tier_decision) plus on-
   disk source / zerops.yaml / spec.

2. **`claudemd-author`** — Zerops-free. Authors only
   `codebase/<h>/claude-md` (single slot). Brief is strictly platform-
   free; agent reads package.json / src/* directly and produces
   `/init`-style output. Does NOT read facts; does NOT see Zerops
   integration content; sibling sub-agent owns IG/KB/yaml comments.

## Dispatch shape — main agent's responsibility

For each codebase, the main agent calls `build-subagent-prompt` TWICE
(once for `briefKind=codebase-content`, once for
`briefKind=claudemd-author`), then issues all 2N briefs in a single
message with parallel `Agent` tool calls. For each response, branch on
the inline-or-pointer contract (next section): pass `response.prompt`
byte-identical when set; otherwise wrap `response.briefPath` in a thin
"Read this file first" dispatch.

```
[message]
  Agent(description: "codebase-content-api", prompt: <inline body or "Read <briefPath>" wrapper>)
  Agent(description: "claudemd-author-api",  prompt: <inline body or "Read <briefPath>" wrapper>)
  Agent(description: "codebase-content-app", prompt: <inline body or "Read <briefPath>" wrapper>)
  Agent(description: "claudemd-author-app",  prompt: <inline body or "Read <briefPath>" wrapper>)
  ...
```

Net savings vs serial: 5-15 minutes for 3-codebase dispatches.

## Dispatch contract — pass response.prompt verbatim

Two correct dispatch shapes. Pick by main-agent context budget:

- **Inline**: pass `response.prompt` from `build-subagent-prompt`
  byte-identically as the `Agent` prompt parameter (only when
  `response.prompt` is non-empty — see inline-or-pointer rule below).
- **Self-fetch wrapper**: when context is tight, send the sub-agent a
  one-sentence context cue plus the
  `zerops_recipe action=build-subagent-prompt slug=<slug>
  briefKind=codebase-content codebase=<host>` invocation so it fetches
  the prompt itself.

## Dispatch — multi-file pointer

`build-subagent-prompt` for `briefKind=codebase-content` ALWAYS returns
a multi-file pointer:

- `response.prompt` is empty.
- `response.briefPath` is the absolute path to `index.md` under
  `<outputRoot>/.briefs/codebase-content-<host>-<unixnano>/`.
- `response.briefSize` carries the index file's byte count for
  sanity-check.

The index lists N part files (`part-1-<slug>.md`, `part-2-<slug>.md`,
...) in a "Read order" section. The sub-agent dispatch wrapper MUST
instruct: "Read `<briefPath>` first; then Read each part file listed
in its 'Read order' section in the order shown before authoring any
fragment."

Dispatch with `subagent_type="general-purpose"` — do NOT use
`subagent_type="claude"` (FleetView's default when unspecified).
`claude` triggers worktree isolation on dispatch, which fails on the
non-git recipe-authoring outputRoot and breaks the shared
`zerops_recipe` MCP state every recipe sub-agent depends on.

Why multi-file? The composed brief carries phase-entry, synthesis
workflow, citation guides, platform principles, cross-service
teaching, yaml-comment style rules, codebase metadata, recorded
facts, parent-recipe baseline, and sibling sub-agent notes — at run-31
production load shape this totals 78-94 KB / 36-39K real tokens, well
over the Read-tool 25K-token single-shot cap. Splitting at semantic
boundaries lets each part fit under the cap without dropping any
teaching. Run-31 Fix #1 closure.

Hand-typed paraphrase wrappers — out. Re-stating the brief in your
own words compounds math errors and path drift (run-13 §B2) and at
codebase-content phase historically dropped run-specific findings
(run-26 F-31). The brief carries cross-codebase managed-service
facts so connection-shape decisions stay consistent across codebases —
the engine surfaces a sister codebase's finding (e.g. worker scaffold
recording a NATS auth-shape crash) into this codebase's brief when
both consume the same managed service.

## Why two sub-agents

Mixing CLAUDE.md authoring into the codebase-content brief leaks Zerops
context into CLAUDE.md (run-15 R-15-4: `## Zerops service facts` /
`## Zerops dev (hybrid)` headings appeared because the brief was
Zerops-aware). The sibling Zerops-free brief makes bleed-through
structurally impossible — there is no platform principles atom, no
`zerops.yaml` pointer, no managed-service hints in the
`claudemd-author` brief.

## Engine-emitted facts the codebase-content sub-agent fills

The brief includes engine-emitted shells (§7.1-§7.2):
- Class B universal-for-role: `<host>-bind-and-trust-proxy`,
  `<host>-sigterm-drain`, `<host>-no-http-surface` (worker)
- Class C umbrella: `<host>-own-key-aliases`
- Per-managed-service shells: `<host>-connect-<svc>`

For every shell with empty Why (per-managed-service shells, worker no-
HTTP heading), the agent calls `zerops_knowledge runtime=<svc-type>`
and fills via `fill-fact-slot factTopic=<topic> why=... heading=...`.

## Common record-fragment rejections — pre-empt these

The validator catches many drift classes; these three are the
**most-frequent** rejection patterns observed across recent runs and
account for the bulk of record-fragment iteration. Author with these
in mind from the start. This is NOT an exhaustive list — `docs/spec-
content-surfaces.md` is the surface contract and lists the full
validator set; treat the three below as a head-start, not a
sufficient checklist.

1. **KB stem must be symptom-first or directive-tightly-mapped-to-
   observable.** WRONG: `Re-fire seeds without re-running migrations`
   (author-claim). RIGHT: `Seed silently skipped after a partial-
   failure redeploy` (symptom-first — names what the porter actually
   sees).
2. **Slug citations use descriptive link text OR in-body completion;
   internal corpus slug IDs never appear in porter content.**
   `env-var-model`, `init-commands`, `managed-services-nats`,
   `rolling-deploys`, `cross-service-refs`, `object-storage` are
   internal `zerops_knowledge` corpus slugs the recipe-authoring
   engine uses to look up platform facts; porters never interact
   with that corpus and the slug IDs aren't porter-recognizable.
   WRONG (backticked slug as noun): ``the `env-var-model` guide
   covers ...``. WRONG (topic-name handwave with no URL): `the
   env-var-model guide on Zerops docs covers ...`. WRONG (slug
   name as link text — same leakage with a URL bolted on):
   `[env-var-model](https://docs.zerops.io/zerops-yaml/specification#envvariables-)`.
   RIGHT (in-body completion — preferred for Zerops mechanics):
   finish teaching the platform mechanism directly; no external
   pointer needed. RIGHT (descriptive label as link text): `[per-key
   env shape and cross-service aliases](https://docs.zerops.io/zerops-yaml/specification#envvariables-)`,
   `[Laravel documentation](https://laravel.com/docs/12.x/queues)`,
   `[step-by-step NestJS tutorial](https://docs.zerops.io/frameworks/nestjs/introduction)`
   — the link text reads as porter prose, not as a corpus key. The
   jetstream golden's links (`[step-by-step tutorial]`, `[zsc
   health-check]`, `[Laravel documentation]`, `[multi-container
   setups]`) are the calibration. **Slug-stem test**: if the link text
   contains a corpus slug stem (`rolling-deploys`, `init-commands`,
   `managed-services-*`, `env-var-model`, `env-var-references`,
   `env-var-secrets`, `subdomain-access`, `object-storage`,
   `build-from-git`, etc.) — even when dressed as English with a
   Zerops/managed prefix — it FAILS. Forbidden shapes include
   `[managed NATS service]`, `[Zerops object-storage service]`,
   `[Zerops env-var model]`, `[Zerops rolling-deploys reference]`,
   `[per-deploy init-commands reference]`. Replace with a porter-
   recognized concept or in-body completion.
3. **Classification × surface refusal**: `intersection` is KB-only,
   never IG. If a fact records `candidateClass=intersection`, route
   the body to KB; for IG, restate the principle without the
   intersection class.

## Complete-phase gate

Every codebase declared in `plan.codebases` must have all five
fragment ids recorded (intro + ≥1 integration-guide slot + knowledge-
base + zerops-yaml whole-yaml + claude-md). Codebase-scoped
validators run.
