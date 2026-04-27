# Recipe content research — empirical derivation of the surface contracts

**This doc is the research that produced [docs/spec-content-surfaces.md](../spec-content-surfaces.md).** It captures the side-by-side analysis of two reference recipes, the content-tree diagram, the fact-routing decision tree, and the worked routing examples. The spec is authoritative for *what the rules are*; this doc is the navigation aid for *why they are the rules and how to apply them to a new case*.

When the spec and this doc disagree, the spec wins. This doc is preserved separately because:

1. **The diagrams matter.** The content-tree and routing-tree diagrams aren't in the spec; the spec is text-shaped. A fresh instance picking up content-quality work navigates faster with the visual scaffolding.
2. **The empirical comparison is the ground truth.** Both reference recipes agree within ±20% on every cap. The numerical tables are the defensible answer when someone asks "why 8 KB bullets, not 6 or 10?"
3. **The worked examples teach pattern-matching.** Reading "fact A classifies as B, routes to C, takes shape D" repeatedly builds the routing reflex faster than reading the rule once.

The two reference recipes:

- [`/Users/fxck/www/recipes/laravel-jetstream/`](../../../../recipes/laravel-jetstream/) + [`/Users/fxck/www/laravel-jetstream-app/`](../../../../laravel-jetstream-app/) — **human-authored**, the readability + voice floor.
- [`/Users/fxck/www/recipes/laravel-showcase/`](../../../../recipes/laravel-showcase/) + [`/Users/fxck/www/laravel-showcase-app/`](../../../../laravel-showcase-app/) — **early recipe-flow output**, the mechanism-density floor.

The run-14 deliverable being measured against:

- [`docs/zcprecipator3/runs/14/`](runs/14/) — `nestjs-showcase` recipe, 2026-04-27.

---

## Part 1 — Empirical findings: how the two references actually behave

### 1.1 Length budgets per surface, observed

| Surface | laravel-jetstream (human) | laravel-showcase (early-flow) | run-14 nestjs-showcase | What the data says |
|---|---|---|---|---|
| Root README | 28 lines | 27 lines | 25 lines | Universal: ~25–30 lines |
| **Tier README** | **7–8 lines, 1 sentence in extract** | **7–8 lines, 1–2 sentences in extract** | **41–42 lines, full ladder in extract** | **Universal: extract = 1–2 sentences. Body ≈ extract.** |
| Tier `import.yaml` | 50–80 lines yaml + 30–40 indented comment lines | 75–95 lines yaml + 30–50 indented comment lines | 150–180 lines yaml + 75–83 indented comment lines | Reference: 3–5 lines comment per service block. Run-14 ships 8–10 lines per block — **2–3× over** |
| Apps-repo README | 290 lines, 4 IG items | 360 lines, 5 IG items | 200–344 lines, 8–10 IG items | Reference: 4–5 IG items + 5–8 KB bullets. Run-14: 8–10 IG items + 7–12 KB bullets — **2× over on item count** |
| Apps-repo IG #1 yaml | 180 lines yaml verbatim with comments | 285 lines yaml verbatim with comments | 130–230 lines | Universal: full yaml verbatim is the bulk of IG #1 |
| Apps-repo CLAUDE.md | 32 lines, 3 fixed sections | 33 lines, 3 fixed sections | 46–55 lines, 3+ sections | Reference: ~33 lines, **clearly templated** |
| Apps-repo `zerops.yaml` | 184 lines, 3 setups | 283 lines, 3 setups | 130–230 lines | Comment style varies; density is consistent |

**The single sharpest signal:** tier README extract markers wrap **one sentence** in both references. Run-14 wraps a 35-line ladder. This isn't a soft preference — both human and early-flow agree, and the recipe-page UI is rendering the marker contents as a card description.

### 1.2 The two references aren't equal — they're complementary

