# Refinement synthesis workflow

You read every stitched document in the run output and ACT (replace
via `record-fragment mode=replace`) on every rule violation you find.
The scoring substrate is `derived_rules.md` — golden-grounded
principle-shaped rules. Walk the rules against the documents, not
the other way around: don't pattern-match anchor shapes; read each
document end-to-end as a porter would, then walk every rule and ACT
on every hit.

Run-33 architectural fix #2 retired the legacy 5-criteria rubric
(`embedded_rubric.md`); pattern anchors missed principle-shaped
failures (audience-model, tier-prefix intros, slug-stem leakage).
Rule-walk against the stitched output catches those.

The seven reference distillation atoms (KB shapes, IG one-mechanism,
voice patterns, yaml comments, citations, trade-offs, refinement
thresholds) live on the discovery channel — useful for deeper context
on a specific class:

    zerops_knowledge uri=zerops://themes/refinement-references/<name>

The brief lists every fetchable URI. Fetch on demand; don't preload.

## Fragment id reads BARE codebase name (not slot hostname)

Fragment ids use `codebase/<bare-host>/...` where `<bare-host>` is the
name from `Plan.Codebases[].Hostname` (e.g. `api`, `app`, `worker`).
This is NOT the same as the `service` field on facts, which can be
slot-named (`apidev`, `apidev/runtime`, `workerdev`) — those are
deploy-slot identifiers, not fragment-id components.

Worked example:

- Fact `{ "service": "workerdev", "topic": "worker_keepalive_heartbeat", ... }` — `service` = `workerdev` (slot)
- The corresponding worker codebase fragment id: `codebase/worker/knowledge-base` ← uses `worker` (bare), not `workerdev`

If the engine returns "unknown codebase 'workerdev' (Plan codebases:
[api app worker])", drop the slot suffix and retry with the bare name.

### Codebase name vs slot hostname (filesystem AND MCP)

The bare codebase name (`api`/`app`/`worker`) is the MCP-parameter
form: `codebase=`, `fragmentId=codebase/<host>/...`,
`fragmentId=env/<N>/import-comments/<host>`. The slot hostname
(`apidev`/`apistage`, `appdev`/`appstage`, `workerdev`/`workerstage`)
is the filesystem-mount form: SSHFS mounts live at
`/var/www/<slot>` (the dev slot is the editable mount the recipe
sub-agents wrote to; the stage slot is the deployable). When you
`ls` / `cat` source files at refinement time, use the slot hostname:

```
ls /var/www/<slug>/apidev/src       # filesystem — slot hostname
cat /var/www/<slug>/workerdev/zerops.yaml
```

When you `record-fragment` / `complete-phase`, use the bare codebase
name:

```
record-fragment fragmentId=codebase/api/integration-guide   # MCP — bare name
complete-phase  codebase=worker
```

Run-32's refinement agent burned a wrong-path `ls` round-trip on
`/var/www/<slug>/api/` — there is no `api/` mount; the codebases
mount at `apidev/`/`appdev/`/`workerdev/`. Keep the two forms
straight: filesystem = slot, MCP = bare.

## Rule-walk against the stitched documents

Read every stitched document end-to-end (root README, env READMEs +
import.yaml, codebase READMEs + zerops.yaml + CLAUDE.md). Read as a
porter would — top-to-bottom, no special context. Then walk EVERY
rule in `derived_rules.md` against EVERY document. ACT on every
violation; cite the rule id + the exact phrase + the preserving edit
in your `record-fragment mode=replace` body.

### How to walk

1. **Universal voice rules (V1-V6) apply to every porter-facing
   surface.** V1 (porter-clones-and-runs framing — never recipe-author
   voice), V2 (names what the recipe IS), V3 (slug-stem test — link
   text never carries corpus slugs), V4 (porter-actionable
   phrasings), V5 (defer to docs when sprawl), V6 (no authoring
   vocabulary). Walk every fragment for V-rule violations.
2. **Per-surface rules apply on the surface they name.** R1-R6 root
   README, T1-T4 tier README, TY1-TY5 tier import.yaml (+object-storage priority), IG1-IG6
   apps-repo Integration Guide, KB1/KB3-KB6 apps-repo Knowledge
   Base, Y1-Y15 apps-repo zerops.yaml.
3. **For each violation, ACT** — `record-fragment mode=replace` with
   the corrected body. Cite the rule id + the violating phrase + the
   preserving edit. Bias toward ACT — snapshot/restore reverts wrong
   ACTs automatically (any new blocking violation flips the body
   back to your pre-Replace state).

### Violation patterns to look for

These are the audience-model failures pattern-anchors miss:

- **Tier-prefix intros** (T2 / R2 violations) — "Tier 0
  — AI Agent" / "Tier 5 — Highly-available Production" as lead text
  in env intros. The framing names the tier instead of the delta.
- **`${peer_alias}` raw in prose** (V1/V6 violations) — token like
  `${apistage_zeropsSubdomain}` sitting unwrapped in IG/KB body
  prose without porter-recognizable framing. Porter doesn't know
  what `apistage` means; cite the alias as "the api stage's
  subdomain URL" instead.
- **KB describing already-fixed problems** (KB6 violation) — yaml
  ships `synchronize: false` already, yet KB warns about TypeORM
  schema corruption. The recipe yaml prevents this trap; KB names
  traps the porter STILL hits AFTER cloning + deploying.
- **Cross-recipe references** (V1) — "parent recipe
  nestjs-minimal" in prose. The porter reads ONE recipe; the parent
  graph is engine-internal vocabulary.
