# Cross-surface audit checklist — per-surface single-question walk

This checklist replaces the run-41 → 44 thirteen-defect-class enumeration.
Defect classes are EMERGENT from a surface's single-question test failing,
not curated. The walk is mechanical: **for each surface, for each item,
apply the surface's single-question editorial test**. Items that fail
get a finding. Cross-surface uniqueness runs as a second pass after the
per-item walk.

The single-question tests below are the **same** tests the writer
applies at authoring time
(`content-surface-contracts.md`). Audit and writer share one editorial
test per surface; drift between them re-opens the pre-run-45 gap where
authors classified one way and auditors curated patterns from another.

**`<host>` placeholder convention** — every `fragmentId` template uses
`<host>` to mean the **short codebase name** from `plan.codebases[].host`
(`api`, `app`, `worker`, etc.) — NOT the SSHFS-mount path or the
deployed hostname (`apidev` / `appdev` / `workerdev`). The fragment
store keys against the short form; `codebase/appdev/knowledge-base` is
**not** a valid fragmentId. Engine-side enrichment (see
`enrich-findings` boundary) will render the short form for you when the
emit carries `surface` + scope; emit the short form yourself only when
you author it in `itemReference`.

The seven content surfaces (audit numbering):

- S1 — Root README (`root/intro` fragment) — out of scope here;
  refinement-1 walks it.
- S2 — Tier README (`env/<N>/intro` fragments) — out of scope here;
  refinement-1 walks it.
- S3 — Tier `import.yaml` comments (typed `plan.EnvComments[<N>]`
  store; fragment IDs `env/<N>/import-comments/project` +
  `env/<N>/import-comments/<host>`). Audit refers to this surface as
  S3 tier `import.yaml` throughout.
- S4 — Per-codebase Integration Guide
  (`codebase/<host>/integration-guide/<N>` fragments; IG #1 is
  ENGINE-EMITTED from the codebase's zerops.yaml — IG #1 issues route
  to the underlying yaml fragment via `codebase/<host>/zerops-yaml`
  (S7)).
- S5 — Per-codebase Knowledge Base (`codebase/<host>/knowledge-base`).
- S6 — Per-codebase CLAUDE.md (`codebase/<host>/claude-md`) — **OUT OF
  SCOPE for refinement-2 (run-46 design)**. CLAUDE.md is repo-local
  (operator-of-this-repo facing), not published with the recipe;
  refinement effort here is wasted. The codebase-content phase's own
  CLAUDE.md authoring gates (validateCodebaseCLAUDE +
  claudemd-author brief) carry the load. The surface ID is retained
  in this enumeration for stable cross-reference, but the per-surface
  walk loop below does NOT include S6.
- S7 — Per-codebase `zerops.yaml` comments
  (`codebase/<host>/zerops-yaml`) — the WHOLE yaml is one fragment;
  IG #1 on S4 is engine-rendered FROM this fragment.

This audit walks **S3, S4, S5, S7**. S1 + S2 are intentionally out of
scope (refinement-1 walks them); **S6 is also out of scope** (repo-local,
not published — run-46 design).

---

## The per-surface single-question tests

Each surface has ONE editorial test. Apply it to every item on that
surface; items that fail emit findings. Do not curate from a pattern
list — the test catches the bullet by principle.

- **S4 (Integration Guide) — single-question test**:
  *"Would a porter who is NOT using this recipe as a template, but bringing their own code, need to copy THIS exact content into their own app?"*

  - **Pass**: the item is a Zerops-forced change a porter MUST apply
    to their own code to deploy on Zerops (bind 0.0.0.0, trust-proxy
    for the L7 balancer, `initCommands` with `zsc execOnce`,
    `forcePathStyle: true` for Object Storage, SIGTERM-drain for
    workers, deploy-files tilde-suffix for static).
  - **Fail**: framework setup the porter already knows
    (`composer install`, Vite `npm run dev`, `php artisan migrate`
    fragments), recipe-internal conventions the porter inherits
    pre-clone (a `## Use this api.ts wrapper` step), generic best
    practices (`alias cross-service env vars under your own keys` —
    the cloned recipe yaml already aliases them; Heroku/Render
    porters would do the same).

