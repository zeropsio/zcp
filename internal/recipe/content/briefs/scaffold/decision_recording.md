# Decision recording — record `porter_change` + `field_rationale` facts

You write source code + zerops.yaml at scaffold; you do NOT author IG /
KB / yaml comments yet (a sibling content phase reads your facts +
on-disk artifacts later and synthesizes those surfaces).

For every non-obvious decision, record a structured fact at densest
context — the moment you make the change. Two subtypes cover the
codebase scope:

## `porter_change` — code or library decisions a porter would have to make

Record whenever you write code that's NOT framework-defaults: bind to
0.0.0.0, install a specific library, configure CORS exposed-headers,
mount a same-origin proxy, etc. Engine pre-emits Class B universal-
for-role facts (bind-and-trust-proxy, sigterm-drain, worker-no-http);
you fill agent-specific slots via `fill-fact-slot` after consulting
`zerops_knowledge runtime=<svc-type>`.

```
zerops_recipe action=record-fact slug=<slug>
  fact={
    topic: "<host>-<short-id>",
    kind: "porter_change",
    scope: "<host>/code/<file>",
    phase: "scaffold",
    changeKind: "code-addition",
    library: "<lib-name>",
    diff: "<the-actual-line-or-block>",
    why: "<symptom + mechanism + fix at the platform level>",
    candidateClass: "platform-invariant" | "intersection",
    candidateHeading: "<surface-shaped heading>",
    candidateSurface: "CODEBASE_IG" | "CODEBASE_KB",
    citationGuide: "<topic-id-from-citation-map>"
  }
```

## `field_rationale` — non-obvious zerops.yaml field decisions

Record whenever a yaml field carries reasoning that's not self-evident
from the value (e.g. S3_REGION=us-east-1 is the only region MinIO
accepts; two separate execOnce keys so a seed failure doesn't roll back
the schema migration).

```
zerops_recipe action=record-fact slug=<slug>
  fact={
    topic: "<host>-<short-id>",
    kind: "field_rationale",
    scope: "<host>/zerops.yaml/<field-path>",
    phase: "scaffold",
    fieldPath: "run.envVariables.S3_REGION",
    fieldValue: "us-east-1",
    why: "<reason>",
    alternatives: "<what-fails-if-changed>",
    compoundReasoning: "<optional, when reasoning spans multiple fields>"
  }
```

For compound decisions (e.g. two `initCommands` entries with paired
reasoning), record one `field_rationale` per field with a shared
`compoundReasoning` slot. The content sub-agent merges them into one
yaml comment block.

## Filter rule — when NOT to record

Skip if classification ∈ {`framework-quirk`, `library-metadata`,
`self-inflicted`} — those have no compatible surface. Record only when
classification ∈ {`platform-invariant`, `intersection`,
`scaffold-decision (config|code)`}.

## Examples (run-15 grounded)

- `S3_REGION=us-east-1` because MinIO requires it → `field_rationale`,
  `scope: apidev/zerops.yaml/run.envVariables.S3_REGION`.
- `app.enableCors({ exposedHeaders: ['X-Cache'] })` because cross-origin
  fetch strips the header → `porter_change`,
  `candidateClass: intersection`, `candidateSurface: CODEBASE_KB`.
- `$middleware->trustProxies(at: '*')` because L7 forwards X-Forwarded-*
  → `porter_change`, `candidateClass: platform-invariant`,
  `candidateSurface: CODEBASE_IG`.

If a porter would ask "why?", record it.

## Git hygiene (carried forward from pre-run-16)

Before the first deploy in any codebase, ensure git identity is set on
the dev container:

```
ssh <hostname>dev "git config --global user.name 'zerops-recipe-agent' \
  && git config --global user.email 'recipe-agent@zerops.io'"
```

Then for the scaffold commit:

```
git init
git add -A
git commit -m 'scaffold: initial structure + zerops.yaml'
```

The scaffold sub-agent records git ops in commits, not in fragments —
the apps-repo publish path needs a clean history precondition. (The
phase_entry atom names the recovery path when a deploy commit already
exists from prior runs.)

