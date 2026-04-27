# Run 15 — readiness preparation knowledge

**This is not the readiness plan.** It captures findings from a post-run-14
analysis conversation (2026-04-27) that surfaced AFTER the formal run-14
ANALYSIS / CONTENT_COMPARISON / PROMPT_ANALYSIS docs landed. The findings
re-frame what those docs claimed about content quality and name a
structural gap that should drive the bulk of run-15-readiness scoping.

The full readiness plan (`plans/run-15-readiness.md`) is to be authored
later; this doc feeds into it.

---

## TL;DR — what changed about the run-14 verdict

The run-14 ANALYSIS landed at **8.5/10** content grade with R-14-1 (Cluster
A.2 subdomain race) as the headline defect. A spec-content-surfaces audit
applied to the actual deliverable shows **the honest grade is closer to
6.5-7.0/10**.

The structural insight that re-frames the run:

> **The engine plumbing is working. The defect is that surface-purpose
> teaching is not delivered at the agent's content-authoring decision
> moment. Every engine-side content investment so far has been token-level
> (length, voice words, format markers); none of those can catch the
> failure modes [docs/spec-content-surfaces.md](../../spec-content-surfaces.md)
> exists to prevent.**

The spec literally says so up front:

> *"None of these are caught by token-level checks ... because all three
> failure modes satisfy those patterns trivially. The correction has to
> happen at the mental-model layer — BEFORE content is authored."*
> — [spec-content-surfaces.md §Why this exists](../../spec-content-surfaces.md)

Run-14's content layer is the next bottleneck once Cluster A.1 (stitch
race) closed and finalize started actually authoring. Run-15 readiness
should make this the gating cluster.

---

## Findings that came out of post-run-14 analysis

Cross-referenced to the run-14 deliverables for context.

### R-14-6 — Tier-README intro extract contract violation

**Surface**: Per-tier README. **Spec**: [Surface 2](../../spec-content-surfaces.md).

`<!-- #ZEROPS_EXTRACT_START:intro# -->` markers wrap content that renders
on the recipe page UI as the tier card description. Contract is 1-2
sentences (per laravel-showcase reference, [`recipes/laravel-showcase/5
— Highly-available Production/README.md`](../../../../recipes/laravel-showcase/5%20—%20Highly-available%20Production/README.md)
which is 8 lines total / 1 sentence between markers).

Run-14 wraps **36 lines of ladder content** between those markers in every
tier README ([runs/14/environments/0..5/README.md](../runs/14/environments/0%20—%20AI%20Agent/README.md)).
Recipe page render of the intro card would carry the whole ladder.

**Knock-on**:
- §V `env-readme-too-short threshold 40` is enforcing the wrong shape on
  the wrong content.
- Finalize brief teaching ("env READMEs target 45+ lines; threshold 40")
  is misframed — it's optimizing GitHub README density, not recipe-page
  card density. Two surfaces, opposite contracts.
- Run-12's 52-60 line tier README baseline that run-13 ANALYSIS treated
  as "recovery target" was already wrong by this contract.
- Run-13's 9-line collapse may have been *accidentally closer to correct*.

**Fix shape**: split the surface — intro markers wrap a 1-2 sentence
summary; ladder content (Shape at a glance / Who fits / etc.) lives AFTER
the closing marker. §V validator enforces *short* on extract content and
ladder-structure on body content as separate rules.

[CONTENT_COMPARISON.md §2](../runs/14/CONTENT_COMPARISON.md) re-score:
Per-tier READMEs was 9/10 → honestly **4-5/10**.

### R-14-7 — §V validator surface coverage doesn't reach import.yaml block comments

**Surface**: Env import.yaml comments. **Spec**: [Surface 3](../../spec-content-surfaces.md).

The §V validator family was wired to README / CLAUDE.md / zerops.yaml
content surfaces. The import.yaml block comments themselves are
porter-readable content (Zerops dashboard renders them) but no validator
patrols them.

Three classes of defect slipped through, all six tier yamls:
- Fabricated identifier `project_env_vars` (R-14-8 below)
- Authoring-voice leaks: "recipe author" mentioned in tier 1 + tier 5
  ([1 — Remote (CDE)/import.yaml:9](../runs/14/environments/1%20—%20Remote%20%28CDE%29/import.yaml),
  [5 — Highly-available Production/import.yaml:13](../runs/14/environments/5%20—%20Highly-available%20Production/import.yaml))
- Possibly-unverified "three replicas with automatic failover" claim on
  tier 5 line 5 — same shape as the §V `tier-prose-replica-count-mismatch`
  rule but in a yaml comment, so the validator never sees it

**Fix shape**: extend §V's surface coverage to include import.yaml block
comments. Same rules (causal word, voice, factuality), broader patrol.