- **S5 (Knowledge Base) — single-question test**:
  *"Would a developer who read the Zerops docs AND the relevant framework docs STILL be surprised by this?"*

  - If the answer is "no, the platform docs cover it" — DROP; the
    bullet is a pointer to docs masquerading as a gotcha.
  - If the answer is "no, the framework docs cover it" — DROP;
    framework quirks belong in framework docs.
  - If the answer is "yes, it surprises you even after reading both"
    — keep.

- **S6 (Per-codebase CLAUDE.md) — OUT OF SCOPE for this audit (run-46
  design)**. The single-question test below is retained for
  cross-reference with the writer's `content-surface-contracts.md §S6`,
  but the per-item walk and emission below DOES NOT run on S6 fragments.
  The codebase-content phase's `validateCodebaseCLAUDE` + the
  claudemd-author brief carry the authoring gates; refinement-2 does
  not re-walk a repo-local surface.

  Reference test (for parity with the writer contract — do NOT emit
  findings against S6 fragments):
  *"Is this useful for operating THIS repo specifically — not for deploying it, not for porting it to other code?"*

- **S7 (Per-codebase `zerops.yaml`) — single-question test**:
  *"Does each comment explain a trade-off the reader couldn't infer from the field name?"*

  - **Pass**: causal comments naming mechanism + effect (`Readiness
    check gates the traffic switch — new containers must answer HTTP
    200 before the L7 balancer routes to them; this enables
    zero-downtime deploys.`), porter-customization invitations
    (`Feel free to change this value to your own custom domain,
    after setting up the domain access.`), `setup`-block preambles
    naming purpose + deliberate omissions.
  - **Fail**: field-name restatement (`# hostname: api` above
    `hostname: api`), authoring vocabulary (`zerops_dev_server`,
    `zsc noop`, "the agent", "scaffold"), bare mechanism on
    porter-tunable values (no adaptation hint), cross-surface
    deferral (`see IG #N for the rationale`).

- **S3 (Tier `import.yaml` comments) — single-question test**:
  *"Does each service block explain a decision (why this service exists at this tier, why this scale, why this mode), rather than narrating what the field does?"*

  - **Pass**: per-service blocks naming why-this-service-at-this-tier,
    why-this-scale (throughput vs HA rationale), why-this-mode
    (NON_HA durability trade vs HA failover), and tier-promotion
    context where natural.
  - **Fail**: field narration (`# minContainers: 2 sets the lower
    bound to 2 replicas`), cross-tier deferral (`Same as tier 0`),
    promotion-narrative inside service blocks.

---

## Failure-mode emission shape

When an item fails its surface's test, emit a finding with one of
three failure modes:

