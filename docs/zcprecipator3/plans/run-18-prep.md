# Run-18 prep — handover for fresh instance with codex available

**Status going in**: run-17 shipped against current code on 2026-04-29. The artifact is at [runs/17/](../runs/17/). KB stem shape and CLAUDE.md cleanliness lifted clearly over run-16; the persistent porter-experience misses (IG fusion, tier yaml volume, factual fabrication, scaffold-internal KB selection, internal-slug citation leakage, bare runtime zerops.yamls) survived unchanged. The architectural fallback that was supposed to close them — refinement (Tranche 4) — did not fire because finalize closed clean with non-blocking notices and the agent declined refinement.

**Charter for run-18**: pick the smallest possible engine intervention that closes the surviving misses deterministically, validate it with codex as a second opinion, prove it on the run-17 frozen corpus before any dogfood. **Hard constraint**: no 2000 LoC plan with 10 accompanying 1k LoC files. Run-17 already paid that bill. Run-18 ships ~130 LoC of net engine change or it fails its own brief.

**Reading order**:

1. §1 — what shipped vs what didn't, with file:line citations
2. §2 — the two findings the run-17 ANALYSIS missed
3. §3 — proposed intervention (the focused fix)
4. §4 — codex triple-confirm axes (the load-bearing reason for this handover)
5. §5 — local replay corpus (skip provision/scaffold/feature on every iteration)
6. §6 — what NOT to do
7. §7 — pointers + open questions

---

## §1. Run-17 closure status

The run-17 artifact at [runs/17/](../runs/17/) is the fixed point we measure run-18 against. No re-run needed; the artifact is enough.

### §1.1 What clearly lifted over run-16

| Surface | Lift | Mechanism that earned it |
|---|---|---|
| KB stems (codebase/*/knowledge-base) | 0/21 author-claim across all three codebases | T2 record-time stem regex refusal in `slot_shape.go::checkCodebaseKB` |
| CLAUDE.md (codebase/*/claude-md) | 3/3 Zerops-free, `/init`-shape | claudemd-author sub-agent + slot-shape refusal forcing re-author |
| IG item count | 5/5/5 — within spec §141 cap of 4–5 | T1 brief embed |
| Citation prose-level | guides named in body prose | T1 brief embed of Citation Map |
| Tier README extracts | 1–2 sentences, ≤10 lines/tier | spec §97-100 conformance |
| Root README | 25 lines, navigation-only | spec §74-83 conformance |

These lifts are real and attributable to specific record-time refusals. They are the **floor** for run-18 — they must not regress.

### §1.2 What did not close

Six categories. Each has at least one concrete file:line citation in the artifact.

1. **IG H3 fusion** — apidev IG #2 fuses bind-0.0.0.0 + trust-proxy ([apidev/README.md:133](../runs/17/apidev/README.md#L133)). workerdev IG #5 fuses Valkey + S3 + Meilisearch wiring with three distinct citation guides ([workerdev/README.md:155](../runs/17/workerdev/README.md#L155)). Spec §770-790 explicitly forbids this and provides the worked example; brief teaching alone didn't transfer.

2. **Tier yaml comment volume** — every tier ships 100–135 indented `#` lines vs spec §142 cap of ≤40. Tier 0 [import.yaml](../runs/17/environments/0%20—%20AI%20Agent/import.yaml) = 136 lines. Same magnitude miss as run-14 and run-16. No structural validator counts indented `#` lines today.

