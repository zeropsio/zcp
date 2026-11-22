# Recipe content research — empirical derivation of the surface contracts

**This doc is the research that produced [docs/spec-content-surfaces.md](../spec-content-surfaces.md).** It captures the side-by-side analysis of two reference recipes, the content-tree diagram, the fact-routing decision tree, and the worked routing examples. The spec is authoritative for *what the rules are*; this doc is the navigation aid for *why they are the rules and how to apply them to a new case*.

When the spec and this doc disagree, the spec wins. This doc is preserved separately because:

1. **The diagrams matter.** The content-tree and routing-tree diagrams aren't in the spec; the spec is text-shaped. A fresh instance picking up content-quality work navigates faster with the visual scaffolding.
2. **The empirical comparison is the ground truth.** Both reference recipes agree within ±20% on every cap. The numerical tables are the defensible answer when someone asks "why 8 KB bullets, not 6 or 10?"
3. **The worked examples teach pattern-matching.** Reading "fact A classifies as B, routes to C, takes shape D" repeatedly builds the routing reflex faster than reading the rule once.

The two reference recipes:

- `<recipes checkout>/laravel-jetstream/` + `<laravel-jetstream-app checkout>/` — **human-authored**, the readability + voice floor.
- `<recipes checkout>/laravel-showcase/` + `<laravel-showcase-app checkout>/` — **early recipe-flow output**, the mechanism-density floor.

The current dogfood baseline being measured against:

- [`docs/zcprecipator3/runs/26/`](runs/26/) — `nestjs-showcase` recipe, 2026-05-06 (most recent successfully-completed run; the F-track convergence verdict at [`runs/26/ANALYSIS.md`](runs/26/ANALYSIS.md) confirms above-bar quality on every axis the F-track was meant to move).

The run-14 baseline numbers preserved in §1.1 below are kept as the
historical "before" column showing the over-cap shape the F-track
closed. Run-26 numbers in the same table show the current "after".

---

## Part 1 — Empirical findings: how the two references actually behave

### 1.1 Length budgets per surface, observed

| Surface | laravel-jetstream (human) | laravel-showcase (early-flow) | run-14 (before F-track) | run-26 (after F-track) | What the data says |
|---|---|---|---|---|---|
| Root README | 28 lines | 27 lines | 25 lines | 25 lines | Universal: ~25–30 lines |
| **Tier README** | **7–8 lines, 1 sentence in extract** | **7–8 lines, 1–2 sentences in extract** | **41–42 lines, full ladder in extract** | **6–8 lines, 1 sentence in extract** | **Universal: extract = 1–2 sentences. Run-15 §F.4 `tier-readme-extract-too-long` validator (350-char cap) closed the run-14 ladder leak.** |
| Tier `import.yaml` | 50–80 lines yaml + 30–40 indented comment lines | 75–95 lines yaml + 30–50 indented comment lines | 150–180 lines yaml + 75–83 indented comment lines | 90–140 lines yaml + 30–50 indented comment lines | Reference: 3–5 lines comment per service block. Run-26: 3–5 lines per block — **back at-bar after run-22 R2-RC-6 canonical-set-vs-flavor distinction.** |
| Apps-repo README | 290 lines, 4 IG items | 360 lines, 5 IG items | 200–344 lines, 8–10 IG items | 220–340 lines, 4–5 IG items + 3–7 KB bullets | Reference: 4–5 IG items + 5–8 KB bullets. Run-26: at-bar after run-15 §F.5 item-cap validators (`codebase-ig-too-many-items` ≤5, `codebase-kb-too-many-bullets` ≤8). |
| Apps-repo IG #1 yaml | 180 lines yaml verbatim with comments | 285 lines yaml verbatim with comments | 130–230 lines | 230–250 lines | Universal: full yaml verbatim is the bulk of IG #1. |
| Apps-repo CLAUDE.md | 32 lines, 3 fixed sections | 33 lines, 3 fixed sections | 46–55 lines, 3+ sections | 35–80 lines, 3 sections | Reference: ~33 lines, clearly templated. Run-26 above the 50-line cap on the api codebase (richer service-fan-out → more architecture bullets); within cap on app + worker. |
| Apps-repo `zerops.yaml` | 184 lines, 3 setups | 283 lines, 3 setups | 130–230 lines | 130–280 lines | Comment style varies; density is consistent. |