**laravel-jetstream (human-made) is better at:**
- **Friendly authority.** "Feel free to change this value", "Configure this to use real SMTP sinks in true production setups." Speaks TO the porter, not AT them.
- **Doc cross-references.** Inline links to `docs.zerops.io/...` so curious porters can self-discover. The early-flow recipe and run-14 cite guides only as author-time signals.
- **Operational callouts.** The `> [!CAUTION]` + `zsc health-check disable` block for maintenance mode is GitHub-render-aware and stops a porter from breaking production.
- **Honest gaps.** `# FIXME(tikinang): Deploy Mailpit? Use 'envSecrets'? Or what?` left visible in production tier yaml. A real human said "I don't know yet" and shipped that.
- **"Production vs. Development" section** — a 3-bullet, framework-specific tier-promotion guide separate from the IG.

**laravel-showcase (early-flow) is better at:**
- **Density per word.** Every yaml comment carries mechanism + rationale. No filler. *"readinessCheck gates the traffic switch — new containers must answer HTTP 200 before the L7 balancer routes to them. This enables zero-downtime deploys."*
- **Trade-off naming.** Says WHY each choice over its alternative. *"predis client is a pure-PHP Redis client that needs no compiled extension"* — names BOTH the chosen solution and the rejected one.
- **Cross-tier consistency.** Tier comments narrate "what changes from the previous tier" implicitly through service-block comments; the human jetstream doesn't.
- **KB bullets in the canonical "**Topic** — explanation" shape.** Run-14 follows this shape; jetstream uses inline narrative + headings instead. The early-flow shape is the right one for KB.

**The combined ideal:**
- **Voice:** human jetstream's friendly authority + early-flow's mechanism-density.
- **IG:** 4–5 items max (jetstream count) + early-flow's "**Topic** — diff + reason" structure per item.
- **KB:** 5–8 bullets in the early-flow's "**Topic** — explanation" shape, but with jetstream's inline doc links where applicable.
- **Tier comments:** early-flow's mechanism-density, but capped at 3–5 lines per service block (current run-14 is 8–10).
- **CLAUDE.md:** keep the 33-line / 3-section template — both references converge on it.

### 1.3 The patterns both references share (universals)

These appear identically across both human + early-flow recipes — the **non-negotiable structural rules** of a Zerops recipe:

