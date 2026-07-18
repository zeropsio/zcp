# Phase 1 — Static counter baselines on captured run-32 sim

**Captured target:** `/Users/fxck/www/zcp/docs/zcprecipator3/simulations/32-pattern2-fix-1/`
**Method:** Pure regex/grep/jq over captured artifacts. No sim dispatch. No platform contact.
**Date:** 2026-05-08

These numbers establish the current state. Subsequent interventions are scored as deltas against these.

---

## Counter #1 — Cross-codebase env-var coherence

**Method:** parse all three `<host>dev/zerops.yaml` files, extract `KEY: ${<service>_*}` mappings, flag mismatched left-hand keys for the same source.

**Result: 3 mismatches** (one MORE than the diagnosis named):

| Source | apidev uses | workerdev uses |
|---|---|---|
| `${db_password}` | `DB_PASSWORD:` | `DB_PASS:` |
| `${storage_accessKeyId}` | `S3_ACCESS_KEY_ID:` | `S3_KEY:` |
| `${storage_secretAccessKey}` | `S3_SECRET_ACCESS_KEY:` | `S3_SECRET:` |

The `DB_PASSWORD` / `DB_PASS` mismatch was missed by the head-to-head defect table — only the S3 pair was named. **3-axis coherence violation, not 2-axis.** appdev has zero managed-service references (SPA talks to apistage via URL), so no axis to violate.

---

## Counter #2 — Slug-leakage in published markdown