**The single sharpest signal (closed):** tier README extract markers
wrap **one sentence** in both references. Run-14 wrapped a 35-line
ladder; run-15 §F.4 added the `tier-readme-extract-too-long` validator
(350-char cap from `SurfaceContract.IntroExtractCharCap`); run-26
extracts settle at 1 sentence. This was the loudest cap drift run-14
surfaced and the first the engine closed at validator-time.

**Item-cap drifts (closed):** run-14 shipped 8–10 IG items + 7–12 KB
bullets per codebase; run-15 §F.5 added item-cap validators; run-26
ships within the 4–5 IG / 5–8 KB targets matching the reference
recipes. Run-26's appdev KB at 3 bullets is below the 5-floor — calibration
finding (SPA codebase has fewer Zerops-mechanic-x-this-stack failure
modes than api/worker), not drift.

**Tier yaml comment drift (closed):** run-14 shipped 8–10 indented
comment lines per service block (2–3× over reference). Run-22 R2-RC-6
distinguished "canonical-set dedup" from "per-tier flavor"; run-26
tier yamls land at 3–5 lines per service block matching the early-flow
reference, with run-26 F-30A's friendly-authority voice contract
adding adapt-path phrasings on top.

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

**The combined ideal (run-26 status in parentheses):**
- **Voice:** human jetstream's friendly authority + early-flow's mechanism-density. *(Run-26: F-30A landed friendly-authority phrasings on tier yamls; run-26 codex review confirmed voice at-bar with goldens on 4 quality axes.)*
- **IG:** 4–5 items max (jetstream count) + early-flow's "**Topic** — diff + reason" structure per item. *(Run-26: 4–5 items per codebase, framework-equivalent footers extending each — above-bar relative to goldens on this axis.)*
- **KB:** 5–8 bullets in the early-flow's "**Topic** — explanation" shape, but with jetstream's inline doc links where applicable. *(Run-26: 3–7 bullets per codebase; appdev at 3 = calibration not drift.)*
- **Tier comments:** early-flow's mechanism-density, but capped at 3–5 lines per service block (run-14 was 8–10). *(Run-26: 3–5 lines per block + adapt-path phrasing per F-30A.)*
- **CLAUDE.md:** keep the 33-line / 3-section template — both references converge on it. *(Run-26: 35–80 lines depending on codebase architecture richness; api over the 50-line cap because of the 5-managed-service fan-out — judgment call rather than a regression.)*

### 1.3 The patterns both references share (universals)

These appear identically across both human + early-flow recipes — the **non-negotiable structural rules** of a Zerops recipe:

