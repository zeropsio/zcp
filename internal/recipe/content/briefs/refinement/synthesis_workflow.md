# Refinement synthesis workflow

You walk every stitched fragment in the run output and decide for each
whether to ACT (replace via `record-fragment mode=replace`) or HOLD.
The decision is rubric-driven — `embedded_rubric.md` is the contract;
the seven reference distillation atoms (KB shapes, IG one-mechanism,
voice patterns, yaml comments, citations, trade-offs, refinement
thresholds) live on the discovery channel. Fetch the one matching the
class you're investigating via:

    zerops_knowledge uri=zerops://themes/refinement-references/<name>

The brief lists every fetchable URI under "Reference atoms — fetch on
demand" with a one-line description per atom. Don't preload them all;
fetch the atom WHEN you're scoring a suspect against its criterion.

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

## Refinement actions, by criterion

### Criterion 1 — Stem shape (KB)

Walk every `codebase/<h>/knowledge-base` fragment. For each `- **stem**
— body`:

1. Score the stem against `zerops://themes/refinement-references/kb_shapes` Pass examples.
2. If the stem is symptom-first OR directive-tightly-mapped (body
   carries the observable in its first sentence), HOLD.
3. If the stem is author-claim AND a symptom-first reshape is
   derivable from the body's mechanism + observable, ACT — replace
   the bullet with the reshape.
4. If the stem is author-claim AND no symptom-first reshape is
   derivable (the body lacks an observable failure mode), HOLD and
   record a notice: "fact-recording teaching gap — the deploy-phase
   agent didn't capture the observable; can't refine without it."

### Criterion 2 — Voice (Surface 7 zerops.yaml + Surface 3 tier yaml)

Walk every `codebase/<h>/zerops-yaml` whole-yaml fragment (one per
codebase, owns every block-level comment in that codebase's
zerops.yaml) AND every tier `import.yaml` service-block comment.
Count friendly-authority phrasings per `zerops://themes/refinement-references/voice_patterns`
"The heuristic":

1. If the fragment carries ≥1 phrasing tied to a named porter signal,
   HOLD.
2. If the fragment carries 0 phrasings AND a porter-adapt path is
   namable from the field semantics + recorded facts, ACT — read the
   current whole-yaml body, edit the relevant block-level comment in
   place, and `record-fragment mode=replace
   fragmentId=codebase/<h>/zerops-yaml fragment=<full yaml with edited
   comments>`. Anchor the new phrasing on the 4 reference shapes
   (Feel free to / Configure this to / Replace ... with / Disabling
   ... is recommended).
3. If 0 phrasings AND no adapt path is namable (the field has only one
   valid value, e.g. `httpSupport: true` on a public port), HOLD.
4. NEVER touch KB / CLAUDE.md / Root README — voice criterion is
   `n/a` on those surfaces.

### Criterion 3 — Citations (KB + IG)

Walk every KB bullet and IG H3 body. Cross-check the topic against
the recipe's CitationMap (threaded into the codebase-content brief
under "Citation guides for this recipe"):

1. If the topic IS on the Citation Map AND the body cites the guide
   by name in prose, HOLD.
2. If the topic IS on the Citation Map AND the body lacks the cite,
   ACT — append the cite-by-name pattern from
   `zerops://themes/refinement-references/citations` Pass 1 (final sentence: "The Zerops
   `<guide>` reference covers ..."). Be precise about the guide id.
3. If the topic is NOT on the Citation Map (or the recipe has no
   citationGuides at all), HOLD.

### Criterion 4 — Trade-off two-sidedness (KB)

Walk every KB bullet body. Per `zerops://themes/refinement-references/trade_offs`:

1. If the body names both the chosen path AND the rejected
   alternative (with the consequence of the rejected one), HOLD.
2. If the body names only the chosen path AND the rejected
   alternative is namable from the recorded facts, ACT — extend the
   body with the rejected-alternative consequence in one clause.
3. If only the chosen path is namable (no real alternative), HOLD.

### Criterion 5 — Classification × surface routing

Walk every fragment. The record-time refusal at `slot_shape.go`
already catches many misroutings; refinement is the backstop:

1. If a KB bullet carries content that belongs in IG (a code diff,
   a recipe-preference), ACT — move the principle to IG (or HOLD
   with notice if the move requires a NEW IG item, which exceeds
   refinement scope).
2. If a CLAUDE.md fragment carries Zerops-platform content
   (managed-service hostnames, env-var aliases), the record-time
   check should have refused already; if it slipped through, ACT —
   strip the leakage. Don't re-author the Zerops-related content
   here; the codebase-content sub-agent owns IG/KB/yaml-comments.
3. Cross-check operational content: if a CLAUDE.md fragment carries
   build/run/test commands but the project has none of those, HOLD
   (there's nothing to fix).

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
the violated rubric criterion + the exact fragment + the preserving
edit); HOLD when any of the three is fuzzy.

Either way: do NOT loop. One attempt per fragment.

## End of refinement

When you've walked every stitched path and made every ACT decision,
call:

```
zerops_recipe action=complete-phase phase=refinement
```

The phase has no exit gates beyond the rubric audit logged in the
notice stream.