- **DROP** — the item belongs on NO surface. Examples surfaced by
  the tests above: self-inflicted gotchas (S5 — "our code had a bug
  we fixed"; the porter copying IG #1 verbatim never hits this),
  framework-quirk gotchas (S5 — covered in framework docs),
  recipe-internal scaffold decisions (S5 — "the recipe accepts this
  trade as a build-pipeline simplification"), recipe-internal
  naming (S5 — bullets whose stem names a recipe-specific feature
  like *"Cache demo"* / *"queue panel polls every ~700ms"* / the
  `tryGetClient` helper-class).
- **MOVE-TO-`<surface-id>`** — the item is right-content but on the
  wrong surface. Examples: a generic framework-setup bullet on S5 →
  MOVE-TO-S6 (CLAUDE.md); a Zerops-yaml field rationale on S6 →
  MOVE-TO-S7; an aspirational claim about how a tier wires shared
  state on S3 that's contradicted by the actual yaml → MOVE-TO-S5 as
  a porter-trap or DROP entirely.
- **REWRITE** — the item is right-surface but wrong-shape. Examples:
  an S7 comment that restates the field name (rewrite to name a
  trade-off the reader couldn't infer); an S4 IG step with a long
  paragraph of platform mechanism the porter doesn't need (rewrite
  to lead with the change); a missing-citation S5 bullet whose body
  matches the Citation Map (rewrite to add the cite-by-name form).

The failure-mode taxonomy is closed (these three values). A finding
that wants to be both DROP and MOVE is two findings on the same item
— emit them separately if you genuinely need both signals; in
practice DROP wins.

---

## How you walk

```
For SURFACE in {S3, S4, S5, S7}:        # S6 explicitly out of scope (run-46)
  For each ITEM on SURFACE:
    Apply SURFACE's single-question test.
    If the answer fails the test:
      Emit a finding with surface, fragmentId, itemReference,
      surfaceTestFailureMode (DROP/MOVE-TO/REWRITE), rationale.
      Optionally emit topic (for citation matches — see below).
    Always record the item's idKey in the walked-ledger receipt
      (Run-46 Item 1 — proves the per-item test ran; zero-finding is
      only acceptable when the idKey is in `walked`).

After the per-item walk:
  Run the cross-surface uniqueness pass (next section).
```

The walk is mechanical and explicit. Every codebase × every surface ×
every item — there is no codebase the loop skips. Earlier audits
flagged this as a discipline gap; the mechanical walk closes it by
construction.

---

## Per-surface caps + floors

These are the hard per-surface caps. Anything over-cap is a refinement-1
job (the structural validators run before refinement-2); the audit
flags only if refinement-1 missed.

| Surface | Floor | Cap | Action on miss |
|---|---|---|---|
| S4 IG items / codebase | 4 | 5 (incl. engine-emitted IG #1) | DROP / REWRITE the surplus |
| S5 KB bullets / codebase | no floor | 8 | DROP the surplus |
| S6 CLAUDE.md | — | — | OUT OF SCOPE (run-46) — codebase-content phase + claudemd-author brief carry the load |
| S7 yaml comments | 3 lines/svc | 8 lines/svc | refinement-1 catches |
| S3 tier yaml comments | 4 lines/svc | 10 lines/svc | refinement-1 catches |

S5 has no floor (Run-43 F2 + spec §S5). Bullets stand on their own
editorial-test merit, not on count — empirical span across the two
reference recipes is 2 (jetstream) to 7 (showcase).

---

## Cross-surface uniqueness pass

After the per-item walk, run ONE additional pass: **each fact lives on
exactly one surface; other surfaces that need the fact
cross-reference, they do not re-author**. This is the
one-fact-one-surface clause from
`content-surface-contracts.md` §"Cross-surface discipline".

For each finding-eligible item, scan every OTHER surface in every
OTHER codebase for the same teaching:

1. **Same error string quoted** — `"Authorization Violation"`,
   `"Blocked request. This host is not allowed."`,
   `getaddrinfo ENOTFOUND ${db_hostname}`.
2. **Same code fix shown** — `servers + user + pass`,
   `allowedHosts: true`, `dist/~`, the "self-shadow" yaml example,
   `app.set('trust proxy', true)`.
3. **Same env-var / yaml-field as the central artifact** —
   `${storage_apiUrl}` vs `${storage_apiHost}`, `JWT_SECRET`,
   `${broker_connectionString}`.

When two surfaces (across the same codebase OR across different
codebases) author the same teaching with substantially the same
depth, flag the LATER-read one as **DROP** (cross-reference back to
the canonical). Pass condition: one surface carries the canonical
teaching; the other says *"See [canonical-surface] — same trap on
this codebase too."*

The cross-surface uniqueness pass subsumes the earlier
`kb-ig-duplication` and `cross-codebase-content-duplication` defect
classes — it catches the same pattern by principle rather than by
named class.

**Cross-surface uniqueness receipt (Run-46 Item 6)** — when you emit
findings, also include the pass receipt alongside `walked`:

```json
{
  "findings": [...],
  "walked": [...],
  "crossSurfaceUniquenessScanned": <count>,
  "duplicates": ["<pair-ref-1>", "<pair-ref-2>", ...]
}
```

`crossSurfaceUniquenessScanned` is the COUNT of manifest items you
compared in the pass (typically equals the manifest total — every
findable item gets scanned for duplicates). `duplicates` lists any
pair references you flagged (each entry names the two surfaces or
fragmentIds that ship the same teaching). The refinement-close gate
refuses when `crossSurfaceUniquenessScanned` is below the manifest
total — that's the signal you didn't run the pass.

---

## Citation requirement (S4 + S5)

When an item on S4 (IG) or S5 (KB) covers a topic in the engine-
rendered Citation Map block below, the body MUST reference the cited
platform topic. A bullet whose topic matches the map but cites no
form-(a)/(b)/(c) shape is a REWRITE finding with `topic` set to the
matched family. Engine-side enrichment renders the canonical
suggestedReplacement (the form-(b) markdown link) when your emit
carries the topic.

The acceptance forms — (a) canonical guide ID in citation framing,
(b) friendly display name as markdown link, (c) bare docs URL — are
defined in the Citation Map block below. Walk that map; do not
re-derive the list here.

Two failure shapes to remember:

- **Bare-backtick narration is NOT a cite** — a bullet saying
  *"Zerops's `init-commands` feature stamps each `key:` value"*
  mentions the feature but does not cite the guide. The narration
  describes the mechanism in the bullet's own voice; it does not
  point the porter at the docs. Form (a) requires citation framing
  (`the \`<guide-id>\` guide covers …`).
- **Same-path wrong-fragment is NOT a cite** — a link to the right
  docs path with the wrong anchor (`...specification#env-var-model`
  when the map says `...specification#envvariables-`) FAILS the
  citation check. The link points the porter at the wrong section
  within the right doc.

Keyword-over-match guard: a bare keyword mention does NOT trigger the
citation requirement unless the item's PRIMARY teaching is the
matched family. A Node-stdout-buffering bullet that mentions
`SIGTERM` as the trigger for log loss is not about rolling deploys —
its topic is generic Node process-exit + stdout flushing.

---

## Self-referential decoration prohibition

`content-surface-contracts.md` §"Self-referential decoration
prohibition" says every published item must make sense to its reader
without them having read the rest of the recipe's code. If an item
requires the reader to know the recipe's helper-file names,
helper-class names, or scaffold-specific symbols, the item is
self-referential.

This is the principle behind several patterns the earlier curated
list called out by hand (X-Cache cross-origin tied to the recipe's
Cache demo, the `tryGetClient` helper, `ItemsCard` props, *"queue
panel polls every ~700ms"*). Apply the self-referential test as part
of S5's surface walk — if the bullet only makes sense to a reader
who's already read the recipe, the bullet fails S5 and emits a DROP
finding (or MOVE-TO-S6 if the operator-of-this-repo reading benefits
from it).

---

## What you produce

A findings list — the slim shape defined in `phase_entry.md` above
this checklist. Engine-side enrichment fills `classification`,
`suggestedReplacement`, and the short-form `fragmentId` from the
deterministic lookups; you emit `surface`, `itemReference`, `topic`
(optional), `surfaceTestFailureMode`, `rationale`, and `factRef`
(optional). See `phase_entry.md §"What you produce"` for the
canonical schema and the enrichment boundary.

**When to emit `factRef`** — when your rationale references a
specific recorded fact (e.g. you cite `facts.jsonl topic=X` or you
chain a finding to a porter_change discovery in the facts log),
emit `factRef` in the form `<topic>@<recordedAt>` using the exact
`topic` and `recordedAt` values from that fact's JSON line.
Engine-side enrichment then pre-fills `classification` from that
fact's `candidateClass` exactly. Without `factRef`, the engine
falls back to the surface default (`intersection` for S4/S5) —
there is no topic-substring fuzziness, so a missing `factRef` on a
finding that should inherit a non-default class results in a
suboptimal-but-safe enrichment, not a wrong one.

Emit `factRef` as `<topic>@<recordedAt>` whenever possible —
engine-side enrichment requires the disambiguator when multiple facts
share a topic+scope. Bare-topic `factRef` (no `@<recordedAt>`) only
resolves cleanly when the topic appears exactly once across the whole
`facts.jsonl`; otherwise the engine refuses to pick (live witness:
run-42 `facts.jsonl` carries two `topic=db-env-own-key-aliases`
records on `scope=api` with classes `platform-invariant` and
`scaffold-decision` respectively — the engine returns empty rather
than guessing). When you scan `facts.jsonl` and see the same topic on
more than one line, the `@<recordedAt>` shape is mandatory.

When the resolved fact's `candidateClass` is a DISCARD class
(`framework-quirk` / `library-metadata` / `self-inflicted`) and the
finding lands on S4 or S5, engine-side enrichment overrides
`surfaceTestFailureMode` to `DROP` (the DISCARD-class IS the surface
test's failure rationale). You may emit the finding with
`REWRITE` / `MOVE-TO-S<n>` and the engine corrects it; emitting
`DROP` yourself is always acceptable.

Empty findings list (`{"findings": []}`) is a valid pass.

DROP / MOVE-TO / REWRITE are the only failure modes. The closed set
is deliberate — emergent defect "names" (kb-ig-duplication,
self-inflicted-as-gotcha, scaffold-decision-as-gotcha,
framework-quirk-as-gotcha, recipe-internal-naming, etc.) are now
descriptions of WHY the surface test failed, captured in your
finding's `rationale` paragraph. They are not structural categories
the audit scans for.
