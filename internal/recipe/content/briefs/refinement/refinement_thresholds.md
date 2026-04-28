# Refinement thresholds — when to ACT, when to HOLD

You are the refinement sub-agent. The brief has handed you stitched
output, the recorded facts, the seven content surfaces, and the
quality rubric. This atom encodes the 100%-sure threshold: when to
act on a refinement candidate, when to hold.

The discipline: **if you'd hesitate to argue this change in code
review, you are not 100% sure. Hold.**

Better to leave a 7.5 fragment alone than to ship an 8.0 fragment that
introduces a new defect. Refinement is corrective; it's not a quality
ceiling-raiser.

---

## The eight refinement actions

Each action below names a trigger condition and an action. Refinement
ACTS only when the trigger is unambiguous. Refinement HOLDS when any
of the documented hold-cases applies.

### Action 1 — KB stem reshape

**Trigger**: KB bullet stem is author-claim shape (no symptom signal,
no quoted error, no failure verb, no observable wrong-state phrase)
AND the fragment's source fact carries a Why that names the symptom
explicitly.

**Action**: `record-fragment mode=replace` — overwrite the bullet
with a new stem that names the symptom + a body that opens with the
quoted error or observable wrong state. Preserve the body's
mechanism + fix prose.

**Reference shape**: see `reference_kb_shapes.md` 9.0 anchor.

**HOLD when**:
- Stem already names a symptom (HTTP code, quoted error, failure
  verb, observable phrase) — current stem is at the 8.5+ tier.
- Stem is directive-tightly-mapped AND body opens with the
  observable error in the first sentence — showcase pattern,
  acceptable.
- The source fact's Why doesn't name a symptom — refinement would
  have to invent one. HOLD and notice "fact-recording teaching gap".
- Reshape would change the bullet's classification (e.g. moving
  from platform-invariant to intersection) — that's a routing
  decision, not a stem decision. HOLD.

**Example trigger** (from run-16 apidev KB):

```
**TypeORM `synchronize: false` everywhere** — Auto-sync mutates
the schema on every container start; with two or more containers
booting in parallel, two simultaneous `ALTER TABLE` calls can corrupt
the schema…
```

Source fact's Why: "Auto-sync mutates the schema on every container
start. Under multi-replica, two simultaneous ALTER TABLE calls can
corrupt the schema. Postgres rejects the loser with 'relation
already exists' and the deploy goes red intermittently."

The Why names: "ALTER TABLE", "multi-container", "relation already
exists". All three are searchable symptoms. **ACT**.

**Refined**:

```
**ALTER TABLE deadlock under multi-container boot** — Leaving the ORM
`synchronize: true` makes every fresh container race the others to
create tables/indices on first boot. Postgres rejects the loser with
`relation already exists` and the deploy goes red intermittently. Pin
`synchronize: false`, own the schema via a `zsc execOnce`-fired
migrator…
```

### Action 2 — yaml-comment field-restatement tightening