3. **Factual fabrication: JetStream durability** — tier 4 broker block ([import.yaml:93](../runs/17/environments/4%20—%20Small%20Production/import.yaml#L93)): *"that's the signal to move to a tier-5 clustered broker with JetStream durability."* The recipe uses core NATS pub/sub with queue groups; no JetStream subjects, no streams. Zerops's tier-5 broker is core NATS in HA, not JetStream. Identical fabrication shipped in run-14 ([spec-content-surfaces.md §413](../../spec-content-surfaces.md)). Folk-doctrine the spec already names by example.

4. **Scaffold-internal KB bullets** — bullets that document the recipe's own scaffold, not platform traps:
   - apidev KB #5 (`@types/multer` global declaration) — pure TS+npm metadata ([apidev/README.md:202](../runs/17/apidev/README.md#L202))
   - apidev KB #6 ("queue panel renders empty") — recipe-internal UI panel + recipe's chosen architecture ([apidev/README.md:204](../runs/17/apidev/README.md#L204))
   - appdev KB #2 (CORS preflight on `/api/...`) — names recipe's `src/routes/api/[...path]/+server.js` ([appdev/README.md:170](../runs/17/appdev/README.md#L170))
   - appdev KB #3 (panels show "unreachable") — names recipe's `apiBaseUrl()` from `src/lib/api.js` ([appdev/README.md:172](../runs/17/appdev/README.md#L172))
   - appdev KB #5 (custom headers missing) — debug guidance, not a trap; body says "if you see X, look upstream"
   - appdev KB #6 (`/health` returns 200) — defends recipe's design choice ("That's intentional") ([appdev/README.md:178](../runs/17/appdev/README.md#L178))
   - workerdev KB #8 (`run.ports is empty` warning) — operational acknowledgment; doesn't fit Surface 5 shape ([workerdev/README.md:197](../runs/17/workerdev/README.md#L197))

   Spec §200-218 says discard. Synthesis honored the agent's classification (platform-invariant) and shipped them as KB.

5. **Self-inflicted KB shipped as platform trap** — appdev KB #4 *"Self-deploy on the dev setup wipes `/var/www/appdev`, leaving an empty container"* ([appdev/README.md:174](../runs/17/appdev/README.md#L174)). Spec §380 self-inflicted shape: the fix is in [appdev/zerops.yaml:13](../runs/17/appdev/zerops.yaml#L13) (`deployFiles: .` + comment explaining narrowing wipes the mount). A porter who reads the yaml comment never independently narrows. The bullet is the recipe author's debug journal of choices the recipe already made for the porter. Should have been discarded at fact-recording or at synthesis; neither caught it.

6. **Internal `zerops_knowledge` slug citations leaking as porter-facing references** — every guide cite uses the agent-tool slug, not a docs URL. Examples in artifact: *"See: http-support guide"*, *"See: deploy-files guide"*, *"See: env-var-model guide"*, *"see `init-commands`"*, *"cited in the `rolling-deploys` platform topic"*. A porter who searches Zerops docs for `http-support` finds nothing because that's the agent's tool ID, not a published doc slug. Reference recipes cite by inline prose mention without the trailing slug-as-noun pattern. Plus the trailing `See: X guide.` shape itself reads like a leftover authoring marker.

### §1.3 Bare runtime zerops.yaml — structural gap

[apidev/zerops.yaml](../runs/17/apidev/zerops.yaml) and [workerdev/zerops.yaml](../runs/17/workerdev/zerops.yaml) ship with **zero comments**. Spec §283-302 names Surface 7 as the *primary* site for friendly-authority + trade-off teaching. [appdev/zerops.yaml](../runs/17/appdev/zerops.yaml) shows what the bar looks like — block comments above every directive group, every comment teaches a non-obvious choice. The api + worker yamls should match.

The IG #1 yaml block in [apidev/README.md](../runs/17/apidev/README.md) carries inline `# #`-prefixed comments; those don't ship to disk. A porter cloning the apps repo gets bare yaml.

### §1.4 Validator vs spec contradiction

Env-content phase ran a validator `tier-promotion-verb-missing` (TIMELINE §6) that forced "outgrow"/"promote"/"upgrade from tier N to N+1" tokens into env intros and import-comments. Spec §108 explicitly forbids tier-promotion narratives: *"Cross-tier shifts surface implicitly through the contrast between tiers. Don't."*

Three places in the artifact carry validator-forced spec violations:
- env 0 README: *"Promote to tier 1 once a human porter takes over."*
- env 4 README: *"Upgrade from tier 4 to tier 5 when downtime budget shrinks below a manual-restore window."*
- tier 4 broker: *"that's the signal to move to a tier-5 clustered broker..."*

**Action item: pick one.** Either delete the validator (spec wins) or amend spec §108 to permit the verb (validator wins). They cannot both stand. **Codex should weigh in on which way to resolve.**

---

## §2. Findings the run-17 ANALYSIS missed

These two are additive to §1.2 / §1.3 above.

### §2.1 The slug-as-citation leak

The run-17 review caught "guides cited" as a lift. It is — guides are named in body prose, not parentheticalized. But every cite uses the **agent-tool slug** (`http-support`, `init-commands`, `env-var-model`) rather than the **docs URL** the porter would actually navigate to (`docs.zerops.io/...`). The trailing *"See: foo guide."* lines compound the issue: they read like authoring markers the agent forgot to delete.

Both reference recipes cite by inline prose with no agent-tool slug exposure. Run-18 should resolve this — likely by extending the brief to teach docs-URL citation explicitly AND by lexical refusal of trailing `See: <slug> guide.` lines.

### §2.2 The self-inflicted bullet anti-pattern

appdev KB #4 (deployFiles wipes `/var/www`) is the canonical example. The spec §380 test:

> *"Could this observation be summarized as 'our code did X, we fixed it to do Y'? If yes, discard."*

The bullet's body answers yes: the recipe ships `deployFiles: .` with a comment; a porter respecting that ships fine; the trap exists only if someone narrows the field, which the comment tells them not to do. Self-inflicted = discard.

The lexical signature is detectable:
- Body matches `/deployFiles.*\b(narrow|wipe|empty|replace|strip)\b/`
- Body contains `/we (chose|picked|use)\b.*\b(over|instead of|rather than)\b/`
- Body or stem opens with `/^(That'?s intentional|This is correct|Not a problem)/`

These are not subtle. A small validator catches them.

---

## §3. Proposed intervention — the focused fix

**Total surface: ~130 LoC across two files. No new atoms beyond existing T0.5 / T1 work. No new phases. No new sub-agent kinds.**

### §3.1 Single record-fragment-time validator: `validateAuthoringDiscipline`

**File**: [internal/recipe/slot_shape.go](../../../internal/recipe/slot_shape.go)

Five checks. Each refuses on hit and cites the offending substring. Wires through the existing aggregation primitive from T5 so a single record-fragment call surfaces every offender at once.

```
1. Self-inflicted KB body
   Refuse a KB bullet whose body matches:
     - /deployFiles.*\b(narrow|wipe|empty|replace|strip)\b/i
     - /we (chose|picked|use)\b.*\b(over|instead of|rather than)\b/i
     - /^(\s*)(That'?s intentional|This is correct|Not a problem)/
   Refusal cites spec §380.

2. Recipe-internal scaffold reference in KB
   Refuse a KB bullet referencing a path or noun in the recipe's own scaffold:
     - file paths matching /\bsrc\/[a-z][a-z0-9._/-]+\.(ts|js|svelte|tsx)\b/
     - SvelteKit route shapes: /\+page\.svelte|\+server\.js|\+layout\./
     - the /api/[...path] proxy noun
     - UI nouns: /\b(panel|tab|dashboard|widget)\b/i in stem
   Refusal cites spec §210 with redirect-to-IG message.

3. Internal-slug citation
   Refuse any line in IG, KB, zerops.yaml-comments, or import-comments matching:
     - /\bSee: `?[a-z][a-z0-9-]+`? guide\b/  (trailing form)
     - /\b(see|cf|per|cited in)\s+`[a-z][a-z0-9-]+`/ where the slug is in
       the engine's known zerops_knowledge ID set
   Suggested fix in the refusal: convert to inline prose mention without the
   trailing "See:" pattern (spec §216 reference-recipe shape).

4. IG H3 fusion
   For codebase/*/integration-guide:
     - Count " + " or " and " conjunctions in each ### heading; refuse if ≥2
       distinct mechanism-shaped nouns are conjoined (heuristic: ≥2 verb-form
       phrases or ≥2 distinct managed-service hostnames in the heading text).
     - Count distinct managed-service hostnames in the body of one H3; refuse
       if >1 hostname appears in body, with redirect: split into N H3s.
   Closes apidev IG #2 (2 mechanisms) and workerdev IG #5 (3 services).

5. Tier yaml comment line cap
   For env/*/import-comments/*:
     - Count indented lines starting with `#`; refuse total >40 per tier
       or per-service-block >8.
   Refusal cites spec §142.
```

LoC: ~120. Single file. Existing T5 aggregation surfaces all offenders in one round-trip.

### §3.2 Auto-dispatch refinement on finalize-with-notices

**File**: [internal/recipe/handlers.go](../../../internal/recipe/handlers.go) — `complete-phase phase=finalize` handler.

Current behavior: finalize returns `ok:true` with notices; agent chooses whether to enter refinement; in run-17 the agent declined.

New behavior: if finalize closes with `ok:true` AND notices is non-empty, the engine sets next phase to `refinement` automatically. Agent advances; refinement runs over the artifact with the snapshot/restore safety net (T4) — bad refinements revert. **Refinement becomes the deterministic catcher of what the validator can't deterministically express** (factual fabrication, voice density, intersection-vs-quirk routing judgment).

LoC: ~10–15. Single conditional in `handleCompletePhase`.

### §3.3 What this catches

| Run-17 miss | Caught by |
|---|---|
| Self-inflicted KB (deployFiles) | §3.1 check 1 — deterministic |
| Scaffold-internal KB (queue panel, api.ts, /api/[...path], cache-demo panel) | §3.1 check 2 — deterministic |
| Slug-as-citation ("See: http-support guide.") | §3.1 check 3 — deterministic |
| IG H3 fusion (apidev #2, workerdev #5) | §3.1 check 4 — deterministic |
| Tier yaml volume (3× cap miss) | §3.1 check 5 — deterministic |
| JetStream fabrication | §3.2 — refinement reads `managed-services-nats` and reshapes |
| Bare runtime zerops.yaml (api+worker) | §3.2 — refinement scans Surface 7 coverage and Replaces missing comments |

### §3.4 What this deliberately does not do

- Does not introduce new atoms. T0.5 already shipped them (or was supposed to); validator references existing spec sections by number.
- Does not introduce new phases. Refinement exists; we change when it dispatches.
- Does not change facts-recording. The validator runs at record-fragment time, not record-fact.
- Does not delete the `tier-promotion-verb-missing` validator. That contradiction (§1.4) is a separate decision codex should weigh.
- Does not amend the spec. If codex disagrees with any check, the check goes — the spec is authoritative.

---

## §4. Codex triple-confirm axes

The fresh instance should pose these specific questions to codex with the run-17 artifact + this handover loaded as context. Triple-confirm = (a) the misses are real, (b) the proposed validator catches them without false positives, (c) auto-dispatch refinement is the right semantics.

### §4.1 Misses are real (sanity)

> "Read [docs/spec-content-surfaces.md](../../spec-content-surfaces.md) §200-218 (Surface 5) and §283-302 (Surface 7), then [runs/17/apidev/README.md:194-204](../runs/17/apidev/README.md#L194), [runs/17/appdev/README.md:167-181](../runs/17/appdev/README.md#L167), [runs/17/workerdev/README.md:182-198](../runs/17/workerdev/README.md#L182). Identify every KB bullet that fails the spec §192 test ('Would a developer who read the Zerops docs AND the relevant framework docs STILL be surprised by this?'). Independently of run-18-prep.md §1.2."

Goal: confirm the 7 self-inflicted/scaffold-internal bullets identified in §1.2.4. False positives or false negatives both inform validator tuning.

### §4.2 Validator catches without false positives

> "Below is the proposed validator regex set [paste §3.1 verbatim]. Apply each check mentally to the actual KB bullets in [runs/17/apidev/README.md], [runs/17/appdev/README.md], [runs/17/workerdev/README.md]. Report: (a) which bullets fire each check (correctly or incorrectly), (b) which bullets the spec says should be discarded that no check fires on, (c) which legitimate bullets fire any check (false positive)."

Goal: confirm the validator's precision/recall before code lands. The §3.1 regexes are first cuts; codex's adversarial read tunes them.

### §4.3 Auto-dispatch refinement is the right semantics

> "Spec §1 names refinement as post-finalize quality refinement. Run-17 ran finalize, closed with non-blocking notices, then signed off without entering refinement. The proposal is: when finalize closes with notices, engine auto-advances to refinement (rather than agent choice). Snapshot/restore guarantees a bad refinement reverts. Is auto-dispatch the right call vs (a) refinement-always-on, (b) refinement-never-on-by-default, (c) refinement triggered on specific notice categories only? What's the failure mode of each?"

Goal: triangulate the dispatch policy. Auto-on-notice is defensible; refinement-always-on is more aggressive; refinement-by-category is more surgical. Codex's read shapes which one ships.

### §4.4 The validator vs spec contradiction (§1.4)

> "Spec §108 forbids tier-promotion narratives. Validator `tier-promotion-verb-missing` forces them. Three places in the run-17 artifact carry the validator-forced violation. Which side wins — delete the validator (spec wins) or amend spec to permit the verb (validator wins)? Reason from the porter's experience reading the artifact, not from process equity."

Goal: forced choice with codex weighting in. The artifact reads natural with the verbs; spec says don't. Need an outside read.

### §4.5 Slug citation: replace or augment?

> "Spec §216 says 'cite by name'. Run-17 cites by `zerops_knowledge` agent-tool slug ('See: http-support guide'). Reference recipes cite by inline prose without the trailing pattern. Three options: (a) ban trailing 'See: <slug> guide.' lines AND require docs-URL inline form, (b) ban only the trailing form, allow inline slug mention, (c) keep current behavior — slugs are recognizable to porters who use Claude. Which?"

Goal: settle the citation shape contract. The spec is silent on the slug-vs-URL distinction.

---

## §5. Local replay corpus

**Hard skip provision (5 min) + scaffold (11 min) + feature (24 min) = ~40 min/iteration savings.** All inputs to the codebase-content phase are in [runs/17/](../runs/17/) — no container exports needed.

### §5.1 What's on disk

| Artifact | Location | Status |
|---|---|---|
| Plan (services, codebases, tier) | reconstructable from `runs/17/SESSSION_LOGS/main-session.jsonl` `update-plan` calls | extract |
| Facts (51 records) | reconstructable from `main-session.jsonl` `record-fact` + `replace-by-topic` events | extract |
| Codebase src/ trees | [runs/17/{apidev,appdev,workerdev}/src/](../runs/17/) | already present |
| Codebase zerops.yaml | [runs/17/{apidev,appdev,workerdev}/zerops.yaml](../runs/17/) | already present |
| The codebase-content briefs as dispatched | extractable from `main-session.jsonl` `build-subagent-prompt` tool_result responses | extract |
| Each sub-agent's record-fragment outputs | [runs/17/SESSSION_LOGS/subagents/agent-*.jsonl](../runs/17/SESSSION_LOGS/subagents/) — 11 logs | already present |
| Final stitched fragments | [runs/17/](../runs/17/) — READMEs, CLAUDE.mds, zerops.yamls, env import-comments | already present |

256-line main session log + 11 sub-agent logs hold every relevant byte.

### §5.2 Extractor design (~80 LoC, throwaway)

Single Go binary at `cmd/zcp-replay-extract/`:

```
zcp-replay-extract -run docs/zcprecipator3/runs/17 -out replay/

Produces:
  replay/plan.json                       — engine plan from update-plan event
  replay/facts.jsonl                     — concat of every record-fact + replace-by-topic event
  replay/briefs/<codebase>-codebase-content.md  — brief as dispatched, verbatim
  replay/fragments-actual/<codebase>/<fragment-id>.md  — what the agent produced
```

Walks `main-session.jsonl` once + each `subagents/agent-*.jsonl` once. No engine dependency beyond struct types. **Deletable post-run-18.**

### §5.3 Iteration loop (no further new infra)

1. Edit a brief atom OR `slot_shape.go::validateAuthoringDiscipline` OR `handlers.go` finalize policy.
2. Rebuild the codebase-content brief locally: thin wrapper around `BuildCodebaseContentBrief(plan, codebase, parent, facts)` writes to `/tmp/replay/brief-new.md`.
3. Spawn an Agent in the parent session with: the new brief as instructions; read access to `runs/17/<codebase>/` (src + zerops.yaml); read access to `replay/facts.jsonl`; record-fragment MCP tool wired to write to `/tmp/replay/fragments-new/<codebase>/`.
4. Diff `/tmp/replay/fragments-new/<codebase>/` vs `replay/fragments-actual/<codebase>/`. Look for: bullets removed (validator caught self-inflicted), H3s split (validator caught fusion), citation shape changed.
5. Iterate.

Per-iteration cost: 5–10 min for one codebase, ~30 min for all three in parallel.

### §5.4 Validator pre-flight (the "10-minute sanity check")

Before any code commits, run `validateAuthoringDiscipline` (or its standalone test harness) over the actual run-17 fragments on disk and confirm:

- It fires on appdev KB #4 (deployFiles wipes), apidev KB #6 (queue panel), appdev KB #2/3/5/6 (scaffold-internal), apidev KB #5 (multer types), workerdev KB #8.
- It does NOT fire on apidev KB #1/2 (TypeORM deadlock, NATS Authorization Violation), apidev KB #4 (S3 NoSuchBucket), workerdev KB #2/3/4/6/7, appdev KB #1/4/7.
- It catches the slug-trailing form on every "See: foo guide." line in the artifact (count: ~12 across 3 codebase READMEs).
- It catches IG fusion on apidev IG #2 + workerdev IG #5; doesn't fire on the other 13 IG items.
- It catches every tier yaml >40 indented `#` lines (all 6 tiers).

If any expectation fails, regex set tunes before code lands. This is the load-bearing check.

---

## §6. What NOT to do

- **Do not write run-18 as another 10-tranche plan.** Run-17 paid that bill and the artifact shows what teaching-without-enforcement produces. Run-18 is one validator + one finalize-policy change.
- **Do not introduce new sub-agent kinds, new phases, new fact kinds, new fragment IDs.** Every miss in §1.2 has a closure path through existing primitives.
- **Do not add new atoms beyond what exists.** If a check needs explanation, it goes in the validator's refusal message citing spec section, not in a new atom. Spec is authoritative; atoms re-explain spec for the agent's benefit only.
- **Do not delete v2 atom tree in this run.** Run-17 plan T7 (v2 deletion) was conditional on hitting quality bar. Run-17 didn't. Defer until run-18 closure.
- **Do not amend the spec on validator-vs-spec contradictions without codex weigh-in.** The §1.4 tier-promotion contradiction is a forced choice — both directions have cost; codex picks.
- **Do not ship the validator without the §5.4 pre-flight check.** The regex precision/recall is the load-bearing question, not the code shape.
- **Do not run a fresh dogfood until the validator pre-flight + replay loop confirm closure on the run-17 corpus.** Re-running provision is 1 hour of network operations to discover a regex was wrong.

---

## §7. Pointers + open questions

### §7.1 Authoritative references

- **Spec**: [docs/spec-content-surfaces.md](../../spec-content-surfaces.md) — what content belongs on which surface; classification taxonomy; counter-example catalog. Run-18 must not amend without codex weigh-in.
- **Research**: [docs/zcprecipator3/content-research.md](../content-research.md) — empirical floor (laravel-jetstream + laravel-showcase reference recipes); routing decision tree; worked examples.
- **Run-17 artifact**: [docs/zcprecipator3/runs/17/](../runs/17/) — fixed point; every §1.2 miss is citable here.
- **Run-17 timeline**: [docs/zcprecipator3/runs/17/TIMELINE.md](../runs/17/TIMELINE.md) — phase-by-phase narrative; refinement-not-entered note in §7.
- **Run-17 plan + drafts**: [docs/zcprecipator3/plans/run-17-implementation.md](run-17-implementation.md) + [run-17-drafts/](run-17-drafts/) — what was supposed to happen. Drafts include verbatim FAIL/PASS distillations for KB stems and IG one-mechanism — those embedded into briefs but the misses survived because brief-teaching-without-enforcement is decorative.

### §7.2 Open questions for codex

1. **Validator regex precision**: §4.2 above. Do the §3.1 regexes catch everything the spec wants discarded? Any false positives on legitimate platform-invariant traps?
2. **Refinement dispatch policy**: §4.3 above. Auto-on-notice vs always-on vs by-category.
3. **Tier-promotion validator vs spec contradiction**: §4.4 above. Forced choice.
4. **Slug citation contract**: §4.5 above. Trailing form is clearly wrong; inline form is open.
5. **Tier yaml volume cap as hard refusal vs notice**: spec §142 cap is observed in references. Should over-cap be a record-fragment refusal (force re-author) or a finalize-time notice (let refinement reshape)? Bias: refusal — the cap is structural and the agent has been over by 3× across three runs.
6. **Cross-codebase duplication**: apidev KB #1 (TypeORM/init-commands) and workerdev KB #7 (processed_events/init-commands) teach the same execOnce pattern. Spec §415 says one fact lives on one surface. Should run-18 add a cross-codebase dedup validator? Probably not in the same intervention; defer until validator-pre-flight confirms current scope.

### §7.3 Sanity for the fresh instance

You're inheriting:
- A repo with current run-17 code shipped.
- An artifact at [runs/17/](../runs/17/) that's the fixed point you measure proposals against.
- A 256-line main session log + 11 sub-agent logs containing every input the codebase-content phase had.
- A spec + research doc that are authoritative.
- This handover.

**You are not inheriting**:
- Any unfinished engine work. Run-17 closed.
- Any in-flight branches. Branch is `main`, clean.
- Any obligation to defend run-17's plan size. The plan was ambitious; the artifact under-delivered against it; run-18 is the corrective lean intervention.

The work for run-18 is small, focused, and validatable on the frozen run-17 corpus before any new dogfood. If codex pressure-test in §4 surfaces a flaw in the proposal, the proposal changes. The artifact is the ground truth.

---

## §8. Sign-off

This handover is implementation-ready when:

1. The fresh instance reads §1-§7 in order.
2. Codex is consulted on §4.1-§4.5 (5 specific axes) and the responses are filed in `run-18-codex-review.md` next to this doc.
3. The §5.4 validator pre-flight runs against the run-17 corpus on disk and the regex set is tuned to match expected hits/misses.
4. The §3 intervention (validator + auto-dispatch refinement) lands as a single PR. Net LoC ≤200.
5. Replay loop confirms the run-17 corpus, fed through current-engine + new validator + auto-dispatch refinement, produces an artifact that closes the §1.2 misses without regressing the §1.1 lifts.
6. Only after replay-pass: a fresh dogfood (single recipe, against current code) re-validates end-to-end. ANALYSIS.md + grade per §3 of [run-17-implementation.md](run-17-implementation.md) §13.2 template — but this time filed.

If steps 2-5 surface that the proposal is wrong, the proposal changes before any dogfood. The whole point of this handover is that codex catches what we couldn't.