**Method:** grep published markdown (codebase READMEs + tier READMEs + tier yamls + apps-repo yamls) for English-cased slug verbalizations (the Pattern #2 shape).

**Result: 8 instances** across 3 codebases:

| Pattern | Count |
|---|---|
| `[managed NATS service]` | 3 |
| `[Zerops env-var model]` | 1 |
| `[Zerops object-storage service]` | 2 |
| `[Zerops rolling-deploys guide]` | 2 |

All in apidev/README.md and workerdev/README.md (KB sections). Hyphenated raw slug names: zero direct hits — Pattern #2 fix-pack closed the literal-slug shape, but the English-cased variant remained.

---

## Counter #3 — Cross-framework verb count

**Method:** regex for framework names not in the candidate's stack (NestJS+Vite+Svelte) across published markdown.

**Result: 22 cross-framework appearances** across 2 of 3 codebases:

| Framework | Count | Where |
|---|---|---|
| Express | 6 | apidev (5), apidev IG #2/#3 |
| Fastify | 4 | apidev IG #2/#3, |
| Webpack | 3 | appdev IG #2/#3/KB |
| Astro | 3 | appdev IG #2/#3/KB |
| SvelteKit | 3 | appdev — note: candidate is Svelte SPA, **SvelteKit is the wrong-framework reference** here |
| Next.js | 2 | appdev IG #2/#3 |
| Nuxt | 1 | appdev IG #2 |

apidev's "Adapt path: any Node HTTP framework" (line 218) explicitly enumerates Express + Fastify in the same paragraph. workerdev had 0 cross-framework hits (worker is NestJS standalone — no alternatives to enumerate). appdev's KB even mentions `SvelteKit` as an alternative when the candidate IS a Svelte SPA — distinct from SvelteKit.

---

## Counter #4 — Authoring-token leak in published content

**Method:** grep published files for authoring-vocabulary tokens.

**Result: 12 hits, all `zsc noop`**, none of the others detected as a published-content leak:

- `zsc noop`: 12 instances (mostly inside yaml `start: zsc noop --silent` lines + one `# zsc noop --silent keeps the dev container alive` comment)
- `zerops_dev_server` / `zcli` / `the agent` / `record-fact` / `recipe author` / `scaffold phase` / `feature phase` / `dogfood` / `sub-agent` / `orchestrator`: **0 published hits** each.

Most `zsc noop` hits are legitimate yaml content (the actual `start:` directive). The leak vector is the comments AROUND it carrying authoring voice (e.g. "the agent owns the watcher" reasoning) — but those rendered as voice not as token-name. Counter #4 needed sharpening — the strong leak vector is in facts.jsonl (Counter #5), not published-content tokens.

### Counter #4 — sharpened spec (run-32 Phase 2 follow-up)

Voice-pattern detection, not just token detection. Forbidden patterns to count in published markdown + yaml comments:

```regex
\b(under|via|owned by|managed by) (zerops_dev_server|the agent)\b
\b(during|at) scaffold( phase)?\b
\b(during|at) feature( phase)?\b
\bthe agent (owns|configures|wires|sets|knows|attests)\b
\b(we chose|we use|we set|we wire|we picked)\b
\bthe recipe (sets|wires|configures|generates)\b
\brecipe author\b
\b(scaffold|feature) sub-agent\b
\b(record-fact|record-fragment|fill-fact-slot)\b
```

Run on published surfaces only (codebase READMEs, tier READMEs + import.yamls, root README, apps-repo zerops.yamls — exclude CLAUDE.md per Q2 scope). Each match increments the counter; counter > 0 is a yellow flag, > 5 is red.

Captured run-32 hits under sharpened regex (against the same published file set as the original counter):

- `under zerops_dev_server`: 2 hits (env/0 + env/1 import.yaml comments — exactly the env-fragment leak introduced by original refinement, see [run-32-phase-2-results.md](run-32-phase-2-results.md))
- `the agent owns`: 1 hit (workerdev README inline yaml comment)
- `during scaffold`: 0 hits
- `we use|we wire|we set`: 0 hits
- `the recipe sets`: 0 hits

Total under sharpened regex: **3 voice-leak instances**. Original token-only counter scored 12 (mostly false positives on legit `zsc noop` yaml content); sharpened regex scores 3 real leaks. Counter #4 baseline = 3.

---

## Counter #5 — Fact contamination rate

**Method:** grep facts.jsonl for authoring-vocabulary tokens.

**Result: 142 total records, 17 contaminated (12%) on the strict token set:**

| Token | Hits |
|---|---|
| `zerops_dev_server` | 17 |
| `zsc noop` | 14 |
| `the agent` | 8 |
| `the recipe` | 2 |
| `record-fact` | 0 |
| `scaffold` | 60 (broader — not all leakage; many legitimate technical mentions) |

Records carrying ANY strict-token: **17 (12%)**. This was the diagnosis estimate; it holds.

---

## Counter #6 — tier_decision Why-fill rate

**Method:** jq over facts.jsonl for `kind=tier_decision` records, count those with non-empty `why` field.

**Result: 0/10 — Why-fill rate is 0%.**

Worse than the diagnosis estimated. Every tier_decision record is missing the porter-rationale field. `Validate()` doesn't require Why for this kind ([facts.go:175](internal/recipe/facts.go#L175)). Direct cause of Defect #13 (tier intros leak yaml-field jargon) — there's no rationale to render, so the agent improvises with whatever yaml-field words are nearest.

---

## Counter #7 — Refinement recall

Deferred to Phase 2. LLM-in-loop measurement.

---

## Counter #8 — KB-header consistency across siblings

**Method:** extract first heading inside the `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->` marker per codebase.

**Result: 3 codebases, 3 distinct shapes — 100% inconsistency rate.**

| Codebase | KB section header |
|---|---|
| apidev | (no header — bullets start directly) |
| appdev | `### Gotchas` |
| workerdev | `## Knowledge base` (engine-internal vocab leak — uses literal slot name) |

The codebase-content sub-agents authored these in parallel with no shared shape contract. Defect #11 confirmed.

---

## Bonus — IG item line counts (Defect #14)

| Codebase | IG #2 | IG #3 | IG #4 | IG #5 |
|---|---|---|---|---|
| **Jetstream** (golden) | 4 | 2 | 1 paragraph | 1 paragraph |
| **Showcase** (golden) | 5 | 5 | 5 | 5 |
| apidev | 19 | 24 | 39 | 51 |
| appdev | 20 | 21 | 23 | (no #5) |
| workerdev | 33 | 48 | 55 | 69 |

Candidate IG items run 4-15× longer than goldens. workerdev IG #5 (69 lines) is the worst single offender.

---

## Bonus — "Adapt path:" framing count

| Codebase | "Adapt path:" instances |
|---|---|
| apidev | 4 |
| appdev | 0 |
| workerdev | 4 |
| **Total** | **8** |

This is the structural shape that synthesis_workflow.md endorses as a legitimate IG mechanism (according to codex's first-pass diagnosis at synthesis_workflow.md:145). Direct compliance with brief teaching that we now know is wrong.

---

## Summary table — baseline numbers

| Counter | Number | Verdict |
|---|---|---|
| #1 cross-codebase env-var coherence | 3 mismatches | RED — no enforcer exists |
| #2 slug-leakage (English-cased) | 8 instances | RED — Pattern #2 fix incomplete |
| #3 cross-framework verb count | 22 | RED — IG over-generalization |
| #4 authoring-token leak in published | 12 (mostly legit yaml) | YELLOW — voice-leak counter needed |
| #5 fact contamination rate | 12% (17/142) | RED — zero sanitization |
| #6 tier_decision Why-fill | 0% (0/10) | RED — schema doesn't require it |
| #7 refinement recall | deferred | — |
| #8 KB-header consistency | 100% inconsistent | RED — no cross-codebase shape contract |
| Adapt-path framing | 8 instances | RED — direct compliance with bad brief teaching |
| IG line bloat | 4-15× golden | RED — bigger than diagnosis estimated |

Eight RED counters. None below the diagnosis's expectation; **two (Counter #1 and Counter #6) worse than estimated**.

---

## Implications for Phase 2 prioritization

The Phase 1 numbers say the diagnosis was directionally correct AND undercounted in two places:
- Counter #1 has THREE axis violations, not the two named in the head-to-head table.
- Counter #6 is at 0%, not the "often null" the diagnosis estimated.

Phase 2 (refinement reshape) is the right next step IF refinement can catch shape violations the current rubric misses. The rule list at [run-32-rules-from-jetstream.md](run-32-rules-from-jetstream.md) maps directly to most RED counters — IG2 covers IG bloat, V3 covers slug verbalization, IG4 covers cross-framework drift, KB1 covers KB header inconsistency, V6/Y7 cover authoring voice.

Counter #1 (cross-codebase coherence) is OUTSIDE the rule list because it's structural-not-content. Stays Step 2 in attack order (engine validator at scaffold complete-phase) — refinement won't fix scaffolds.
Counter #6 (tier_decision Why-fill) is upstream of refinement — fix at facts.go Validate().