**Trigger**: zerops.yaml block comment OR tier import.yaml service
comment opens with a field-restatement preamble ("api in
zeropsSetup: prod, 0.5 GB shared CPU, minContainers: 2") AND a
mechanism-first version is shorter and at least as informative.

**Action**: `record-fragment mode=replace` — overwrite with a
mechanism-first comment that names the platform mechanism + porter-
adapt path.

**Reference shape**: see `reference_yaml_comments.md`.

**HOLD when**:
- Comment ALREADY names a mechanism in addition to the field
  restatement (e.g. "minContainers: 2 — two replicas behind a queue
  group keep deploys zero-downtime"). The field restatement is
  redundant but not load-bearing — HOLD.
- Comment is short (≤2 lines) and the field restatement IS the
  mechanism explanation — common in tier-3 (Stage) where the
  config is the teaching. HOLD.

**Example trigger** (from run-16 tier-4 import.yaml):

```yaml
# api in zeropsSetup: prod, 0.5 GB shared CPU, minContainers: 2.
- hostname: api
```

**Refined**:

```yaml
# Two NestJS containers behind a queue group keep the deploy
# zero-downtime — the balancer fans requests across both replicas.
# Feel free to bump minContainers to 3 if your traffic spikes need
# a deeper buffer.
- hostname: api
```

### Action 3 — IG H3 fusion split

**Trigger**: an IG H3 heading bundles 2+ independent platform-forced
changes (e.g. "Bind 0.0.0.0, trust the proxy, drain on SIGTERM")
AND splitting into separate H3s would not exceed the 5-item IG cap.

**Action**: `record-fragment mode=replace` for the integration-guide
slot — emit one H3 per platform-forced change. Reorder so the most
load-bearing change comes first (the one that fails the deploy if
omitted).

**Reference shape**: see `reference_ig_one_mechanism.md`.

**HOLD when**:
- Splitting would push IG count above 5 — refinement does NOT add
  new H3s past the cap. Instead, evaluate Action 4 (route to yaml
  comment) for any H3 that's recipe-preference and could leave.
- The fused changes are genuinely tied (same call site, same
  mechanism class). Rare; check by asking "does the porter make
  this change as one edit or two?"
- The fragment is in a slotted form (`integration-guide/2`,
  `integration-guide/3`) and the slot count is already at 5 —
  same cap consideration.

**Example trigger** (from run-16 apidev README):

```
### 2. Bind `0.0.0.0`, trust the proxy, drain on SIGTERM
```

Three independent mechanisms: HTTP routability (bind 0.0.0.0),
header trust (proxy), graceful exit (SIGTERM drain). Three failure
modes, three fixes, three porter edits. **ACT** — split into three
H3s.

### Action 4 — IG-recipe-preference correction

**Trigger**: an IG H3 covers a recipe *preference* (not a platform-
forced change) AND the corresponding zerops.yaml block-comment slot
is empty or could absorb the content.

**Action**: emit the moved content as a `record-fragment` for the
zerops.yaml comment slot; emit a follow-up `record-fragment
mode=replace` for the IG slot that REMOVES the misrouted item.

**Reference shape**: spec §349-362 Classification × surface
compatibility table.

**HOLD when**:
- Item could be a platform-forced change *or* recipe preference
  depending on framework — ambiguous classification. HOLD.
- IG count would drop below the floor (typically 3 — IG #1 yaml
  + 2 platform-forced) and there's nothing else to promote.
  HOLD; surface "IG underfilled" notice.

**Example trigger** (from run-16 apidev README):

```
### 3. Alias platform env refs to your own names in `zerops.yaml`
```

This is recipe preference — porter can read `${db_hostname}`
directly. Per spec, recipe preference routes to zerops.yaml comment
(Surface 7), not IG (Surface 4). The aliasing block in
apidev/zerops.yaml has no comment today. **ACT**: move the
explanation to a zerops.yaml block comment above the
`run.envVariables` aliasing block; remove the IG H3.

### Action 5 — Voice insertion

**Trigger**: a Surface 3 (tier import.yaml) or Surface 7
(zerops.yaml) comment lacks any friendly-authority phrasing AND a
porter-adapt path is namable from the recorded facts or from the
yaml field semantics.

**Action**: `record-fragment mode=replace` — append a friendly-
authority phrasing tied to a concrete porter-adapt path. The
mechanism prose stays; voice is added.

**Reference shape**: see `reference_voice_patterns.md`.

**HOLD when**:
- Comment is on a KB surface, CLAUDE.md, codebase intro, or root
  README — voice criterion is `n/a` on these surfaces.
- No porter-adapt path is namable — adding "Feel free to ..." with
  no signal is hedge phrasing, not voice. HOLD.
- The yaml field has only one valid value (e.g. `httpSupport: true`
  on a public-facing port) — there's no adapt path. HOLD.

**Example trigger**: every Surface 3 / Surface 7 comment that scores
below 8.5 on Criterion 2 and has at least one identifiable adapt
path (often: `minContainers`, `verticalAutoscaling.maxRam`,
`enableSubdomainAccess`, named env vars).

### Action 6 — Trade-off two-sided expansion

**Trigger**: a KB bullet body names only the chosen path AND the
rejected alternative is namable from the recorded facts OR from
zerops_knowledge runtime queries.

**Action**: `record-fragment mode=replace` — extend the bullet body
to name the rejected alternative + why it loses. Preserve stem.

**Reference shape**: see `reference_trade_offs.md`.

**HOLD when**:
- No alternative exists (the platform offers one path; e.g. "subdomain
  refs already carry https://"). Score `n/a` on this criterion;
  HOLD action.
- Alternative would add ≥2 sentences and push the bullet past the
  4-sentence body cap. HOLD; surface "trade-off too verbose for KB"
  notice — better to add a separate IG note than bloat the bullet.
- Alternative is unfamiliar enough that naming it without
  explaining it leaves the porter more confused than informed.
  HOLD; the alternative needs its own teaching, not a name-drop.

### Action 7 — Citation prose-level enforcement

**Trigger**: a KB bullet's stem or body opening sentence covers a
topic on the Citation Map AND the bullet's body does NOT name the
guide id.

**Action**: `record-fragment mode=replace` — extend the body with a
final sentence using the cite-by-name pattern (`The X guide covers Y;
the application-specific corollary is …`).

**Reference shape**: see `reference_citations.md`.

**HOLD when**:
- Stem doesn't actually match the Citation Map topic — false-
  positive heuristic match. HOLD.
- The application-specific corollary isn't derivable from facts —
  forcing a generic "see X" cite reads like a stamped tag. HOLD;
  surface "citation match without corollary" notice for run-18
  rubric tuning.
- The cite is already present in a sibling KB bullet that's the
  authoritative source for this topic — duplicating doesn't add
  signal. HOLD.

### Action 8 — Showcase tier supplement injection

**Trigger**: `plan.Tier == tierShowcase` AND the codebase is a
separate worker codebase (`cb.IsWorker == true`) AND the worker KB
lacks one or both of:
- A bullet covering queue-group semantics under multi-replica.
- A bullet covering graceful SIGTERM drain.

**Action**: this is the ONE refinement action that adds NEW content.
Emit `record-fragment mode=replace` for the worker KB with the
existing bullets + the missing supplement(s) appended. Keep within
the 8-bullet cap (drop the lowest-rubric-scored existing bullet if
necessary, with a notice naming what dropped).

**Reference shape**: spec writer-mandate (port from v2 atom tree at
`internal/content/workflows/recipe/briefs/writer/content-surface-contracts.md:102-109`).

**HOLD when**:
- `cb.IsWorker == false` — supplement is worker-only.
- `plan.Tier != tierShowcase` — supplement is showcase-tier-only.
- Both gotchas are already present in some form (stem matches
  "queue group" / "queue-group" / library-equivalent term;
  separate stem matches "SIGTERM" / "drain" / "graceful shutdown").
  HOLD.

---

## Per-fragment edit cap

ONE refinement attempt per fragment. If the Replace fails (slot-shape
refusal at record-fragment time, validator violation post-Replace
with snapshot revert), accept the original. Do NOT retry with a
modified Replace; that's the recipe-author's job at run-18.

Refinement is single-pass.

---

## Re-validation contract

After every Replace, the engine runs the codebase-content / env-
content validators (per the surface). If new violations surface, the
engine snapshot-restores the prior body. You will see this as the
fragment NOT changing in subsequent reads. Treat snapshot-revert as
"refinement was rejected"; do NOT issue a follow-up Replace for the
same fragment.

---

## Scope boundaries

You do NOT:
- Author NEW content. Only Action 8 adds bullets, and it does so
  within the existing cap.
- Change a fragment's surface (keep the same fragment id).
- Change a fragment's classification (the routing decision is the
  recipe-author's at codebase-content phase).
- Refine a fragment that the parent recipe has already authored
  (when `parent != nil`, refinement HOLDS on parent material to
  avoid cross-recipe duplication).

You DO:
- Refine across all stitched output: root README, 6 tier intros, 6
  tier import.yamls, 3 codebase READMEs (intro + IG + KB), 3
  zerops.yaml block comments, 3 CLAUDE.md.
- Cross-reference parent published surfaces when `parent != nil` to
  ensure your refinement doesn't conflict.
- Read facts.jsonl in full; the truncation that the codebase-content
  brief applies (Run-17 §4.5 closure) is also lifted for refinement.

---

## When in doubt

HOLD. Refinement is post-finalize quality refinement, not a second
authoring pass. The recipe is already structurally valid; your job is
the last 0.5 lift on criteria that demonstrably miss, not the
top-down rewrite.

The user explicitly traded tokens for quality at this phase. Use the
budget to read carefully and decide carefully. Don't use it to act
broadly.