1. **Root README is a navigation page**, not a learning page. Title + 1-sentence intro + deploy button + cover image + tier list + footer. Always ~25 lines.
2. **Tier README is a card description**, not a teaching page. Title + 1-sentence extract. Always ~7 lines.
3. **Tier `import.yaml` is annotated**. `project:` block + `services:` block. Each service block carries 2–4 lines of comment explaining presence and choices. Project block carries 2–3 lines explaining tier-level choices (HA, corePackage, secret minting).
4. **Apps-repo README structure is fixed:** Title → 1-2 sentence intro extract → Deploy button → Cover image → `## Integration Guide` (numbered items, IG #1 always = "Adding zerops.yaml" with full yaml verbatim) → `## Tips`/`## Gotchas` (KB bullets in "**Topic** — explanation" form).
5. **CLAUDE.md is 3-section templated:** Title + intro → `## Zerops service facts` (terse bullets with hostname/port/siblings/runtime base) → `## Zerops dev (hybrid)` (the dev-loop story) → `## Notes` (3–5 dense bullets).
6. **Apps-repo `zerops.yaml` carries causal comments per directive group** — never narrating what the field does, always explaining a non-obvious choice.
7. **Cross-service env vars use the `${hostname_*}` pattern** with own-key aliases. Both references treat this as standard Zerops vocabulary.
8. **Secrets are project-level, generated via `<@generateRandomString(<32>)>`**. APP_KEY in both Laravel recipes; APP_SECRET in nestjs-showcase. Same pattern.

---

## Part 2 — The first-principles content tree

The reader of every published surface is **the porter** — a developer who clicked deploy on `zerops.io/recipes` and is now operating their own copy. Every routing rule below derives from "what is THIS reader, at THIS surface, doing right now?"

```
PUBLISHED RECIPE
│
├─ Root README                             ← reader: scanning recipe page; needs to pick recipe + tier in 30s
│  └─ {title, 1-sentence stack intro, deploy button, cover, tier list, footer}
│
├─ Tier 0..5/                              ← reader: deciding WHICH tier; needs the tier-card description
│  ├─ README.md                            ← extract markers wrap 1-2 sentences (recipe-page card description)
│  └─ import.yaml                          ← reader: opened the manifest in dashboard; needs "why this service at this tier"
│     └─ {project block + service blocks, 2-4 line causal comments per block}
│
└─ APPS REPO (per codebase)
   ├─ README.md                            ← reader: cloned the repo; needs to port their own code OR understand this one
   │  ├─ {title, 1-2 sentence intro extract, deploy button, cover}
   │  ├─ ## Integration Guide              ← reader: bringing their OWN code; needs concrete diffs
   │  │  ├─ 1. Adding zerops.yaml          ← always-first; full yaml verbatim with comments (engine-emitted)
   │  │  ├─ 2. {framework integration}     ← composer/npm require + 3-line code diff
   │  │  ├─ 3. {framework integration}     ← composer/npm require + 3-line code diff
   │  │  ├─ 4. {framework integration}     ← composer/npm require + 3-line code diff
   │  │  └─ 5. {optional}                  ← cap at 5; more = re-evaluate
   │  └─ ## Tips & Gotchas (KB)            ← reader: hit a confusing failure; needs platform traps
   │     └─ {5-8 bullets in "**Topic** — 2-4 sentence explanation" shape}
   │
   ├─ CLAUDE.md                            ← reader: AI agent or human OPERATING this exact repo
   │  ├─ {title, 1-line intro}
   │  ├─ ## Zerops service facts           ← machine-extractable bullets
   │  ├─ ## Zerops dev (hybrid)            ← the dev-loop story for this codebase
   │  └─ ## Notes                          ← 3-5 dense porter-relevant operational bullets
   │
   └─ zerops.yaml                          ← reader: editing the deploy config to suit their app
      └─ {comments above directive groups OR per field — author's choice; never narrating the field}
```

**The hard caps that the data supports:**

| Surface | Hard line cap | Hard item cap |
|---|---|---|
| Root README | 35 lines | 6 tier links + footer |
| Tier README extract slot | **1–2 sentences (≤ 350 chars)** | n/a |
| Tier README total | 10 lines | n/a |
| Tier import.yaml comments | **40 indented lines per tier** | 3–4 lines per service block |
| Apps-repo README intro extract | **1–3 sentences (≤ 500 chars)** | n/a |
| Apps-repo IG | n/a | **4–5 items including IG #1** |
| Apps-repo KB | n/a | **5–8 bullets** |
| CLAUDE.md | 50 lines | 3 fixed top sections + 3–5 Notes bullets |
| Apps-repo zerops.yaml | n/a | comment density: ~1 comment block per directive group OR per field |

These caps are not arbitrary — they're observed in BOTH references. Under the cap = clean. Over the cap = padding. The reference data lets us defend each cap.

The same table lives in [docs/spec-content-surfaces.md](../spec-content-surfaces.md#per-surface-line-budget-table) as the operational contract.

---

## Part 3 — Per-surface contracts (overview)

The full per-surface contracts are in [docs/spec-content-surfaces.md §The seven content surfaces](../spec-content-surfaces.md#the-seven-content-surfaces). Each surface section answers:

- **Reader.** Who is reading this surface, in what context, with what mental state.
- **Purpose.** What this surface exists to accomplish.
- **Test.** The single one-sentence question the author applies to each item before publishing.
- **Belongs / Does not belong.** Concrete inclusions + exclusions.
- **Length / Item caps.** From the budget table above.
- **Voice notes.** Surface-specific style.

This research doc doesn't duplicate the contracts — read the spec for those. The contracts derive from the empirical findings in Part 1.1–1.3 + the universal patterns in 1.3.

---

## Part 4 — Style rules (overview)

Universal style rules are in [docs/spec-content-surfaces.md](../spec-content-surfaces.md). The high-level groupings:

1. **Voice rules** — porter audience always; no authoring-process language; imperative for actions, declarative for facts; friendly authority over hedging.
2. **Sentence-shape rules** — yaml comments are mechanism + reason; KB bullets are `- **Topic** — 2–4 sentences.`; IG items are verb-first heading + diff/install line.
3. **Density rules** — line counts are constraints not targets; one fact per surface; one reason per comment block.
4. **Citation rules** — inline doc links encouraged in zerops.yaml + IG; KB bullets cite `zerops_knowledge` guide names when topic is covered.
5. **Anti-patterns (forbid list)** — tier README ladders inside extract markers; KB describing recipe-internal scaffold; IG describing recipe-internal scaffold; yaml comments narrating fields; authoring-tool names in published surfaces; self-inflicted incidents as KB; framework quirks as KB; cross-surface duplication; fabricated yaml field names; ladder structures padding to a line floor.

---

## Part 5 — The fact-routing decision tree

Every observation a recipe run produces — surprise, incident, scaffold decision, operational hack, framework quirk — gets classified BEFORE it lands on any surface. Classification → routing → final shape.

```
                       ┌─────────────────────────────────────┐
                       │   New observation from the run      │
                       └────────────────┬────────────────────┘
                                        ▼
                       ┌────────────────────────────────────────────────┐
                       │ Q1: Could this happen on Zerops with           │
                       │     completely different framework code?       │
                       └─────┬───────────────────────────────────┬──────┘
                          NO ▼                                YES ▼
        ┌────────────────────┴──────────┐         ┌──────────────┴────────────┐
        │ Q1a: Is it framework-only?    │         │ Q1b: Is it covered by an  │
        │ (Vite peer-dep, Nest          │         │      existing             │
        │  decorator collision, etc.)   │         │      zerops_knowledge     │
        │                               │         │      guide?               │
        └────┬──────────────────────┬───┘         └─────┬──────────────┬──────┘
         YES ▼                    NO▼                YES ▼            NO ▼
   ┌─────────┴─────────┐ ┌─────────┴─────────┐ ┌─────────┴─────────┐ ┌─┴────────┐
   │  DISCARD          │ │ Q1c: library      │ │ Surface 5 (KB)    │ │ Surface 5 │
   │  (framework docs) │ │ metadata          │ │ + cite the guide  │ │ (KB) —    │
   │                   │ │ (npm peer-dep,    │ │ in the bullet     │ │ genuinely │
   │                   │ │  Composer pin)?   │ │                   │ │ new       │
   └───────────────────┘ └────┬──────────┬───┘ └───────────────────┘ └──────────┘
                          YES ▼        NO ▼
                    ┌─────────┴───┐ ┌────┴──────────┐
                    │ DISCARD     │ │ Q2: Is this   │
                    │ (dep        │ │     "our code │
                    │ manifest)   │ │     had a bug;│
                    │             │ │     we fixed  │
                    └─────────────┘ │     it"?      │
                                    └────┬──────┬───┘
                                     YES ▼    NO ▼
                              ┌──────────┴┐ ┌─┴──────────────────────────────┐
                              │ DISCARD   │ │ Q3: Is this a config / code    │
                              │ — fix is  │ │     CHOICE the recipe made     │
                              │ in the    │ │     (chose X over Y)?          │
                              │ code      │ └────┬───────────────────────┬───┘
                              └───────────┘ YES  ▼                    NO ▼
                                       ┌────────┴───────────────┐ ┌─────┴──────┐
                                       │ Q3a: Is the choice     │ │ Q4: Is it  │
                                       │  visible in zerops.yaml│ │ "how to    │
                                       │  fields?               │ │ iterate    │
                                       └────┬──────────────┬────┘ │ THIS repo  │
                                        YES ▼          NO  ▼      │ locally"?  │
                                ┌──────────┴───┐  ┌─────────┴──┐  └─┬───────┬──┘
                                │ Surface 7    │  │ Surface 4  │ YES│       │NO
                                │ (zerops.yaml │  │ (IG): teach│ ▼  │       │
                                │ comment)     │  │ the        │┌───┴────┐  │
                                │              │  │ PRINCIPLE  ││ Surface│  │
                                │ "WHY this    │  │ + diff;    ││ 6      │  │
                                │  config      │  │ describe   ││(CLAUDE)│  │
                                │  choice"     │  │ implementa-││        │  │
                                │              │  │ tion only  │└────────┘  │
                                │              │  │ if porter  │            ▼
                                │              │  │ literally  │       (re-classify
                                │              │  │ copies it  │        — likely
                                │              │  │            │        framework
                                │              │  │            │        decoration
                                │              │  │            │        that should
                                └──────────────┘  └────────────┘        DISCARD)
```

**The router as a one-line predicate per surface:**

| Classification | Test | Route to |
|---|---|---|
| Platform invariant | Hits regardless of framework | KB (Surface 5) — cite guide if one exists |
| Platform × framework | This framework on this platform | KB — name both sides |
| Framework quirk | Same framework off-platform hits it too | DISCARD |
| Library/dep metadata | npm/Composer/cargo concern | DISCARD |
| Scaffold config choice (visible in yaml fields) | "We use X over Y" — config decision | zerops.yaml comment (Surface 7) |
| Scaffold code choice (porter literally copies code) | "We use X over Y" — code-level | IG item with the diff (Surface 4) |
| Scaffold code choice (porter does NOT copy) | "Our scaffold has X" but porter has different code | DISCARD or move principle to IG |
| Operational detail | "How to iterate THIS repo" | CLAUDE.md (Surface 6) |
| Self-inflicted | "Our code had a bug; we fixed it" | DISCARD entirely |

The same routing structure lives in [docs/spec-content-surfaces.md §Classification × surface compatibility](../spec-content-surfaces.md#classification--surface-compatibility) as the engine refusal table; that's the operational form.

---

## Part 6 — Worked routing examples

Walking facts from a typical run through the router. Each row demonstrates one classification + routing decision; reading them as a set builds the routing reflex.

| Observed during run | Classification | Routes to | Surface form |
|---|---|---|---|
| "predis is needed because php-nginx@8.4 doesn't include phpredis C extension" | Platform × framework | KB | `**Predis over phpredis** — The php-nginx@8.4 base image does not include the phpredis C extension. Use the predis/predis Composer package and set REDIS_CLIENT=predis to avoid 'class Redis not found' errors.` |
| "We chose predis over phpredis" (the same fact, but framed as a decision) | (same as above) | (same as above) | (same as above; classify by what the fact IS, not how the agent encountered it) |
| "The agent's appstage build ran before the api was deployed; literal `${apistage_zeropsSubdomain}` shipped" | Platform invariant (build-time consumer + alias resolution timing) | KB + cite `env-var-model` | `**Build-time consumers freeze unresolved aliases** — VITE_* envs replace at bundle time. If the api hasn't deployed when the app's build runs, the literal token ships in the JS. Order the api's first deploy before the app's first build. (See env-var-model guide.)` |
| "We picked tabbed layout because zerops_browser's headless viewport clips clicks below 577px" | Self-inflicted (recipe-internal scaffold decision driven by an authoring-tool constraint that the porter never operates) | DISCARD | (don't ship; the porter gets the working layout, not the diagnostic narrative) |
| "Cache commands belong in initCommands not buildCommands because the build path differs from the runtime path" | Platform × framework | KB + cite `init-commands` | (verbatim from laravel-showcase reference) |
| "We use `zsc execOnce ${appVersionId}-migrate` per command for distinct lock keys" | Scaffold config choice (visible in yaml) | Surface 7 (zerops.yaml comment) | The yaml itself carries the comment; IG #1 inherits it. Doesn't ALSO go in KB. |
| "minContainers: 2 because rolling deploys need a second healthy peer" | Scaffold config choice (visible in yaml) | Surface 7 | Tier 4+ yaml comment per `app` block. |
| "@sveltejs/vite-plugin-svelte^5 peer-requires Vite 6" | Framework-only quirk | DISCARD | npm registry metadata; not Zerops content. |
| "`nest start --watch` rotates child PIDs so pidfile-based liveness is unreliable" | Operational | CLAUDE.md | Notes bullet. |
| "Mailpit included for SMTP testing in dev, production tier uses real SMTP" | Scaffold config choice + tier-promotion concern | Surface 3 (tier import.yaml comment) | Tier-0/1 yaml: "Mailpit is included for SMTP testing." Tier-4/5 yaml: "Mailpit removed; replace MAIL_* with your provider." |

**How to use this table:** when a new observation surfaces during a run, find the closest analog in this table; the routing decision should match. If no analog fits, walk the decision tree in Part 5 from Q1.

---

## Part 7 — Engine implications (overview)

How the engine reads from the spec at compose-time + record-time + commit-time:

| Stage | Mechanism | Where |
|---|---|---|
| Compose-time (brief preface) | Brief atoms read spec section names + tests; compose into dispatched prompt | `internal/recipe/content/briefs/<phase>/*.md` |
| Compose-time (templates) | Engine pre-renders root README / env README / codebase README / CLAUDE.md skeletons with the right marker positions | `internal/recipe/content/templates/*.tmpl` |
| Record-time (per fragment) | `record-fragment` response payload carries the per-fragment-id `SurfaceContract` (Reader / Test / LineCap / ItemCap from spec) so agent reads the surface test verbatim at authoring decision moment | `internal/recipe/handlers.go` (run-15 §F.2) |
| Record-time (refusal) | `record-fragment` accepts `classification`; engine refuses incompatible (classification × fragmentId) pairs per spec compatibility table | `internal/recipe/handlers.go` + `classify.go` (run-15 §F.3) |
| Commit-time (validators) | Per-surface `ValidateFn` registered to each `Surface`; checks structural caps from spec line-budget table; runs at `complete-phase` gate | `internal/recipe/validators_*.go` |

The full implementation plan is in [docs/zcprecipator3/plans/run-15-readiness.md](plans/run-15-readiness.md). The spec is the single source of truth that all five stages read from.

---

## Part 8 — How this doc is maintained

This is a research artifact. Updates land here when:

- **A new reference recipe enters the corpus.** Re-run the side-by-side comparison; update the line-budget table in §1.1; check if the universal patterns in §1.3 still hold or need amendment.
- **A new failure mode surfaces in a dogfood run** that the routing tree in §5 doesn't cover cleanly. Add the case to §6 worked examples; if the failure mode reveals a missing branch in the decision tree, extend the tree.
- **A new managed-service category, framework, or runtime base is dogfooded.** Validate the line-budget caps still hold for the new shape. The caps are framework-agnostic by construction; if a new framework needs a higher cap, that's a finding worth documenting.

This doc grows by example. It does NOT grow by adding rules — rules go in [the spec](../spec-content-surfaces.md). If you find yourself writing a new rule here, it belongs in the spec; copy it there and reference back.

The doc also does NOT grow by adding implementation detail — implementation goes in run-N readiness plans. If you find yourself writing engine code here, it belongs in a readiness plan; copy it there and reference back.

What stays here: empirical observations, comparative analysis, diagrams, worked examples. The visual + comparative scaffolding that makes the spec usable for someone walking in cold.