- **Slug-stem leakage** (V3 violation) — `[Zerops rolling-deploys
  reference]` / `[managed NATS service]` link text. Porter doesn't
  know corpus slugs; rewrite to porter concept or in-body completion.
- **Recipe-author voice** (V1 / V6 violations) — "during scaffold",
  "the agent owns", "we chose", "recipe author". Strip authoring
  vocabulary; reframe in porter-runtime terms.
- **IG steps that are recipe-internal conventions** (IG6 violation) —
  "Alias cross-service env vars under your own keys" — convention,
  not Zerops-forced. The cloned recipe yaml already aliases them.
  IG steps must be Zerops-forced or recipe-feature-specific.
- **Tier-vocab on codebase surfaces** — codebase README/IG/KB/yaml
  comments are read at every tier; "tier 5" / "stage" / "small
  prod" tokens belong to env-content surfaces only.

### Cross-surface non-duplication

Build a per-codebase topic-by-mechanism index across IG H3 + KB
bullet stems + zerops.yaml block comments. Each topic on EXACTLY
ONE surface:

- IG owns porter-transferable mechanisms (one mechanism per H3).
- KB owns post-deploy symptoms — symptom-first or
  directive-tightly-mapped stem.
- zerops.yaml comments own field-adjacent WHY-choices, not
  mechanism teaching (cross-reference IG instead).

When a topic spans surfaces, ACT to consolidate per surface
ownership (collapse the duplicating body to the natural-prose
cross-reference). KB can complement IG when KB carries a UNIQUE
symptom angle (HTTP code, quoted error string, observable
wrong-state) IG doesn't surface.

### CLAUDE.md leakage

If a CLAUDE.md fragment carries Zerops-platform content
(managed-service hostnames, env-var aliases), strip the leakage —
do NOT re-author Zerops content there (the codebase-content
sub-agent owns IG/KB/yaml-comments). If CLAUDE.md fragment carries
build/run/test commands the project doesn't have, HOLD (no fix).

## Showcase tier worker supplements

Refinement is the place to enforce the showcase tier worker supplement
contract from `briefs/codebase-content/worker_kb_supplements.md` (the
KB-content-shape atom; the code-shape contract lives at
`briefs/feature/worker_subscription_shape.md` and is enforced earlier
by `gateWorkerSubscription`). If `plan.Tier == "showcase"` AND the
worker codebase's KB lacks BOTH the queue-group / consumer-group
gotcha AND the SIGTERM drain gotcha, ACT — append the missing
bullet(s) using the sample shapes in the supplement atom.

This is the ONE exception to the "no NEW content" rule: the queue-
group + SIGTERM drain bullets are required by tier shape, not
discretionary.

## Surface order

Refine in this order so cross-surface dependencies stabilize:

1. `codebase/<h>/zerops-yaml` (Surface 7 voice — whole-yaml fragment;
   read it, edit the comment block(s) in place, replace the whole-yaml
   body)
2. `codebase/<h>/knowledge-base` (Surface 5 stem + trade-off + cite)
3. `codebase/<h>/integration-guide/<n>` (Surface 4 one-mechanism + cite)
4. `env/<N>/import-comments/<host>` (Surface 3 voice)
5. `codebase/<h>/intro` / `env/<N>/intro` / `root/intro` —
   non-trivial refinement is rare here; usually HOLD.

## Hold the line on parent recipe re-authoring

If the run has `parent != nil`, refinement reads the parent's
published surfaces (path threaded into the brief's pointer block). On
any fragment whose body would re-author parent material, HOLD. The
porter reads parent + this recipe together; duplicating parent
content here weakens both.

## record-fragment classification field is required

`record-fragment mode=replace` on `CODEBASE_KB`
(`codebase/<host>/knowledge-base`) or `CODEBASE_IG`
(`codebase/<host>/integration-guide/<N>`) MUST carry a
`classification` argument; the engine refuses with
`classification is required for fragments on surface "CODEBASE_KB"`
/ `"CODEBASE_IG"` when missing (recurring run-40 + run-42 failure
— two wasted record-fragment calls per ambiguous fragment +
re-read cycle).

Seven enum values per [spec-content-surfaces.md §"Fact classification taxonomy"](../../../../docs/spec-content-surfaces.md#fact-classification-taxonomy):
`platform-invariant`, `intersection`, `framework-quirk`,
`library-metadata`, `scaffold-decision`, `operational`,
`self-inflicted`. The common KB classification is `intersection`
(platform × framework, both contribute materially); worked example:
a `codebase/api/knowledge-base` bullet on nats.js v2 URL-credential
parsing → `classification: intersection`.

## Per-fragment edit cap + revert semantics

You make ONE replace attempt per fragment. For codebase fragments
(`codebase/<host>/integration-guide/<n>`, `codebase/<host>/knowledge-
base`, `codebase/<host>/zerops-yaml`, `codebase/<host>/claude-md`,
`codebase/<host>/intro`), the engine
wraps your Replace in a snapshot/restore transaction: surface
validators run scoped to the named codebase before AND after your
Replace; if the post-replace set has a new blocking violation absent
from the pre-replace set, the engine reverts to your pre-Replace
body. The response surfaces a `refinement-replace-reverted` notice
naming the violation that fired.

For env / root fragments the wrapper does not fire — slot-shape is
the only safety net at record time. Apply the edit threshold (cite
the violated rule + the exact fragment + the preserving edit); HOLD
when any of the three is fuzzy.

Either way: do NOT loop. One attempt per fragment.

## End of refinement

When you've walked every stitched path and made every ACT decision,
call:

```
zerops_recipe action=complete-phase phase=refinement
```

The phase has no exit gates beyond the rule-walk audit logged in
the notice stream.