### R-14-8 — Fabricated yaml field names in comments

**Surface**: Env import.yaml comments + per-codebase zerops.yaml comments.

The agent invented `project_env_vars` (snake_case) for a field that
actually exists as `project.envVariables` (camelCase, nested). All six
tier yamls reference this fictional name in their preamble comments.

A porter searching the yaml for `project_env_vars` finds nothing. Run-12
§A's alias-type contracts table teaches platform identifier conventions
(`${db_hostname}`) but not yaml field naming (camelCase, nested,
schema-derived).

**Fix shape**: brief teaching extension naming the yaml field convention
positively, OR engine-side validator that ensures comment-named fields
exist in the yaml below the comment.

### Surface-purpose failures across IG / KB content (the big finding)

**Surface**: Per-codebase IG (Surface 4) + KB (Surface 5).

Walking [docs/spec-content-surfaces.md](../../spec-content-surfaces.md)
tests against [appdev/README.md](../runs/14/appdev/README.md) and
[apidev/README.md](../runs/14/apidev/README.md):

**appdev IG (8 items, test: "Does a porter bringing their own
Svelte/Vite app need to copy THIS exact content?"):**

| # | Item | Verdict |
|---|------|---------|
| 1 | Adding zerops.yaml | ✓ engine-emitted |
| 2 | Bind 0.0.0.0 + read PORT | ✓ concrete diff |
| 3 | Vite allowedHosts: true | ✓ concrete diff |
| 4 | Bake api URL at build time | ⚠️ explanation paragraph, no fresh code, duplicates IG #1's yaml comment |
| 5 | Serve compiled assets via sirv | ✗ describes server.js (recipe scaffold) |
| 6 | Drain on SIGTERM | ✗ describes server.js handler |
| 7 | Independent /healthz | ✗ describes recipe's /healthz design |
| 8 | Use the dev server, not deploys | ✗ wrong surface — `zsc noop --silent` IS THE Surface 7 example in the spec |

**3 of 8 IG items pass cleanly.** Items 5/6/7 are the v28 `api.ts` wrapper
anti-pattern named in the spec. Item 8 is wrong-surface (zerops.yaml
comment material).

**appdev KB (11 bullets, test: "Would a developer who read the Zerops
docs AND framework docs STILL be surprised?"):**

| # | Bullet | Verdict |
|---|--------|---------|
| 1 | Vite define is build-time-only | ⚠️ borderline (Vite docs cover) |
| 2 | apistage subdomain resolves only after first deploy | ✓ |
| 3 | Subdomain access is httpSupport: true side effect | ✓ |
| 4 | sirv single: true SPA fallback | ✗ scaffold decision (sirv is recipe's chosen library) |
| 5 | Static runtime is wrong when dev needs Node | ✓ |
| 6 | SIGTERM drain is mandatory | ✓ |
| 7 | Vite allowedHosts: true is positive knob | ✓ |
| 8 | Demonstration panels — one tab per managed service | ✗ describes recipe's SPA design |
| 9 | X-Cache header crosses L7 boundary | ✓ but verbose |
| 10 | Queue panel polls every ~700ms | ✗ describes recipe's SPA implementation |
| 11 | Tabs over single-column for browser-walk | ✗ leaks zerops_browser authoring tool + describes recipe-internal design driven by R-14-5 |

**6 of 11 KB bullets pass cleanly.** Items 4/8/10/11 are the spec's
"self-referential decoration" anti-pattern verbatim — agent documents its
own scaffold + SPA design as if they were platform contracts.

**apidev KB spot-check** shows the same density of self-referential
decoration:
- #7 Health endpoint composition — recipe's /health design choice
- #9 Cache-demo endpoint shape — recipe's TTL choice
- #10 Queue-demo state machine — recipe's jobs-table contract
- #11 Status aggregator vs Terminus — recipe's /status design choice
- #12 NATS subjects emitted by /items — recipe's specific subject names

**5 of 12 apidev KB bullets fail.** Same pattern.

**Aggregate**: ~30-40% of IG/KB content fails its surface-purpose test
across all three codebase READMEs.

[CONTENT_COMPARISON.md §8](../runs/14/CONTENT_COMPARISON.md) re-score:
- Per-codebase IG: was 9 → **5-6**
- Per-codebase KB: was 9 → **5**

---

## The cascade — honest re-scored aggregate

| Surface | I scored | Spec-honest |
|---------|----------|-------------|
| Recipe-root README | 8 | 8 (held) |
| Per-tier READMEs ×6 | 9 | **4-5** (intro extract contract violation) |
| Per-tier yaml comments ×6 | 8 | **5** (fabricated identifier + voice leaks + uncovered surface) |
| Per-tier yaml fields ×6 | 9 | 9 (held; engine-emit-correct) |
| apidev/zerops.yaml | 9 | 8 (held minus minor) |
| appdev/zerops.yaml | 9 | 8 |
| workerdev/zerops.yaml | 9 | 8 |
| apidev/README IG+KB | 9 | **6** |
| appdev/README IG+KB | 9 | **5-6** |
| workerdev/README IG+KB | 9 | **7** (worker shape is platform-distinct so more bullets pass) |
| apidev/CLAUDE.md | 9 | 9 (held) |
| appdev/CLAUDE.md | 9 | 9 |
| workerdev/CLAUDE.md | 9 | 9 |
| Showcase SPA panels | 9 | 9 (held) |

**Aggregate honest: 6.5-7.0 / 10** (vs 8.5/10 claimed in [ANALYSIS.md §7](../runs/14/ANALYSIS.md)
verdict).

The lift from run-13's 6.5 is structurally real (Cluster A.1 + finalize
authoring + tier yaml comment density), but the content authored
downstream of A.1's closure doesn't pass spec tests at the rate the
optimistic scoring assumed. Run 14 is approximately at run-12 quality
(7/10), not at the 8.5 stretch target.

---

## The structural insight — engine plumbing works, content-context delivery doesn't

Layer-by-layer (from [the conversation that produced this doc]):

| Layer | Working? | Evidence |
|-------|----------|----------|
| File system / write | ✅ | Cluster A.1 closure |
| Fragment ID → file routing | ✅ | 61 finalize fragments correct |
| Validator infrastructure | ✅ | §V drove tier-3 db prose fix |
| Brief composition (~0% wrapper) | ✅ | §B trajectory saturated |
| Brief content for token/format/length | ✅ | Voice rules, IG numbered shape, KB triple format reach the agent |
| **Brief content for surface-purpose** | ❌ | Spec exists; brief teaches around it (voice, format) but not directly (per-fragment-id surface litmus at write-time) |
| **Per-fragment-id surface teaching at record-time** | ❌ | Brief preface bulk-loads surface contracts; agent reads them once, then drifts |
| **Surface-purpose validator class** | ❌ | No engine check ever asks "does this fragment's content match this fragment-id's surface contract" |

The bottom three are the gap. Every Cluster D-style content investment
so far reaches the brief preface; none reach the agent's decision-time
moment when it's about to call `record-fragment fragmentId=codebase/<h>/knowledge-base`.

---

## Run-15 readiness — Cluster F candidate (content surface routing)

This is the one-paragraph framing for the eventual run-15-readiness.md to
expand:

> **Cluster F — content surface routing.** Operationalize spec-content-
> surfaces.md as per-fragment-id teaching delivered at record-time, not
> as a brief-preface contract. The agent doesn't need a longer surface-
> contract preamble; it needs the spec's one-sentence test verbatim at
> the moment it's about to record a fragment. Plus the v28 wrong-surface
> catalog inline as concrete pattern matches.

### Two implementation directions

**Direction 1 — Brief restructure (cheaper)**

Surface contracts move from a "voice rules" preamble to per-fragment-id
sections. When the brief mentions `codebase/<h>/knowledge-base`, the
adjacent teaching is the spec §Surface 5 test verbatim:

> *"Would a developer who read the Zerops docs AND the framework docs
> STILL be surprised by this? If 'no, it's in the framework docs' →
> framework quirk, not a gotcha. If 'no, it's in the docs' → remove.
> Self-inflicted ('our code did X, we fixed it') → discard."*

Plus 2-3 v28 anti-pattern examples (the `api.ts` wrapper, the silently-
exiting seed). The agent is forced to test its draft against the
specific surface contract at the moment of authoring.

Cost: brief content rewrite (~60-100 lines redistributed). No engine
LoC.

**Direction 2 — Engine-side classifier at record-time (structural)**

`record-fragment` accepts a `classification` field per the spec's
taxonomy (platform-invariant / intersection / scaffold-decision /
operational / framework-quirk / library-metadata / self-inflicted).
Engine routes based on classification:
- platform-invariant → KB or IG (depending on actionability)
- intersection → KB
- scaffold-decision → zerops.yaml comment OR CLAUDE.md
- operational → CLAUDE.md
- framework-quirk / library-metadata / self-inflicted → REJECT (don't
  publish)

The classification field becomes mandatory; the engine refuses the
record on mismatch (e.g., classifying a bullet as "scaffold-decision"
but routing to `codebase/<h>/knowledge-base`).

Cost: engine ~30-50 LoC + brief teaching update. Higher value: closes
the dump pattern structurally.

### §B trajectory continuation

The §B trajectory is moving Plan-derivable decisions into engine-side
machinery:
- §B (run-12) — Plan-derivable content into brief composer
- §B2 (run-13) — Plan-derivable content into dispatch prompt composer
- §B3 (run-15-readiness candidate per [PROMPT_ANALYSIS.md §5](../runs/14/PROMPT_ANALYSIS.md))
  — Plan-derivable corrective patterns into yaml emit (closes R-14-4)
- **§B4 / Cluster F (this doc)** — surface-purpose decision teaching at
  fragment-record time

§B4 is the natural next dimension. The pattern: each step reduces what
the agent has to decide on its own; what's left is what genuinely
varies recipe-to-recipe (per the system.md §4 DISCOVER-side line).

### Cross-references to run-14 deliverables

Where in the run-14 docs the content-surface findings sit:
- [ANALYSIS.md §3 R-14-1..R-14-5](../runs/14/ANALYSIS.md) — defects
  numbered before the post-analysis conversation; R-14-6/7/8 from this
  prep doc are additions
- [ANALYSIS.md §6.5 root-cause synthesis](../runs/14/ANALYSIS.md) — the
  positive structural insight (Cluster A.1 took, §B trajectory holds)
  remains correct; the negative structural insight (R-14-1's I/O race)
  is one of two; the second negative insight (surface-purpose teaching
  not delivered at decision-time) emerged AFTER the doc landed and
  should land in run-15-readiness §6 risks
- [CONTENT_COMPARISON.md §11 honesty pass](../runs/14/CONTENT_COMPARISON.md)
  — partially right (the laravel comparison framing, the "useful vs
  misleading" pass) but the per-surface scores were optimistic across
  the board; this doc's re-score table corrects them
- [PROMPT_ANALYSIS.md §5 fix stack](../runs/14/PROMPT_ANALYSIS.md) —
  R-14-P-1 (B.3 reachable-slug stealth regression) holds. R-14-P-4
  (finalize env-README teaching) compounds with R-14-6 from this doc:
  the brief teaches the wrong target (45+ lines) for the wrong surface
  (intro extract slot)

---

## Open questions for the run-15 content exploration

The user is running content exploration in a fresh instance. Questions
that should drive that exploration:

1. **What's the right shape for the intro extract slot?** 1 sentence?
   2-3? Should the recipe-page render also include the ladder, or just
   the extract? Does the platform's recipe-page UI have a known character
   budget for the card description?

2. **What's the right structural division between IG and KB?** Per the
   spec, IG is "concrete diffs the porter copies" and KB is "platform
   traps that surprise". Run-14 shows the line drifts even with the spec
   in hand. Is the answer:
   - Tighter brief teaching (Direction 1)?
   - Engine-side mandatory classification (Direction 2)?
   - Restructured fragment-id namespace (e.g., split KB into `kb/platform-traps`
     and `kb/platform-x-framework` so the routing decision happens at
     fragment-id pick time, not at content-write time)?

3. **What's the cleanest representation of the spec's seven-surface
   taxonomy in agent-facing teaching?** Bullet list with one-sentence
   tests + 1-2 anti-pattern examples per surface? Decision tree? Worked
   example walking a fact through the taxonomy?

4. **Where does the v28 wrong-surface catalog live durably?** The spec
   has it in §Counter-examples; the brief should pull from it but should
   it pull verbatim, summarized, or with run-14 entries appended?

5. **Should some surfaces be removed entirely?** The KB section seems
   to attract the most self-referential decoration. Could the recipe
   ship without a KB section by default, with KB only added when there's
   genuinely a platform trap to document?

6. **How does this interact with Direction 2's classification field?**
   If `record-fragment` accepts `classification`, what happens when the
   classification is "self-inflicted"? Engine refuses with a teaching
   message? Engine accepts but routes to a non-published `discarded/`
   buffer? Engine treats it as a record-fact instead?

7. **What's the right floor + ladder split for tier READMEs?** Body
   ladder content lives AFTER the closing extract marker. Does the §V
   validator's "ladder structure" check then fire on the body, or is
   the ladder optional?

These should drive the content exploration's first pass. The user has
additional thoughts to feed in.

---

## Status

- This doc landed: 2026-04-27, post-run-14 analysis conversation
- Next step: fresh-instance content exploration walks
  [docs/spec-content-surfaces.md](../../spec-content-surfaces.md) +
  [run-14 deliverables](../runs/14/) +
  [/Users/fxck/www/recipes/laravel-showcase/](../../../../recipes/laravel-showcase/)
  + this prep doc, produces a content-routing redesign proposal
- After that: run-15-readiness.md authored from the redesign proposal +
  R-14-1 + R-14-P-1 + R-14-4 (§B3) carryforwards from
  [PROMPT_ANALYSIS.md §5](../runs/14/PROMPT_ANALYSIS.md)
- Then: run-15 dogfood
