# Content extension

## Voice — the reader is a porter, never another recipe author

Everything you write — fragment bodies, `zerops.yaml` inline comments,
committed source-code comments, README prose — is read by someone
deploying this recipe into their own project, not another recipe
author. **Never write:** "the scaffold", "feature phase", "pre-ship
contract item N", "showcase default", "showcase tradeoff", "the
recipe", "we chose", "we added", "grew from", "scaffold smoke test".
**Always write:** descriptions of the finished product. The product
IS wired; the product HANDLES the upload; there is no authoring-phase
"before" for a porter.

Your additions EXTEND the scaffold's fragments — they do not replace.
`record-fragment` on IG / knowledge-base / claude-md/* appends to the
existing body; root/env ids overwrite. Same placement rubric as
scaffold — yaml-comment, IG, KB, CLAUDE.md notes.

KB bullets use `**Topic** — prose` only. Do NOT use `**symptom**:` /
`**mechanism**:` / `**fix**:` triples; debugging runbooks live in
`claude-md/notes`.

- Adding a dep → extend KB if the choice is non-obvious
- Adding an env var → extend `zerops.yaml` with an inline comment
- Adding an `initCommand` (seed, scout:import) → consult the execOnce
  key-shape atom below. Once-per-lifetime seed with a documented
  re-run lever? Use key shape #3: `<slug>.<operation>.v1`, bump the
  suffix to re-run.

Keep `codebase/<h>/claude-md/*` extensions terse (30–50 lines, cap
60). Never add `Quick curls`, `Smoke test`, `Local curl`, `Redeploy
vs edit`, or `Boot-time connectivity` subsections — cross-codebase
runbooks live in the recipe root README.

Typical scale: 1–2 KB bullets + 0–1 IG item per feature. Most features
change code, not topology.

## IG scope (extending scaffold's items)

If your feature genuinely changes what a porter has to do to deploy
their NestJS / Laravel / SvelteKit app on Zerops (binding,
trust-proxy, env-aliasing, init-commands, deploy-files),
`record-fragment mode=append fragmentId=codebase/<h>/integration-guide`
adds the new item.

If your feature adds a recipe-internal CONTRACT (a new endpoint shape,
a cache-demo wrapper's TTL convention, a NATS subject naming rule, a
queue-group name), that goes to KB
(`record-fragment mode=append fragmentId=codebase/<h>/knowledge-base`)
or claude-md/notes
(`record-fragment mode=append fragmentId=codebase/<h>/claude-md/notes`)
— NOT to integration-guide. The IG audience is a porter bringing their
own code; recipe-internal contracts are not their concern.

Examples:

- Adding `forcePathStyle: true` for object storage → IG (porter needs
  to do this in their own code)
- Adding a `/items/:id` cache-demo wrapper with `X-Cache: HIT/MISS` →
  KB or claude-md/notes (recipe-internal endpoint shape)
- Adding `app.enableCors({ exposedHeaders: ['X-Cache'] })` → KB
  (platform × framework intersection: CORS expose-headers behaviour)
- Adding a `/status` aggregator endpoint → claude-md/notes
  (recipe-internal liveness pattern)

Aim for 0-1 IG appends per feature; 1-2 KB bullets is normal; 0-3
claude-md/notes additions is normal. If you find yourself adding 2+ IG
items, check whether the additions are recipe-internal contracts that
belong elsewhere.

Mount vs container execution-split (editor tools on the mount,
framework CLIs via ssh) lives in `principles/mount-vs-container.md`
(injected above). Local `npm install` / `npx build` against the SSHFS
mount tunnels through FUSE and misses the container's env vars — run
framework CLIs via `ssh <hostname>dev "..."`.

# Recording feature-phase facts

Record every platform-trap, porter-change, scaffold-decision, and
browser-verification fact via **`zerops_recipe action=record-fact`**
(the v3 tool) — **NOT** the legacy `zerops_record_fact` tool. v3
records land in `facts.jsonl` where the classifier and surface
validators see them; the legacy tool writes to `legacy-facts.jsonl`
(invisible to v3's classification pipeline).

Shape (all fields required — camelCase):

- `topic` — short kebab-case
- `symptom` — observable failure or signal (status + quoted line)
- `mechanism` — why (platform-side; both sides if intersection)
- `surfaceHint` — one of: `root-overview`, `tier-promotion`,
  `tier-decision`, `porter-change`, `platform-trap`, `operational`,
  `scaffold-decision`, `browser-verification`
- `citation` — `zerops_knowledge` guide id, published-recipe URL, or
  `none` for browser-verification / operational observations
- `scope` — optional `<service>/<area>` string when the fact is tied
  to a specific codebase or tab

Classification before routing (same contract as scaffold):

- Self-inflicted findings (code bugs you authored then fixed) → DISCARD
- Platform × framework intersections → KB bullet + cite the guide
- Genuine platform traps → KB bullet + cite the guide
- Operational observations (logs, dev-loop ergonomics) → CLAUDE.md notes
- Browser-walk verifications (see phase-entry step 7) → `surfaceHint:
  browser-verification` with the screenshot + console digest in
  `extra.screenshot` / `extra.console`

## Self-inflicted litmus

Spec rule 4: could this observation be summarized as "our code did X,
we fixed it to do Y"? If yes, **discard** — the fix belongs in the
code, there is no teaching for a porter cloning the finished recipe.
The engine classifier auto-overrides agent-supplied surfaceHint when
fixApplied describes a recipe-source change without platform-side
mechanism vocabulary in failureMode (V-1). See scaffold brief
"Self-inflicted litmus" subsection.

## Self-validate before terminating

Before you terminate, call:

    zerops_recipe action=complete-phase phase=feature codebase=<host>

per codebase you touched. Same shape as scaffold's self-validate path:
runs the codebase-scoped validators against just that codebase, so you
see only your own work and can correct it in-session.

If `ok:false` with violations on `codebase/<host>/{integration-guide,
knowledge-base,claude-md/*}` ids → fix via `record-fragment
mode=replace fragmentId=codebase/<host>/<name> fragment=<corrected
body>`. Re-call until `ok:true`, then move on to the next codebase.

Calling `complete-phase phase=feature` without a codebase is also
valid — it validates ALL touched codebases at once. Use the scoped
form when iterating on a single codebase's violations; use the
phase-level form when you've cleared every codebase and are about to
hand back to main for phase advance.

## At feature close — commit per-feature

Commit each feature extension separately with a descriptive message
(`git commit -m 'feat: add CRUD endpoints + Postgres wiring'`) from
`<cb.SourceRoot>`. The per-feature commit shape is what a porter sees
when scrolling git history; squashing or deferring loses the narrative.
