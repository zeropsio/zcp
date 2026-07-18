# Run-44 validation — G1–G8 substrate-fix dogfood

> **Headline: ITERATE-TO-45.** Reading run-44 as a porter against the
> goldens: the **deliverable surface is roughly comparable to
> run-43** — codebase friendly-authority hits jumped from 1 to ~7 across
> three yamls (G6/F-FRIENDLY pattern broadened); aspirational JWT was
> reintroduced by run-44's author on three surfaces (api KB #3, env/1
> intro, env/2 intro) and **caught + ACTed by refinement-2 — net zero
> aspirational claims shipping**, holding the run-43 closure; tier
> yamls hold 18 friendly-authority adapt-path hits across 6 tiers with
> zero cross-tier deferrals; codebase yamls hold zero cross-surface
> deferrals.
>
> **But six of the eight G1–G8 substrate fixes did not move authoring
> behavior at run-44's surface**, three by audit-emit failure:
>
> 1. **G2 substrate FAILED** — refinement-2 brief carries the
>    "Walk BOTH surfaces — every KB bullet AND every IG H3 item body"
>    instruction ([audit_checklist.md:670](internal/recipe/content/briefs/refinement2/audit_checklist.md#L670)),
>    but the sub-agent emitted **ZERO `S4`/`CODEBASE_IG` findings**.
>    Codex-validated: all 10 findings carry `S5` or `S3` surface;
>    findings #8/#9/#10 are missing-citation but all on `CODEBASE_KB`.
>    Run-44 ships 0/13 IG H3 citations — exact same gap as run-43.
> 2. **G5 substrate FAILED — but the failure mode is render-pipeline
>    divergence, NOT sub-agent noncompliance.** Local
>    [phase_entry.md:47-65](internal/recipe/content/briefs/refinement2/phase_entry.md#L47-L65)
>    requires `classification` on findings with `rewrite-as-symptom` /
>    `reword-conditional` / `add-citation` / `fix-named-constant` /
>    `cross-reference-canonical-surface`. **The rendered brief shown to
>    the run-44 sub-agent does NOT include `classification` in the JSON
>    output schema** (codex-verified at
>    `agent-a900b4fac6504a81b.jsonl` rendered-brief lines 72-91: the
>    "What you produce" schema ends at `suggestedReplacement`, no
>    `classification` key). The local substrate file has the text, but
>    the brief composer rendered a version that lacks the field — the
>    agent literally never saw it in its output schema. Main agent
>    then hit 2 classification rejections at
>    [main-session.jsonl:273,275](docs/zcprecipator3/runs/44/SESSION_LOGS/main-session.jsonl)
>    (down from run-43's 3 — within stochastic floor). **This is the
>    same failure shape as G7, not the same as G2/G6.**
> 3. **G6 substrate FAILED** — audit_checklist.md G6 requires
>    `suggestedReplacement` as a form-(b) markdown link on every
>    missing-citation finding
>    ([audit_checklist.md:733-739](internal/recipe/content/briefs/refinement2/audit_checklist.md#L733-L739)),
>    but the sub-agent emitted **ZERO suggestedReplacement** on
>    findings #8/#9/#10. Main agent then composed wrong-path URLs
>    (`docs.zerops.io/features/rolling-deploys` instead of canonical
>    `docs.zerops.io/features/scaling-ha`), triggered G1's anchored URL
>    rejection, snapshot-reverted four times
>    ([main-session.jsonl:281](docs/zcprecipator3/runs/44/SESSION_LOGS/main-session.jsonl)),
>    then retried with cite-by-name form which worked. **G1 worked
>    as designed but G6's emit failure prevented G1's URL-acceptance
>    path from being exercised cleanly.**
>
> Two more fixes were latent (rule didn't fire, run-44 authoring shape
> didn't trigger):
>
> 4. **G3 not exercised** — named-artifact pattern requires `IG #1
>    ships exposedHeaders: [...] allowlist in a code block`. Run-44's
>    apidev IG #1 yaml does NOT ship `exposedHeaders` (it lives in
>    main.ts only, surfaced in KB #2's fix snippet). The strict
>    G3 condition isn't met — the X-Cache cross-origin bullet
>    (apidev KB #2) ships as intersection, with the recipe-internal
>    stem `Cache demo silently returns null for X-Cache from the
>    SPA` naming the recipe's demo endpoint by name (spec §S5
>    anti-pattern adjacent — "Demonstration panels — one tab per
>    managed service" / scaffold-code naming).
> 5. **G4 not exercised on appdev** — the refinement-2 sub-agent
>    reasoned about kb-ig-duplication on api + worker codebases and
>    concluded they pass the rule, but **didn't walk appdev KB #1 vs
>    appdev IG #3 / KB #2 vs appdev IG #5**. Both pairs are arguably
>    pure-IG echoes with thin adapt-path additions; G4's blocker
>    promotion clause should have fired but the sub-agent never
>    applied the test to appdev.
>
> One was a brief-render gap:
>
> 6. **G7 substrate text NOT in the refinement-1 brief** — derived_rules.md
>    line 62 has the KB-DEFENSIVE-FLOOR bullet; the rendered refinement-1
>    brief jumps from KB6 (line 61) directly to "## Apps-repo zerops.yaml"
>    (line 64) — line 62 renders as `\t\n` (empty). Either a brief-composer
>    bug or the running zcp binary was built before G7 landed
>    (substrate committed 22:40, run started 09:13 next day — ~10.5h
>    cache window). Either way, the rule did not reach run-44.
>
> Only **two of eight G-fixes worked at the surface**:
>
> 7. **G1 (anchored URL acceptance) — works**, demonstrated by the
>    four snapshot/restore reverts that correctly rejected
>    `docs.zerops.io/features/rolling-deploys` and
>    `docs.zerops.io/features/init-commands` (neither in
>    `CitationGuideURL` map). The URL-form citation path wasn't
>    exercised positively because G6 didn't pre-resolve; the agent
>    fell back to cite-by-name which the legacy validator path
>    accepted.
> 8. **G8 (writer-brief KB floor reconciliation) — works**, appdev
>    KB ships 2 bullets (run-43 was 2, golden span is 2–7), zero
>    floor-violation noise.
>
> **The three audit-emit failures split into TWO root causes, not one
> (codex correction)**:
>
> - **G2 + G6** — local brief text says it, rendered brief shows it,
>   sub-agent saw it and did NOT exercise it. Genuine sub-agent
>   brief-following gap (brief text alone doesn't propagate to emit
>   when buried in a paragraph).
> - **G5 + G7** — local substrate file has the text, but the **rendered
>   brief shown to the sub-agent does NOT carry the text**. G5: the
>   `classification` field is missing from the rendered output-schema
>   block. G7: derived_rules.md line 62 (KB-DEFENSIVE-FLOOR) renders
>   as empty in the refinement-1 brief. **Brief-render pipeline
>   bug or composer divergence — not a sub-agent compliance gap.**
>
> The original plan over-collapsed all three into one "sub-agent
> doesn't exercise newly-added schema/scope additions" framing.
> Codex-validated correction: G5/G7 won't be fixed by stronger prose
> in the local substrate — they need brief-render verification.
> G2/G6 won't be fixed by render-verification — they need the audit
> walk restructured so the new instruction has nowhere to hide.
>
> Plus four content observations at the deliverable surface:
>
> (a) **Run-44 author REGRESSED on aspirational JWT** — re-authored
> three aspirational claims at api KB #3 + env/1 intro + env/2 intro,
> exactly the defect class run-43 already closed. Refinement-2
> caught all three + main agent ACTed (1 drop, 2 reword-conditional).
> Net at deliverable: zero aspirational JWT shipping. **Stochastic
> regression closed by audit.**
>
> (b) **Recipe-internal naming creeps into apidev KB** —
> KB #2 stem `Cache demo silently returns null for X-Cache from the
> SPA` names the recipe's demo card by name; KB #4 body cites
> `tryGetClient()` helper by name (spec §S5 anti-pattern: "Helper
> code the recipe authored — describe the principle, not the recipe's
> specific implementation"). Refinement-2 did not flag either.
>
> (c) **appdev KB-IG duplication held weakly** —
> appdev KB #1 stem `Dev container returns "Blocked request. This
> host is not allowed."` repeats IG #3's quoted error AND IG #3's
> shipped `allowedHosts: true` fix; KB #2 repeats IG #5's
> `VITE_API_URL` literal-token teaching. Each adds a one-sentence
> adapt-path. G4's blocker promotion clause never fired because
> refinement-2 didn't walk appdev for kb-ig-duplication.
>
> (d) **KB voice trajectory unchanged from run-43** — 9/10 bullets
> defensive trap-cataloging with platform-anchor; 1 bullet
> half-operational (apidev KB #4 `Apply the same pattern to any new
> cached read paths`). Voice matches showcase golden's defensive
> shape but lacks jetstream-style `### Maintenance Mode` operational
> entries.
>
> **Recommendation: ITERATE-TO-45.** Six substrate fixes (G2, G3, G4,
> G5, G6, G7) need follow-through before another dogfood. G2/G5/G6
> share one root cause — newly-added refinement-2 schema/scope fields
> are present in the brief text but the sub-agent doesn't exercise
> them. Substrate-design weakness: the rendered brief embeds the new
> rule but doesn't restructure the audit walk to enforce it. G3's
> strict-letter condition has substrate-design weakness (the trap can
> live in main.ts not IG #1 yaml). G4 needs an explicit per-codebase
> walk in the audit checklist (don't trust the sub-agent to apply
> the rule to every codebase). G7 needs the brief-render path
> verified end-to-end. The deliverable is **not regressed** at the
> reader-facing surface vs run-43 — three small improvements
> (friendly-authority codebase-yaml count, citation coverage,
> aspirational JWT closure under regression pressure), one small
> regression (recipe-internal naming creep in apidev KB), and four
> persistent gaps (IG citations, X-Cache borderline, appdev KB-IG
> dup, defensive-dominant KB voice).

---

## Per-substrate-fix score

| Fix | Verdict | Surface evidence |
|---|---|---|
| **G1** kb-citation-missing canonical-URL acceptance | **WORKS / NOT-EXERCISED-POSITIVELY** | Validator correctly rejected non-canonical URLs (`docs.zerops.io/features/rolling-deploys`, `docs.zerops.io/features/init-commands`) at [main-session.jsonl:281](docs/zcprecipator3/runs/44/SESSION_LOGS/main-session.jsonl) — 4 distinct snapshot/restore reverts on apidev KB. The anchored host+path match is functioning. But run-44 didn't land any canonical-URL form citation because G6 didn't pre-render — agent fell back to cite-by-name form. URL-acceptance path is wired, just dormant in this run. |
| **G2** missing-citation walks both S5 KB AND S4 IG | **FAILED at audit-emit path** | Codex-validated: refinement-2 emitted 10 findings, **zero S4 / CODEBASE_IG**. Run-44 IG citations: 0/13 across three codebases (apidev IG #2/#3/#4/#5, workerdev IG #3, appdev IG #2/#3/#4/#5 all have docs.zerops.io citation-topic matches; none cite). Same as run-43. Brief includes the G2 text ("every IG H3 item body" appears in the rendered brief); sub-agent didn't walk IG. |
| **G3** self-inflicted Check #1 named-artifact patterns | **NOT EXERCISED — strict-letter substrate-design weakness** | G3 trigger requires `IG #1 ships an exposedHeaders: [...] allowlist in a code block`. Run-44's apidev IG #1 yaml (lines 18-227 of apidev/README.md) does NOT include `exposedHeaders` — the array lives in main.ts only, surfaced in KB #2's fix snippet at line 372. The strict condition isn't met. KB #2 ships with the borderline self-inflicted X-Cache + recipe-internal `Cache demo` stem. Substrate-design weakness: the rule looks for the artifact in IG #1 yaml but real authoring can place it elsewhere. |
| **G4** kb-ig-duplication blocker promotion | **NOT EXERCISED on appdev** | The refinement-2 sub-agent considered the rule for api KB #1 + IG #3 (Authorization Violation pair — passed: KB adds symptom dimension, cross-references IG) and for worker KB #2 + IG #3 (drain pair — passed: KB adds CAUTION block detail). It did NOT walk appdev's KB #1 + IG #3 or KB #2 + IG #5. Both pairs are arguably pure-IG echoes with thin adapt-path additions. G4's per-bullet blocker clause never fired. |
| **G5** classification field on findings | **FAILED at brief-render path (codex correction)** | Local [phase_entry.md:47-65](internal/recipe/content/briefs/refinement2/phase_entry.md#L47-L65) has the rule. But the **rendered brief shown to the run-44 sub-agent does NOT include `classification` in its JSON output schema** — codex-verified at the rendered-brief content in the sub-agent JSONL (output schema ends at `suggestedReplacement`, no `classification` key). The agent never saw the field requirement. Zero findings carry classification; main agent hit 2 classification rejections on retry. Same failure mode as G7 (render-pipeline gap), NOT same as G2/G6 (compliance gap). |
| **G6** suggestedReplacement pre-resolved markdown link | **FAILED at audit-emit path** | Codex-validated: three missing-citation findings (#8/#9/#10) emit `suggestedAction: "add-citation"` with NO `suggestedReplacement` field. Main agent composed wrong-path URLs → G1 rejected → 4 snapshot/restore reverts → agent retried with cite-by-name form. |
| **G7** KB-DEFENSIVE-FLOOR derived rule | **BRIEF-RENDER GAP** | derived_rules.md line 62 contains the rule. The refinement-1 brief renders KB6 (line 61) then jumps directly to "## Apps-repo zerops.yaml" (line 64); line 62 renders as empty (`\t\n`). Rule never reached the sub-agent. Either brief-composer bug or stale embedded binary at run-time. |
| **G8** writer-brief KB floor reconciliation | **WORKS** | appdev KB ships 2 bullets, within spec §S5 "no floor; cap 8" envelope (golden span 2–7). Zero kb-below-floor violation noise at finalize gate. |

---

## Refinement-2 dispatch + findings (verbatim)

Dispatched once at [main-session.jsonl:258](docs/zcprecipator3/runs/44/SESSION_LOGS/main-session.jsonl), brief written to disk (size cap). Sub-agent at [agent-a900b4fac6504a81b.jsonl](docs/zcprecipator3/runs/44/SESSION_LOGS/subagents/agent-a900b4fac6504a81b.jsonl); ~300s wall time. Emitted **10 findings** in a single JSON block, no re-iteration.

**Findings tally:**

| Defect class | Count | Severity | Surfaces | Triage outcome |
|---|---:|---|---|---|
| `aspirational-as-current` | 3 | blocker | S5×1, S3×2 | ACT/drop API KB #3; ACT/reword env/1 intro; ACT/reword env/2 intro |
| `cross-codebase-named-constant-drift` | 1 | blocker | S5 | MOOT after #1 drop |
| `cross-codebase-content-duplication` | 1 | blocker | S5 | ACT/cross-reference worker KB #3 collapses to api-pointer |
| `scaffold-decision-as-gotcha` | 1 | blocker | S5 | ACT/drop worker KB #4 |
| `framework-quirk-as-gotcha` | 1 | blocker | S5 | ACT/drop API KB #5 search-staleness |
| `missing-citation` | 3 | advisory | S5×3 | ACT/add-citation API KB #1 + KB #4; MOOT worker KB #3 (collapsed) |
| `kb-ig-duplication` | **0** | — | — | — |
| `self-inflicted-as-gotcha` | **0** | — | — | — |
| `missing-citation` on S4 IG | **0** | — | — | — |
| **TOTAL** | **10** | — | — | **7 ACTed, 2 MOOT, 1 collapsed** |

**Per-finding triage compliance**: blocker-class findings (8 of 10) received explicit ACTs or MOOT recoveries; advisory missing-citation findings (3) received per-finding ACT decisions rather than category-HOLD (a substrate-progress vs run-43). The wrong-path URL forms attempted at the ACT path triggered G1's anchored rejection (4 reverts on apidev KB), then re-authored with cite-by-name. Net at deliverable surface: 5 of 10 KB bullets ship with cite-by-name citations (run-43 had 3 of 10 with citations).

**Classification field omission** at main-agent ACT path (L273, L275): two KB record-fragments rejected with error `record-fragment: classification is required for fragments on surface "CODEBASE_KB"`. Retries succeeded with `classification: intersection`. **G5 substrate did not propagate to audit-emit path; main-agent compensated.**

**Wrong-path URL rejection** (apidev KB #3, init-commands + rolling-deploys topics): main agent composed `[per-deploy gate](docs.zerops.io/features/init-commands)` + `[zero-downtime overlap](docs.zerops.io/features/rolling-deploys)` markdown links. G1's anchored matcher rejected because neither URL is in `CitationGuideURL` (canonical paths are `docs.zerops.io/zerops-yaml/specification#initcommands-` and `docs.zerops.io/features/scaling-ha`). Snapshot/restore reverted ×4 (one per topic-keyword). Main agent re-authored with `The Zerops \`rolling-deploys\` guide covers…` + `The Zerops \`init-commands\` guide details…` cite-by-name form. **G1 substrate enforced correctly; the rejection happened because G6 didn't pre-resolve the canonical URL for the agent to copy.**

---

## Content audit — three-codebase walk-through

### Per-codebase KB inventory + voice classification

| Codebase | Bullet | Voice | Citation form | Self-inflicted litmus | Notes |
|---|---|---|---|---|---|
| **apidev** (4 bullets) | #1 `Authorization Violation` on first NATS CONNECT | Defensive | cite-by-name `managed-services-nats` ✓ | Intersection (KEEP) | Clean. Same trap as run-43 KB #1; reshaped from "Invalid URL" to literal error string. |
| | #2 Cache demo silently returns null for X-Cache | Defensive + recipe-internal naming | None | **Borderline self-inflicted** | Stem names recipe-internal "Cache demo" feature. The trap (browsers hide custom headers cross-origin) is real platform-anchored intersection. G3's strict trigger doesn't fire — IG #1 doesn't ship `exposedHeaders`. |
| | #3 `relation "items" already exists` on second replica's boot | Defensive | cite-by-name `rolling-deploys` + `init-commands` ✓✓ | Platform-invariant (KEEP) | **Clean platform-invariant teaching**. Two guide citations in cite-by-name form. New in run-44 (run-43 had different shape). |
| | #4 Cache reads `5xx` the entire request path on a Valkey blip | Half-operational + recipe-internal `tryGetClient()` | None | **Borderline scaffold-decision** | Ends with porter-action prose "Apply the same pattern to any new cached read paths". Names recipe-internal helper `tryGetClient()` (spec §S5 anti-pattern). |
| **workerdev** (3 bullets) | #1 Missing queue group crashes exactly-once delivery | Defensive | cite-by-name `rolling-deploys` ✓ | Intersection (KEEP) | Solid teaching. |
| | #2 `sub.unsubscribe()` on SIGTERM drops in-flight messages | Defensive + `> [!CAUTION]` callout | cite-by-name `rolling-deploys` ✓ | Intersection (KEEP) | Includes jetstream-style CAUTION callout. |
| | #3 `NatsError: Authorization Violation` on first CONNECT | Symptom-first cross-reference | Cross-ref to api KB (citation lives there) | Cross-ref (KEEP) | Post-refinement-2 collapse to pointer + symptom dim. Run-43 was purer pointer. |
| **appdev** (2 bullets) | #1 Dev container returns `Blocked request. This host is not allowed.` | Defensive | None | Intersection (KEEP) | **Arguable pure-IG echo of IG #3** — same quoted error, same `allowedHosts: true` fix. Adds adapt-path "If you tighten the list…". G4 didn't fire. |
| | #2 `fetch()` requests the literal string `${API_URL}` after deploy | Defensive | None | Intersection (KEEP) | **Arguable pure-IG echo of IG #5** — same VITE_API_URL build-time inlining teaching. Adds "set the project env first; redeploy the SPA second" ordering hint. G4 didn't fire. |

**Voice tally**: 9/10 defensive trap-cataloging, 1/10 half-operational (apidev KB #4). **Voice trajectory unchanged from run-43.** Matches showcase golden's defensive shape (showcase ships 7 H3 bullets, all symptom-first defensive); lacks jetstream golden's operational entries.

**Citation tally**: 5/10 with cite-by-name citations (apidev #1 NATS, apidev #3 rolling-deploys + init-commands, worker #1 + #2 rolling-deploys); 5/10 with no citation (apidev #2, #4; worker #3 via cross-reference; appdev #1, #2). **Improved from run-43 (3/10 with citations).** G1's URL-acceptance path remains unexercised; G6's failure to pre-render replacements kept the agent on cite-by-name form.

**Comparison vs goldens**:
- Jetstream KB: 2 operational entries (`### Maintenance Mode`, `### Temporary Upscaling`). Both teach porter commands.
- Showcase KB: 7 defensive bullets in `### Gotchas`, all symptom-first with platform-mechanism anchors.
- Run-44 KB: 9 defensive + 1 half-operational across 3 codebases — matches showcase shape, doesn't match jetstream shape.

### Per-codebase friendly-authority adaptation hint inventory (codebase zerops.yaml)

| Codebase | Hits | Citations |
|---|---:|---|
| apidev | 2 | L60: *"the static-key option is `zsc execOnce bootstrap-seed ...` if you want once-per-lifetime semantics for a non-idempotent seed"*; L140: *"Promote to `npm ci` if you want reproducible dev builds matching prod"* |
| workerdev | 1 | L72: *"If you later add a metrics or admin endpoint, declare a port here and add a readiness check at the same time"* |
| appdev | 4 | L22: *"bump this if you change Vite's port"*; L32-33: *"Swap to a foreground `npm run dev` if you'd rather the dev server start on container boot"*; L57: *"rotate it (then redeploy) for a custom domain swap"*; plus implicit |

**Total: ~7 friendly-authority hits across 3 codebase yamls in run-44** (run-43 had 1, only in apidev). **Material improvement** — the substrate's F6 / F-FRIENDLY-AUTH pattern has finally landed on appdev + workerdev surfaces where run-43 had nothing to fire on. Distribution now spread across all three codebases.

### Tier import.yaml friendly-authority inventory

| Tier | Hits | Sample shapes |
|---|---:|---|
| 0 AI Agent | 2 | "bump it when uploads outgrow the current quota"; "bump verticalAutoscaling.minRam if your index size grows past the 0.25 GB ceiling" |
| 1 Remote (CDE) | 1 | "bump it when uploads outgrow the current quota" |
| 2 Local | 3 | "disable it once you have a custom domain configured"; "bump verticalAutoscaling.minRam if dataset growth pushes latency past your local-loop SLO"; "Bump verticalAutoscaling.minRam if index size pushes query latency past your local SLO" |
| 3 Stage | 4 | "disable it once you have a stage domain configured"; "Bump verticalAutoscaling.minRam when QA loads push working-set size past the floor"; "Bump `objectStorageSize` when rehearsal uploads outgrow the current quota"; "Bump verticalAutoscaling.minRam if stage search latency correlates with index growth" |
| 4 Small Production | 4 | "Bump verticalAutoscaling.maxRam when monitoring shows containers approaching the current ceiling"; "Bump verticalAutoscaling.minRam if production hit rates drop"; "Bump `objectStorageSize` when production uploads outgrow"; "Bump verticalAutoscaling.minRam if production search latency correlates" |
| 5 HA Production | 4 | "Bump verticalAutoscaling.maxRam when monitoring shows containers approaching"; "Bump verticalAutoscaling.minRam if working-set growth pushes latency past your SLO"; "Bump verticalAutoscaling.minRam if production hit rates drop"; "Bump verticalAutoscaling.minRam if production search latency correlates" |

**Tier yaml friendly-authority total: 18 adaptation hints across 6 tiers — run-43 had 15+, holds.**

### Yaml comment cross-surface deferrals (codebase zerops.yaml)

Walked apidev + appdev + workerdev zerops.yamls for "see IG #N", "live below", "the pattern is taught in", "see KB", "for the rationale see":

- apidev: zero hits.
- appdev: zero hits.
- workerdev: zero hits.

**F-XSURF-REF held at run-44.** All comments state mechanism+reason in one breath. Note: the README's IG #3 prose (line 277) does contain "(see IG #1)" as an intra-IG cross-reference, which is acceptable per spec §"Cross-surface references are fine; cross-surface duplication is not."

### Tier import.yaml cross-tier deferrals

Walked all 6 tier import.yamls for "Same as tier N", "Same X as tier N", "as the previous tier", "same shape as tier", "see tier N", "promote to":

- Tiers 0–5: zero hits each.

**F5 cross-tier deferrals stay closed.** Each tier's service blocks carry self-contained per-tier rationale. Tier 3 README has the comparative "Stage environment uses the same shape as production but runs at the lowest scaling settings" — this is the tier-defining preamble pattern (matches jetstream's "Stage environment uses the same configuration as production" shape) and is positively comparative rather than cross-tier deferral.

### execOnce semantic match audit

| zerops.yaml | execOnce line | Key shape | Comment claim | Verdict |
|---|---|---|---|---|
| apidev prod | `zsc execOnce ${appVersionId} -- node dist/migrate.js` + `… dist/seed.js` | `${appVersionId}` (per-deploy) | "the platform gates each command per deploy version across all replicas, so the migrate + seed scripts run exactly once per code version no matter how many containers boot in parallel" + adapt-path "the static-key option is `zsc execOnce bootstrap-seed ...` if you want once-per-lifetime semantics" | ✓ Semantic match + friendly-authority adapt-path |
| apidev dev | Same shape with ts-node | Same | "Migrate + seed via ts-node so the dev container runs straight from src/ without a build step. Same execOnce gating semantics as prod — runs once per code version across replicas." | ✓ Semantic match |
| workerdev | (no initCommands) | n/a | n/a | n/a |
| appdev | (no initCommands) | n/a | n/a | n/a |

**F-EXECONCE-SEMANTICS / P3: no mismatch authored in run-44.** The dev/prod pair both correctly describe the per-deploy gate. Apidev prod yaml gained a friendly-authority extension naming the static-key alternative.

### Citation URL audit per KB + IG bullet

**KB citations (run-44 final state)**:

| Surface | Bullet | Citation form | Topic |
|---|---|---|---|
| apidev KB #1 | NATS CONNECT | `The Zerops \`managed-services-nats\` guide covers` (cite-by-name) | managed broker |
| apidev KB #2 | X-Cache cross-origin | NONE | (intersection — no topic-map hit?) |
| apidev KB #3 | relation already exists | `The Zerops \`rolling-deploys\` guide covers…` + `the Zerops \`init-commands\` guide details…` (cite-by-name ×2) | rolling-deploys + init-commands |
| apidev KB #4 | Cache 5xx Valkey blip | NONE | Valkey (no citation-map row?) |
| worker KB #1 | queue group | `The Zerops \`rolling-deploys\` guide covers` (cite-by-name) | rolling-deploys |
| worker KB #2 | drain on SIGTERM | `The Zerops \`rolling-deploys\` guide covers` (cite-by-name) | rolling-deploys |
| worker KB #3 | NatsError (collapsed) | Cross-ref to api KB | managed broker |
| appdev KB #1 | Blocked request | NONE | http-support / subdomain |
| appdev KB #2 | Literal ${API_URL} | NONE | env-var-model |

**KB citation coverage: 5/9 effective bullets (worker KB #3 cross-refs).** Run-43 had 3/10. **Material improvement on KB citation.**

**IG citations (run-44 final state)**:

| Surface | Item | Topic | Citation |
|---|---|---|---|
| apidev IG #2 | Trust proxy | http-support | NONE |
| apidev IG #3 | NATS Pattern A | managed broker / env-var-model | NONE |
| apidev IG #4 | forcePathStyle | object-storage | NONE |
| apidev IG #5 | CORS via project envs | env-var-model | NONE |
| worker IG #2 | Standalone context | (framework topic — nestjs.com link) | NestJS docs only |
| worker IG #3 | Drain on SIGTERM | rolling-deploys | NONE |
| appdev IG #2 | Bind 0.0.0.0 | http-support | NONE |
| appdev IG #3 | allowedHosts | http-support / subdomain | NONE |
| appdev IG #4 | dist/~ tilde-strip | deploy-files | NONE |
| appdev IG #5 | Project-scope env | env-var-model | NONE |

**IG citation coverage: 0/10 zerops-docs citations.** Run-43 was 0/12. **G2 substrate FAILED — refinement-2 sub-agent didn't walk IG H3 items for missing-citation.** The brief instruction "every IG H3 item body" was present; the sub-agent didn't follow it.

### Self-inflicted bullet inventory (apply porter-following-IG#1 test)

| Bullet | IG #1 ships | Trap fires when porter… | Verdict |
|---|---|---|---|
| apidev #1 NATS Auth Violation | NATS Pattern A separate vars | …reaches for `${broker_connectionString}` | Intersection (KEEP) |
| apidev #2 X-Cache cross-origin | CORS_ORIGINS (no exposedHeaders array in yaml) | …ships custom response headers without `exposedHeaders` allow-list | **Borderline self-inflicted** — recipe stamps X-Cache (recipe-internal), but the underlying cross-origin trap is real platform-anchored intersection. G3's strict letter doesn't fire — exposedHeaders isn't in IG #1 yaml. |
| apidev #3 relation already exists | execOnce-gated migrate + seed | …runs migrations at container boot without execOnce | Platform-invariant (KEEP) |
| apidev #4 Cache 5xx Valkey blip | (yaml doesn't address this) | …throws on Valkey transient instead of fall-through | **Borderline scaffold-decision** — names recipe-internal `tryGetClient()` helper; teaches a generic resilience pattern. |
| worker #1 queue group | (yaml doesn't address subscribe call) | …subscribes without queue group at minContainers≥2 | Intersection (KEEP) |
| worker #2 drain | (yaml doesn't address it) | …unsubscribes instead of draining on SIGTERM | Intersection (KEEP) |
| worker #3 NatsError (cross-ref) | Pattern A | …uses connection-string URL | Intersection (KEEP) |
| appdev #1 Blocked | httpSupport: true | …uses default Vite allowedHosts | Intersection (KEEP) — but IG-echo of IG #3 |
| appdev #2 ${API_URL} literal | `VITE_API_URL: ${API_URL}` in build | …points VITE_API_URL at peer subdomain alias | Intersection (KEEP) — but IG-echo of IG #5 |

**Net**: 2 borderline (apidev #2 X-Cache, apidev #4 Cache 5xx); 0 clear self-inflicted. G3 substrate's strict letter didn't fire on either because both reference artifacts (`exposedHeaders`, `tryGetClient`) live outside IG #1 yaml. The audit emitted zero self-inflicted findings — defensible reasoning visible in transcript ("Zerops's subdomain shape forces cross-origin → intersection"), but the spec litmus #4 ("Could this observation be summarized as 'our code did X, we fixed it to do Y'?") would route the Cache demo / tryGetClient bullets to DISCARD.

### Cross-codebase content duplication

- worker KB #3 IS the cross-reference to api KB (post-refinement-2 collapse). **Substrate finding #5 ACTed cleanly.**
- No other cross-codebase duplications visible.

### Aspirational-as-current

**Run-44 author REGRESSED on aspirational JWT** — re-authored:
- api KB #3 "Worker rejects api-minted JWTs as invalid" — claimed the worker validates tokens. Worker has zero JWT code. **Caught + ACTed/dropped.**
- env/1 intro "JWT_SECRET is generated once at import and shared across api + worker so token verification works" — "so token verification works" clause. **Caught + ACT/reword to** *"so any future verifier (worker, sidecar, etc.) can validate api-minted tokens with the same key"*.
- env/2 intro — identical claim. **Caught + ACT/reword to** *"so any future verifier can validate api-minted tokens with the same key"*.

**Tier 5 + tier 0 + tier 3 + tier 4 retained run-43's already-clean conditional shape.** Aspirational claims surviving to deliverable: **zero.** Stochastic regression closed by audit.

### Surface placement audit

| Surface | Content check | Verdict |
|---|---|---|
| S1 Root README (27 lines) | Tier links, deploy buttons, no narrative | ✓ within cap |
| S2 Tier README extracts | All 6 tiers ship 1-2 sentence extracts | ✓ |
| S3 Tier import.yaml comments | Self-contained per-tier rationale, 18 friendly-authority hits | ✓ F5/F6 |
| S4 IG | api=5, app=5, worker=3 items per codebase (api/app at cap; worker under = legitimate sub-feature scope) | ✓ counts; **0/10 citations** |
| S5 KB | api=4, worker=3, app=2 (under cap 8; F2/G8 floor removed; goldens span 2-7) | ✓ counts; 5/9 cite-by-name; voice mostly defensive |
| S6 CLAUDE.md | api 30 lines, worker 22 lines, app 33 lines; zero `zsc`/`zerops_*`/`zcli` tokens | ✓ |
| S7 zerops.yaml comments | Self-contained, mechanism+reason; **~7 friendly-authority hits** | ✓ structural + F6 substantive improvement |

---

## Spec-content audit — surface-by-surface

| Section | Verdict | Notes |
|---|---|---|
| §"Empirical floor" (goldens) | **Tier yamls match jetstream/showcase shape; codebase yamls match jetstream voice (now); KB matches showcase defensive shape, lacks jetstream operational entries** | F6 substrate finally landed across all 3 codebase yamls. |
| §"Why this exists — journal failure mode" | **Partial** | Cross-surface deferrals zero, fabricated mechanisms zero, but recipe-internal naming (`Cache demo`, `tryGetClient()`) creeps into apidev KB. |
| §"Fact classification taxonomy" | **Audit-emit honored; main-agent retry still bites** | Authoring sub-agents emit classification cleanly; **refinement-2 sub-agent does NOT** (G5 substrate failed); main agent compensates with retry. |
| §"Self-inflicted" litmus #4 | **Partial** | Two borderline bullets (apidev KB #2 X-Cache stem names "Cache demo"; KB #4 names `tryGetClient()` helper) ship as intersection with platform-anchor narrative. Per spec litmus would route to DISCARD; per audit's narrowed Check #1 + G3 didn't fire. |
| §"Friendly-authority voice" | **Codebase yamls ✓ (7 hits) / Tier yamls ✓ (18 hits)** | F6 landed cleanly on previously-empty appdev + workerdev surfaces. |
| §"Surface 5" editorial test | **Mostly pass — IG-echo borderlines persist** | appdev KB #1 + #2 thin-adapt-path IG echoes; G4 didn't fire. |
| §"Surface 7" (yaml comments) | **✓** | Mechanism+reason in one breath; no cross-surface deferrals; friendly-authority adapt-paths on porter-tunable directives. |
| §"Surface 3" (tier yaml) | **✓** | No cross-tier deferrals; 18 friendly-authority hits across 6 tiers. |
| §"Citation map" — KB | **Improved 3/10 → 5/9 cite-by-name** | G6 emit-failure prevented G1 URL-form path from being exercised; cite-by-name held. |
| §"Citation map" — IG | **0/10 — unchanged from run-43** | **G2 substrate failed at emit path.** |

---

## Golden voice alignment

### KB shape comparison

**Jetstream KB** (2 H3 bullets): both operational — `### Maintenance Mode` teaches `php artisan down` workflow with `> [!CAUTION]` + fenced shell block; `### Temporary Upscaling` teaches `zsc scale ram +0.5GB 10m`. Porter takes action.

**Showcase KB** (7 H3 bullets): all defensive symptom-first with platform-mechanism anchors. `### Gotchas` H3 (parent) + bullet-list children (`No .env file`, `Cache commands in initCommands`, `APP_KEY is project-level`, etc.).

**Run-44 KB** (9 distinct H3 bullets across 3 codebases): 9/10 defensive, 1/10 half-operational. Shape matches showcase's defensive-platform-anchor pattern. Voice does NOT match jetstream's operational entry pattern.

**Verdict**: Run-44 KB is shape-aligned with showcase, voice-aligned with showcase. The "operational" gap with jetstream persists — no `### Maintenance Mode`-style entry in any codebase KB. Whether this is a defect or a deliberate scope choice depends on framing: run-44 doesn't ship an obvious operational concern in NestJS land (no down/up sequence equivalent), so the absence may be legitimate scope. G7 substrate would have flagged a fully-defensive KB; with porter-action prose in at least one bullet per codebase, G7 wouldn't have fired anyway.

### Yaml comment voice comparison vs jetstream

**Jetstream zerops.yaml** has 3+ friendly-authority hits in a single yaml — custom-domain, SMTP, port-25.

**Run-44 codebase zerops.yamls** have 7 hits across 3 yamls — distributed (`Promote to`, `bootstrap-seed if you want`, `If you later add`, `bump this if you change`, `Swap to`, `rotate it for a custom domain swap`). Mathematically less per-yaml than jetstream (where one yaml carries 3+) but **shape-aligned** with jetstream's "declarative statement + adapt invitation" pattern.

**Verdict**: F6 substrate has landed on the codebase-yaml surface across all 3 codebases. Run-43 had 1 hit total; run-44 has 7. **Substantive improvement attributable to substrate.**

---

## Content quality progression vs run-41/42/43

### apidev KB stem progression

| Run | Bullets | Notable shape |
|---|---:|---|
| **41** | 6 | NATS Auth Violation + Object-storage 403/virtual-host + redis://user:pass fails + Meili http vs https + literal CORS + X-Cache cross-origin |
| **42** | 5 | NATS Invalid URL + Object-storage **UnknownError on first read** (self-inflicted) + X-Cache cross-origin + CORS literal subdomain tokens + NATS publish-drop |
| **43** | 5 | NATS Invalid URL + `forcePathStyle: 403` + Cross-origin SPA headers + Valkey no user/password aliases + Meilisearch master key |
| **44** | 4 | `Authorization Violation` NATS + Cache demo X-Cache (recipe-internal stem) + relation already exists (NEW clean platform-invariant) + Cache 5xx Valkey blip (recipe-internal `tryGetClient`) |

**Run-44 progression**: Dropped Valkey no-user/pass (run-43 borderline self-inflicted); dropped forcePathStyle (moved to IG #4 only); dropped Meilisearch master-key (refinement-2 framework-quirk drop); ADDED `relation already exists` (clean platform-invariant teaching with two citations); ADDED Cache 5xx Valkey blip (mixed quality with recipe-internal helper naming); kept Cache demo X-Cache (run-43's borderline survivor) with regressed stem naming "Cache demo".

### workerdev KB stem progression

| Run | Bullets | Notable shape |
|---|---:|---|
| **41** | 5 | queue option + drain + TypeORM sync + NATS URL crashes + log buffer |
| **42** | 4 | queue-group + drain + NATS Invalid URL + same-key shadow |
| **43** | 3 | queue-group + drain + cross-ref to api KB |
| **44** | 3 | queue group + drain + NatsError (post-refinement cross-ref + symptom dim) |

**Run-44 worker KB**: same 3-bullet structure as run-43, but KB #3 has symptom-first stem (`NatsError: Authorization Violation`) AND cross-references api. Slightly more verbose than run-43's cleaner pointer.

### appdev KB stem progression

| Run | Bullets | Notable shape |
|---|---:|---|
| **41** | 4 | VITE_API_URL not configured + Vite blocked-host + base:static 404 + Tailwind CDN |
| **42** | 4 | Dev server Blocked + VITE_API_URL literal + SPA 404 base:static + vue-tsc not found |
| **43** | 2 | Dev preview Blocked + ${apistage_zeropsSubdomain} literal |
| **44** | 2 | Dev container Blocked + ${API_URL} literal (both arguable IG-echoes) |

**Run-44 appdev KB**: same 2-bullet structure as run-43. Both bullets are arguable IG echoes with one-sentence adapt-path additions. G4's blocker-promotion clause should have fired; didn't because refinement-2 sub-agent didn't walk appdev for kb-ig-duplication.

### Citation coverage progression

| Run | apidev KB citations | workerdev KB citations | appdev KB citations | IG citations (all 3) |
|---|:---:|:---:|:---:|:---:|
| **40** | 0 of 7 | 0 of 5 | 0 of 4 | 0 |
| **41** | 0 of 6 | 0 of 5 | 0 of 4 | 0 |
| **42** | 4 of 5 | 2 of 4 | 3 of 4 | 0 |
| **43** | 1 of 5 (wrong-path) | 2 of 3 (wrong-path) | 0 of 2 | 0 |
| **44** | 2 of 4 (cite-by-name) | 2 of 3 (cite-by-name) | 0 of 2 | **0 — G2 failed** |

**Run-44 KB citation progression**: 4 of 9 effective bullets carry citations (counting worker KB #3 as cross-ref); **net improvement vs run-43** (3 of 10). All citations are cite-by-name form (the URL form path wasn't exercised because G6 didn't pre-render).

**IG citation: 0 of 10 across three codebases. UNCHANGED from runs 40/41/42/43.** G2 substrate text reached the brief but the sub-agent didn't walk IG. This is the most material substrate failure of run-44.

### What "the substrate caught" looks like across runs (content lens)

- **Run-42**: 17 findings → 17 ACTs. Caught 6 aspirational JWT + 2 cross-cb dup + 1 framework-quirk + 1 scaffold-decision + 6 missing-citation. **Missed**: self-inflicted UnknownError, X-Cache cross-origin, semantic-lie execOnce, cross-surface deferrals, cross-tier deferrals.
- **Run-43**: 22 findings → 11 ACTed + 11 HELD (10 missing-citation category-held). Caught aspirational + cross-cb dup + scaffold-decision + framework-quirk + wrong-path URL. **Missed**: 2 borderline self-inflicted, 2 KB-IG dup advisories, **12 IG citations (scope gap)**.
- **Run-44**: 10 findings → 7 ACTed + 2 MOOT + 1 collapsed. Caught reintroduced aspirational JWT + named-constant drift + cross-cb dup + scaffold-decision + framework-quirk + 3 KB missing-citation. **Missed**: 10 IG missing-citations (G2 emit-failure), borderline self-inflicted X-Cache + tryGetClient (G3 substrate-design weakness + recipe-internal naming), appdev KB-IG dups (G4 not exercised).

**Run-44 catch rate is comparable to run-43.** The new substrate rules didn't expand the catch surface because they didn't fire. What's not caught is the same frontier: IG citations, self-inflicted with thin-but-real platform anchor, appdev KB-IG dup with thin adapt-path, recipe-internal naming creep.

### Bottom-line content quality vs run-43

Reading run-44 as a porter: apidev README gained one clean platform-invariant teaching (`relation "items" already exists`) with two citations; lost one borderline self-inflicted (Valkey no user/pass) but the surviving X-Cache bullet regressed to name the recipe's "Cache demo" feature; added a Cache 5xx bullet that names recipe-internal `tryGetClient()`. workerdev held shape with one minor expansion (KB #3 now has symptom-first stem + cross-ref). appdev held shape with same 2 bullets. **Codebase zerops.yamls gained 6 friendly-authority adapt-paths** — the biggest substrate-attributable surface improvement. Tier yamls held the run-43 shape. CLAUDE.mds clean.

**On the content-quality axes the run-43 validation flagged**:
- IG citations (run-43 gap, G2 target): **NOT CLOSED.** 0/10 still.
- F3 substrate-internal contradiction (G1 target): **CLOSED for URL form, but G6 emit-failure prevented URL form from shipping.** Cite-by-name form held.
- Borderline self-inflicted (G3 target): **NOT CLOSED.** Different bullets, same defensive-with-thin-platform-anchor shape.
- KB-IG advisory HOLDs (G4 target): **NOT CLOSED.** Different codebase, same shape.
- KB voice operational shift (G7 target): **substrate didn't reach the brief.** Voice unchanged.

**Run-44 represents substrate-attributable improvement on codebase friendly-authority hits, no other axis.** The deliverable is at quality parity with run-43, with different specific defects.

---

## Substrate operations

### Refinement-class sub-agent dispatch count (F7 / Edit D test)

| Dispatch | Run-40 | Run-41 | Run-42 | Run-43 | Run-44 |
|---|---:|---:|---:|---:|---:|
| refinement-1 | 1 | 1 | 2 (incl. rulewalk) | 1 | **1** |
| refinement-2 | 0 | 1 | 1 | 1 | **1** |
| refinement-rulewalk | 0 | 0 | 1 | 0 | **0** |
| Total refinement-class | 1 | 2 | 4 | 2 | **2** |

**F7 + Edit D state-machine consolidation holds.** Run-44 ships exactly one refinement-1 + one refinement-2 dispatch — same as run-43.

### Phase ordering trace

```
L27   complete-phase research → ok
L66   complete-phase provision → ok
L108  complete-phase scaffold → ok
L140  complete-phase feature → ok
L204  complete-phase codebase-content → ok
L230  complete-phase finalize → ok
...   build-subagent-prompt refinement-1 + dispatch
...   build-subagent-prompt refinement-2 + dispatch
L273  record-fragment codebase/api/knowledge-base WITHOUT classification → REJECTED
L275  record-fragment codebase/worker/knowledge-base WITHOUT classification → REJECTED
...   record-fragment retries with classification=intersection → ok
L281  record-fragment codebase/api/knowledge-base URL-form citation → 4× refinement-replace-reverted
...   record-fragment cite-by-name form → ok
...   stitch-content × 2
L302  complete-phase refinement → ok
```

**Edit D phase ordering held.** complete-phase finalize closed cleanly; refinement happened at phase=refinement. Refinement-close re-ran validators (the 4 URL-form reverts at L281 prove the validators executed).

### Classification field omission (B-1 / G5 target)

| Surface | Authoring sub-agent rejections | Refinement-2 sub-agent omissions | Main-agent rejections |
|---|---:|---:|---:|
| CODEBASE_KB | 0 | **10 of 10 findings emitted without classification (G5 substrate failed)** | **2** at L273/275 |
| CODEBASE_IG | 0 | n/a (zero IG findings emitted) | 0 |

**G5 substrate FAILED at the audit-emit path.** Run-43 had 3 main-agent rejections; run-44 has 2 — within model variance, NOT a substrate effect. The main agent compensated with classification-on-retry; the substrate fix to emit classification ON the finding (so main agent copies verbatim) didn't propagate.

### Refinement-close gate execution evidence

`main-session.jsonl:281` carries four `refinement-replace-reverted` notices on apidev KB URL-form citation attempts:
- "post-replace validator surfaced kb-citation-missing … KB mentions \"appVersionId\" but does not cite \`zerops_knowledge\` guide \"init-commands\""
- "post-replace validator surfaced kb-citation-missing … KB mentions \"minContainers\" but does not cite \`zerops_knowledge\` guide \"rolling-deploys\""
- "post-replace validator surfaced kb-citation-missing … KB mentions \"execOnce\" but does not cite \`zerops_knowledge\` guide \"init-commands\""
- "post-replace validator surfaced kb-citation-missing … KB mentions \"migrations\" but does not cite \`zerops_knowledge\` guide \"init-commands\""

**G1's anchored URL matcher functioning correctly.** Agent used `docs.zerops.io/features/rolling-deploys` (not in `CitationGuideURL`; canonical is `docs.zerops.io/features/scaling-ha`) and `docs.zerops.io/features/init-commands` (canonical is `docs.zerops.io/zerops-yaml/specification#initcommands-`). Both URLs are anchored-prefix-mismatched, so validator correctly rejected. The snapshot/restore reverted to pre-refinement body; the agent retried with cite-by-name form which the legacy validator path accepts on the bare guide-id substring.

### plan.json finalize-snapshot diff spot-check

Did not run bytewise comparison; the TIMELINE reports no `refinement-replace-reverted` notices on `complete-phase phase=refinement` and the deliverable trees on disk are consistent with TIMELINE's claim — no obvious snapshot/restore inconsistency.

### Sub-agent durations

Run-44 durations are roughly comparable to run-43:
- refinement-1: ~25 min (run-43: 1129s ≈ 19 min — slight increase)
- refinement-2: ~5 min (run-43: 387s ≈ 6.5 min — slight decrease)
- codebase-content × 3: 28/34/36 min (run-43 was similar)
- features-backend: 22 min
- features-frontend: 10 min (much faster than run-42's 25 min)

Refinement-2's drop from 387s → 300s is within variance. No N-1 regression.

### Parent-recipe fetch at research

Did not directly verify; substrate-endorsed per `phase_entry/research.md`. Not flagged.

### features-frontend completeness (B-2)

frontend sub-agent ran ~10 min, browser-walked every tab with screenshots under `screenshots/`. No silent self-stop visible in TIMELINE. **B-2 not biting in run-44.**

---

## Known-substrate-issues confirmed still present

- **G2 substrate failed at audit-emit path** — refinement-2 sub-agent does not walk IG H3 items for missing-citation despite the brief instruction. IG citation coverage: 0/10. Fix candidate: restructure the audit checklist to make the IG-walk an explicit numbered step the sub-agent can't skip (currently the brief says "every IG H3 item body" embedded in a paragraph; the sub-agent's mental walk seems to default to KB-only).

- **G5 substrate failed at audit-emit path** — refinement-2 sub-agent does not populate `classification` on findings despite the brief making the field REQUIRED on five suggestedAction types. Fix candidate: same as G2 — make field population explicit in the audit's emission pattern, not buried in a long paragraph.

- **G6 substrate failed at audit-emit path** — refinement-2 sub-agent does not populate `suggestedReplacement` on missing-citation findings despite the brief making it canonical. Fix candidate: same as G2 — make the field population an explicit step. Or: have the engine pre-render `suggestedReplacement` server-side when the audit is rendered to the agent (engine-derived field rather than sub-agent-emitted field).

- **G7 substrate text NOT in the rendered refinement-1 brief** — derived_rules.md line 62 has the KB-DEFENSIVE-FLOOR bullet; the rendered brief skips it. Either the brief composer is filtering content (unlikely from the structure) or the running binary's embedded brief was compiled before G7 landed. Fix candidate: verify brief-render path end-to-end; consider explicit content tests pinning derived_rules.md bullets to brief output.

- **G3 substrate-design weakness** — the named-artifact pattern requires the artifact to live in IG #1 yaml; real authoring (run-44) places it in main.ts code. The X-Cache cross-origin bullet's "borderline self-inflicted" classification depends on litmus judgment, not on the IG #1 yaml content. Fix candidate: broaden G3's pattern-match to consider artifacts surfaced anywhere in IG code blocks OR in KB fix snippets (not just IG #1 yaml).

- **G4 substrate not exercised on all codebases** — sub-agent walked api + worker for kb-ig-duplication but skipped appdev. Fix candidate: explicit per-codebase walk in the audit checklist ("For each of {api, worker, app}, run the kb-ig-duplication check") rather than rely on the sub-agent to generalize across codebases.

- **Recipe-internal naming creeping into apidev KB** — Cache demo / tryGetClient. Per spec §S5 anti-pattern. Not directly a G-fix target; would need a new refinement-2 rule. Fix candidate: explicit pattern-list of "recipe-internal feature names" the audit scans for (Cache demo, queue panel, ItemsCard, tryGetClient, etc.) — over-fires risk, but the litmus is clear when the bullet's stem names a recipe-specific endpoint.

---

## Run-45 substrate design — first principles (replaces P1-P5)

**The user's redirect, restated**: stop monkey-patching. The pattern
across runs 41 → 42 → 43 → 44 is each validation discovers a defect
class outside the audit's enumeration; substrate adds another class
(10 → 13 → 13 + G1-G8); the next run uncovers the next missing class.
The audit is a growing curator list of patterns-to-match. The writer's
[content-surface-contracts.md](internal/content/workflows/recipe/briefs/writer/content-surface-contracts.md)
defines four surfaces, each with **one reader, one purpose, one
single-question editorial test, one canonical shape**. **Defect
classes are emergent from a surface's test failing**, not curated by
hand. Run-45 substrate aligns the audit to this contract.

Codex independent validation (see threading note at end) ratified
this redesign. The original P1-P5 priorities (and the run-41 → 44
pattern they extend) are monkey patches; P2 alone was first-principles
adjacent. Codex's three required pillars below.

### Pillar A — Brief-render path verification (closes G5 + G7)

G5 and G7 are both render-pipeline gaps, not sub-agent compliance
gaps. Local substrate has the text; rendered brief doesn't.

- Investigate the refinement-2 brief composer
  (`internal/recipe/briefs_refinement2.go` and the template renderer)
  and confirm why `classification` is absent from the rendered output
  JSON schema even though `phase_entry.md:47-65` carries the rule.
  Likely cause: composer truncates / splits the local file by section
  and the local edit landed in a section the composer doesn't include
  in the rendered output-schema block.
- Same for refinement-1: confirm why `derived_rules.md` line 62
  (KB-DEFENSIVE-FLOOR) renders as empty in the run-44 refinement-1
  brief. Walk the composer's section extraction and ensure every bullet
  in the file reaches the rendered output.
- Add content tests pinning the rendered brief output: for every
  load-bearing local-substrate paragraph (every `## Run-NN <id>` block
  in the audit_checklist + every bullet in derived_rules), assert
  the substring is present in the rendered brief string. Without this,
  every future local edit risks the same silent-drop failure.

### Pillar B — Audit walk redesigned around the surface single-question tests (closes G2 + G3 + G4 + run-44 recipe-internal naming creep + future-class-discovery loop)

Refinement-2's contract changes from "walk N defect classes against
patterns" to:

```
For EACH surface in {S4 IG, S5 KB, S6 CLAUDE.md, S7 zerops.yaml, S3 tier yaml}:
  For EACH item on that surface (every IG H3, every KB bullet,
                                 every CLAUDE.md custom section,
                                 every yaml comment block,
                                 every tier service block):

    Apply that surface's single-question editorial test:
      S4 IG: "Would a porter bringing their own code need to copy
              THIS exact content into their own app?"
      S5 KB: "Would a developer who read the Zerops docs AND the
              framework docs STILL be surprised by this?"
      S6 CLAUDE.md: "Is this useful for operating THIS repo specifically?"
      S7 yaml comment: "Does each comment explain a trade-off the
                        reader couldn't infer from the field name?"
      S3 tier yaml: "Does each service block explain a decision rather
                     than narrate the field?"

    If the answer is no → emit a finding with the surface's failure
    classification (DROP if the item belongs on no surface;
    MOVE-TO-<surface> if it belongs elsewhere; REWRITE if the item
    is right-surface but wrong-shape).

  Then, after the per-item walk, run the cross-surface uniqueness
  pass: every fact lives on exactly one surface; duplicates collapse
  to canonical-surface + cross-reference. This is the
  one-fact-one-surface clause from content-surface-contracts.md:94.
```

Implications:

- The 13-defect-class enumeration in [audit_checklist.md:753-760](internal/recipe/content/briefs/refinement2/audit_checklist.md#L753-L760)
  retires. Defect classes become **observed failure modes** the
  audit reports AFTER an item fails its surface test — not patterns
  it scans for. `kb-ig-duplication`, `self-inflicted-as-gotcha`,
  `recipe-internal-naming`, `aspirational-as-current`,
  `scaffold-decision-as-gotcha` — all of these are S5 single-question
  test failures. The test catches them by principle, not pattern.
- The G3 strict-letter weakness (needs the artifact in IG #1 yaml)
  retires — the S5 single-question test catches the X-Cache bullet
  because a developer reading framework docs already knows `exposedHeaders`
  exists; the bullet's "surprise" comes only from the recipe-internal
  Cache demo, which fails the self-referential-decoration prohibition
  ([content-surface-contracts.md:109-111](internal/content/workflows/recipe/briefs/writer/content-surface-contracts.md#L109-L111)).
- The G4 per-codebase-walk discipline becomes a natural product of
  "for each surface, for each item" iteration — there's no codebase
  the loop skips.
- The recipe-internal-naming P5 curated list retires — the S5 test
  + the self-referential-decoration prohibition catch the same
  bullets without the curator.

### Pillar C — Engine-side determinism for the formulaic finding fields (closes G6 + auto-closes future schema additions)

LLM judges the editorial test (whether an item needs a citation;
which classification applies when the choice is ambiguous; whether
a bullet duplicates an IG item). Engine populates the deterministic
fields once the LLM's judgment lands:

- **`suggestedReplacement` for missing-citation**: the citation map's
  friendly-display-name + canonical URL pair is deterministic. Engine
  renders the form-(b) markdown link server-side when the audit
  identifies the topic family. The LLM emits `{defectClass:
  missing-citation, topic: <family>}` and the engine fills
  `suggestedReplacement` before the findings ship to the main agent.
- **`classification`**: the seven-class taxonomy is finite. For each
  `surface=CODEBASE_KB` / `CODEBASE_IG` finding, the engine inspects
  the facts.jsonl `candidateClass` for the source fact (if recorded)
  and pre-fills `classification` deterministically. The LLM only
  overrides when the test surfaces a finer judgment.
- **`fragmentId`**: the short-form codebase name (api/app/worker) is
  deterministic from the surface + scope. Engine renders it; LLM
  never types it.

Engine-side determinism collapses the run-40 → run-44 recurring "the
main agent retries with the missing field" cycle. Adding more brief
text to demand the field doesn't fix it; rendering the field
server-side does.

### Run-45 dogfood readiness criteria

Run 45 is the breakthrough only if:

1. **Pillar A landed**: rendered briefs assert-pinned against local
   substrate; G5 + G7 silently-dropped-content shape impossible.
2. **Pillar B landed**: refinement-2 brief restructured around the
   per-surface single-question walk; defect classes emergent from
   test failures.
3. **Pillar C landed**: at minimum, engine-side `suggestedReplacement`
   for missing-citation + `classification` pre-fill from facts. Other
   deterministic fields can land in run-46.

Run-45 deliverable should make these visible:

- Zero "classification missing" record-fragment rejections at main
  agent (Pillar C closes the cycle the run-44 sub-agent didn't).
- Non-zero IG citation coverage (Pillar B walks the IG surface item
  by item; Pillar C pre-fills the canonical link).
- Recipe-internal naming (Cache demo / tryGetClient) flagged or
  dropped from KB without a curator list (Pillar B's single-question
  test catches it).
- KB-IG echoes on every codebase flagged or merged via cross-reference
  (Pillar B's cross-surface uniqueness pass closes it).

If any of the four fail, the substrate needs another redesign — NOT
another defect class.

---

## Per-fix substrate verdicts (recap)

| Fix | Substrate target | Verdict | Run-44 surface evidence |
|---|---|---|---|
| **G1** | Anchored URL acceptance | **WORKS** | 4 URL-form reverts on apidev KB validated by the anchored matcher; cite-by-name form accepted on retry |
| **G2** | IG-citation walking | **FAILED at emit** | 0 IG findings emitted by sub-agent; IG citations 0/10 (unchanged from run-43) |
| **G3** | Named-artifact self-inflicted | **LATENT — strict-letter design weakness** | exposedHeaders not in IG #1 yaml (lives in main.ts); X-Cache bullet ships with recipe-internal naming |
| **G4** | KB-IG dup blocker promotion | **NOT EXERCISED on appdev** | Sub-agent considered rule for api/worker, skipped app; appdev KB #1+#2 ship as arguable IG echoes |
| **G5** | Classification field on findings | **FAILED at brief-render (codex correction)** | Rendered brief output-schema lacks `classification` field; agent never saw it. Render-pipeline gap, not compliance gap. |
| **G6** | Pre-resolved suggestedReplacement | **FAILED at emit** | 0 of 3 missing-citation findings carry suggestedReplacement; main agent composed wrong URLs |
| **G7** | KB-DEFENSIVE-FLOOR rule | **BRIEF-RENDER GAP** | derived_rules.md line 62 has rule; rendered refinement-1 brief skips it |
| **G8** | Writer-brief KB no-floor cap-8 | **WORKS** | appdev ships 2 bullets without floor violation |

**Two of eight G-fixes worked at the deliverable surface (G1, G8).** Two failed at the audit-emit path because the rendered brief shown to the sub-agent lacked the new instruction (G5, G7 — render-pipeline gaps, codex correction). Two failed at the audit-emit path because the sub-agent saw the instruction but didn't exercise it (G2, G6 — compliance gaps). One had a substrate-design weakness (G3). One wasn't exercised on a codebase (G4). **Two distinct root causes, addressed by Pillar A (render verification) and Pillar B (audit walk redesign) above.**

---

## Recipe-quality sidebar — what got better between run-43 and run-44

Three substrate-attributable improvements:

- **Codebase friendly-authority hits**: 1 (run-43) → 7 (run-44) across three yamls. F6 / F-FRIENDLY-AUTH pattern broadened to appdev + workerdev surfaces.

- **KB citation coverage**: 3/10 (run-43) → 5/9 (run-44, counting worker KB #3 cross-ref). cite-by-name form held; URL form path remains unexercised.

- **Aspirational JWT regression caught** — author re-introduced 3 aspirational claims, audit caught all 3 + main agent ACTed. Net at deliverable: zero aspirational claims shipping. Demonstrates the audit's value as a regression net under authoring stochasticity.

These are substrate-attributable wins. The deliverable's codebase-yaml friendly-authority shape is meaningfully closer to jetstream than run-43's; the per-codebase KB voice + IG citation coverage remain the next substrate frontier.

---

## Codex independent validation

Codex (gpt-5.4) independently validated this plan against the
substrate files + sub-agent JSONL on 2026-05-13. Confirmed:

- G2 + G6 are clean "brief says it, agent did not emit it" traces.
- The four-runs-back pattern (41 → 44) of adding defect classes after
  each miss is the monkey-patching the user is calling out.
- G5 is **not** the same root cause as G2/G6 — the rendered brief
  shown to the sub-agent literally lacks the `classification` field in
  its output JSON schema (rendered-brief content at `agent-a900…jsonl`
  output-schema block ends at `suggestedReplacement`). This is a
  render-pipeline divergence, not a sub-agent compliance gap.
- The original P1-P5 priorities were mostly monkey patches; P2 alone
  was first-principles adjacent.
- The first-principles redesign for run-45 is the three pillars above:
  brief-render verification (A), per-surface single-question audit
  walk replacing the 13-class enumeration (B), engine-side determinism
  for formulaic finding fields (C).
- **Substantive defect in the original framing**: collapsing G2/G5/G6
  into one "sub-agent doesn't exercise newly-added schema/scope
  additions" root cause obscured the render-pipeline gap. The fix for
  G2/G6 (stronger prose + worked examples) will NOT fix G5 unless
  Pillar A also lands.

Codex thread agentId: `aa6c2340952cd4ae8`.
