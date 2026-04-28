# Codebase-content synthesis workflow

You author per-codebase docs (intro + slotted IG + KB + zerops.yaml
comments) by reading three sources:

1. The recorded fact stream (porter_change + field_rationale,
   filtered to your codebase scope)
2. On-disk artifacts (zerops.yaml, src/, parent recipe surfaces)
3. The spec (`docs/spec-content-surfaces.md`) for surface contracts

Use facts as the bridge — the deploy-phase agents recorded WHY they
made each non-obvious change at densest context. Your job is to group
them into surface-shaped output.

## Step 1 — Read facts + on-disk content

Walk the brief's fact list. For each `porter_change` fact, read its
`scope` field (e.g. `apidev/code/src/main.ts`) and `Read` that file
to ground the diff in actual code. For each `field_rationale` fact,
read the corresponding `<SourceRoot>/zerops.yaml` block.

## Step 2 — Fill engine-emitted shells

For every shell with empty Why (per-managed-service shells, worker
no-HTTP heading), call:

```
zerops_knowledge runtime=<svc-type>
```

then merge into the shell:

```
zerops_recipe action=fill-fact-slot factTopic=<topic>
  fact={ topic: "<topic>", why: "<grounded prose>",
         candidateHeading: "<framework-specific name>",
         library: "<chosen library>" }
```

Do NOT paraphrase from memory — the per-service knowledge atom IS the
single source of truth.

## Step 3 — Author IG slots (Surface 4)

For each `CandidateSurface=CODEBASE_IG` fact (filled or pre-filled),
emit one `codebase/<h>/integration-guide/<n>` fragment per item.
Numbering starts at 2 (engine emits IG #1 = "Adding zerops.yaml" at
stitch). Spec cap is 5 IG items per codebase.

Bundled-class caveat: prefer pure-class headings when content density
supports it; bundling Class B teaching inside a Class C heading is
valid synthesis (jetstream IG #3 "Utilize Environment Variables"
absorbs TRUSTED_PROXIES alongside ${db_hostname} cross-service refs).

## Step 4 — Author KB (Surface 5)

For each `CandidateSurface=CODEBASE_KB` fact, emit one bullet in the
single `codebase/<h>/knowledge-base` fragment. Format: `- **Topic** — 2-4 sentences explaining symptom + mechanism + fix at the platform level`.
Cap 8 bullets.

Cross-surface dedup: if a topic is taught in IG (with code/diff), do
NOT duplicate in KB. KB is for topics that DON'T have a codebase-side
landing point (R-15-6 closure).

## Step 5 — Author zerops.yaml comments (Surface 7)

For each `field_rationale` fact, emit one
`codebase/<h>/zerops-yaml-comments/<block-name>` fragment per yaml
block (e.g. `run.envVariables`, `run.initCommands`, `build`,
`readinessCheck`). 6-line cap per block. Compound-decision facts
sharing `compoundReasoning` merge into one block.

## Step 6 — Author intro (Surface 4 head)

`codebase/<h>/intro` — 1-2 sentence framing. ≤ 500 chars, no `## `
headings. Says what the codebase IS, not what Zerops does with it.

## Self-validate

Call `zerops_recipe action=complete-phase phase=codebase-content
codebase=<host>` to run codebase-scoped validators against your
codebase only. Fix violations via `record-fragment mode=replace`
until the gate passes, then terminate.