1. **Root README is a navigation page**, not a learning page. Title + 1-sentence intro + deploy button + cover image + tier list + footer. Always ~25 lines.
2. **Tier README is a card description**, not a teaching page. Title + 1-sentence extract. Always ~7 lines.
3. **Tier `import.yaml` is annotated**. `project:` block + `services:` block. Each service block carries 2–4 lines of comment explaining presence and choices. Project block carries 2–3 lines explaining tier-level choices (HA, corePackage, secret minting).
4. **Apps-repo README structure is fixed:** Title → 1-2 sentence intro extract → Deploy button → Cover image → `## Integration Guide` (numbered items, IG #1 always = "Adding zerops.yaml" with full yaml verbatim) → `## Tips`/`## Gotchas` (KB bullets in "**Topic** — explanation" form).
5. **CLAUDE.md is Zerops-FREE per run-21 R2-5** — the run-13 architecture pivot retired the engine-injected `## Zerops dev loop` block; run-21 R2-5 retired the engine-side Zerops-content guards (word blacklisting was wrong-side per system.md §4 — banning common English tokens like `db`/`cache`/`search`). Brief at `briefs/claudemd-author/zerops_free_prohibition.md` is the authoring contract; validators do structural shape only (line cap ≤80, H2 count 2–4, min size ≥200 bytes). Canonical 3-section shape: Title + 1-2 sentence intro → `## Build & run` (npm/composer scripts verbatim with one-line labels) → `## Architecture` (one bullet per source dir / entrypoint, framework-canonical layout). Authored by a sibling `claudemd-author` sub-agent dispatched in parallel with `codebase-content` so Zerops bleed-through is structurally impossible (no platform principles atom, no `zerops.yaml` pointer, no managed-service hints in the brief).
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
   │  │  (Zerops-free by construction — run-21 R2-5; sibling sub-agent
   │  │   authors this in parallel with codebase-content so platform
   │  │   bleed-through is structurally impossible)
   │  ├─ {title, 1-2 sentence intro extracted from package.json/source}
   │  ├─ ## Build & run                    ← framework-canonical commands (npm/composer scripts) verbatim
   │  └─ ## Architecture                   ← one bullet per source dir / entrypoint
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

| Classification (enum) | Test | Route to |
|---|---|---|
| `platform-invariant` | Hits regardless of framework | KB (Surface 5) — cite guide if one exists |
| `intersection` | This framework on this platform | KB — name both sides |
| `framework-quirk` | Same framework off-platform hits it too | DISCARD |
| `library-metadata` | npm/Composer/cargo concern | DISCARD |
| `scaffold-decision` | "We use X over Y" — recipe-specific choice | Routing depends on *flavor* (English description, NOT enum value): config flavor (visible in yaml fields) → zerops.yaml comment (Surface 7); code flavor (porter literally copies the diff) → IG item with the diff (Surface 4); recipe-internal flavor (porter would have different code) → DISCARD or move the principle (without specific implementation) to IG |
| `operational` | "How to iterate THIS repo" | CLAUDE.md (Surface 6) |
| `self-inflicted` | "Our code had a bug; we fixed it" | DISCARD entirely |

The `Classification` enum in [internal/recipe/classify.go](../../internal/recipe/classify.go) has exactly these 7 values. There is no parenthesized variant of `scaffold-decision` — the (config / code / recipe-internal) split is an *English description* of where a given scaffold-decision routes, not a separate enum value. Run-21 P0-2 + R3-4 cleaned up the engine, brief, and spec sites that had the parenthesized form leaking as if they were enum values; sub-agents who read those leaks invented kinds (`scaffold-decision`, `platform-invariant`) as bogus `kind` values and got 24% record-fact reject rates.

The same routing structure lives in [docs/spec-content-surfaces.md §Classification × surface compatibility](../spec-content-surfaces.md#classification--surface-compatibility) as the engine refusal table; that's the operational form.

---

## Part 6 — Worked routing examples

Walking facts from a typical run through the router. Each row demonstrates one classification + routing decision; reading them as a set builds the routing reflex.

| Observed during run | Classification (enum) | Routes to | Surface form |
|---|---|---|---|
| "predis is needed because php-nginx@8.4 doesn't include phpredis C extension" | `intersection` | KB | `**Predis over phpredis** — The php-nginx@8.4 base image does not include the phpredis C extension. Use the predis/predis Composer package and set REDIS_CLIENT=predis to avoid 'class Redis not found' errors.` |
| "We chose predis over phpredis" (the same fact, but framed as a decision) | (same as above) | (same as above) | (same as above; classify by what the fact IS, not how the agent encountered it) |
| "The agent's appstage build ran before the api was deployed; literal `${apistage_zeropsSubdomain}` shipped" | `platform-invariant` (build-time consumer + alias resolution timing) | KB + cite `env-var-model` | `**Build-time consumers freeze unresolved aliases** — VITE_* envs replace at bundle time. If the api hasn't deployed when the app's build runs, the literal token ships in the JS. Order the api's first deploy before the app's first build. (See env-var-model guide.)` |
| "We picked tabbed layout because zerops_browser's headless viewport clips clicks below 577px" | `self-inflicted` (recipe-internal scaffold decision driven by an authoring-tool constraint that the porter never operates) | DISCARD | (don't ship; the porter gets the working layout, not the diagnostic narrative) |
| "Cache commands belong in initCommands not buildCommands because the build path differs from the runtime path" | `intersection` | KB + cite `init-commands` | (verbatim from laravel-showcase reference) |
| "We use `zsc execOnce ${appVersionId}-migrate` per command for distinct lock keys" | `scaffold-decision` (config flavor — visible in yaml) | Surface 7 (zerops.yaml comment) | The yaml itself carries the comment; IG #1 inherits it. Doesn't ALSO go in KB. |
| "minContainers: 2 because rolling deploys need a second healthy peer" | `scaffold-decision` (config flavor — visible in yaml) | Surface 7 | Tier 4+ yaml comment per `app` block. |
| "@sveltejs/vite-plugin-svelte^5 peer-requires Vite 6" | `framework-quirk` | DISCARD | npm registry metadata; not Zerops content. |
| "`nest start --watch` rotates child PIDs so pidfile-based liveness is unreliable" | `operational` | CLAUDE.md | Notes bullet. |
| "Mailpit included for SMTP testing in dev, production tier uses real SMTP" | `scaffold-decision` (config flavor + tier-promotion concern) | Surface 3 (tier import.yaml comment) | Tier-0/1 yaml: "Mailpit is included for SMTP testing." Tier-4/5 yaml: "Mailpit removed; replace MAIL_* with your provider." |

**How to use this table:** when a new observation surfaces during a run, find the closest analog in this table; the routing decision should match. If no analog fits, walk the decision tree in Part 5 from Q1.

---

## Part 7 — Engine implications (overview)

How the engine reads from the spec at compose-time + record-time + commit-time. The base mechanisms landed in run-15; subsequent runs extended each stage with cb-precision filtering, fact propagation, and engine-mandatory transitions. See system.md §4 verdict table for per-extension TEACH/DISCOVER classification.

| Stage | Mechanism | Where |
|---|---|---|
| Compose-time (brief preface) | Brief atoms read spec section names + tests; compose into dispatched prompt | `internal/recipe/content/briefs/<phase>/*.md` |
| Compose-time (templates) | Engine pre-renders root README / env README / codebase README / CLAUDE.md skeletons with the right marker positions | `internal/recipe/content/templates/*.tmpl` |
| Compose-time (engine-built dispatch prompt) | `build-subagent-prompt` composes FULL dispatch prompt (recipe-level context wrapper + brief body + close criteria) from Plan + Research.Description; eliminates hand-typed wrappers | `internal/recipe/briefs_subagent_prompt.go::buildSubagentPromptForPhase` (run-13 §B2) |
| Compose-time (per-codebase atom selection) | `cb.ConsumesServices` (parsed from each codebase's `run.envVariables` at scaffold complete-phase) drives atom filtering — SPA that consumes nothing managed drops `nats-shapes.md` + `cross-service-urls.md`; codebase-content brief's Managed services block filtered the same way | `internal/recipe/consumes_services.go` (run-21 R2-2/R2-3) |
| Compose-time (cross-codebase fact propagation) | `CrossCodebaseManagedServiceFacts` extends `FilterByCodebase` with env-key intersection: sister-codebase facts referencing a service THIS codebase consumes via `${<host>_*}` propagate into the brief — closes the run-26 apidev-NATS factuality drift | `internal/recipe/consumes_services.go::CrossCodebaseManagedServiceFacts` (run-26 F-31) |
| Compose-time (engine-emitted facts) | Engine seeds shells (Class B universal-for-role, Class C umbrella, per-managed-service connect shells) into `FactsLog` so the codebase-content sub-agent fills empty slots via `fill-fact-slot` | `internal/recipe/handlers.go::seedEngineEmittedFacts` (run-16 §6.4 / §7.1) |
| Compose-time (parent recipe baseline) | Embedded fallback path: when filesystem mount empty AND `parentSlugFor(slug)!=""` AND `internal/knowledge/recipes/<parent>.md` exists in the `//go:embed` corpus, scaffold brief carries the parent .md verbatim | `internal/recipe/embedded_recipes.go` (run-22 R3-RC-0) |
| Record-time (per fragment) | `record-fragment` response payload carries the per-fragment-id `SurfaceContract` (Reader / Test / LineCap / ItemCap from spec) so agent reads the surface test verbatim at authoring decision moment | `internal/recipe/handlers.go` (run-15 §F.2) |
| Record-time (refusal) | `record-fragment` accepts `classification`; engine refuses incompatible (classification × fragmentId) pairs per spec compatibility table | `internal/recipe/handlers.go` + `classify.go` (run-15 §F.3) |
| Record-time (warn-on-record) | `recordFact` emits non-blocking `Notice` when `candidateClass × candidateSurface` is incompatible — same lookup the fragment-time refusal uses, faster feedback | `internal/recipe/handlers.go::recordFact` (run-22 R3-C-3) |
| Record-time (snapshot/restore) | `record-fragment mode=replace` at refinement phase wraps each call in snapshot/restore: reverts the fragment if the post-replace validator surfaces a NEW violation | `internal/recipe/handlers.go` (run-17 §9 T4) |
| Record-time (read-then-replace baseline) | `record-fragment mode=replace` returns `priorBody` so refinement merges against verbatim instead of grep+reconstructing | `internal/recipe/handlers.go` (run-14 §B.1) |
| Boundary-time (engine refusals) | `OpenOrCreate` refuses `outputRoot` paths AT or ABOVE SSHFS mount roots; closes run-25's HIGH-severity workspace-pollution regression | `internal/recipe/handlers.go::OpenOrCreate` + `mount.go` (run-26 F-28) |
| Boundary-time (engine-mandatory phase transition) | `complete-phase phase=finalize` refuses closure until `Session.RefinementDispatched` flag is set (flipped by `build-subagent-prompt briefKind=refinement`); auto-advance finalize → refinement is the ONE engine-driven phase transition | `internal/recipe/handlers.go` (run-18 + run-23 F-26) |
| Commit-time (validators) | Per-surface `ValidateFn` registered to each `Surface`; checks structural caps from spec line-budget table; runs at `complete-phase` gate | `internal/recipe/validators_*.go` |
| Commit-time (worker subscription gate) | `gateWorkerSubscription` regex-scans showcase-tier worker codebases; refuses naked `nc.subscribe()` (no queue option) and warns on `unsubscribe()` shutdown | `internal/recipe/validators_worker_subscription.go` (run-22 R2-WK-1+2) |
| Commit-time (frontend integration validator) | `gateFrontendIntegration` checks every backend route in fact stream has matching `[data-test=*]` selector + fetch via `${VITE_API_URL}` | `internal/recipe/validators_*.go` (run-23 F-23) |
| Emit-time (single-slot URL rewrite) | `writeProject` extends with `rewriteURLsForSingleSlot` for tiers 2-5: drops DEV_*-prefixed keys, collapses slot-named hostnames, preserves `${zeropsSubdomainHost}` literal | `internal/recipe/yaml_emitter.go` (run-22 R3-RC-3) |

The spec ([docs/spec-content-surfaces.md](../spec-content-surfaces.md)) is the single source of truth that every stage reads from.

---

## Part 8 — How this doc is maintained

This is a research artifact. Updates land here when:

- **A new reference recipe enters the corpus.** Re-run the side-by-side comparison; update the line-budget table in §1.1; check if the universal patterns in §1.3 still hold or need amendment.
- **A new failure mode surfaces in a dogfood run** that the routing tree in §5 doesn't cover cleanly. Add the case to §6 worked examples; if the failure mode reveals a missing branch in the decision tree, extend the tree.
- **A new managed-service category, framework, or runtime base is dogfooded.** Validate the line-budget caps still hold for the new shape. The caps are framework-agnostic by construction; if a new framework needs a higher cap, that's a finding worth documenting.

This doc grows by example. It does NOT grow by adding rules — rules go in [the spec](../spec-content-surfaces.md). If you find yourself writing a new rule here, it belongs in the spec; copy it there and reference back.

The doc also does NOT grow by adding implementation detail — implementation goes in run-N readiness plans. If you find yourself writing engine code here, it belongs in a readiness plan; copy it there and reference back.

What stays here: empirical observations, comparative analysis, diagrams, worked examples. The visual + comparative scaffolding that makes the spec usable for someone walking in cold.
