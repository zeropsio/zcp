# Run-16 readiness — fields/comments architecture + R-15-1 forensic closure

**Status**: implementation-ready. Supersedes [run-16-prep.md](run-16-prep.md) by absorbing every triple-verification correction, the R-15-1 forensic diagnosis, and the design pivots that surfaced during walk-through review (CLAUDE.md authored by a dedicated `claudemd-author` sub-agent with a Zerops-free brief, peer-dispatched alongside codebase-content; brief composition uses on-disk-readable pointers instead of verbatim embeds; legacy atom deletions are conditionalized on dogfood; per-managed-service hint table replaced by fact-shell pattern; `tier_decision` engine-emit corrected to use a new `TierServiceModeDelta` helper rather than the whole-tier `Diff`).

Every cite in this guide is checked against the actual artifact with a paginated read; head-truncated greps are forbidden as evidence (see §0 — the prep-verification pass mistakenly claimed `ANALYSIS.md §7 doesn't exist` based on a `head -25` grep that cut off at line 760, when §7 lives at line 984).

**Predecessor chain**:

- [run-16-prep.md](run-16-prep.md) — design doc, supersedes [run-15-prep.md](run-15-prep.md)
- This doc — implementation plan, tranche-by-tranche, with per-commit titles + tests + risk-mitigation gates

**Reading order for the fresh instance**:

1. §0 — verification protocol (read FIRST; learn the truncation failure mode)
2. §1 — the shift in three sentences
3. §2 — corrections that anchor the design
4. §3 — IG taxonomy (with bundled-class caveat)
5. §4 — phase shape (with parent-recipe threading)
6. §5 — fact schema (3 subtypes + legacy back-compat)
7. §6 — engine-side changes by file (incl. claudemd-author sub-agent dispatch, `/init`-style)
8. §7 — engine-emit hooks
9. §8 — slot-shape refusal
10. §9 — R-15-1 closure: subdomain dual-signal eligibility (forensically grounded)
11. §10 — test plan
12. §11 — tranche ordering with commit titles + gates
13. §12 — risk register
14. §13 — R-15-N defect closure mapping
15. §14 — pre-implementation triple-verification checklist (replaces prep §13)
16. §15 — spec `§Surface 6` rewrite (lands alongside engine implementation)

---

## §0. Verification protocol — the truncation failure mode

The prep-verification pass produced one factually-wrong anomaly out of seven: it claimed `ANALYSIS.md §7 doesn't exist` based on this grep:

```
grep -n "^## \|^### " ANALYSIS.md | head -25
```

The `head -25` truncated output before line 984 (where §7 lives). The verifier treated the truncated table-of-contents as authoritative for a negative-existence claim. That's the structural failure mode this section names so future verifiers don't repeat it.

**Rules**:

1. **Negative existence claims require unbounded reads.** Do not claim "X is not in file F" based on a `head -N` grep. Either read F in full, or use `grep -n PATTERN F` without piping to `head` and let the shell return all matches.
2. **Line-range cites require paginated reads.** Use `Read file_path=F offset=L limit=K` to verify a function spans lines L..L+K-1. Do not verify line ranges from a single grep match.
3. **Existence claims with sample evidence are fine.** "Function F exists at line L" backed by a single grep match is fine — the grep's output IS the evidence. Just don't extrapolate "and nothing else of this shape exists" without removing the truncation.
4. **Table-of-contents-style greps are navigation aids, not authoritative.** They tell you *where to look*; they do not tell you *what is absent*.
5. **Per-cite line-number verification.** Before depending on any "F at line L" cite in this guide, verify with `grep -n PATTERN F` (no head limit). Section anchors and function signatures shift across edits; line numbers can drift.
6. **Load-bearing claim verification.** Line-cite verification (rules 1-5) confirms whether a cited line still exists; it does NOT confirm whether the cited *mechanism* actually produces the claimed *output*. Two findings in this doc's prior revision passed the line-cite protocol while being load-bearing-wrong:

   - §5.3 claimed `tier_decision` engine-emit "from `tiers.go::Diff`." `Diff` exists at the cited line; the line cite passes. But `Diff` returns whole-tier deltas only — the per-service `tier_decision` shape requires a helper that didn't exist. Caught only by reading what `Diff` actually returns vs what `tier_decision` requires.
   - §6.8 claimed `validateCrossSurfaceDuplication` Jaccard ≥ 0.7 closes R-15-6. The validator could be implemented at the line. But Jaccard against the actual R-15-6 dup pairs in run-15 artifacts scored 0.14-0.16 — the threshold catches *zero* real dups. Caught only by computing Jaccard against actual artifacts.

   These are *content claims*, not *citation claims*. Verifying them requires running the cited mechanism (or its specification) against the cited input and checking the output matches.

   **Required verification for every "engine X produces Y" claim**: read what Y actually returns / contains; confirm Y carries the data needed to compute X; if not, identify the missing data source by name and add it to the tranche commit specification.

   **Required verification for every "validator V at threshold T catches case C" claim**: pull the actual C artifact; compute V's score on C using V's specification; confirm the score crosses V's threshold. If not, V's design is wrong — fix design before the commit lands.

   Tranche commits MUST include calibration evidence in the commit message for any new validator with a threshold, and MUST name every data source for any new engine-emit. "Reuses existing function" requires citing the function + the field path.

This guide's §14 checklist is structured so every step is unambiguous about which mode applies.

---

## §1. The shift in three sentences

Today every content surface is authored mid-deploy by the agent that's also wrestling the codebase to boot — content quality slips because the agent is wearing two hats. Tomorrow the deploy phases (scaffold + feature) write **only deploy-critical fields and code, plus structured `porter_change` / `field_rationale` / `tier_decision` facts capturing every non-obvious choice at densest context**; two new content phases (codebase content, env content) read those facts plus on-disk canonical content (spec, source, zerops.yaml) and synthesize all documentation surfaces with single-author / cross-surface-aware authoring. CLAUDE.md is authored by a peer `claudemd-author` sub-agent at phase 5 with a strictly Zerops-free brief. The engine emits structure (templates, slots) and refuses structurally-wrong fragment bodies at record-fragment time, so post-hoc validators that polish structure get retired in favour of slot-shape refusal at the source.

One new fact subtype (`tier_decision`) joins the prep's two (`porter_change`, `field_rationale`) for Surface 3 (env-import-yaml comments) coverage. CLAUDE.md (Surface 6) is authored by a dedicated sub-agent with a Zerops-free brief — no fact stream needed, no `operational_note` subtype. Operational class facts route to existing surfaces (zerops.yaml comments / README KB / code comments) without a new subtype.

---

## §2. The corrections that anchor this design

Each is grounded in run-15 forensic evidence (R-15-N defects), spec/engine code that the prep-verification pass surfaced, or design pivots from the walk-through review.

### §2.1 Workspace yaml vs deliverable yaml

Two distinct yaml shapes per [docs/zcprecipator3/system.md §"Workspace YAML vs deliverable YAML"](../system.md#L149) (lines 149-162):

- **Workspace yaml** (used at provision via `zerops_import`): services-only, no project block, dev runtimes carry `startWithoutCode: true`, no ports declared. Workspace yaml DOES include `enableSubdomainAccess: true` per [yaml_emitter.go:164,181](../../../internal/recipe/yaml_emitter.go) — but per the §9 forensic, that import-time intent does NOT flip `detail.SubdomainAccess` until first enable.
- **Deliverable yaml** (the published tier import.yaml; what end-users click-deploy): full structure, ports declared, `enableSubdomainAccess: true` per HTTP-serving service.

**R-15-1 closure** is detailed in §9 below — a separate engine fix that ORs the import-time intent signal with the deploy-time port signal so both end-user and recipe-authoring code paths auto-enable correctly.

### §2.2 Fields vs comments split

For zerops.yaml:

- **Fields** (`build:`, `run.envVariables:`, `ports:`, `initCommands:`, `start:`, `deployFiles:`, `readinessCheck:`) — must exist at scaffold time because [`zerops_deploy`](../../../internal/tools/) consumes them. Feature legitimately extends them. **Both phases need write access to the field layer.**
- **Comments** (the causal "why" prose above each block) — pure documentation. Has zero deploy-time consumer. The deploy works fine with no comments. **Can be deferred to a phase with full context.**

The same split applies to every authored surface (§4). The engine doesn't need to model yaml STRUCTURE (scaffold + feature can author fields fine); the engine needs to defer COMMENTS to a phase where the writer has full context.

### §2.3 Comments need full field context

Deferring comment authoring isn't enough — the writer needs to know what each field IS so it can explain WHY it was chosen. The "why" is currently in the deploy-phase agent's head and gets lost across phases unless captured.

Solution: the deploy phases record **structured facts** at the moment of writing. Content phase reads the facts + the committed yaml + the committed code + spec (on-disk) and authors documentation. Facts are the bridge.

### §2.4 Filtered fact recording — covers all routable classes

The agent does NOT record every code change as a fact. Recording rule mirrors the spec's [classification × surface compatibility table](../../spec-content-surfaces.md#classification--surface-compatibility) — only changes that classify into a class with a NON-EMPTY compatible-surface set become facts. Operational class facts route to existing surfaces (zerops.yaml comments / KB / code comments) without a dedicated subtype because Surface 6 (CLAUDE.md) is authored by a peer sub-agent with a Zerops-free brief and doesn't consume the deploy-phase fact stream (§2.6 below).

| Classification | Routes to surface | Fact subtype that carries it |
|---|---|---|
| platform-invariant | KB, IG | `porter_change` |
| intersection | KB | `porter_change` |
| scaffold-decision (config) — codebase yaml | Surface 7 (zerops.yaml comments) | `field_rationale` |
| scaffold-decision (config) — env import.yaml | Surface 3 (env-yaml comments) | `tier_decision` *(new)* |
| scaffold-decision (config) — IG | Surface 4 (when copyable) | `porter_change` |
| scaffold-decision (code) | Surface 4 (with diff) | `porter_change` |
| scaffold-decision (recipe-internal) | none | discard |
| operational | Surface 7 / KB / code-comment / DISCARD | (no fact subtype — anchor at the relevant site) |
| framework-quirk / library-metadata / self-inflicted | none | discard |

**Why no `operational_note` fact subtype**:

- **Surface 6 (CLAUDE.md) is authored by the dedicated `claudemd-author` sub-agent** (§2.6). The deploy-phase fact stream does NOT feed CLAUDE.md authoring — the sub-agent reads the codebase directly via Read/Glob/Bash and authors generic `/init`-shape content from what it observes.
- Operational content with a Zerops anchor (e.g. "execOnce burns the key on a crashed init — touch source to mint fresh appVersionId") belongs in the corresponding `zerops.yaml` comment. That makes it a `field_rationale` fact, not a separate subtype.
- Operational content without a Zerops anchor (e.g. "nest start --watch sub-second rebuilds") belongs in `/init`-style CLAUDE.md and is sub-agent-authored from package.json scripts the agent reads directly.
- Operational content that's a porter-relevant Zerops platform trap (e.g. "execOnce honors exit 0; a silently-failing init returns success") belongs in README KB → `porter_change` fact.
- Repository-internal operational facts ("the seed script re-pushes to Meilisearch as a recovery lever") belong in code comments at the relevant site.

The prep-doc's `operational_note` subtype was introduced because the prep coupled CLAUDE.md authoring with the deploy-phase agent's fact stream. With the dedicated `claudemd-author` sub-agent reading the codebase directly, the subtype's purpose collapses.

### §2.5 IG taxonomy with bundled-class caveat (ANOMALY 2 FIX)

Four IG classes derived from references; the prep §3.2 erroneously listed Jetstream IG #2 "Add Support For Object Storage" under Class B (universal-for-role) when it's pure Class C (per managed service consumed). The corrected taxonomy — and a structural caveat about Class B bundled inside Class C headings — is in §3 below.

### §2.6 CLAUDE.md is authored by a dedicated sub-agent in `/init` shape (DESIGN PIVOT)

[docs/spec-content-surfaces.md §Surface 6](../../spec-content-surfaces.md#L221-L266) carries the right TEST:

> *"Is this useful for operating THIS repo specifically — not for deploying it to Zerops, not for porting it to other code?"*

But the prescribed STRUCTURE puts "Zerops" in two of three section headings:

```
## Zerops service facts
## Zerops dev (hybrid)
## Notes
```

The reference recipes converged on this Zerops-flavored structure and it propagated through every dogfood run. Run-15 apidev/CLAUDE.md is the perfect example — hostname / port / managed-service wiring / migrations all there, but those are facts a porter or AI agent gets from `Read zerops.yaml` + `Glob src/**/*.controller.ts`. Duplicating them in CLAUDE.md was wrong.

**The new CLAUDE.md contract**: authored by a **dedicated sub-agent** at phase 5, dispatched in parallel alongside the codebase-content sub-agent, with a tightly-scoped Zerops-free brief. Output is `/init`-shaped — three sections: project overview, build & run, architecture.

```markdown
# {codebase-hostname}

{1-2 sentence framing — framework, version, what this codebase does}

## Build & run

- `npm install` — install dependencies
- `npm run start:dev` — dev with hot reload
- {... per package.json / composer.json scripts, with one-line labels}

## Architecture

- `src/<entry>` — {one-line label}
- `src/<dir>/` — {one-line label per framework convention}
- {... top-level src/ entries}
```

**Why a dedicated sub-agent, not engine-emit**: engine-emit would require either a per-framework detector registry (catalog-drift at recipe-portfolio scale: hundreds of frameworks → hundreds of detectors + fixture tests, hand-curated) or a labels-free minimal emit (loses the framework-specific polish that makes CLAUDE.md actually useful). A dedicated sub-agent reads the codebase, knows the framework directly (this is exactly what `claude /init` does on every framework every day — strongest LLM skill), and produces `/init`-quality output for ANY framework with zero engine-side framework knowledge. Brief stays Zerops-free so the bleed-through that drove run-15's wrong shape ("`## Zerops service facts`") doesn't recur.

**Why a SEPARATE sub-agent from codebase-content**: the codebase-content brief is necessarily Zerops-aware (it's authoring IG / KB / zerops.yaml comments — Zerops-platform content). Layering CLAUDE.md authoring into the same brief lets that Zerops context bleed into the CLAUDE.md output — exactly what happened in run-15. A peer-dispatched sub-agent with a **strict Zerops-free brief** ("you are writing CLAUDE.md for a porter cloning this repo; do NOT include Zerops platform content, managed-service hostnames, `zsc` / `zerops_*` tool names, dev-loop instructions — those live in IG/KB/zerops.yaml comments authored by a sibling agent") solves it cleanly. Validator backstops the prohibition.

No engine-side framework registry. No `emitCodebaseClaudeMD`. The Zerops integration is in IG (Surface 4) / KB (Surface 5) / zerops.yaml comments (Surface 7) — the surfaces porters actually need when bringing their own code.

The spec's §Surface 6 needs corresponding rewrite (§15 below) so the contract aligns with the sub-agent-authored shape.

### §2.7 TEACH / DISCOVER alignment audit

system.md §4 frames the question (TEACH = same for every recipe + positive shape; DISCOVER = recipe-varies or expressible only as a phrase ban). It's a useful summary but not a religious test — when first-principles reasoning lands somewhere else, follow the reasoning. Each run-16 engine-side artifact below carries its own argument so the audit doesn't reduce to "system.md says so":

| Run-16 artifact | Side | First-principles argument |
|---|---|---|
| Sub-agent-authored CLAUDE.md (`/init` shape via dedicated dispatch, §6.7) | DISCOVER (per-recipe) | Engine-emit was the original §2.6 pivot but a per-framework detector registry would catalog-drift at recipe-portfolio scale (hundreds of frameworks). A dedicated `claudemd-author` sub-agent at phase 5 reads the codebase, knows the framework directly, and produces `/init`-quality output for ANY framework with zero engine-side framework knowledge. Validator backstops the contract (3-section shape; zero Zerops content). |
| Slot-shape refusal at record-fragment (§8) | TEACH | Slot caps (≤ N lines, exactly one heading) are positive constraints, not phrase patterns. Refusal at record-time gives the agent same-context recovery; refusal at finalize gives the agent post-hoc surprise. Same content moved closer to the author. |
| Engine-emit Class B "Bind 0.0.0.0 + trust L7" (§7.1) | TEACH | Every HTTP-serving role on Zerops faces this regardless of framework. The why-prose is mechanism-level (L7 balancer routes to VXLAN IP); framework syntax is the diff slot the agent fills. Engine emits one fact, agent writes one diff line. |
| Engine-emit Class B "Drain on SIGTERM" (§7.1) | TEACH | Rolling deploys send SIGTERM regardless of framework; nodejs roles need explicit handling. Same shape as bind/trust. |
| Engine-emit Class B worker "no HTTP surface" (§7.1) | TEACH (defensible) | Why-prose is mechanism-level (worker has no HTTP listener, framework's default bootstrap binds anyway). **Heading is agent-filled, not engine-emitted**: in our portfolio today every worker is NestJS, but BullMQ / Celery / plain-process workers are likely future shapes — the agent is in the codebase and knows the framework, so the engine should not pre-name it. Cost of agent-filled heading: one extra synthesis step. Cost of hardcoded NestJS: one PR every time the heading is wrong. |
| Engine-emit Class C own-key-aliases umbrella (§7.1) | TEACH | The `${db_hostname}` cross-service pattern IS the platform contract; same for every recipe consuming any managed service. One umbrella fact covers the pattern; per-service alias names are agent-derived from `zerops_discover`. |
| Per-service connection idiom — engine pre-seeds **fact shell**, agent fills Why (§7.2 rewritten) | Hybrid | The structural shape (one IG item per consumed managed service) is recipe-stable — engine pre-seeds the fact shell with topic + citation-guide ID + heading slot. The Why-prose is service × runtime × version specific — the per-service knowledge atom is the single source of truth, agent fetches via `zerops_knowledge runtime=<type>` and fills Why on the pre-seeded shell. Avoids two failure modes: (a) engine-side hint table that grows combinatorially and drifts from atoms; (b) agent forgetting to record per-service IG items. |
| Engine-emit `tier_decision` facts from `tiers.go::Diff` (§5.3) | TEACH | Per-tier capability matrix delta is deterministic from `Tiers` data; pre-emit reduces agent recording surface. Agent-extended `tierContext` slot is the discovery half. |
| `validateCrossSurfaceDuplication` validator (§6.8) | TEACH (defensible), **Notice not blocking** | Jaccard ≥ 0.7 is a heuristic; misfires would gate publication on a brittle similarity check. The primary R-15-6 closure is the structural fix in §4 (subagent sees both IG and KB candidate lists in one phase). A heuristic validator should not be the gate when the primary fix is structural — it's a backstop signal, not a tripwire. |
| `validateCrossRecipeDuplication` (§6.8) | TEACH (defensible), Notice | Same structural shape, parent-recipe scope. Notice from inception. |
| Subdomain dual-signal eligibility (§9) | TEACH | Platform-mechanics fix: read both REST-authoritative signals (`detail.SubdomainAccess` + `Ports[].HTTPSupport`) and OR them. No catalog. |

**Anti-dogma footnote**: where this audit deviates from system.md §4's strict TEACH/DISCOVER literalism (the per-service connection-idiom row is the clearest case), the deviation is argued, not asserted. system.md is the summary doc; first-principles reasoning is the load-bearing one. When this audit and system.md disagree, the better-argued side wins, and system.md gets corresponding update at tranche 7 commit 2.

---

## §3. The empirically-derived IG taxonomy

Derived from three recipes:

- [/Users/fxck/www/laravel-jetstream-app/README.md](../../../../laravel-jetstream-app/README.md) — 4 IG items (human-authored)
- [/Users/fxck/www/laravel-showcase-app/README.md](../../../../laravel-showcase-app/README.md) — 5 IG items, 7 KB bullets (early recipe-flow)
- [/Users/fxck/www/zcp/docs/zcprecipator3/runs/15/apidev/README.md](../runs/15/apidev/README.md) — 5 numbered IG items + 1 unnumbered, 8 KB bullets (run-15 dogfood)

### §3.1 Class A — Engine-emittable structural item

**One per codebase: IG #1 = "Adding `zerops.yaml`".**

Engine reads the committed `<cb.SourceRoot>/zerops.yaml` from disk, generates a 1-2-sentence intro, embeds the yaml verbatim in a fenced code block. No agent authoring at all.

Already implemented at [internal/recipe/assemble.go:170::injectIGItem1](../../../internal/recipe/assemble.go#L170) and [:204::codebaseIGItem1](../../../internal/recipe/assemble.go#L204). Intro generated by [yamlIntroSentence:223](../../../internal/recipe/assemble.go#L223).

**No change needed.** Class A is the engine-emit IG #1 precedent. Other engine-emit on the read-time surfaces (per-service umbrella facts, fact shells, tier_decisions) follows the same shape; CLAUDE.md was originally a candidate but pivoted to dedicated sub-agent authoring per §2.6.

### §3.2 Class B — Universal-for-role

**Per HTTP-serving codebase (RoleAPI, RoleFrontend, RoleMonolith), N items where N = the platform contract's surface area for that role.**

Genuine Class B examples from the audit (corrected — Object Storage moved to Class C):

| Recipe | Item | Source |
|--------|------|--------|
| Showcase IG #2 | "Trust the reverse proxy" — `$middleware->trustProxies(at: '*')` in `bootstrap/app.php` | showcase README §2 |
| Run-15 apidev IG #2 | "Bind `0.0.0.0` and trust the L7 proxy" | run-15 apidev README §2 |
| Run-15 apidev IG #5 | "Drain in-flight requests on SIGTERM" | run-15 apidev README §5 |

Pattern: every HTTP-serving role gets these contracts independently of framework. Framework-specific syntax IS the diff slot; the why prose is engine-knowable.

**Engine knows from `cb.Role` and `cb.BaseRuntime`**:

- Role=API/Frontend/Monolith → "Bind 0.0.0.0 + trust L7" applies
- Role=API/Frontend/Monolith + nodejs → "Drain on SIGTERM" applies
- Role=API/Frontend/Monolith + php-nginx → no SIGTERM item (PHP-FPM handles it)
- Role=Worker → "Boot application context, not HTTP server" applies (NestJS pattern)

**Engine-emit decision: YES.** Engine pre-emits a `porter_change` fact per applicable item; agent fills only the framework-specific code-diff slot.

#### Structural caveat: Class B bundled inside Class C heading

Human-authored recipes don't always factor cleanly. **Jetstream has no pure Class B item** — its TRUSTED_PROXIES teaching is bundled inside IG #3 "Utilize Environment Variables" (which is primarily Class C). Phase 5 codebase-content sub-agent should not require a pure Class B heading; absorbing Class B into a Class C heading is a valid synthesis choice when the content is denser that way.

Codebase-content brief (§6.2 below) phrases this as: *"prefer pure-class headings when the content density supports it; bundle Class B teaching inside a Class C heading when both items reinforce each other (jetstream IG #3 is the precedent — TRUSTED_PROXIES taught alongside `${db_hostname}` cross-service refs)."*

### §3.3 Class C — Universal-for-recipe (per managed service consumed)

**Per managed service the codebase consumes, one IG item teaching how to connect.**

| Recipe | Item | Source |
|--------|------|--------|
| Jetstream IG #2 | "Add Support For Object Storage" — `composer require league/flysystem-aws-s3-v3` | jetstream §2 |
| Showcase IG #3 | "Configure Redis client" — `composer require predis/predis`, `REDIS_CLIENT=predis` | showcase §3 |
| Showcase IG #4 | "Configure S3 object storage" — `composer require league/flysystem-aws-s3-v3`, path-style | showcase §4 |
| Showcase IG #5 | "Configure Meilisearch search" — `composer require laravel/scout meilisearch/meilisearch-php` | showcase §5 |
| Run-15 apidev IG #4 | "Connect to NATS with credentials as connect options" — connect-options syntax not URL-embedded | run-15 §4 |
| Run-15 apidev IG #3 | "Read managed-service credentials from own-key aliases" — DB_HOST: ${db_hostname} pattern | run-15 §3 |

Pattern: per-service idiom. Engine knows the service type from `plan.Services`; per-service connection idioms live in `zerops_knowledge runtime=<type>` content.

**Engine-emit decision: SPLIT (umbrella + fact-shell hybrid).**

- **The "own-key-aliases" platform contract IS engine-emitted** as a single Class C umbrella fact (§7.1) — the `${db_hostname}` cross-service pattern is the same for every recipe consuming managed services regardless of which services they are.
- **The per-service connection idiom uses the fact-shell pattern.** Engine pre-seeds an empty-Why `porter_change` fact for each managed service the codebase consumes, populated with `topic` + `candidateHeading` slot + `citationGuide=<topic-id>` + `library` suggestion. The agent calls `zerops_knowledge runtime=<svc-type>` and fills Why on the pre-seeded shell via the new `fill-fact-slot` action (§6.4). Why this hybrid: the structural shape (one fact per consumed managed service) is recipe-stable, so engine pre-seed prevents the agent from forgetting; the why-prose is service × runtime × version specific, so the atom (single source) is canonical and engine-side curated prose would only duplicate it. Avoids the catalog-drift trap of mirroring atom content into hand-curated engine code.

### §3.4 Class D — Framework × scenario (feature-discovered)

**Items only relevant because of a specific scenario the recipe demonstrates.**

| Recipe | Item | Why it's scenario-specific |
|--------|------|----------------------------|
| Jetstream IG #4 | "Setup Production Mailer" — change MAIL_* for real SMTP | Only relevant if porter wants production mail (jetstream demo uses Mailpit) |
| Run-15 apidev (unnumbered) | "Custom response headers across origins" — `app.enableCors({ exposedHeaders: ['X-Cache'] })` | Only relevant because the cache panel exposes X-Cache cross-origin |
| Run-15 appdev (unnumbered) | "Streamed proxy bodies need duplex: 'half'" | Only relevant because there's a same-origin proxy in this recipe |

Pattern: emerges from feature-phase work. Engine doesn't know these a priori — they surface when feature sub-agent observes the trap.

**Engine-emit decision: NO.** Class D facts are ALWAYS agent-recorded during feature phase as `porter_change` facts. The classification + candidate routing is the agent's call (some Class D items are KB-class — see R-15-6 — others are IG-class). The content sub-agent decides routing using spec §Cross-surface duplication.

### §3.5 Summary table

| Class | Source | Engine knows? | Author | Per-codebase count typical |
|-------|--------|---------------|--------|----------------------------|
| A — Engine-emittable | Committed yaml | ✓ Fully | Engine (no agent) | 1 (always IG #1) |
| B — Universal-for-role | `cb.Role` × `cb.BaseRuntime` | ✓ Why prose; ✗ framework-specific diff | Engine pre-fills fact; agent fills diff slot | 1-3 (or 0 if bundled into C) |
| C — Universal-for-recipe (umbrella) | `len(plan.Services) > 0` | ✓ Fully (own-key-aliases pattern) | Engine pre-fills umbrella fact | 1 (covers all consumed services) |
| C — Per-service idiom | Engine pre-seeds shell from `plan.Services`; agent fills Why from `zerops_knowledge runtime=<svc-type>` | ✓ Shape; ✗ Why-prose | Engine pre-seeds shell, agent fills via `fill-fact-slot` | 1 per managed service consumed |
| D — Framework × scenario | Agent observation during feature phase | ✗ Unknown a priori | Agent records porter_change fact; content sub-agent classifies + routes (IG or KB) | 0-2 |

Per-codebase IG total = 1 (A) + 0-3 (B) + 1-N (C) + 0-2 (D). Spec cap is 5.

---

## §4. The proposed phase shape

7 phases (5 today + 2 new):

```
1 Research          | main agent           | plan, contracts            | (no fragments)
2 Provision         | main agent           | workspace yaml + import    | (no fragments)
                    |                      |                            | tier_decision facts
                    |                      |                            | (research-phase rationale)
3 Codebase deploy   | sub × N (parallel)   | code + zerops.yaml fields  | (no fragments)
                    |                      |                            | porter_change facts
                    |                      |                            | field_rationale facts
4 Feature deploy    | sub × 1              | feature code + yaml field  | (no fragments)
                    |                      | extensions                 | porter_change facts
                    |                      |                            | field_rationale facts
5 Codebase content  | sub × N codebase-    | (no code changes)          | codebase/<h>/intro
                    |  content (parallel)  |                            | codebase/<h>/integration-guide/<n>
                    |  + sub × N           |                            | codebase/<h>/knowledge-base
                    |  claudemd-author     |                            | codebase/<h>/zerops-yaml-comments/<block>
                    |  (parallel; peer)    |                            | codebase/<h>/claude-md (claudemd-author only)
6 Env content       | sub × 1              | (no code changes)          | root/intro
                    |                      |                            | env/<N>/intro × 6
                    |                      |                            | env/<N>/import-comments/* × 54
7 Finalize          | main                 | stitch                     | (validator iterations only)
```

### §4.1 What changes vs today

- **Phases 3 + 4 stop authoring fragments.** Records facts instead.
- **Phases 5 + 6 are new.** Phase 5 dispatches **two parallel sub-agents per codebase**: `codebase-content` (Zerops-aware: IG / KB / zerops.yaml comments / intro) and `claudemd-author` (Zerops-free: CLAUDE.md `/init`-shape only). Today's finalize phase becomes phase 7 (stitch + validate only).
- **Three fact subtypes:** `porter_change`, `field_rationale`, `tier_decision`. Existing `Kind=""` platform-trap shape preserved as back-compat.
- **CLAUDE.md is sub-agent-authored** by a dedicated `claudemd-author` peer dispatched alongside `codebase-content` in phase 5 (§2.6, §6.7). Brief is Zerops-free; validator confirms shape held + zero Zerops content.
- **Engine pre-emits universal-for-role and universal-for-recipe facts** at scaffold dispatch. Agent fills only framework-specific slots.

### §4.2 Parent-recipe context threads through phase 5/6 (GAP B FIX)

Today's [BuildScaffoldBriefWithResolver:144](../../../internal/recipe/briefs.go#L144) takes a `parent *ParentRecipe` argument and threads it. Phase 5/6 brief composers MUST accept the same parameter. Concrete impact: jetstream-as-child-of-laravel-minimal would re-author Surface 4 IG #2 "Trust the reverse proxy" if the parent already covers it; cross-surface duplication validator (R-15-6 closure) only catches duplication within the same recipe, not parent/child duplication.

Brief composer signatures:

```go
func BuildCodebaseContentBrief(plan *Plan, codebase Codebase, parent *ParentRecipe) (Brief, error)
func BuildEnvContentBrief(plan *Plan, parent *ParentRecipe) (Brief, error)
```

When `parent != nil`, the brief includes a parent-recipe-pointer block: parent slug + paths to parent's published surfaces, with the instruction *"Read these and cross-reference instead of re-author."* Sub-agent fetches parent content on demand via Read.

---

## §5. The fact schema extension

Three fact subtypes (`porter_change`, `field_rationale`, `tier_decision`) plus the existing `Kind=""` platform-trap shape preserved as back-compat. No `operational_note` subtype (§2.4 + §2.6 — operational class facts route to existing surfaces).

### §5.1 `porter_change` fact

Captures: a code change a porter would have to make. Recorded at the moment the deploy-phase agent makes the change.

```jsonc
{
  "topic": "apidev-cors-expose-x-cache",
  "kind": "porter_change",
  "scope": "apidev/code/src/main.ts",
  "phase": "feature",
  "changeKind": "code-addition",
  "library": "@nestjs/common",
  "diff": "app.enableCors({ origin, credentials: true, exposedHeaders: ['X-Cache'] });",
  "why": "Browsers strip every non-CORS-safelisted response header from cross-origin JS reads unless the server explicitly lists them in Access-Control-Expose-Headers. Without exposedHeaders, fetch(...).headers.get('x-cache') returns null even when the server sets X-Cache: HIT.",
  "candidateClass": "intersection",
  "candidateHeading": "Custom response headers across origins",
  "candidateSurface": "CODEBASE_KB",
  "citationGuide": "http-support",
  "engineEmitted": false
}
```

**Recording rule**: classification ∈ {`platform-invariant`, `intersection`, `scaffold-decision (config)`, `scaffold-decision (code)`} AND candidateSurface compatible with class per spec table.

### §5.2 `field_rationale` fact

Captures: a non-obvious yaml field's reasoning at codebase scope. Surface 7 (per-codebase zerops.yaml comments). Includes operational-class facts that have a yaml-field anchor (e.g. "execOnce key burns on crashed init" anchors to `run.initCommands`).

```jsonc
{
  "topic": "apidev-s3-region-us-east-1",
  "kind": "field_rationale",
  "scope": "apidev/zerops.yaml/run.envVariables.S3_REGION",
  "phase": "scaffold",
  "fieldPath": "run.envVariables.S3_REGION",
  "fieldValue": "us-east-1",
  "why": "us-east-1 is the only region MinIO accepts. The value is ignored downstream but every S3 SDK requires it set.",
  "alternatives": "Setting any other region throws SignatureDoesNotMatch on first bucket call.",
  "candidateClass": "scaffold-decision-config",
  "candidateSurface": "CODEBASE_ZEROPS_COMMENTS",
  "citationGuide": "object-storage"
}
```

**Compound-decision support**: when a comment block reasons across multiple fields (e.g. run-15 apidev's "Two separate `execOnce` keys so a seed failure does not roll back the schema migration" reasons over `initCommands[0]` and `initCommands[1]` jointly), the agent records two `field_rationale` facts with a shared `compoundReasoning` slot:

```jsonc
{
  "topic": "apidev-execonce-distinct-keys",
  "kind": "field_rationale",
  "scope": "apidev/zerops.yaml/run.initCommands",
  "fieldPath": "run.initCommands[0]",
  "fieldValue": "zsc execOnce ${appVersionId}-migrate ...",
  "why": "Per-deploy gate: every container in the rolling deploy races for the key; only the winner runs the body.",
  "compoundReasoning": "Two separate keys so a seed failure does not roll back the schema migration; each command owns its own key.",
  ...
}
```

The content-phase sub-agent merges facts sharing a `compoundReasoning` slot into a single zerops.yaml comment block.

### §5.3 `tier_decision` fact (closes Gap A.2)

Captures: tier-scaling / tier-mode rationale at the env-import-yaml level. Surface 3 (env import.yaml comments) authored at phase 6.

```jsonc
{
  "topic": "tier-4-db-non-ha",
  "kind": "tier_decision",
  "scope": "env/4/services.db",
  "phase": "research",
  "tier": 4,
  "service": "db",
  "fieldPath": "services[name=db].mode",
  "chosenValue": "NON_HA",
  "alternatives": "HA (replicated, 2x cost) — chosen at tier 5",
  "tierContext": "Tier 4 audience: small-prod single-region. Cost ceiling forces NON_HA at this tier; HA promotion lives at tier 5 where the audience accepts the failover-cost trade.",
  "candidateClass": "scaffold-decision-config",
  "candidateSurface": "ENV_IMPORT_COMMENTS",
  "candidateHeading": "PostgreSQL NON_HA at tier 4"
}
```

**Recording: 100% engine pre-emit.** The agent does not record `tier_decision` facts directly. Engine pre-emits per-tier capability deltas from two data sources:

1. **Whole-tier deltas** — runtime/CPU/RAM fields, computed via [tiers.go:113-129::Diff](../../../internal/recipe/tiers.go#L113-L129). `Diff` returns `TierDiff{Changes []FieldChange}` covering `RunsDevContainer`, `ServiceMode` (whole-tier baseline), `RuntimeMinContainers`, `CPUMode`, `CorePackage`, `MinFreeRAMGB`, `RuntimeMinRAM`, `ManagedMinRAM`. Each `FieldChange` becomes one `tier_decision` fact with empty `service` field (whole-tier scope).

2. **Per-service mode deltas** — for the "Postgres NON_HA at tier 4 vs HA at tier 5" shape, **a new helper is required**: `Diff` does NOT carry per-service detail (verified at tiers.go:121 — `add("ServiceMode", ...)` emits one whole-tier change, not one per service). The new helper reuses the per-service downgrade logic that already lives in [yaml_emitter.go:319-327](../../../internal/recipe/yaml_emitter.go#L319-L327) (where `mode == "HA" && !svc.SupportsHA && !managedServiceSupportsHA(svc.Type) → "NON_HA"` at emit time) and the per-service capability combination already used by [briefs_tier_facts.go:107-108](../../../internal/recipe/briefs_tier_facts.go#L107-L108):

   ```go
   // internal/recipe/tier_service_deltas.go (NEW FILE)

   type ServiceModeDelta struct {
       Service string // hostname, e.g. "db" / "cache" / "search"
       From    string // "NON_HA" or "HA"
       To      string
   }

   // TierServiceModeDelta returns per-service mode changes between two tiers,
   // applying the same downgrade logic as yaml_emitter.go:325.
   func TierServiceModeDelta(fromTier, toTier Tier, plan *Plan) []ServiceModeDelta {
       resolveServiceMode := func(t Tier, svc Service) string {
           mode := t.ServiceMode
           if mode == "HA" && !svc.SupportsHA && !managedServiceSupportsHA(svc.Type) {
               return "NON_HA"
           }
           return mode
       }
       var deltas []ServiceModeDelta
       for _, svc := range plan.Services { // only managed services consumed by this plan
           from := resolveServiceMode(fromTier, svc)
           to   := resolveServiceMode(toTier, svc)
           if from != to {
               deltas = append(deltas, ServiceModeDelta{Service: svc.Hostname, From: from, To: to})
           }
       }
       sort.Slice(deltas, func(i, j int) bool { return deltas[i].Service < deltas[j].Service })
       return deltas
   }
   ```

   For each `ServiceModeDelta`, engine emits one `tier_decision` fact with `service=delta.Service` + `chosenValue=delta.To` + auto-derived `tierContext` from `Tier.AudienceLine` + the family-table reasoning. Phase 6 sub-agent extends `tierContext` via `fill-fact-slot` if the auto-derived prose is insufficient.

This removes the ambiguity about agent recording timing the prep had — research phase doesn't see per-tier yaml shapes; provision phase emits workspace yaml (single shape, no tier ladder); there's no natural agent recording site. Engine-only emit is the clean answer.

**Tranche 2 commit 3 specification dependency**: `TierServiceModeDelta` (~30 LoC + 4 tests) lands in the same commit as `tier_decision` engine pre-emit. Pre-existing claim "from `tiers.go::Diff`" was incorrect about the data source; this is the corrected specification.

### §5.4 `contract` fact (cross-codebase)

Captures: a contract between codebases (NATS subject schema, route paths, payload shapes). Recorded by main agent at research phase OR by deploy-phase agent when the contract surfaces during code authoring.

```jsonc
{
  "topic": "nats-items-created-contract",
  "kind": "contract",
  "scope": "cross-codebase/contract",
  "phase": "research",
  "publishers": ["api"],
  "subscribers": ["worker"],
  "subject": "items.created",
  "queueGroups": ["worker-indexer"],
  "payloadSchema": "{ id: uuid, name: string, createdAt: ISO8601 }",
  "purpose": "Worker mirrors items into Meilisearch on create"
}
```

**Use site**: read by all codebase-content sub-agents in phase 5 — both publisher and subscriber sides see the contract; KB / IG references the same contract from both sides without divergence. Not load-bearing — KB can be authored without it (the contracts live in code at the publish/subscribe sites and are findable via Glob); but contracts improve cross-codebase wording awareness.

### §5.5 Schema extension (polymorphic FactRecord)

```go
type FactRecord struct {
    // ─── Existing fields (preserved; required for Kind="" platform-trap) ───
    Topic       string            `json:"topic"`
    Symptom     string            `json:"symptom,omitempty"`     // → optional with Kind extension
    Mechanism   string            `json:"mechanism,omitempty"`   // → optional with Kind extension
    SurfaceHint string            `json:"surfaceHint,omitempty"` // → optional with Kind extension
    Citation    string            `json:"citation,omitempty"`    // → optional with Kind extension
    FailureMode string            `json:"failureMode,omitempty"`
    FixApplied  string            `json:"fixApplied,omitempty"`
    Evidence    string            `json:"evidence,omitempty"`
    Scope       string            `json:"scope,omitempty"`
    RecordedAt  string            `json:"recordedAt,omitempty"`
    Author      string            `json:"author,omitempty"`
    Extra       map[string]string `json:"extra,omitempty"`

    // ─── Discriminator + per-Kind fields ───
    Kind string `json:"kind,omitempty"` // "" = platform-trap (back-compat); "porter_change" | "field_rationale" | "tier_decision" | "contract"

    // PorterChange (Kind=porter_change):
    Phase            string `json:"phase,omitempty"`
    ChangeKind       string `json:"changeKind,omitempty"`
    Library          string `json:"library,omitempty"`
    Diff             string `json:"diff,omitempty"`
    Why              string `json:"why,omitempty"`
    CandidateClass   string `json:"candidateClass,omitempty"`
    CandidateHeading string `json:"candidateHeading,omitempty"`
    CandidateSurface string `json:"candidateSurface,omitempty"`
    CitationGuide    string `json:"citationGuide,omitempty"`
    EngineEmitted    bool   `json:"engineEmitted,omitempty"`

    // FieldRationale (Kind=field_rationale):
    FieldPath         string `json:"fieldPath,omitempty"`
    FieldValue        string `json:"fieldValue,omitempty"`
    Alternatives      string `json:"alternatives,omitempty"`
    CompoundReasoning string `json:"compoundReasoning,omitempty"` // shared across multi-field comment blocks

    // TierDecision (Kind=tier_decision):
    Tier        int    `json:"tier,omitempty"`
    Service     string `json:"service,omitempty"`
    ChosenValue string `json:"chosenValue,omitempty"`
    TierContext string `json:"tierContext,omitempty"`

    // Contract (Kind=contract):
    Publishers    []string `json:"publishers,omitempty"`
    Subscribers   []string `json:"subscribers,omitempty"`
    Subject       string   `json:"subject,omitempty"`
    QueueGroups   []string `json:"queueGroups,omitempty"`
    PayloadSchema string   `json:"payloadSchema,omitempty"`
    Purpose       string   `json:"purpose,omitempty"`
}
```

**Validate() extension**: dispatch on `Kind`. `Kind=""` validates as platform-trap (back-compat, requires Symptom/Mechanism/SurfaceHint/Citation). New Kind values validate against per-Kind required fields.

### §5.6 FactsLog reader migration audit

Every consumer of `FactRecord` must be audited and confirmed/migrated:

| Consumer | Location | Behaviour today | Behaviour required post-migration |
|---|---|---|---|
| `FilterByHint` | [facts.go:134-145](../../../internal/recipe/facts.go#L134-L145) | Filters on `SurfaceHint` (legacy field) | New records have empty SurfaceHint by design — returns empty for new records. Add `FilterByKind(records, kind string) []FactRecord` for the new path. Document that FilterByHint is platform-trap-only. |
| `ClassifyWithNotice` | [classify.go::ClassifyWithNotice](../../../internal/recipe/classify.go), called from [handlers.go:295-297](../../../internal/recipe/handlers.go#L295-L297) | Reads classification from legacy fields | Audit: does it crash on empty Symptom/Mechanism/SurfaceHint when Kind != ""? Add early-return for non-empty Kind (only platform-trap classification matters here). |
| `BuildFinalizeBrief` | [briefs.go:339](../../../internal/recipe/briefs.go#L339) | Reads facts via `FactsLog.Read()`; consumes platform-trap shape | Becomes stitch+validate only (§6.2). Old consumers retire. |
| `BuildScaffoldBrief` / `BuildFeatureBrief` | [briefs.go:144,267](../../../internal/recipe/briefs.go) | Don't read facts (record-only) | No change. |
| New `BuildCodebaseContentBrief` | (new) | Reads facts via `FilterByKind(records, "porter_change")` etc. | New consumer; uses new path. |
| New `BuildEnvContentBrief` | (new) | Reads `tier_decision` facts via `FilterByKind` | New consumer. |
| `Validate` | [facts.go:40-54](../../../internal/recipe/facts.go#L40-L54) | Requires Symptom/Mechanism/SurfaceHint/Citation | Extended dispatch on Kind (§5.5 above). |
| `FactsLog.Append` | [facts.go:75-98](../../../internal/recipe/facts.go#L75-L98) | Calls Validate; writes JSONL | No change (Validate handles Kind dispatch). |
| `FactsLog.Read` | [facts.go:102-132](../../../internal/recipe/facts.go#L102-L132) | Decodes JSONL into FactRecord | No change (extra fields decode as zero values). |

Validation step: every consumer above requires a test exercising both Kind="" (legacy) and Kind="porter_change" / "field_rationale" / "tier_decision" inputs.

---

## §6. Engine-side changes by file

### §6.1 [internal/recipe/workflow.go](../../../internal/recipe/workflow.go)

**Current** ([workflow.go:14-22](../../../internal/recipe/workflow.go#L14-L22)):

```go
const (
    PhaseResearch  Phase = "research"
    PhaseProvision Phase = "provision"
    PhaseScaffold  Phase = "scaffold"
    PhaseFeature   Phase = "feature"
    PhaseFinalize  Phase = "finalize"
)
```

**Change**: extend Phase enum.

```go
const (
    PhaseResearch         Phase = "research"
    PhaseProvision        Phase = "provision"
    PhaseScaffold         Phase = "scaffold"
    PhaseFeature          Phase = "feature"
    PhaseCodebaseContent  Phase = "codebase-content" // NEW
    PhaseEnvContent       Phase = "env-content"     // NEW
    PhaseFinalize         Phase = "finalize"
)
```

Adjacent-forward order updates: research → provision → scaffold → feature → codebase-content → env-content → finalize.

**Tests**:

- New: `TestPhase_AdjacentForward_CodebaseContentAfterFeature`
- New: `TestPhase_AdjacentForward_EnvContentAfterCodebaseContent`
- New: `TestPhase_AdjacentForward_FinalizeAfterEnvContent`
- Update: any test that asserts the 5-phase ordering.

**Risk**: phase-switch sites at [phase_entry.go:29::gatesForPhase](../../../internal/recipe/phase_entry.go#L29) and [phase_entry.go:12::loadPhaseEntry](../../../internal/recipe/phase_entry.go#L12) must add cases for the two new phases.

### §6.2 [internal/recipe/briefs.go](../../../internal/recipe/briefs.go) — pointer-based composition

**Current**: 3 brief composers — [BuildScaffoldBriefWithResolver:144](../../../internal/recipe/briefs.go#L144), [BuildFeatureBrief:267](../../../internal/recipe/briefs.go#L267), [BuildFinalizeBrief:339](../../../internal/recipe/briefs.go#L339).

**Composition principle**: subagents have `Read`, `Glob`, `Bash` access. The brief carries session state (filtered facts, codebase metadata, fragment slots) plus pointers to canonical files. Subagents Read the canonical content (spec, content-research, source, parent surfaces) on demand at the authoring moment. This collapses brief size from prep's ~80 KB/dispatch estimate to ~25-29 KB/dispatch and matches today's working pattern (scaffold brief embeds principle atoms ~5 KB, points at zerops_knowledge for managed-service idioms).

**Changes**:

1. **Strip content-authoring atoms from scaffold brief.** [`content/briefs/scaffold/content_authoring.md`](../../../internal/recipe/content/briefs/scaffold/content_authoring.md) (15.3 KB) drops; replaced by ~2 KB `decision_recording.md`. Net brief size: -13 KB.

2. **Strip content-extension atoms from feature brief.** [`content/briefs/feature/content_extension.md`](../../../internal/recipe/content/briefs/feature/content_extension.md) (7.4 KB) drops; replaced by ~2 KB `decision_recording.md`. Net brief size: -5 KB.

3. **Add `BuildCodebaseContentBrief(plan, codebase, parent)`** — new composer. Brief content (per-dispatch ~25-29 KB):

   **Embedded (must be in brief; subagent can't fetch otherwise)**:
   - Phase entry atom (`content/phase_entry/codebase-content.md`) — ~2 KB
   - Synthesis workflow atom (`content/briefs/codebase-content/synthesis_workflow.md`) — ~3 KB
   - Voice + bundled-class caveat atom — ~2 KB
   - Platform principles atom (`content/briefs/scaffold/platform_principles.md` — universal Zerops mechanics: env-var aliasing, execOnce semantics, dev-iteration model, MinIO/S3 idioms) — ~5 KB
   - This codebase's filtered facts: porter_change + field_rationale + Kind="" platform-trap — ~10 KB
   - Cross-codebase contract facts where this codebase is publisher OR subscriber — ~2 KB
   - Codebase metadata (hostname, role, base runtime, services consumed, source root, slots) — ~1 KB
   - Engine-derived "fragment slots to fill" list with engine-emitted fact topic IDs — ~1 KB
   - Parent recipe pointer block (when `parent != nil`) — slug + paths to parent's published surfaces — ~1 KB

   **Pointed at (subagent Reads on demand)**:
   - `/Users/fxck/www/zcp/docs/spec-content-surfaces.md` (~38 KB, NOT embedded)
   - `/Users/fxck/www/zcp/docs/zcprecipator3/content-research.md` Part 5 + Part 6 (~11 KB worth, NOT embedded; subagent Reads relevant parts)
   - `<SourceRoot>/zerops.yaml`
   - `<SourceRoot>/src/**` (Glob + Read on demand)
   - `<MountRoot>/<parent-slug>/...` (when parent != nil)

   Brief explicitly instructs the sub-agent: *"Before authoring any fragment, Read these files in order: spec-content-surfaces.md (the seven Surface contracts + classification taxonomy), this codebase's zerops.yaml (the deploy config you're commenting), Glob src/ then Read key files. Use the embedded facts + atoms as your authoring guidance; use the on-disk content as canonical truth. For every engine-pre-seeded fact with empty Why (per-managed-service shells, §7.2), call `zerops_knowledge runtime=<svc-type>` first, then fill Why + Heading via `fill-fact-slot` grounded in the atom's content — do not paraphrase from memory."*

   **No `claude-md/*` slots in this brief** — Surface 6 is authored by the sibling `claudemd-author` sub-agent (§2.6, §6.7). The codebase-content brief explicitly tells the sub-agent: *"a sibling sub-agent authors CLAUDE.md from a Zerops-free brief in parallel; you do not author CLAUDE.md content. If you encounter Zerops-platform content that belongs in a porter dev guide, check whether it actually belongs in IG/KB/zerops.yaml comments instead — those are your surfaces."*

4. **Add `BuildEnvContentBrief(plan, parent)`** — new composer. Brief content (~25 KB):

   **Embedded**:
   - Phase entry atom (`content/phase_entry/env-content.md`) — ~2 KB
   - Per-tier authoring atom (`content/briefs/env-content/per_tier_authoring.md`) — ~3 KB
   - Friendly-authority voice atom — ~1 KB
   - Per-tier capability matrix (already computed) — ~3 KB
   - Cross-tier deltas ([tiers.go:95-129::Diff](../../../internal/recipe/tiers.go#L95-L129)) — ~2 KB
   - All engine-emitted `tier_decision` facts (one per tier × per service-block delta) — ~5 KB
   - All codebases + roles + services — ~2 KB
   - Fragment slots to fill (61 fragments) — ~3 KB
   - Parent recipe pointer block (when `parent != nil`) — ~1 KB

   **Pointed at**:
   - `/Users/fxck/www/zcp/docs/spec-content-surfaces.md` (NOT embedded)
   - `<MountRoot>/<parent-slug>/...` (when parent != nil)

5. **Reduce `BuildFinalizeBrief`** to stitch + validate only. ~3 KB brief.

**Tests**:

- New: `TestBuildCodebaseContentBrief_CarriesFilteredPorterChangeFacts`
- New: `TestBuildCodebaseContentBrief_CarriesFilteredFieldRationaleFacts`
- New: `TestBuildCodebaseContentBrief_CarriesPlatformPrinciplesAtomVerbatim`
- New: `TestBuildCodebaseContentBrief_PointsAtSpecAndSourceRoot` (asserts brief contains the file paths and the Read instruction, not the file contents)
- New: `TestBuildCodebaseContentBrief_NoClaudeMdSlots` (asserts fragment-slots list does NOT include `claude-md/*` IDs)
- New: `TestBuildCodebaseContentBrief_CarriesParentRecipePointer_WhenParentNonNil`
- New: `TestBuildEnvContentBrief_CarriesPerTierCapabilityMatrix`
- New: `TestBuildEnvContentBrief_CarriesEngineEmittedTierDecisionFacts`
- New: `TestBuildEnvContentBrief_CarriesParentRecipePointer_WhenParentNonNil`
- New: `TestCodebaseContentBrief_SizeUnder40KB` (regression guard against accidental verbatim embeds)
- Update: tests that assert scaffold brief size or content-authoring atom presence.

### §6.3 [internal/recipe/briefs_subagent_prompt.go](../../../internal/recipe/briefs_subagent_prompt.go)

**Current**: [buildSubagentPromptForPhase:49-97](../../../internal/recipe/briefs_subagent_prompt.go#L49-L97) routes scaffold/feature/finalize via [buildBriefForKind:109-124](../../../internal/recipe/briefs_subagent_prompt.go#L109-L124).

**Change**: extend `BriefKind` enum + `buildBriefForKind` switch + `buildSubagentPromptForPhase` per-kind context block to handle:

- `BriefKind = "codebase-content"` → `BuildCodebaseContentBrief(plan, codebase, parent)`
- `BriefKind = "claudemd-author"` → `BuildClaudeMDBrief(plan, codebase)` — Zerops-free brief, peer-dispatched alongside codebase-content
- `BriefKind = "env-content"` → `BuildEnvContentBrief(plan, parent)`

**Locus of parallel dispatch**: the engine's `build-subagent-prompt` action returns one brief per `briefKind` per codebase per call. **The MAIN agent (not the engine) dispatches the two phase-5 sub-agents per codebase as parallel `Agent` tool calls in a single message** — `codebase-content` + `claudemd-author` issued together. This is a `phase_entry/codebase-content.md` (or new `phase_entry/phase-5.md`) **brief teaching**, not engine code: the atom instructs the main agent to call `build-subagent-prompt` twice (once per kind) per codebase, then dispatch all 2N briefs in a single `Agent`-tool-calls message. There is no parallel-dispatch logic inside `briefs_subagent_prompt.go` — the engine produces briefs serially; the main agent runs them in parallel by message-batching. Tests at §10.4 #12 verify the *main-agent dispatch shape* (two-tool-calls-in-one-message) via session-jsonl observation, not engine code.

**Tests**: extend [briefs_dispatch_test.go](../../../internal/recipe/briefs_dispatch_test.go) to cover the three new dispatch kinds (codebase-content, claudemd-author, env-content). Each test exercises one `build-subagent-prompt` call per kind — the parallel-dispatch shape is exercised separately at §10.4 #12 as an e2e session-jsonl test.

### §6.4 [internal/recipe/handlers.go](../../../internal/recipe/handlers.go)

**Current**: action switch at [handlers.go:228::dispatch](../../../internal/recipe/handlers.go#L228) (function declared at line 205). 13 actions per [RecipeInput jsonschema:107-128](../../../internal/recipe/handlers.go#L107-L128).

**Changes**:

1. **Add action `fill-fact-slot`** — for engine-pre-seeded facts where the agent contributes specific slot values. Input: `factTopic` + slot values. Engine merges into the existing fact record. Use sites:
   - Engine-emitted Class B facts (worker no-HTTP, etc.): agent fills `candidateHeading` (framework-specific name) + optional `diff`.
   - Engine pre-seeded per-managed-service shells (§7.2): agent fills `why` + `candidateHeading` + optional `library`, after consulting `zerops_knowledge runtime=<svc-type>`.
   - Engine-emitted `tier_decision` facts: agent extends `tierContext` slot when auto-derived prose is insufficient.

2. **Extend `record-fragment` with slot-shape refusal** at [handlers.go:349-385::handleRecordFragment](../../../internal/recipe/handlers.go#L349-L385). Per-fragment-id constraints (§8.1 below).

3. **Add action `register-contract`** — for `contract` facts. Same shape as `record-fact` but with the cross-codebase fact subtype.

**Tests**:

- New: `TestRecordFragment_RefusesNestedExtractMarkers` (R-15-3)
- New: `TestRecordFragment_RefusesMultiHeadingInIGSlot` (R-15-5)
- New: `TestRecordFragment_RefusesNonTopicBulletInKBSlot`
- New: `TestRecordFragment_RefusesOversizeYamlCommentBlock`
- New: `TestFillFactSlot_MergesDiffIntoEngineEmittedFact`
- New: `TestFillFactSlot_MergesTierContextIntoEngineEmittedTierDecision`

### §6.5 [internal/recipe/handlers_fragments.go](../../../internal/recipe/handlers_fragments.go)

**Current**: [isValidFragmentID:153-199](../../../internal/recipe/handlers_fragments.go#L153-L199) accepts 9 fragment ID shapes including `codebase/<h>/claude-md/service-facts` and `codebase/<h>/claude-md/notes`.

**Changes**:

- **Add 2 new shapes**:
  - `codebase/<h>/integration-guide/<n>` (n = 1..5) — slotted IG item.
  - `codebase/<h>/zerops-yaml-comments/<block-name>` — per-block yaml comment.
- **Reactivate `codebase/<h>/claude-md` as primary** — single-fragment slot authored by the dedicated `claudemd-author` sub-agent (§2.6, §6.7). The previously-prep-retired `codebase/<h>/claude-md/{service-facts,notes}` sub-slots stay accepted for back-compat (legacy recipes that referenced them still validate) but new dispatches use the single `codebase/<h>/claude-md` slot. Slot-shape refusal at record-fragment time enforces the contract (§8.1).
- **Migration**: keep the old `codebase/<h>/integration-guide` shape accepted for back-compat during rollout; add new slotted shapes; switch IG content sub-agent to slotted form.

**Tests**:

- New: `TestIsValidFragmentID_SlottedIGItems`
- New: `TestIsValidFragmentID_ZeropsYamlCommentsByBlock`
- New: `TestIsValidFragmentID_ClaudeMdSingleSlot` — `codebase/<h>/claude-md` is the primary slot shape
- New: `TestIsValidFragmentID_LegacyClaudeMdSubslotsStillAccepted` (back-compat for `claude-md/service-facts`, `claude-md/notes`)
- New: `TestIsValidFragmentID_LegacyIntegrationGuideStillAccepted` (back-compat)

### §6.6 [internal/recipe/facts.go](../../../internal/recipe/facts.go)

Schema extension per §5.5. Validate dispatch per §5.6 audit. Add `FilterByKind`. Add `FilterByCodebase(records, hostname string)` helper for content-phase brief composition.

**Tests**:

- New: `TestFactRecord_Validate_PorterChange_RequiresFields`
- New: `TestFactRecord_Validate_FieldRationale_RequiresFields`
- New: `TestFactRecord_Validate_TierDecision_RequiresFields`
- New: `TestFactRecord_Validate_Contract_RequiresFields`
- New: `TestFactRecord_Validate_PlatformTrap_BackCompat`
- New: `TestFilterByKind_ReturnsMatchingSubset`
- New: `TestFilterByCodebase_ReturnsMatchingSubset`
- New: `TestFactsLog_RoundTrip_NewKindRecords`

### §6.7 [internal/recipe/assemble.go](../../../internal/recipe/assemble.go) — slotted IG + yaml comments + `/init`-style CLAUDE.md

**Current**: `injectIGItem1` at [:170](../../../internal/recipe/assemble.go#L170); `substituteFragmentMarkers` at [:388](../../../internal/recipe/assemble.go#L388).

**Changes**:

1. **New `injectIGItems`**: generates IG #1 (yaml verbatim, unchanged) + concatenates IG items #2..N from slotted fragments `codebase/<h>/integration-guide/<n>`. Falls back to legacy single-string fragment for back-compat.

2. **New `injectZeropsYamlComments`**: at finalize stitch, reads `codebase/<h>/zerops-yaml-comments/<block-name>` fragments. **Line-anchor insertion is the primary (and only-shipped) path**: regex-locate the named block's anchor line in the committed yaml (e.g., `^run:`, `^  envVariables:`, `^  initCommands:`), insert comment-prefixed fragment lines immediately above with matching indentation. No AST round-trip.

   **Why line-anchor not AST round-trip**: `gopkg.in/yaml.v3` is already in the codebase ([validators_import_yaml.go:8,88](../../../internal/recipe/validators_import_yaml.go)) but only for unmarshal-to-AST direction; **zero existing round-trip use anywhere in zcp**. The laravel-jetstream / laravel-showcase fixtures carry 24+ heavily-quoted `"${var}"` / `"true"` / `"false"` patterns that yaml.v3 commonly re-canonicalizes on emit. Shipping AST round-trip primary means writing 250 LoC + corpus-fuzz infrastructure + an untested line-anchor fallback; shipping line-anchor primary means one straightforward regex-based insertion path with mainline test coverage. The simpler design dominates: less code, no round-trip risk, easier to test, the fixtures reveal nothing AST-only could do that line-anchor can't.

   **AST round-trip is deferred**, not retired — if a future readiness round identifies a yaml shape line-anchor genuinely can't handle (e.g., comments inside flow-style mappings), the AST path can land then with the corpus gate at that time. Run-16 ships line-anchor only.

3. **New `injectIGItem1Intro`**: extends [yamlIntroSentence:223](../../../internal/recipe/assemble.go#L223) to read `field_rationale` facts and weave them into the intro where a non-obvious field's why prose is captured.

4. **No `emitCodebaseClaudeMD` engine function**. CLAUDE.md is authored by the dedicated `claudemd-author` sub-agent (§6.7a below). Stitch consumes the agent-authored `codebase/<h>/claude-md` fragment as a regular fragment substitution.

**Tests**:

- New: `TestInjectIGItems_ConcatenatesSlottedFragments`
- New: `TestInjectIGItems_FallsBackToLegacySingleFragment`
- New: `TestInjectZeropsYamlComments_LineAnchor_BlockBoundaryDetection` — regex correctly identifies block starts (`run:`, `envVariables:`, `initCommands:`, `build:`, etc.) across nested structures
- New: `TestInjectZeropsYamlComments_LineAnchor_PreservesFieldOrder` — confirms no reordering when comments are inserted
- New: `TestInjectZeropsYamlComments_LineAnchor_PreservesIndentation` — comment-prefixed lines match the target block's indentation
- New: `TestInjectZeropsYamlComments_LineAnchor_NoMatchInsideStringValues` — regex anchored to line-start to avoid false matches inside multi-line string values
- New: `TestInjectZeropsYamlComments_LineAnchor_CorpusYamlValidity` (5 fixtures: laravel-jetstream + laravel-showcase + run-15 × 3) — confirms post-insertion yaml still parses + retains all original fields. Risk gate.

### §6.7a [internal/recipe/briefs.go](../../../internal/recipe/briefs.go) — `BuildClaudeMDBrief` composer

**New composer**: `BuildClaudeMDBrief(plan *Plan, codebase Codebase) (Brief, error)`. Produces the brief for the `claudemd-author` sub-agent dispatched in parallel with `codebase-content` at phase 5.

**Design intent — the brief is `/init`, full stop.** The sub-agent's job is to produce CLAUDE.md as if the porter had run `claude /init` in this codebase: a generic codebase operating guide, no platform context whatsoever. We could in principle invoke `/init` directly; we dispatch a sub-agent because (a) the recipe-authoring run needs the output recorded as a fragment, not written to disk by a foreign tool; (b) phase 5 already has parallel-dispatch infrastructure for `codebase-content`. The brief carries `/init`'s contract verbatim PLUS a hard prohibition against Zerops content. **The brief deliberately includes NO reference recipes, NO sample CLAUDE.md from the recipe portfolio, and NO voice anchor pointing at the run-15 wrong-shape precedent** — pointing the sub-agent at the existing reference recipes' CLAUDE.md files would teach the very shape this doc is trying to escape (per §16, those reference recipes are flagged for future rewrite; they are anti-patterns, not exemplars).

The brief is **strictly Zerops-free** by construction: no platform principles atom, no env-var-aliasing teaching, no managed-service hints, no dev-loop instructions, no reference recipe pointers.

**Brief content (~2-3 KB; smaller than prior draft because the voice-anchor bullet was retired)**:

- **Phase entry atom** (`content/phase_entry/claudemd-author.md`, ~1 KB) — voice + `/init` contract:
  > *"You are running `claude /init` for the codebase rooted at `<SourceRoot>`. Output: a CLAUDE.md with three sections — project overview (1-2 sentences naming the framework + version + what this codebase does, derived only from package.json / composer.json / source code you read; do not infer from project structure), build & run (every package.json/composer.json script as a bullet with a one-line label drawn from the script body itself), architecture (top-level entries under `src/` or framework-canonical roots with one-line labels grounded in framework conventions you observe). Use the codebase to label, never invent."*
- **Codebase metadata** — hostname, role, source root path. ~0.5 KB.
- **Hard prohibition block** (verbatim, prominent — ~0.5 KB; lives in `content/briefs/claudemd-author/zerops_free_prohibition.md` so the same string can be asserted by `TestBuildClaudeMDBrief_ContainsHardProhibitionBlock`):
  > *"This is the porter's `/init`-style codebase guide. Do NOT include: Zerops platform content, managed-service hostnames (e.g. `db`, `cache`, `search`), env-var aliases (`${db_hostname}`, `${apidev_zeropsSubdomain}`), dev-loop tooling (`zsc`, `zerops_*`, `zcp`, `zcli`), Zerops dev-vs-stage container model, init-commands semantics, anything from `zerops.yaml`. A sibling sub-agent authors all Zerops integration content (IG/KB/zerops.yaml comments) for this codebase in parallel — that's not your surface. If a fact is Zerops-platform-specific, it does not belong in CLAUDE.md. Do NOT read `zerops.yaml` or any IG/KB/README content as voice anchors — those carry Zerops content by design."*
- **Pointers (sub-agent Reads on demand)** — `<SourceRoot>/package.json`, `<SourceRoot>/composer.json` (optional), `<SourceRoot>/src/**`, `<SourceRoot>/app/**` (laravel), framework-specific Glob hints. The agent uses Read + Glob + Bash directly — no engine pre-fetching. **Explicit exclusion**: `<SourceRoot>/zerops.yaml` is NOT in the pointer list — the prohibition above forbids it.
- **Output instruction**: record the result via `record-fragment fragmentId=codebase/<hostname>/claude-md mode=replace`. Single fragment, single slot. Slot-shape refusal at record-time (§8.1) blocks bodies containing `## Zerops`, `zsc`, `zerops_*`, `zcp`, `zcli`, or any managed-service hostname declared in `plan.Services` — same-context recovery if the agent's output drifts.

**No voice anchor**: the prior draft pointed the sub-agent at `~/www/laravel-showcase-app/CLAUDE.md` as a "voice anchor" example. That file IS the run-15 wrong-shape precedent — pointing the sub-agent at it would teach the exact shape (`## Zerops service facts`, etc.) the architecture pivot escapes. Voice anchor retired; the phase entry atom's `/init` contract IS the voice. If the sub-agent's output drifts, slot-shape refusal catches it; the architecture has three layers of defense (brief prohibition → record-time refusal → finalize validator) without needing a positive voice anchor.

**Tests**:

- New: `TestBuildClaudeMDBrief_ContainsHardProhibitionBlock` (assert the verbatim no-Zerops-content prohibition is in the brief)
- New: `TestBuildClaudeMDBrief_NoVoiceAnchorPointers` (assert the brief does NOT contain paths to `~/www/laravel-showcase-app/CLAUDE.md`, `~/www/laravel-jetstream-app/CLAUDE.md`, or any reference-recipe CLAUDE.md path — the run-15 wrong-shape precedent must not be teachable via the brief)
- New: `TestBuildClaudeMDBrief_NoZeropsYamlPointer` (assert `<SourceRoot>/zerops.yaml` is NOT in the brief's Read-pointer list)
- New: `TestBuildClaudeMDBrief_NoPlatformPrinciplesAtom` (assert brief does NOT include the Zerops platform principles atom — that would defeat the prohibition)
- New: `TestBuildClaudeMDBrief_PointsAtSourceRoot` (asserts brief contains the source-root path + Read instruction)
- New: `TestBuildClaudeMDBrief_SizeUnder8KB` (regression guard against scope creep)
- New: `TestClaudeMDDispatch_ParallelWithCodebaseContent` (e2e: phase 5 dispatches BOTH sub-agents per codebase in a single message)

### §6.8 [internal/recipe/validators_*.go](../../../internal/recipe/) — validator inventory changes

| Validator | Surface | Action |
|-----------|---------|--------|
| `validateRootREADME` | SurfaceRootREADME | **Keep**, narrow scope |
| `validateEnvREADME` | SurfaceEnvREADME | **Reduce**: char-cap moves to record-fragment slot constraint (closes R-15-3 by construction) |
| `validateEnvImportComments` | SurfaceEnvImportComments | **Keep**, narrow scope |
| `validateCodebaseIG` | SurfaceCodebaseIG | **Reshape**: item-cap = 5 enforced by slot existence; validator narrows to per-item content checks. Closes R-15-5 |
| `validateCodebaseKB` | SurfaceCodebaseKB | **Keep**, narrow scope. Add cross-surface duplication check vs IG (closes R-15-6) |
| `validateCodebaseCLAUDE` | SurfaceCodebaseCLAUDE | **Reshape**: validates the `claudemd-author` sub-agent's output shape (3 sections: project overview / build & run / architecture; no `## Zerops` headings; no managed-service hostnames; no `zsc` / `zerops_*` / `zcp` tool names; ≤ 80 lines). Sub-agent authors; validator confirms the brief's Zerops-free contract held. Closes R-15-4 — the wrong shape came from a Zerops-aware brief, not from agent-authoring per se; the dedicated Zerops-free brief solves the bleed-through. |
| `validateCodebaseYAML` | SurfaceCodebaseZeropsComments | **Keep**, narrow scope |

**New validators**:

- `validateCrossSurfaceDuplication` — Notice (not blocking). Method + threshold are **NOT pre-locked** in this readiness doc and must be calibrated as part of tranche 5 commit 4 itself. Empirical baseline (computed against run-15 dogfood artifacts):
  - Real R-15-6 dup #1 (apidev IG sub-section "Custom response headers" ↔ KB "CORS exposes peer subdomain aliases + custom response headers"): Jaccard ≈ 0.16
  - Real R-15-6 dup #2 (appdev IG sub-section "Streamed proxy bodies" ↔ KB "Same-origin proxy needs duplex: 'half'"): Jaccard ≈ 0.14
  - Non-dup control (appdev IG #4 API_URL vs IG #5 health endpoint): Jaccard ≈ 0.06

  **Pure Jaccard ≥ 0.7 catches zero of the real R-15-6 dups.** The R-15-6 anti-pattern is *semantic* duplication — KB bullets summarize IG sub-sections in a different vocabulary (KB names topics + generalizes; IG carries concrete code constructs). Token-level overlap is naturally low for healthy semantic dups. Plausible designs the commit must evaluate empirically:
  - **Topic-name matching**: extract the `### N. <heading>` IG headings + `**Topic** —` KB bullet topics, normalize, flag overlap on the topic key — fires only when both surfaces name the same topic.
  - **Lower-threshold Jaccard with shingle widening**: 3-gram shingles + Jaccard around 0.10-0.20.
  - **Hybrid**: topic-name primary signal + sub-threshold Jaccard as tie-breaker.

  Tranche 5 commit 4 lands the validator only after running each candidate design against the run-15 corpus and confirming it catches the 2 real dups while sparing ≥ 5 representative non-dup IG/KB pairs. Calibration evidence + chosen design recorded in the commit message. R-15-6 primary closure remains the structural fix in §4 (single-author content phase); this validator is the heuristic backstop.
- `validateCrossRecipeDuplication` — when `parent != nil`, structural similarity check between this recipe's content and parent's. Notice from inception. Same calibration discipline as above.

### §6.9 [internal/recipe/content/](../../../internal/recipe/content/) — atom changes

**Add**:

- `briefs/scaffold/decision_recording.md` (~2 KB) — teaches `porter_change`, `field_rationale` recording. Filter rule. Examples grounded in run-15 apidev: "S3_REGION=us-east-1 → field_rationale; CORS exposeHeaders → porter_change candidate KB; trustProxies middleware → porter_change candidate IG."
- `briefs/feature/decision_recording.md` (~2 KB) — same shape, scoped to feature-added fields + scenario-discovered traps.
- `briefs/codebase-content/intro.md` (~2 KB)
- `briefs/codebase-content/synthesis_workflow.md` (~3 KB) — how to read facts, group into IG items, dedup against KB, author zerops.yaml comments per block.
- `briefs/codebase-content/parent_recipe_dedup.md` (~1 KB) — when `parent != nil`, how to cross-reference instead of re-author
- `briefs/env-content/intro.md` (~2 KB)
- `briefs/env-content/per_tier_authoring.md` (~3 KB) — how to author each env fragment using tier_decision facts
- `briefs/claudemd-author/intro.md` (~1 KB) — frame as `claude /init` running inside this codebase
- `briefs/claudemd-author/init_voice.md` (~1 KB) — three-section structure, label-from-observation rule
- `briefs/claudemd-author/zerops_free_prohibition.md` (~0.5 KB) — verbatim hard-prohibition block (the same string asserted by `TestBuildClaudeMDBrief_ContainsHardProhibitionBlock`)
- `briefs/research/tier_decision_recording.md` (~1 KB) — research phase records initial tier_decision facts; new

**Phase entry atoms** ([content/phase_entry/](../../../internal/recipe/content/phase_entry/)):

- New: `codebase-content.md` — also teaches the **main-agent parallel-dispatch shape** for phase 5 (call `build-subagent-prompt` twice per codebase — once for codebase-content, once for claudemd-author — then issue all 2N briefs in a single `Agent`-tool-calls message)
- New: `claudemd-author.md` — sub-agent voice + prohibition reminder
- New: `env-content.md`
- Update: `scaffold.md` — remove content-authoring; add decision-recording (porter_change + field_rationale)
- Update: `feature.md` — same shape
- Update: `finalize.md` — strip authoring; finalize is stitch + validate only
- Update: `research.md` — add tier_decision recording instructions

**Conditional deletions** (deferred to a later tranche; see §11):

- `briefs/scaffold/content_authoring.md` (15.3 KB)
- `briefs/feature/content_extension.md` (7.4 KB)
- `briefs/finalize/intro.md`, `briefs/finalize/validator_tripwires.md`, `briefs/finalize/anti_patterns.md`

These deletions are conditional on tranche-3+ dogfood success. Per Concern 4, deleting them inside tranche 3 ties their fate to the tranche's revertibility; they're moved to tranche 6 (post-dogfood-confirmed) so the legacy-atom safety net stays in place during the architectural cut-over.

---

## §7. Engine-emit hooks for universal-for-role facts

When `BuildCodebaseDeployBrief` (renamed scaffold brief) dispatches per codebase, the engine emits facts ahead of the agent's work.

### §7.1 Per-role rule table

```go
// internal/recipe/engine_emitted_facts.go (NEW FILE)

func emittedFactsForCodebase(plan *Plan, cb Codebase) []FactRecord {
    var facts []FactRecord

    // Class B: universal-for-role
    if cb.Role == RoleAPI || cb.Role == RoleFrontend || cb.Role == RoleMonolith {
        facts = append(facts, FactRecord{
            Topic: cb.Hostname + "-bind-and-trust-proxy",
            Kind:  "porter_change",
            Scope: cb.Hostname + "/code",
            ChangeKind: "code-addition",
            Why: "Default bind to 127.0.0.1 is unreachable from the L7 balancer (which routes to the container's VXLAN IP). Trust the X-Forwarded-* headers so request.ip / request.protocol reflect the real caller.",
            CandidateClass:   "platform-invariant",
            CandidateHeading: "Bind 0.0.0.0 and trust the L7 proxy",
            CandidateSurface: "CODEBASE_IG",
            EngineEmitted:    true,
        })

        if strings.HasPrefix(cb.BaseRuntime, "nodejs") {
            facts = append(facts, FactRecord{
                Topic: cb.Hostname + "-sigterm-drain",
                Kind:  "porter_change",
                ChangeKind: "code-addition",
                Why: "Rolling deploys send SIGTERM to the old container while the new one warms up. Without explicit shutdown handling, in-flight requests fail mid-response.",
                CandidateClass:   "platform-invariant",
                CandidateHeading: "Drain in-flight requests on SIGTERM",
                CandidateSurface: "CODEBASE_IG",
                EngineEmitted:    true,
            })
        }
    }

    if cb.Role == RoleWorker {
        if strings.HasPrefix(cb.BaseRuntime, "nodejs") {
            facts = append(facts, FactRecord{
                Topic: cb.Hostname + "-no-http-surface",
                Kind:  "porter_change",
                // Why-prose is platform-invariant (every nodejs worker hits this).
                // Heading is left empty: the framework-specific name (NestJS application
                // context, BullMQ Worker, plain node script, ...) is agent-filled at
                // codebase-content phase, NOT engine-emitted — system.md §4 keeps
                // framework specifics on DISCOVER.
                Why: "A worker has no HTTP surface. Default framework bootstraps that start an Express/Fastify listener fight the platform's empty run.ports — the listener has nothing to bind to and the framework crashes or hangs at boot. Boot the framework's no-HTTP equivalent (e.g. NestJS createApplicationContext, BullMQ Worker, plain process loop) instead.",
                CandidateClass:   "platform-invariant",
                CandidateHeading: "", // agent-filled
                CandidateSurface: "CODEBASE_IG",
                EngineEmitted:    true,
            })
        }
    }

    // Class C: universal-for-recipe (per managed service)
    services := managedServicesConsumedBy(plan, cb)
    if len(services) > 0 {
        facts = append(facts, FactRecord{
            Topic: cb.Hostname + "-own-key-aliases",
            Kind:  "porter_change",
            Why: "Cross-service references like ${db_hostname} auto-inject project-wide under platform-side keys. Reading those names directly couples the application to Zerops naming. Declare own-key aliases in zerops.yaml run.envVariables and read those.",
            CandidateClass:   "platform-invariant",
            CandidateHeading: "Read managed-service credentials from own-key aliases",
            CandidateSurface: "CODEBASE_IG",
            CitationGuide:    "env-var-model",
            EngineEmitted:    true,
        })
    }

    // Per-service fact shells — engine pre-seeds shape, agent fills Why
    // from `zerops_knowledge runtime=<type>` at codebase-content phase via
    // fill-fact-slot. See §7.2 below for the rationale.
    for _, svc := range services {
        guideID := citationGuideForServiceType(svc.Type) // e.g. "managed-services-nats"
        facts = append(facts, FactRecord{
            Topic: cb.Hostname + "-connect-" + svc.Hostname,
            Kind:  "porter_change",
            // Why deliberately empty — agent fills after consulting the
            // per-service knowledge atom. The pre-seeded shell prevents the
            // agent from forgetting per-service IG items; the agent owns the
            // why-prose because the atom is the single source of truth.
            Why:              "",
            CandidateClass:   "intersection",
            CandidateHeading: "", // agent-filled (framework-specific name)
            CandidateSurface: "CODEBASE_IG",
            CitationGuide:    guideID,
            EngineEmitted:    true,
        })
    }

    return facts
}
```

No `operational_note` pre-emit — that subtype was retired (§2.4 + §2.6). Operational facts that the engine could emit (e.g. "Vite manifest missing after fresh deploy") either anchor at a yaml field (→ `field_rationale`) or sit outside the recipe-authored surfaces (→ the `claudemd-author` sub-agent picks up dev-loop content from package.json scripts directly).

### §7.2 Per-managed-service fact shells (replaces prep's hint-table)

The prep proposal (and earlier drafts of this readiness doc) put a hand-curated `managedServiceHints` map indexed by service-type × runtime in engine code, with paired reference-data tests against the per-service knowledge atom. This is retired in favor of the **fact-shell pattern** below.

**Why the hint table loses on first principles, independent of system.md §4 framing**:

1. **Combinatorial growth.** Every new managed service adds a row; every new runtime adds a column; service version bumps fork the entries. The cell at `nats@2.12 × nodejs@22` doesn't help a future `nats@2.13 × bun@1.5` recipe.
2. **Dual source of truth.** The per-service knowledge atom (`internal/knowledge/.../managed-services-nats.md`) IS the canonical curation site. A hint table re-writes prose drawn from the atom — paired ref-data tests can prevent literal drift but can't prevent the engine code and atom from saying *different versions of the same thing*.
3. **Tool call cost is trivial.** `zerops_knowledge runtime=nats` is one MCP call returning a few KB. Saving that call in exchange for the two costs above is a bad trade.

**The fact-shell pattern**:

Engine pre-seeds an empty-Why `porter_change` fact for each managed service the codebase consumes, populated with topic + citation-guide ID. The fact appears in the codebase's emitted-facts list at scaffold dispatch. At codebase-content phase, the agent walks every shell, calls `zerops_knowledge runtime=<svc-type>` for each, and fills `Why` + `Heading` + (optional) `Library` via the `fill-fact-slot` action (§6.4). The shell guarantees structural coverage (one IG item per consumed managed service); the atom remains the single source for connection-idiom prose.

**Engine-side curation surface**: just one helper, `citationGuideForServiceType(svcType string) string`, mapping runtime types to the matching topic ID in the citation map ([system.md §4 line 305-310](../system.md#L305) "Citation map atom"). This IS TEACH-side appropriately — the citation map already exists as TEACH; mapping `nats@2.x` → `managed-services-nats` is a lookup, not a content catalog.

**No paired ref-data tests needed**, because no engine-side why-prose exists to drift. The atom IS the source.

**What gets dropped vs prep**:

- `internal/recipe/managed_service_hints.go` — NOT created. (~80 LoC saved)
- `ConnectionHint` struct — NOT introduced.
- Per-hint commits in tranche 2 (~5 commits, ~200 LoC) — NOT made.
- `TestEngineEmitted_<Hint>WhyMatchesAtom` reference-data tests — NOT needed.

**What gets added**:

- `citationGuideForServiceType` helper in `engine_emitted_facts.go` (~20 LoC).
- Brief teaching at `briefs/codebase-content/synthesis_workflow.md`: "for every engine-pre-seeded fact with empty Why, call `zerops_knowledge runtime=<svc-type>` first; fill Why + Heading via `fill-fact-slot` grounded in atom content; do not paraphrase from memory."
- A test asserting fact-shell topics are stable (`TestEmittedFactShells_PerConsumedManagedService`).

Net for this section: **dropped from ~280 LoC + 5+ tests to ~50 LoC + 2 tests.**

### §7.3 Test pinning engine-emitted prose to atom content

```go
// internal/recipe/engine_emitted_facts_test.go (NEW)

func TestEngineEmitted_BindAndTrustProxy_WhyMatchesAtom(t *testing.T) {
    t.Parallel()
    cb := Codebase{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}
    plan := &Plan{Codebases: []Codebase{cb}}
    facts := emittedFactsForCodebase(plan, cb)

    var bindFact *FactRecord
    for i := range facts {
        if facts[i].Topic == "api-bind-and-trust-proxy" {
            bindFact = &facts[i]
            break
        }
    }
    if bindFact == nil { t.Fatal("expected bind-and-trust-proxy fact") }

    atom := mustReadAtom(t, "scaffold/platform_principles.md")
    if !atomHTTPSectionMentions(atom, "Bind 0.0.0.0") {
        t.Error("atom doesn't teach 'Bind 0.0.0.0'; engine-emit Why might drift")
    }
    if !strings.Contains(bindFact.Why, "L7 balancer") {
        t.Errorf("Why prose should mention L7 balancer; got: %s", bindFact.Why)
    }
}
```

This is the safety net against engine-emitted prose drifting away from atom-source content. Risk 1 mitigation. Required for tranche 2 (engine-emit) commits, NOT tranche 1 (schema).

---

## §8. Slot-shape refusal at record-fragment

Move structural caps from finalize-validator (post-hoc) to record-fragment refusal (record-time).

### §8.1 Refusal table

| Fragment ID | Constraint | Refusal message |
|-------------|------------|-----------------|
| `root/intro` | ≤ 500 chars; no markdown headings | "root/intro is a 1-sentence string, ≤ 500 chars, no markdown. See spec §Surface 1." |
| `env/<N>/intro` | ≤ 350 chars; no `## ` headings; no `<!-- #ZEROPS_EXTRACT_*` tokens | "env/<N>/intro is a 1-2 sentence string, ≤ 350 chars, no markdown headings, no nested extract markers. See spec §Surface 2." |
| `env/<N>/import-comments/project` | ≤ 8 lines | "tier import.yaml project comment ≤ 8 lines per spec §Surface 3." |
| `env/<N>/import-comments/<host>` | ≤ 8 lines | "tier import.yaml service-block comment ≤ 8 lines per spec §Surface 3." |
| `codebase/<h>/intro` | ≤ 500 chars; no `## ` headings | "codebase intro is a 1-2 sentence string, ≤ 500 chars per spec §Surface 4." |
| `codebase/<h>/integration-guide/<n>` | exactly 1 `### ` heading per slot; body ≤ 30 lines | "IG slot is one item: one `### ` heading + body ≤ 30 lines. See spec §Surface 4." |
| `codebase/<h>/knowledge-base` | each `- ` bullet must start with `**Topic** —`; total bullets ≤ 8 | "KB bullet shape: `- **Topic** — 2-4 sentences`. Cap 8 bullets per spec §Surface 5." |
| `codebase/<h>/zerops-yaml-comments/<block>` | ≤ 6 lines | "zerops.yaml block comment ≤ 6 lines per spec §Surface 7." |
| `codebase/<h>/claude-md` | exactly 3 `## ` headings (`Build & run`, `Architecture`, optionally one more); body ≤ 80 lines; **must NOT contain any of**: `## Zerops`, `zsc `, `zerops_`, `zcp `, `zcli `, `${<host>_zeropsSubdomain}`, managed-service hostnames declared in `plan.Services` | "CLAUDE.md slot is `/init`-shaped and Zerops-free per spec §Surface 6. Found `<offending-token>` — that content belongs in IG/KB/zerops.yaml comments, not CLAUDE.md. Re-author without Zerops platform content." |

Legacy `codebase/<h>/claude-md/{service-facts,notes}` IDs remain accepted by `isValidFragmentID` (back-compat for older recipes) but new dispatches use the single-slot `codebase/<h>/claude-md` shape with the refusal contract above.

### §8.2 Implementation site

[`handlers.go::handleRecordFragment`](../../../internal/recipe/handlers.go#L349-L385) — after `isValidFragmentID` passes, before `recordFragment` writes:

```go
if violation := checkSlotShape(in.FragmentID, in.Fragment); violation != "" {
    r.OK = false
    r.Notice = violation
    return r
}
```

`checkSlotShape` lives in new file `internal/recipe/slot_shape.go`.

### §8.3 Pre-tranche-1 test-fixture audit (GAP D FIX)

**Run before tranche 1 lands** to ensure existing fragment-authoring tests don't regress under new slot constraints:

```bash
# Step 1: enumerate all record-fragment call sites in tests
grep -rn '"record-fragment"\|action="record-fragment"' internal/recipe/*_test.go
grep -rn 'FragmentID:' internal/recipe/*_test.go
grep -rn 'in\.Fragment =\|Fragment:\s*`' internal/recipe/*_test.go

# Step 2: for each fixture, check fragment body against §8.1 refusal rules
# Manually inspect each match for:
# - codebase/<h>/integration-guide single-string fragments with multiple ### headings (legacy path; should be back-compat-accepted)
# - codebase/<h>/knowledge-base bullets not starting with `- **`
# - env/<N>/intro bodies with `<!-- #ZEROPS_EXTRACT_*`

# Step 3: for each fixture violating new constraints, decide:
#   (a) update fixture to comply (preferred — tests should reflect post-migration shape)
#   (b) move to a legacy back-compat regression test asserting old shape still accepted
```

Report fixture changes in tranche 1's commit message. If any fixture cannot be cleanly migrated, surface as a tranche-1 risk before merge.

### §8.4 What this closes

R-15-3 (duplicate extract markers): `env/<N>/intro` slot refuses bodies containing `<!-- #ZEROPS_EXTRACT_*` tokens.

R-15-4 (duplicate H2 in CLAUDE.md): closed via the dedicated `claudemd-author` sub-agent with a Zerops-free brief (§2.6, §6.7a) + slot-shape refusal at record-fragment time (§8.1 — refuses bodies containing `## Zerops`, `zsc`, `zerops_*`, managed-service hostnames). The wrong shape originated from a Zerops-aware brief bleeding into CLAUDE.md authoring; a sibling sub-agent with a strict no-Zerops-content prohibition + record-time refusal closes the failure mode by construction. Validator at §6.8 backstops at finalize.

R-15-5 (unnumbered IG sub-section): `codebase/<h>/integration-guide/<n>` is per-slot. Slot 6 doesn't exist.

---

## §9. R-15-1 closure — subdomain dual-signal eligibility (FORENSIC TRANCHE)

This tranche stands separately from the architecture reshape. Forensically grounded in the run-15 deploy timeline. Implementable independently of §§1-8.

### §9.1 Forensic timeline

scaffold-app deploy timeline from run-15 session jsonl ([runs/15/SESSSION_LOGS](../runs/15/SESSSION_LOGS)):

```
12:35:24  DEPLOY (appdev, setup=dev)    → SubdomainAccessEnabled=None, no warnings
12:37:00  VERIFY appdev                  (presumably succeeded but subdomain off)
12:37:07  zerops_subdomain enable appdev (agent manual fix)
12:37:39  DEPLOY (appstage, setup=prod) → SubdomainAccessEnabled=None, no warnings
12:38:59  VERIFY appstage
12:39:02  zerops_subdomain enable appstage (agent manual fix)
12:40:17  DEPLOY (appdev again)         → SubdomainAccessEnabled=True, url populated, warning="HTTP 502 after 10s"
```

**Diagnosis**: `maybeAutoEnableSubdomain` did NOT fire on the first two deploys. No warnings, no `SubdomainAccessEnabled: true` in the result. After the agent manually enabled, subsequent deploys show the field set (because `detail.SubdomainAccess` is now true).

### §9.2 Why §A's run-15 fix actually failed in production

The §A premise (per [deploy_subdomain.go:138-141](../../../internal/tools/deploy_subdomain.go#L138-L141)):

> "non-workers import with `enableSubdomainAccess: true` (yaml_emitter.go) and surface `SubdomainAccess=true` on every subsequent GetService"

**This premise is wrong against this platform.** Workspace import emits `enableSubdomainAccess: true` (confirmed at [yaml_emitter.go:164](../../../internal/recipe/yaml_emitter.go#L164) for dev, [:181](../../../internal/recipe/yaml_emitter.go#L181) for stage). But `client.GetService(svc.ID).SubdomainAccess` does NOT come back `true` after import alone — it only flips after someone calls the enable API. Import sets intent in some platform-internal field; `detail.SubdomainAccess` reflects actual subdomain state, not intent.

So `platformEligibleForSubdomain` at [deploy_subdomain.go:145-162](../../../internal/tools/deploy_subdomain.go#L145-L162) returns `false` on every recipe-authoring first-deploy, auto-enable is skipped silently (no warning — soft-fail at line 158/161), and the agent manually enables to recover.

### §9.3 The corrected understanding of pre-§A vs post-§A

| Code path | Pre-§A (read `Ports[].HTTPSupport`) | Post-§A (read `detail.SubdomainAccess`) |
|---|---|---|
| End-user click-deploy (deliverable yaml) | Worked once ports propagated; raced on first deploy of stage slots → R-14-1 | Works (deliverable yaml import → SubdomainAccess true) |
| Recipe-authoring scaffold/feature deploys | Worked once agent's zerops.yaml shipped with httpSupport ports | **Broken** — SubdomainAccess never flips to true from workspace import alone |

The §A fix traded one bug (R-14-1: race for end-user stage first-deploy) for a different bug (R-15-1: never auto-enables for recipe-authoring). It moved the failure mode but didn't eliminate it.

### §9.4 The legitimate fix — OR both signals

```go
// internal/tools/deploy_subdomain.go::platformEligibleForSubdomain

func platformEligibleForSubdomain(
    ctx context.Context,
    client platform.Client,
    projectID, targetService string,
) bool {
    svc, err := ops.LookupService(ctx, client, projectID, targetService)
    if err != nil || svc == nil {
        return false
    }
    if svc.IsSystem() {
        return false
    }

    detail, err := client.GetService(ctx, svc.ID)
    if err != nil || detail == nil {
        return false
    }

    // End-user click-deploy path: detail.SubdomainAccess is set by the platform
    // when the imported deliverable yaml carries enableSubdomainAccess: true AND
    // a subdomain has actually been provisioned. Holds for end-user runs.
    if detail.SubdomainAccess {
        return true
    }

    // Recipe-authoring path: workspace yaml emits enableSubdomainAccess: true
    // (yaml_emitter.go:164/181) but the platform doesn't flip detail.SubdomainAccess
    // until first enable. Fall back to deploy-time port signal: any port with
    // httpSupport=true means the deployed zerops.yaml intends HTTP, so auto-enable
    // is the right move.
    for _, port := range detail.Ports {
        if port.HTTPSupport {
            return true
        }
    }
    return false
}
```

**Why this is safe against R-14-1 (the race the §A fix was originally trying to close)**: the bounded backoff retry already in [`enableSubdomainAccessWithRetry` at ops/subdomain.go:64-68](../../../internal/ops/subdomain.go#L64-L68) absorbs the `noSubdomainPorts` rejection without changing the eligibility predicate. So OR-ing both signals is safe: the retry absorbs propagation race; the dual signal handles both code paths.

### §9.5 What this requires concretely

**1. Engine fix at [internal/tools/deploy_subdomain.go:145-162](../../../internal/tools/deploy_subdomain.go#L145-L162).**

LoC: ~10 (add the Ports loop fallback inside the existing function).

**2. New unit tests at `internal/tools/deploy_subdomain_test.go`**:

- `TestPlatformEligible_DetailSubdomainAccessTrue_Eligible` (existing case, regression coverage)
- `TestPlatformEligible_DetailSubdomainAccessFalse_HTTPSupportTrue_Eligible` (NEW — recipe-authoring path)
- `TestPlatformEligible_DetailSubdomainAccessFalse_HTTPSupportFalse_NotEligible` (worker path / non-HTTP service)
- `TestPlatformEligible_GetServiceError_NotEligible` (existing soft-fail case, regression coverage)

**3. Operational verification gate** — `make verify-dogfood-no-manual-subdomain-enable`:

```bash
# scripts/verify-dogfood-subdomain.sh
RUN_DIR="docs/zcprecipator3/runs/$(ls -1 docs/zcprecipator3/runs/ | sort -n | tail -1)"
JSONL_PATH="$RUN_DIR/SESSSION_LOGS"
hits=$(grep -c '"name":"mcp__zerops__zerops_subdomain"[^}]*"action":"enable"' \
       "$JSONL_PATH"/*.jsonl "$JSONL_PATH"/subagents/*.jsonl 2>/dev/null \
       | grep -v ':0$' | wc -l)
if [ "$hits" -gt 0 ]; then
    echo "FAIL: $hits manual zerops_subdomain action=enable invocations in latest dogfood"
    grep -l '"name":"mcp__zerops__zerops_subdomain"[^}]*"action":"enable"' \
         "$JSONL_PATH"/*.jsonl "$JSONL_PATH"/subagents/*.jsonl
    exit 1
fi
echo "PASS"
```

Wired into a `Makefile` target. Run after every dogfood; refuses run-N readiness sign-off if any manual enable appears.

**4. Brief teaching update** — [`phase_entry/scaffold.md`](../../../internal/recipe/content/phase_entry/scaffold.md) adds:

> "Subdomain auto-enable: every `zerops_deploy` of a non-worker codebase auto-enables the L7 subdomain on first deploy (when zerops.yaml has `httpSupport: true` on a port). The deploy result carries `SubdomainAccessEnabled: true` + the URL. Do NOT preemptively call `zerops_subdomain action=enable` — that's the recovery path only, used when a deploy result returns a warning indicating auto-enable failed."

The teaching is needed even with the engine fix because the agent's mental model (carried in run-15 facts.jsonl) propagates to future runs unless corrected at the brief level.

**5. Annotate the run-15 facts.jsonl `apidev-subdomain-not-auto-enabled` entry as superseded** (Concern 3 fix — additive, preserves run-15 forensic record).

The recorded fact carries the pre-§A and now-pre-fix mental model:

> "Subdomain auto-enable on first deploy keys off the runtime's port stack at deploy time; if the dev-setup port stack arrives without httpSupport flagged on the live container metadata, the L7 route is not provisioned."

That's wrong even relative to the corrected understanding. Future runs consulting this fact will internalize the bad mental model.

**Action**: append a `supersededBy` field to the entry referencing the tranche 0 commit SHA + the corrected mental model in `phase_entry/scaffold.md`. Don't delete — facts.jsonl is run-15's published artifact; rewriting history loses forensic value. Annotation tells future readers "this fact reflects pre-fix understanding; the corrected model lives at <commit>/<atom>." Concretely, add to the fact's `extra`:

```json
"supersededBy": "<tranche-0-commit-1-sha>",
"supersededReason": "Recipe-authoring auto-enable now works via detail.SubdomainAccess OR Ports[].HTTPSupport (run-16 R-15-1 closure). Brief teaching at phase_entry/scaffold.md is the corrected mental model."
```

---

## §10. Test plan

### §10.1 Tranche 0 tests (R-15-1 forensic closure)

- 4 unit tests at `deploy_subdomain_test.go` (§9.5 step 2)
- 1 integration test exercising the dual-signal path against the `internal/platform/mock` test platform
- 1 e2e regression: dogfood verify gate (§9.5 step 3) passes on the next dogfood

### §10.2 Tranche 1 tests (RED-then-GREEN)

For each new function, write a RED test first; then implement until GREEN.

**Order**:

1. Phase enum + AdjacentForward tests → workflow.go change
2. FactRecord.Validate per-Kind tests → facts.go schema extension
3. FilterByKind / FilterByCodebase tests → facts.go new helpers
4. FactsLog reader migration audit tests (FilterByHint platform-trap-only, ClassifyWithNotice early-return)
5. checkSlotShape per-fragment-ID tests → slot_shape.go new file
6. handleRecordFragment refusal tests → handlers.go integration

### §10.3 Tranche 2 tests

7. emittedFactsForCodebase per-role × runtime tests → engine_emitted_facts.go new file
8. Per-consumed-managed-service fact-shell tests (`TestEmittedFactShells_PerConsumedManagedService`, `TestEmittedFactShells_EmptyWhy`, `TestEmittedFactShells_CitationGuideMapped`) — NO per-hint paired tests (§7.2 retired the hint table; no engine-side why-prose to pin to atom content)

### §10.4 Tranche 3 tests

9. BuildCodebaseContentBrief content presence tests (filtered facts, atoms embedded, file pointers, no claude-md slots, parent-recipe context, size regression guard) → briefs.go new composer
10. BuildEnvContentBrief content presence tests (per-tier matrix, tier_decision facts engine-emitted, parent-recipe context) → briefs.go new composer
11. BuildClaudeMDBrief content presence tests (hard prohibition block present, no platform principles atom, no reference-recipe CLAUDE.md voice-anchor pointer, no `zerops.yaml` pointer, source-root pointer, size regression < 8 KB) → briefs.go new composer
12. ClaudeMDDispatch parallel-with-codebase-content e2e test (phase 5 dispatches BOTH sub-agents per codebase in a single message with parallel `Agent` tool calls) → `phase_entry/codebase-content.md` (main-agent dispatch teaching) + e2e session-jsonl observation. **Locus correction (per §6.3)**: parallel dispatch is the main agent's behavior, not engine code; the test verifies the dispatch shape via session-jsonl, not via `briefs_subagent_prompt.go` (which produces briefs serially, one per `build-subagent-prompt` call).
12a. **Load-bearing claim verification (per §0 rule 6) is a manual dogfood gate, NOT a unit test.** The §0.6 discipline requires running the cited mechanism against the actual artifact — for the claudemd-author sub-agent that means dispatching the sub-agent against a real codebase and capturing its output. Doing this in a unit test is impractical (real LLM calls, cost, non-determinism — unit tests must be deterministic). Test 11 (`TestBuildClaudeMDBrief_*` family) verifies the brief's *composer-time contract* (no platform-principles atom, no reference-recipe pointer, hard-prohibition block present, size cap held); that's a brief-shape verification, NOT a mechanism verification.

    The mechanism verification lives in the **dogfood gate `verify-claudemd-zerops-free`** (run-16 §17 step 5 — see also §9.5 step 3's `verify-dogfood-no-manual-subdomain-enable` precedent). Concretely: when the run-16 dogfood completes, a script greps every dispatched `claudemd-author` sub-agent's output (recorded in session jsonl) for the prohibited tokens (`## Zerops`, `zsc`, `zerops_*`, `zcp`, `zcli`, every hostname in `plan.Services`); zero matches required for run-16 readiness sign-off. This is the §0.6-mandated mechanism verification — proves the design works on the actual run-16 artifacts the design is fixing, not just that the brief composer's output looks right structurally.

    Tranche 0 ships the script at `scripts/verify-claudemd-zerops-free.sh` and `make verify-claudemd-zerops-free` target alongside the subdomain verifier. Tranche 7 sign-off requires the gate to pass on the run-16 dogfood output.

### §10.5 Tranche 4 tests

13. injectIGItems concatenation + back-compat tests → assemble.go change
14. injectZeropsYamlComments line-anchor tests + 5-fixture corpus validity → assemble.go new function
15. CLAUDE.md slot-shape refusal tests (`TestCheckSlotShape_ClaudeMD_RefusesZeropsHeading`, `_RefusesManagedServiceHostname`, `_Refuses_zsc_Token`, `_Refuses_zerops_Token`, `_AcceptsZeropsFreeContent`) → slot_shape.go extension

### §10.5a Tranche 5 tests

16. `validateCodebaseCLAUDE` finalize-time shape confirmation tests (3 sections, body ≤ 80 lines, no Zerops content) → `validators_*.go` reshape
17. `validateCrossSurfaceDuplication` tests + calibration script + per-design comparison vs run-15 corpus → `validators_*.go` new (the commit message must record which design — topic-name match / shingled Jaccard / hybrid — was selected based on calibration evidence per §6.8)
18. `validateCrossRecipeDuplication` tests (parent-recipe similarity check, when `parent != nil`) → `validators_*.go` new

### §10.6 e2e dispatch tests

Every brief / response / validator extension has an e2e test that observes production output (per run-15 §0 lesson):

- `TestCodebaseContentDispatch_BriefSizeUnder40KB`
- `TestCodebaseContentDispatch_BriefCarriesParentRecipePointer_WhenParentNonNil`
- `TestClaudeMDDispatch_ParallelWithCodebaseContent` — phase 5 dispatches BOTH sub-agents per codebase in a single message (parallel `Agent` tool calls). **Implementation site: `phase_entry/codebase-content.md` (main-agent dispatch teaching), NOT engine code** (per §6.3 + §10.4 #12 locus correction). The test verifies the dispatch shape via session-jsonl observation; there is no parallel-dispatch logic in `briefs_subagent_prompt.go`.
- `TestClaudeMDDispatch_BriefIsZeropsFree` — dispatched brief contains the hard-prohibition block + does NOT contain platform-principles atom content. (Composer-shape test; the §0.6 mechanism verification of the sub-agent's actual output lives in the `make verify-claudemd-zerops-free` operational gate per §10.4 test 12a.)
- `TestEnvContentDispatch_BriefCarriesEngineEmittedTierDecisions`
- `TestRecordFragment_RefusalReachesAgent_AndAgentCanRecover`
- `TestStitch_CodebaseClaudeMD_FromAgentFragment` — stitch reads `codebase/<h>/claude-md` fragment as primary path

### §10.7 Reference-data tests

- `TestSurfaceContract_LineCaps_MatchSpecLineBudgetTable` — asserts `SurfaceContract` LineCap / ItemCap / IntroExtractCharCap in [surfaces.go:128-200](../../../internal/recipe/surfaces.go#L128-L200) match [spec §Per-surface line-budget table](../../spec-content-surfaces.md#per-surface-line-budget-table) parsed live.
- `TestEmittedFactShell_CitationGuideMatchesCitationMap` — asserts `citationGuideForServiceType(svcType)` returns a topic ID present in the engine's citation-map atom. (One test, not a per-hint test family — the citation map IS the source.)

---

## §11. Tranche ordering with commit titles + gates

Each tranche has: pre-audit step, commit titles, LoC estimate, test count, risk-mitigation checkpoint.

### Tranche 0 — R-15-1 forensic closure (independent of architecture reshape)

**Pre-audit**: confirm forensic findings against current [deploy_subdomain.go:145-162](../../../internal/tools/deploy_subdomain.go#L145-L162) — function unchanged since §A landed; no other consumers added.

**Commits**:

1. `tools(subdomain): OR detail.SubdomainAccess with deploy-time HTTPSupport (R-15-1)`
   - LoC: ~10 (deploy_subdomain.go) + ~80 (4 new tests)
   - Risk gate: 4 unit tests + manual review of dual-signal semantics with someone who held the §A fix history
2. `ops(verify): add make verify-dogfood-no-manual-subdomain-enable`
   - LoC: ~25 (Makefile target + scripts/verify-dogfood-subdomain.sh)
3. `ops(verify): add make verify-claudemd-zerops-free — operational gate for §0.6 mechanism verification of the claudemd-author sub-agent output`
   - LoC: ~30 (Makefile target + `scripts/verify-claudemd-zerops-free.sh`). Greps every dispatched claudemd-author sub-agent's output in the latest dogfood session jsonl for the prohibited tokens (`## Zerops`, `zsc`, `zerops_*`, `zcp`, `zcli`, every hostname in `plan.Services`); zero matches required for run-N readiness sign-off. This gate is the substantive §0.6 mechanism verification — composer-time tests (Tranche 3 test 11) cover the brief's promise, this gate covers what the agent actually produces.
   - **Script exit semantics mirror `verify-dogfood-subdomain.sh`** (§9.5 step 3): on FAIL, exit 1 + list the offending sub-agent jsonl file paths + dump the actual prohibited tokens found in each so the CI failure message is actionable. On PASS, exit 0 + print the count of inspected jsonls. Implementer copies the subdomain script's pattern.
4. `recipe(brief): teach scaffold.md that subdomain auto-enables on first deploy`
   - LoC: ~30 (phase_entry/scaffold.md addition)
5. `recipe(facts): annotate run-15 apidev-subdomain-not-auto-enabled fact as superseded`
   - LoC: +5 (annotation in facts.jsonl entry's `extra`; do NOT delete entry)

Net tranche 0: ~180 LoC, 4 unit tests, 2 verification gates. Independently shippable; closes R-15-1 without touching the architecture reshape; ships the §0.6 mechanism-verification gate ahead of the architecture work so run-16 dogfood can be checked against it from tranche 4 onward.

### Tranche 1 — Schema + slot refusal (closes R-15-3, R-15-5)

**Pre-audit**: §8.3 fragment-fixture audit — enumerate every `record-fragment` call in `internal/recipe/*_test.go`, classify each fixture body against §8.1 refusal rules, decide migrate-vs-back-compat for each.

**Commits**:

1. `recipe(facts): extend FactRecord with Kind discriminator + per-Kind validation`
   - LoC: ~120 (facts.go schema + Validate dispatch + tests)
2. `recipe(facts): add FilterByKind + FilterByCodebase helpers`
   - LoC: ~50
3. `recipe(facts): audit FactsLog consumers against new schema (FilterByHint, ClassifyWithNotice)`
   - LoC: ~40 (defensive early-returns + tests)
4. `recipe(slot-shape): add checkSlotShape refusal at record-fragment time`
   - LoC: ~150 (slot_shape.go + handlers.go integration)
5. `recipe(handlers): wire slot-shape refusal into handleRecordFragment`
   - LoC: ~30 + ~5 tests
6. `recipe(handlers): add fill-fact-slot action for engine-emitted facts`
   - LoC: ~80 + 2 tests

Net tranche 1: ~470 LoC, ~18 unit + 3 integration tests.

**Risk gate**: §10.7 reference-data test `TestSurfaceContract_LineCaps_MatchSpecLineBudgetTable` must pass; existing fragment fixtures pass either as migrated or as legacy-back-compat regression tests.

### Tranche 2 — Engine-emit + per-service fact shells

**Pre-audit**: confirm tranche 1 merged + tests green.

**Commits**:

1. `recipe(engine-emit): add emittedFactsForCodebase scaffold + Class B universal-for-role facts`
   - LoC: ~150 (engine_emitted_facts.go + tests for nodejs/php-nginx × API/Frontend/Monolith/Worker; worker fact uses agent-filled heading per §2.7)
2. `recipe(engine-emit): add Class C own-key-aliases umbrella fact for codebases consuming managed services`
   - LoC: ~50
3. `recipe(engine-emit): pre-emit tier_decision facts from Diff (whole-tier) + new TierServiceModeDelta helper (per-service)`
   - LoC: ~130 (engine_emitted_facts.go for the whole-tier loop + new tier_service_deltas.go for `TierServiceModeDelta` helper, ~30 LoC, reusing yaml_emitter.go:325 downgrade logic + plan.go::managedServiceSupportsHA + sort + 4 tests). Per §5.3 the prep claim "from `tiers.go::Diff`" was incomplete — Diff returns whole-tier deltas only; the per-service half lives in this new helper.
4. `recipe(engine-emit): pre-seed per-service fact shells with citationGuide; agent fills Why via fill-fact-slot`
   - LoC: ~50 (citationGuideForServiceType helper + emit loop + 3 tests; NO managed_service_hints.go file, NO ConnectionHint struct, NO per-hint commits)

Net tranche 2: ~350 LoC, ~12 unit tests (one per Class B item × role × runtime + tier_decision pre-emit + fact-shell coverage). Down from ~580 LoC / 25 tests in the prep version because the hint table is retired (§7.2).

**Risk gate**: `TestEmittedFactShell_CitationGuideMatchesCitationMap` (§10.7) confirms every per-service shell points at a topic the citation map actually carries — the only engine-side curation surface.

### Tranche 3 — New phase enum + new brief composers (additive only)

**Pre-audit**: confirm tranches 1+2 merged + tests green; tier_decision schema (tranche 1) covers env-content brief needs.

Tranche 3 is **additive only** — new files added, existing files extended. Legacy atom file deletions move to tranche 6 (post-dogfood-confirmed, per Concern 4) so the architectural cut-over has a revertible safety net.

**Commits**:

1. `recipe(workflow): extend Phase enum with codebase-content + env-content`
   - LoC: ~60 (workflow.go + phase_entry.go cases + tests)
2. `recipe(briefs): add BuildCodebaseContentBrief — pointer-based composition`
   - LoC: ~250 (briefs.go composer + content/briefs/codebase-content/ atoms + tests, including size regression guard)
3. `recipe(briefs): add BuildEnvContentBrief — engine-emitted tier_decision facts`
   - LoC: ~200 (briefs.go composer + content/briefs/env-content/ atoms + tests)
4. `recipe(briefs): add BuildClaudeMDBrief — Zerops-free brief for dedicated CLAUDE.md sub-agent`
   - LoC: ~150 (briefs.go composer + content/briefs/claudemd-author/ atoms + tests including hard-prohibition-block presence + size regression < 8 KB)
5. `recipe(briefs): wire codebase-content + claudemd-author + env-content into buildBriefForKind dispatch`
   - LoC: ~70 (`briefs_subagent_prompt.go`: three new BriefKinds added to `buildBriefForKind` switch; engine produces one brief per `build-subagent-prompt` call).
   - **Locus note (per §6.3)**: parallel dispatch of `codebase-content` + `claudemd-author` per codebase is the **main agent's responsibility**, taught in `phase_entry/codebase-content.md` (tranche 3 commit 12). The engine produces one brief per `build-subagent-prompt` call serially; the main agent batches multiple `Agent` tool calls in a single message. There is NO parallel-dispatch logic in `briefs_subagent_prompt.go` — that's by design.
6. `recipe(brief-atoms): add codebase-content/{intro,synthesis_workflow,parent_recipe_dedup}.md`
   - LoC: ~600 (atoms only, no engine code)
7. `recipe(brief-atoms): add claudemd-author/{intro,init_voice,zerops_free_prohibition}.md`
   - LoC: ~200 (atoms; the prohibition block lives here verbatim)
8. `recipe(brief-atoms): add env-content/{intro,per_tier_authoring}.md`
   - LoC: ~400
9. `recipe(brief-atoms): add scaffold/decision_recording.md (additive — does NOT delete content_authoring.md yet)`
   - LoC: ~150
10. `recipe(brief-atoms): add feature/decision_recording.md (additive)`
    - LoC: ~150
11. `recipe(brief-atoms): add research/tier_decision_recording.md`
    - LoC: ~80
12. `recipe(brief-atoms): update phase_entry/{scaffold,feature,finalize,research,codebase-content,claudemd-author,env-content}.md`
    - LoC: ~330 net

**Net tranche 3 — engine code is ~350 LoC LESS than prior version**: ~2570 LoC total (mostly atom content; engine code under ~600 LoC), ~35 unit + 7 e2e tests. Total LoC is up vs prior version (~2240 LoC) because atom content moved into this tranche — but **the architecture pivot's value is the engine-LoC reduction**: §6.7's engine-emit + per-framework detector registry was retired in favor of the `claudemd-author` sub-agent, dropping ~200 LoC of detector code + ~150 LoC of fixture infrastructure. No per-framework detector registry exists; the sub-agent reads each codebase directly. Headline number up; engine code down; framework-knowledge surface bounded.

**Risk gate**: brief size — `TestCodebaseContentBrief_SizeUnder40KB` must pass (regression guard). Per-codebase brief target is ~25-29 KB; failure indicates accidental verbatim embed creeping in.

### Tranche 4 — Slotted IG + zerops.yaml comments + claude-md slot wiring

**Pre-audit**: confirm tranches 1-3 merged.

**Commits**:

1. `recipe(fragments): extend isValidFragmentID with slotted IG + zerops-yaml-comments + single-slot claude-md shapes`
   - LoC: ~100 + tests (includes back-compat for legacy `claude-md/{service-facts,notes}` sub-slots)
2. `recipe(assemble): rewrite injectIGItems for slotted fragments with legacy fallback`
   - LoC: ~150 + tests
3. `recipe(assemble): add injectZeropsYamlComments line-anchor insertion + 5-fixture corpus validity test`
   - LoC: ~120 (line-anchor regex insertion path only; no AST round-trip — see §6.7 rationale: yaml.v3 is in the codebase only for unmarshal-to-AST today; round-trip would need 250 LoC + corpus-fuzz infrastructure for zero benefit over line-anchor on the actual fixtures)
4. `recipe(assemble): extend yamlIntroSentence to read field_rationale facts`
   - LoC: ~60 + tests
5. `recipe(slot-shape): add CLAUDE.md slot-shape refusal — Zerops-free contract enforced at record-fragment time`
   - LoC: ~80 (slot_shape.go extension covering `## Zerops`, `zsc`, `zerops_*`, `zcp`, `zcli`, managed-service-hostname tokens + 5 refusal tests + 1 accepts-Zerops-free-content test). The contract lives in slot_shape.go (record-time refusal); validateCodebaseCLAUDE in tranche 5 backs it up at finalize.
6. `recipe(stitch): wire codebase/<h>/claude-md fragment into stitch path (replaces engine-emit precedent)`
   - LoC: ~30 + 2 e2e tests (`TestStitch_CodebaseClaudeMD_FromAgentFragment`, `TestStitch_CodebaseClaudeMD_LegacySubslotsFallback`)

Net tranche 4: ~540 LoC, ~22 unit tests. Down from the **original prep proposal's ~780 LoC** (which included `emitCodebaseClaudeMD` ~200 LoC + per-framework detectors + 5 fixture tests, all retired in the §6.7 + §6.7a pivot to `claudemd-author` sub-agent + slot-shape refusal). Comparison anchor is the prep, not an intermediate readiness draft.

**Risk gate**: line-anchor yaml comment insertion must produce yaml that still parses + retains all original fields against the 5-fixture corpus. CLAUDE.md slot-shape refusal must trigger on every Zerops-flavored token in §8.1's refusal list and must NOT trigger on legitimate `/init`-shaped content (regression test: feed run-15's apidev README's would-be `/init` content through the slot — must not refuse).

### Tranche 5 — Validator narrowing / new cross-surface duplication validator (closes R-15-6)

**Pre-audit**: confirm tranches 1-4 merged.

**Commits**:

1. `recipe(validators): narrow validateEnvREADME — char-cap moved to slot refusal`
   - LoC: -100 + ~20 (net deletion)
2. `recipe(validators): narrow validateCodebaseIG — item-cap enforced by slot existence`
   - LoC: -80 + ~20
3. `recipe(validators): reshape validateCodebaseCLAUDE — confirms claudemd-author sub-agent's Zerops-free shape (no Zerops content, /init structure, ≤ 80 lines)`
   - LoC: ~80 (full rewrite — slot-shape refusal at record-time is the primary contract; this validator is the finalize backstop)
4. `recipe(validators): add validateCrossSurfaceDuplication — Notice; method + threshold calibrated against run-15 corpus`
   - LoC: ~120 + tests + calibration script. **Pre-commit calibration step** (recorded in commit message): run 3 candidate designs (topic-name match, lower-threshold Jaccard with shingles, hybrid) against the 2 known R-15-6 dup pairs in run-15 apidev/appdev READMEs + ≥ 5 representative non-dup IG/KB pairs from the same files. Empirical baseline already computed: Jaccard ≥ 0.7 catches zero real dups (real-dup Jaccard ≈ 0.14-0.16; non-dup Jaccard ≈ 0.06). Notice not blocking; primary R-15-6 closure is the structural fix in §4.
5. `recipe(validators): add validateCrossRecipeDuplication (parent-recipe similarity → notice)`
   - LoC: ~80 + tests. Same calibration discipline.

Net tranche 5: ~140 LoC (-180 deletion + 320 addition), ~10 unit tests.

### Tranche 6 — Dogfood-conditional legacy atom deletion

**Pre-audit**: a complete run-16 dogfood has shipped on tranche-3+4+5 architecture; codebase-content + env-content briefs produced acceptable output across all surfaces; no agent regressions traced to missing atoms.

This tranche is **explicitly conditional** on dogfood signal. If dogfood reveals that one of the legacy atoms was load-bearing in a way the new atoms don't replace, delay this tranche and patch the new atom set first.

**Commits**:

1. `recipe(brief-atoms): delete scaffold/content_authoring.md (superseded by decision_recording.md, dogfood-confirmed)`
   - LoC: -500
2. `recipe(brief-atoms): delete feature/content_extension.md (superseded by decision_recording.md)`
   - LoC: -250
3. `recipe(brief-atoms): delete finalize/{intro,validator_tripwires,anti_patterns}.md (superseded by stitch-only finalize)`
   - LoC: -160

Net tranche 6: -910 LoC (pure deletion). Zero new tests.

### Tranche 7 — Sign-off

**Commits**:

1. `docs(spec-content-surfaces): rewrite §Surface 6 to align with sub-agent-authored /init shape (§15 of readiness)`
   - LoC: ~80 (spec edit per §15)
2. `docs(zcprecipator3): run-16 readiness sign-off — CHANGELOG entry + system.md verdict-table updates`
   - LoC: ~200 (CHANGELOG.md + system.md). Pre-specified verdict-table rows to land (per §2.7 audit):
     - `Sub-agent-authored CLAUDE.md via dedicated claudemd-author dispatch + Zerops-free brief (run-16 §6.7a)` — DISCOVER ✅ Replaces `claudeMDForbiddenSubsections` ban-list with strict-prohibition brief + record-time slot refusal + finalize validator backstop. No engine-side framework registry — agent reads the codebase directly
     - `Slot-shape refusal at record-fragment time (run-16 §8)` — TEACH ✅ Per-fragment-id structural caps moved from finalize-validator to record-time refusal
     - `Phase enum extended to 7 phases (run-16 §6.1)` — Section 3 narrative update; pipeline becomes research → provision → scaffold → feature → codebase-content → env-content → finalize
     - `FactRecord polymorphic Kind discriminator (run-16 §5.5)` — TEACH ✅ Engine routes per-Kind validation; legacy Kind="" platform-trap preserved as back-compat
     - `Engine-emit Class B/C umbrella facts (run-16 §7.1)` — TEACH ✅ Per-role + own-key-aliases facts emitted with mechanism-level Why; framework-specific slots agent-filled
     - `Per-managed-service fact shells (run-16 §7.2)` — Hybrid ✅ Engine pre-seeds shape + citation-guide; agent fills Why from atom via fill-fact-slot. Replaces prep's hint-table proposal — the table would have been catalog-drift
     - `Engine-emit tier_decision facts (run-16 §5.3)` — TEACH ✅ Per-tier capability matrix delta computed from tiers.go::Diff; agent-extended tierContext slot is the discovery half
     - `Subdomain dual-signal eligibility (run-16 §9, R-15-1 closure)` — TEACH ✅ OR detail.SubdomainAccess with Ports[].HTTPSupport; closes recipe-authoring auto-enable failure mode
     - `validateCrossSurfaceDuplication (run-16 §6.8)` — TEACH (defensible) ⚠️ Notice — Jaccard IG↔KB heuristic backstop; promotion to Blocking deferred pending dogfood signal (parallels tier-prose-*-mismatch precedent)
     - `validateCrossRecipeDuplication (run-16 §6.8)` — TEACH (defensible) ⚠️ Notice — parent/child similarity check
     - `validateCodebaseCLAUDE reshape (run-16 §6.8)` — TEACH ✅ Confirms claudemd-author sub-agent's Zerops-free /init shape held; finalize backstop for slot-shape refusal at record-time
3. `docs(zcprecipator3): archive run-16-prep.md + run-16-readiness.md to plans/archive/`
   - LoC: 0 (git mv only)
4. `docs(spec-content-surfaces): amend any further drift surfaced during implementation`
   - LoC: as needed

---

## §12. Risk register

### Risk 1 — Engine-emitted prose drifts from atom content

The engine's `Why` prose for universal-for-role facts (Class B: bind/trust, sigterm-drain, worker-no-http) is curated in code; the underlying mechanism teaching also lives in `briefs/scaffold/platform_principles.md`. They can drift.

**Mitigation**: §10.7's `TestEmittedFactShell_CitationGuideMatchesCitationMap` keeps the citation-guide mapping in sync. For Class B facts, the surface is small (3-4 hardcoded mechanism explanations) and changes are review-gated. **The bigger drift risk — engine-curated per-service why-prose drifting from per-service atoms — was eliminated by retiring the prep's `managedServiceHints` table (§7.2)**. With the fact-shell pattern, no engine-side why-prose exists for the per-service idioms; the atom IS the source.

### Risk 2 — Brief size headroom (resolved structurally with regression guard)

Structurally resolved by §6.2's pointer-based composition. Per-codebase brief is ~25-29 KB (~today's scaffold brief size); env-content brief is ~25 KB; claudemd-author brief is ~2-3 KB. The prep's "subset spec verbatim per phase if size bites" mitigation is no longer needed because the spec isn't embedded in the first place.

**Regression guards** (named explicitly so future cross-include accidents fire before reaching the agent):
- `TestCodebaseContentBrief_SizeUnder40KB` — tranche 3 risk gate; if a future change accidentally adds a verbatim embed or cross-includes a large atom, this test fires before the brief is dispatched.
- `TestBuildClaudeMDBrief_SizeUnder8KB` — same shape for the claudemd-author brief; size cap below 8 KB ensures Risk 7's "no platform principles atom" assertion can't be silently bypassed by an accidental include.
- `TestEnvContentBrief_SizeUnder40KB` — same shape for env-content brief.

### Risk 3 — Yaml comment insertion regex misfires on edge-case shapes

`injectZeropsYamlComments` ships line-anchor insertion only (§6.7 commit 3). Regex-based block boundary detection has edge cases: comments could land inside multi-line strings if the regex isn't anchored carefully, or duplicate insertions could occur if a block name (e.g. `envVariables:`) appears nested under multiple parents.

**Mitigation**: 5-fixture corpus validity test confirms post-insertion yaml still parses + retains all original fields (laravel-jetstream + laravel-showcase + run-15 apidev/appdev/workerdev). Per-test families in §6.7 cover boundary detection, indentation preservation, no-match-inside-strings, and field-order preservation. AST round-trip path is **deferred** — if a future yaml shape genuinely requires it, that round can add it with corpus fuzz at that time; run-16 ships the simpler path that has no round-trip risk.

### Risk 4 — Per-codebase content sub-agent doesn't see deploy-iteration narrative

Today scaffold sub-agent has the densest context. Tomorrow's content sub-agent reads facts.jsonl + on-disk source/zerops.yaml, NOT live iteration.

**Mitigation**: facts capture mechanism + why at densest context. Three fact subtypes (porter_change, field_rationale, tier_decision) cover every routable class. Operational class facts route to existing surfaces (zerops.yaml comments / KB / code comments) — no separate stream needed. If a deploy-phase agent forgets to record, the content sub-agent doesn't know — same as today's loss across phase boundaries. The decision_recording.md atom teaches "if a porter would ask why?, record it" with worked examples grounded in run-15 apidev's published comments.

### Risk 5 — Migration: existing recipes shouldn't have to re-author

**Mitigation**: keep the legacy `codebase/<h>/integration-guide` (single fragment) shape accepted by `isValidFragmentID`. `injectIGItems` falls back to legacy if no slotted fragments present. `FactRecord.Validate` Kind="" path preserves platform-trap semantics. Legacy `codebase/<h>/claude-md/{service-facts,notes}` sub-slot IDs remain accepted by `isValidFragmentID` for back-compat; new dispatches use the single `codebase/<h>/claude-md` slot authored by the claudemd-author sub-agent.

### Risk 6 — R-15-2 (slug list consumer absent) not addressed by this plan

R-15-1 is closed by tranche 0 (forensic). R-15-2 (slug list section without a concrete consumer) is out of scope for this run-16 plan; recommend deletion of the section if no consumer surfaces — fold into tranche 7 sign-off if confirmed dead.

### Risk 7 — claudemd-author brief discipline (Zerops bleed-through)

The dedicated CLAUDE.md sub-agent's correctness depends on the brief staying strictly Zerops-free. If the brief composer accidentally pulls in platform-principles atoms, env-var-aliasing teaching, or managed-service hints (e.g., via cross-includes from shared atom files), the sub-agent will replicate the run-15 wrong shape — Zerops-flavored sections in CLAUDE.md.

**Mitigation (defense in depth)**:

- **Composer-time test** (`TestBuildClaudeMDBrief_NoPlatformPrinciplesAtom`) explicitly asserts the platform principles atom is NOT in the brief. Catches accidental cross-includes.
- **Brief content test** (`TestBuildClaudeMDBrief_ContainsHardProhibitionBlock`) asserts the verbatim no-Zerops-content prohibition is present.
- **Record-time slot refusal** (§8.1): the `codebase/<h>/claude-md` slot refuses bodies containing `## Zerops`, `zsc`, `zerops_*`, `zcp`, `zcli`, or any managed-service hostname declared in `plan.Services`. Same-context recovery — the agent sees the refusal message and re-authors.
- **Finalize validator** (`validateCodebaseCLAUDE`) backstops at stitch — if a CLAUDE.md slips through with Zerops content despite the slot refusal, the validator blocks publication.
- **Framework variance is NOT a risk** — the sub-agent reads the actual codebase (Read + Glob + Bash on src/), so framework-specific output works for any framework Claude can read (effectively all of them). No engine-side framework registry to keep current.

### Risk 8 — Tier_decision fact accuracy (Gap A.2 mitigation)

`tier_decision` is engine-pre-emitted from tiers.go::Diff output (§5.3 — agent does not record directly). If `tiers.go::Diff` misses a tier-scaling change OR if the agent-extended `tierContext` slot is sparse, Surface 3 (env-import-yaml comments) loses authentic rationale and falls back to engine-derived prose only.

**Mitigation**:

- Validator at finalize: if Surface 3 comments are ≤ engine-derived shape only (no agent-extended `tierContext`), notice (not blocking).
- Tranche 2 commit 3 includes tests asserting engine pre-emit covers every cross-tier delta documented in `tiers.go::Diff`.

### Risk 9 — Deferred legacy atom deletion creates short-term clutter

Tranche 6 conditionalizes legacy atom deletion on dogfood. During tranches 3-5, the codebase carries BOTH old (`scaffold/content_authoring.md`) and new (`scaffold/decision_recording.md`) atoms; the `briefs.go` composer references only the new atoms but the old files exist.

**Mitigation**: tranche 3 commits 7-8 add the new atoms but explicitly note in the commit message that the old atoms are NOT deleted yet pending dogfood. CHANGELOG entry at tranche 3 lands names the deferred deletion. The old atoms are NOT included in any new brief, so they don't pollute dispatched bytes — they're orphaned in the repo until tranche 6 removes them.

### Risk 10 — Catalog-drift recurrence on future engine-emit additions

Run-16 retires the prep's `managedServiceHints` table because per-service connection idioms drift unboundedly when curated in engine code. The same trap can recur: future readiness plans may propose new engine-side hand-curated content tables (per-framework SIGTERM patterns, per-managed-service health-check shapes, etc.) for the same plausible reason ("agent saves a tool call").

**Mitigation**:

- §2.7's TEACH/DISCOVER alignment audit becomes a required section of every future run-N readiness plan. Each engine-side artifact must carry a first-principles argument, not appeal-to-authority.
- The fact-shell pattern (§7.2) is the recommended template for "structurally predictable, content discovery-bound" cases — engine pre-seeds shape, agent fills via knowledge-atom lookup. Future analogous needs should use this pattern before considering hand-curated tables.
- When a future plan proposes a new engine-side hint/lookup table, reviewer obligation: ask "does this grow combinatorially with framework × runtime × version? does the canonical content already live in an atom?" If yes to either, default to fact-shell.

---

## §13. R-15-N defect closure mapping

| Defect | Closed by | How |
|--------|-----------|-----|
| R-15-1 (subdomain) | Tranche 0 | Dual-signal eligibility — OR `detail.SubdomainAccess` with deploy-time `Ports[].HTTPSupport` (§9). Verification gate refuses readiness sign-off if any manual enable appears. |
| R-15-2 (slug list) | Tranche 7 | Recommend deletion if no concrete consumer; fold into sign-off if confirmed dead. |
| R-15-3 (duplicate extract markers) | Tranche 1 | `env/<N>/intro` slot refuses bodies with `<!-- #ZEROPS_EXTRACT_*` tokens (§8.1). |
| R-15-4 (duplicate H2 in CLAUDE.md) | Tranche 3 (brief) + 4 (slot refusal) + 5 (validator) | Dedicated `claudemd-author` sub-agent with Zerops-free brief (§2.6, §6.7a) prevents the bleed-through that produced the wrong shape. Slot-shape refusal at record-fragment time (§8.1) blocks `## Zerops` / managed-service / `zsc` / `zerops_*` tokens. validateCodebaseCLAUDE backstops at finalize. |
| R-15-5 (unnumbered IG sub-section) | Tranche 1+4 | `codebase/<h>/integration-guide/<n>` is slotted (n=1..5). Slot 6 doesn't exist. |
| R-15-6 (cross-surface duplication X-Cache + duplex) | Tranche 3 (primary) + 5 (backstop) | **Primary**: codebase-content sub-agent sees BOTH IG and KB candidate fact lists in one phase (§4) — structural single-author closure. **Backstop**: `validateCrossSurfaceDuplication` lands as Notice (Jaccard ≥0.7) — heuristic signal, not blocking. Promotion to blocking deferred per `tier-prose-*-mismatch` precedent. |
| R-15-7 (F.3 classification adoption 5.3%) | Tranche 1+3 | porter_change facts carry `candidateClass` mandatorily (§5.1); classification reach extends to fact-time. New decision_recording.md atom teaches it positively. |
| R-15-P-1 (slug list not in brief) | Same as R-15-2 | |
| R-15-P-2 (F.2 not in feature brief) | Tranche 3 | Feature phase no longer authors content; codebase-content phase points at spec on disk (§6.2). |
| R-15-P-3 (§A's e2e observer not in readiness) | Tranche 0 | `make verify-dogfood-no-manual-subdomain-enable` operational gate (§9.5 step 3). |
| R-15-P-4 (F.3 adoption) | Same as R-15-7 | |

**Closure**: 6/7 R-15-N defects closed by structural changes (R-15-1, -3, -4, -5, -6, -7). R-15-2 closure pending tranche-7 confirmation. R-15-P-3 closed by the operational verification gate.

---

## §14. Pre-implementation triple-verification checklist

Replaces prep §13. Each item names the verification command + the expected match + termination guard against truncation. The fresh instance walks this checklist BEFORE writing any code.

**Per §0 rule 5**: every "F at line L" cite below must be verified with `grep -n PATTERN F` (no head limit) before relied on. Section anchors and function signatures shift across edits.

### §14.1 Architecture corrections (§2)

- [ ] Read [docs/zcprecipator3/system.md §"Workspace YAML vs deliverable YAML"](../system.md#L149-L162). Verify line range with `grep -n "Workspace YAML vs deliverable YAML" docs/zcprecipator3/system.md`. Confirm: workspace = "no `project:` block, ... `startWithoutCode: true`"; deliverable = "full `project:` + `envVariables` + `buildFromGit` + `zeropsSetup`".
- [ ] Read [run-15 environments/0 — AI Agent/import.yaml](../runs/15/environments/0%20—%20AI%20Agent/import.yaml). Confirm `enableSubdomainAccess: true` appears on every HTTP-serving service block. Use `grep -n "enableSubdomainAccess" "<path>"` (no head limit).
- [ ] Read [docs/spec-content-surfaces.md §Classification × surface compatibility](../../spec-content-surfaces.md). Verify line range with `grep -n "Classification × surface compatibility" docs/spec-content-surfaces.md` (no head limit). Confirm `operational → CLAUDE.md` row present in the table. Note: this readiness doc's §2.4 + §2.6 SUPERSEDE the spec routing — operational facts route to existing surfaces (zerops.yaml comments / KB / code-comments); CLAUDE.md is sub-agent-authored from a Zerops-free brief and doesn't consume the deploy-phase fact stream. The §Surface 6 rewrite per §15 below aligns the spec with the new contract.
- [ ] Read [internal/recipe/classify.go::compatibleSurfaces](../../../internal/recipe/classify.go). Verify with `grep -n "func compatibleSurfaces" internal/recipe/classify.go` (no head limit). Confirm `case ClassOperational: return []Surface{SurfaceCodebaseCLAUDE}` is present. Engine and spec agree operational class is publishable today; the readiness doc's pivot moves operational routing to existing surfaces while leaving the engine routing intact for back-compat.

### §14.2 IG taxonomy derivation (§3)

- [ ] Read [/Users/fxck/www/laravel-jetstream-app/README.md](../../../../laravel-jetstream-app/README.md). Count IG items: `grep -nE "^### [0-9]" /Users/fxck/www/laravel-jetstream-app/README.md` (no head limit). Expect 4.
- [ ] Read [/Users/fxck/www/laravel-showcase-app/README.md](../../../../laravel-showcase-app/README.md). Count IG items (expect 5) and KB bullets after `## Integration Guide`'s closing `## Gotchas`: `awk '/^## Gotchas/,EOF' | grep -c "^- \*\*"`. Expect 7.
- [ ] Read [docs/zcprecipator3/runs/15/apidev/README.md](../runs/15/apidev/README.md). Count `### N.` numbered IG items (expect 5) + unnumbered `### ` headings inside IG (expect 1) + KB bullets (expect 8).
- [ ] Cross-check §3.2 Class B examples against jetstream / showcase / run-15 — confirm Object Storage is NOT listed in Class B (it belongs in Class C). Confirm bundled-class caveat is present.

### §14.3 Engine code refs (§6)

For each cite below, run `grep -n PATTERN F` first (no head limit) to confirm current line, then `Read file_path=F offset=L limit=K` (paginated read) to verify the function spans the claimed range:

- [ ] [workflow.go:14-22](../../../internal/recipe/workflow.go#L14-L22) — Phase enum, 5 const values today. Verify with `grep -n "PhaseResearch\|PhaseProvision" internal/recipe/workflow.go`.
- [ ] [briefs.go:144,267,339](../../../internal/recipe/briefs.go) — three brief composers: `BuildScaffoldBriefWithResolver`, `BuildFeatureBrief`, `BuildFinalizeBrief`. Verify with `grep -n "^func Build" internal/recipe/briefs.go`.
- [ ] [handlers.go:205::dispatch](../../../internal/recipe/handlers.go#L205) — dispatch function. Verify with `grep -n "^func dispatch\|^func handleRecordFragment" internal/recipe/handlers.go`. Confirm 13 actions per `grep -n "jsonschema:.*One of:" internal/recipe/handlers.go`.
- [ ] [handlers.go:349::handleRecordFragment](../../../internal/recipe/handlers.go#L349) — record-fragment handler.
- [ ] [handlers_fragments.go:153::isValidFragmentID](../../../internal/recipe/handlers_fragments.go#L153) — fragment ID validation. Verify with `grep -n "^func isValidFragmentID" internal/recipe/handlers_fragments.go`.
- [ ] [facts.go:24-37::FactRecord](../../../internal/recipe/facts.go#L24-L37) + [:40-54::Validate](../../../internal/recipe/facts.go#L40-L54). Verify with `grep -n "^type FactRecord\|^func (.*) Validate" internal/recipe/facts.go`.
- [ ] [surfaces.go:101::SurfaceContract](../../../internal/recipe/surfaces.go#L101) + [:128::surfaceContracts](../../../internal/recipe/surfaces.go#L128). Verify with `grep -n "^type SurfaceContract\|^var surfaceContracts" internal/recipe/surfaces.go`.
- [ ] [assemble.go:170::injectIGItem1](../../../internal/recipe/assemble.go#L170), [:204::codebaseIGItem1](../../../internal/recipe/assemble.go#L204), [:223::yamlIntroSentence](../../../internal/recipe/assemble.go#L223), [:388::substituteFragmentMarkers](../../../internal/recipe/assemble.go#L388). Verify with `grep -n "^func injectIGItem1\|^func codebaseIGItem1\|^func yamlIntroSentence\|^func substituteFragmentMarkers" internal/recipe/assemble.go`.
- [ ] [validators.go:200-206](../../../internal/recipe/validators.go#L200-L206) — 7 validator registrations. Verify with `grep -n "RegisterValidator" internal/recipe/validators.go`.
- [ ] [phase_entry.go:12::loadPhaseEntry](../../../internal/recipe/phase_entry.go#L12) + [:29::gatesForPhase](../../../internal/recipe/phase_entry.go#L29). Verify with `grep -n "^func loadPhaseEntry\|^func gatesForPhase" internal/recipe/phase_entry.go`.
- [ ] `ls internal/recipe/content/briefs/scaffold/content_authoring.md` exists (will be deleted in tranche 6 per Concern 4 — deferred from tranche 3).
- [ ] `ls internal/recipe/content/briefs/feature/content_extension.md` exists (will be deleted in tranche 6).
- [ ] **Confirm proposed NEW files do NOT already exist.** Run `ls internal/recipe/{engine_emitted_facts,slot_shape,tier_service_deltas}.go 2>&1` — expect "No such file or directory" for each. Also confirm `ls internal/recipe/codebase_claude_emit.go 2>&1` returns "No such file or directory" (it should NEVER exist — §6.7 commit 4 retired engine-emit). If any of these files exists, the doc's "NEW FILE" / "NOT created" claims are stale; flag as a tranche-blocker before implementation begins (a previous round may have shipped a partial implementation under one of these names that this readiness plan didn't account for).

### §14.4 Schema details (§5)

- [ ] [handlers.go:107-128::RecipeInput](../../../internal/recipe/handlers.go#L107-L128) — Classification field at line 127. Verify with `grep -n "^type RecipeInput\|Classification string" internal/recipe/handlers.go`.
- [ ] [handlers.go:135::RecipeResult](../../../internal/recipe/handlers.go#L135) — confirm SurfaceContract, FragmentID, BodyBytes, Appended, PriorBody, Notice fields. Verify with `grep -n "^type RecipeResult\|FragmentID\|BodyBytes" internal/recipe/handlers.go`.
- [ ] [classify.go::classificationCompatibleWithSurface](../../../internal/recipe/classify.go) — refusal logic. Verify with `grep -n "^func classificationCompatibleWithSurface" internal/recipe/classify.go`.
- [ ] [classify.go::compatibleSurfaces](../../../internal/recipe/classify.go) — class × surface map. Same grep.
- [ ] [facts.go:24-54](../../../internal/recipe/facts.go#L24-L54) — FactRecord struct + Validate.

### §14.5 Test conventions (§10)

- [ ] [validators_kb_quality_test.go](../../../internal/recipe/validators_kb_quality_test.go) — `t.Parallel()` + body+SurfaceInputs test pattern + `containsCode(vs, "code-name")` helper. Verify with `grep -n "t.Parallel\|containsCode" internal/recipe/validators_kb_quality_test.go`.
- [ ] [handlers_test.go::TestDispatch_StartStatusRecordFactEmitYAML](../../../internal/recipe/handlers_test.go) — sequential-dispatch handler test pattern. Note: name has NO "Finish" (corrected from prep §13.5). Verify with `grep -n "^func TestDispatch_StartStatusRecord" internal/recipe/handlers_test.go`.
- [ ] [assemble_test.go::TestAssemble_TemplateRendersStructuralData](../../../internal/recipe/assemble_test.go) — syntheticShowcasePlan + AssembleX + (out, missing, error) pattern. Verify with `grep -n "^func TestAssemble_TemplateRendersStructuralData" internal/recipe/assemble_test.go`.

### §14.6 Defect closure (§13)

For ANALYSIS.md / CONTENT_COMPARISON.md cites, verify each with `grep -nE "^## §[A-Z0-9]" runs/15/ANALYSIS.md` and `grep -nE "^## §[0-9]" runs/15/CONTENT_COMPARISON.md` (no head limit, both files) BEFORE depending on a line number.

- [ ] [docs/zcprecipator3/runs/15/ANALYSIS.md §7](../runs/15/ANALYSIS.md#L984) — "R-15-N defect numbering — full table". Confirm at line 984. (The prep-verification falsely claimed "no §7" based on a head-truncated grep — see §0.)
- [ ] [runs/15/ANALYSIS.md §A](../runs/15/ANALYSIS.md#L243), §A.2, §A.3 — R-14-1 closure attempt forensic + R-15-1 emergence. The full per-cluster evidence; complements §7's summary.
- [ ] [runs/15/CONTENT_COMPARISON.md §2](../runs/15/CONTENT_COMPARISON.md#L113) — R-15-3 evidence (search for "R-15-3" in §2).
- [ ] [runs/15/CONTENT_COMPARISON.md §6](../runs/15/CONTENT_COMPARISON.md#L571) — R-15-4 evidence (search for "DUPLICATE H2" in §6).
- [ ] [runs/15/CONTENT_COMPARISON.md §4](../runs/15/CONTENT_COMPARISON.md#L367) — R-15-5 + R-15-6 evidence.

### §14.7 R-15-1 forensic (§9)

- [ ] [internal/tools/deploy_subdomain.go:138-141](../../../internal/tools/deploy_subdomain.go#L138-L141) — confirm the §A premise comment that this readiness doc names as wrong. Verify with `grep -n "non-workers import" internal/tools/deploy_subdomain.go`.
- [ ] [internal/tools/deploy_subdomain.go::platformEligibleForSubdomain](../../../internal/tools/deploy_subdomain.go) — confirm current implementation reads only `detail.SubdomainAccess` (no Ports loop fallback). Verify with `grep -n "^func platformEligibleForSubdomain" internal/tools/deploy_subdomain.go`.
- [ ] [internal/recipe/yaml_emitter.go:164,181](../../../internal/recipe/yaml_emitter.go) — confirm workspace yaml emits `enableSubdomainAccess: true` for dev (line 164) and stage (line 181). Verify with `grep -n "enableSubdomainAccess" internal/recipe/yaml_emitter.go`.
- [ ] [internal/ops/subdomain.go::enableSubdomainAccessWithRetry](../../../internal/ops/subdomain.go) — confirm bounded backoff exists; absorbs `noSubdomainPorts` race independently of eligibility predicate. Verify with `grep -n "^func enableSubdomainAccessWithRetry" internal/ops/subdomain.go`.
- [ ] Read run-15 session jsonl for scaffold-app (see [runs/15/SESSSION_LOGS](../runs/15/SESSSION_LOGS)) — verify the 5-line forensic timeline at §9.1 against actual log entries. Reproduction command (use this verbatim rather than deriving from scratch):

  ```bash
  # Print DEPLOY / VERIFY / SUBDOMAIN events with timestamps from the scaffold-app subagent jsonl.
  # Replace the path if a fresher run exists; this scopes to run-15 forensic.
  python3 - <<'PY'
  import json, glob
  for path in sorted(glob.glob('/Users/fxck/www/zcp/docs/zcprecipator3/runs/15/SESSSION_LOGS/subagents/*.jsonl')):
      with open(path) as f:
          for line in f:
              try: entry = json.loads(line)
              except: continue
              tool = (entry.get('message', {}).get('content', [{}])[0] or {}).get('name', '') if isinstance(entry.get('message', {}).get('content'), list) else ''
              if any(k in tool for k in ('zerops_deploy', 'zerops_verify', 'zerops_subdomain')):
                  ts = entry.get('timestamp', '?')
                  print(f"{ts}  {tool}  {path.split('/')[-1]}")
  PY
  ```

  Expected output mirrors §9.1's 5-line timeline (12:35:24 DEPLOY appdev → 12:37:07 zerops_subdomain enable appdev → 12:37:39 DEPLOY appstage → 12:39:02 zerops_subdomain enable appstage → 12:40:17 DEPLOY appdev with SubdomainAccessEnabled=true). If the timeline doesn't match, §9's diagnosis is stale — recompute against the latest run before tranche 0 lands.

### §14.8 Risk register (§12)

- [ ] Confirm Risk 1 mitigation: §10.7's `TestEmittedFactShell_CitationGuideMatchesCitationMap` covers per-service shells; Class B mechanism prose surface is small (3-4 hardcoded entries). No paired per-hint tests required (§7.2 retired the hint table).
- [ ] Confirm Risk 2 mitigation: §6.2 brief composition is pointer-based, ~25-29 KB target; tranche-3 risk gate `TestCodebaseContentBrief_SizeUnder40KB` enforces the regression guard.
- [ ] Confirm Risk 3 mitigation: tranche 4 commit 3 ships line-anchor insertion as the **only path** (no fallback exists). 5-fixture corpus validity test confirms post-insertion yaml still parses + retains all original fields. AST round-trip is deferred to a future readiness round if line-anchor proves insufficient on a yaml shape we haven't seen yet; run-16 ships line-anchor only.
- [ ] Confirm Risk 5 mitigation: §6.5 keeps legacy `codebase/<h>/integration-guide` ID accepted for back-compat; `injectIGItems` falls back to legacy.
- [ ] Confirm Risk 7 mitigation: composer-time test asserts platform principles atom is NOT in claudemd-author brief; brief content test asserts hard-prohibition block is present; record-time slot refusal blocks Zerops content; finalize validator backstops.
- [ ] Confirm Risk 8 mitigation: `tier_decision` is engine-pre-emit-only (§5.3); no agent recording site to under-fill.

### §14.9 Coverage matrix exercise — with worked example (Concern 6 fix)

Walk the publishable-class × fact-subtype matrix in §2.4. For each row that routes to a non-empty surface, confirm:

- The fact subtype's required fields cover the class's content needs.
- The recording rule excludes the discard classes mechanically.
- At least one engine-emit pre-fill OR at least one decision_recording.md atom example targets that row.

If any row lacks both engine-emit AND atom-example, the row's surface will starve under load — flag as a tranche-3 gap before merge.

**Worked example — row "platform-invariant → KB,IG → porter_change → record"**:

- ✓ Required fields: Topic, CandidateClass="platform-invariant", CandidateSurface∈{CODEBASE_KB, CODEBASE_IG}, Why, optional CandidateHeading + Diff + CitationGuide. Cover: a porter-actionable change with the platform-side mechanism.
- ✓ Recording rule: classification ∈ {platform-invariant, intersection, scaffold-decision (config|code)} (§5.1); operational/framework-quirk/library-metadata/self-inflicted/scaffold-decision (recipe-internal) all filtered out before record-fact.
- ✓ Engine pre-emit: §7.1 emits "Bind 0.0.0.0 + trust L7 proxy" (RoleAPI/Frontend/Monolith) + "Drain on SIGTERM" (nodejs roles) + "Read managed-service credentials from own-key aliases" (when services consumed) + per-managed-service connection hints (§7.2).
- ✓ Atom example: `briefs/scaffold/decision_recording.md` teaches "if you wrote `app.enableCors({ exposedHeaders: ['X-Cache'] })` because a custom response header would be stripped cross-origin, that's a `porter_change` with `candidateClass=intersection` and `candidateSurface=CODEBASE_KB`."

**Worked example — row "operational → existing surfaces → no fact subtype"**:

- ✓ Routing: operational facts with a yaml-field anchor → `field_rationale` (e.g. "execOnce key burns on crashed init" anchors to `run.initCommands`). Operational facts without a yaml anchor that are porter-relevant Zerops platform traps → `porter_change` with `candidateSurface=CODEBASE_KB`. Repository-internal operational facts → code comments at the relevant site. Generic dev-loop content → CLAUDE.md, authored by the dedicated `claudemd-author` sub-agent reading package.json scripts directly (no fact-stream feed).
- ✓ No starvation risk because Surface 6 (CLAUDE.md) is authored by a peer sub-agent reading the codebase directly; the row doesn't depend on deploy-phase agent recording.
- ✓ Atom example: `briefs/scaffold/decision_recording.md` teaches "operational observations like 'execOnce burns the key on crashed init' belong in the `zerops.yaml` comment for the `initCommands` field; record as `field_rationale` with the yaml field path as scope."

Walk the remaining rows the same way before claiming coverage.

### §14.10 Triple-verification result format

The fresh instance reports back:

```
## Triple-verification report — run-16-readiness

§14.1 Architecture corrections: PASS / FAIL with notes
§14.2 IG taxonomy: PASS / FAIL with notes
§14.3 Engine code refs: PASS / FAIL with notes
§14.4 Schema details: PASS / FAIL with notes
§14.5 Test conventions: PASS / FAIL with notes
§14.6 Defect closure: PASS / FAIL with notes
§14.7 R-15-1 forensic: PASS / FAIL with notes
§14.8 Risk register: PASS / FAIL with notes
§14.9 Coverage matrix: PASS / FAIL with notes (each row has worked example or fail)

## Anomalies found
<list of any discrepancies between this guide and the actual artifacts>

## Truncation guard violations
<any negative-existence claim made without an unbounded read; §0 rules 1-4 forbid them>

## Load-bearing claim violations
<any "engine X produces Y" or "validator V catches C" claim that wasn't verified per §0 rule 6: read what the cited mechanism actually returns; compute the validator's score on the actual C artifact>

## Line-cite drift
<any cite where line number differs from current artifact state — note the corrected line>

## Recommended corrections to the guide
<list of changes the guide needs before implementation starts>

## Confidence level for implementation
<HIGH / MEDIUM / LOW with reasoning>
```

If all checklist items PASS, no anomalies surface, and no truncation/drift violations are recorded, the guide is implementation-ready. Otherwise, this guide gets corrected before any code is written.

---

## §15. Spec §Surface 6 rewrite — lands alongside the claudemd-author sub-agent (tranche 7 commit 1)

The current [docs/spec-content-surfaces.md §Surface 6](../../spec-content-surfaces.md#L221-L266) prescribes a Zerops-flavored 3-section structure that contradicts its own test. Tranche 7 commit 1 lands the rewrite. Preview of the new contract (drop-in replacement for the existing §Surface 6):

```markdown
### Surface 6 — Per-codebase CLAUDE.md

**Reader**: Someone (human or AI agent) with this repo checked out locally,
working on the codebase.

**Purpose**: Generic codebase operating guide — the same shape `claude /init`
would produce. Project overview, build/run/test commands, code architecture.
Zero Zerops-specific content; the Zerops integration is documented in IG
(Surface 4) / KB (Surface 5) / zerops.yaml comments (Surface 7).

**The test**: *"Would `claude /init` produce content of this shape for this
repo? Is anything Zerops-specific here that belongs in IG / KB / yaml comments
instead?"*

**Authoring**: SUB-AGENT-AUTHORED via a dedicated `claudemd-author` peer
dispatched in parallel with the codebase-content sub-agent at phase 5. Brief
is **strictly Zerops-free** (no platform principles, no env-var aliasing, no
managed-service hints, no dev-loop teaching) so that the bleed-through that
produced the wrong run-15 shape ("`## Zerops service facts`") cannot recur.
The sub-agent reads the codebase (`Read`, `Glob`, `Bash`) and produces
`/init`-quality output for any framework — no engine-side framework registry.

**Structure (fixed; matches `/init` shape)**:

​```
# <repo-name>

<1-sentence framing — framework, version, what this codebase does>

## Build & run

- <command from package.json/composer.json scripts, with one-line label>
- ...

## Architecture

- `src/<entry>` — <auto-derived label>
- `src/<dir>/` — <auto-derived label per framework convention>
- ...
​```

**Belongs here**:
- Project overview line.
- Build/dev/test commands enumerated from package.json scripts (or
  composer.json scripts for PHP codebases).
- Top-level src/ structure with one-line per-entry labels.

**Does not belong here**:
- Zerops platform mechanics (→ IG / KB / zerops.yaml comments).
- Managed-service hostnames or env-var aliases (→ zerops.yaml comments).
- Migration / seed recovery procedures (→ zerops.yaml comments at
  `initCommands`, OR code comments in the migration script).
- Cross-codebase contracts (→ code comments at publish/subscribe sites).
- Recipe-internal architectural decisions (→ code comments).

**Length**: ~30-50 lines depending on codebase complexity. No hard cap;
shape and Zerops-content-absence are the contract.

**Validator**: `validateCodebaseCLAUDE` confirms the sub-agent's output
shape held — 3 sections, no `## Zerops` headings, no managed-service hostname
references, no `zsc` / `zerops_*` / `zcp` / `zcli` tool name leaks, body
≤ 80 lines. Blocking violation if shape drift. Record-time slot-shape
refusal at `record-fragment` (§Surface contracts) catches violations earlier
with same-context recovery; the validator is the finalize backstop.

**Anti-pattern**: any of the run-15 reference recipes' CLAUDE.md sections
that embed Zerops platform facts (`## Zerops service facts` listing managed
services; `## Zerops dev (hybrid)` describing dev-loop quirks). Those facts
belong in zerops.yaml comments / IG / KB. The reference recipes set this
precedent before the dedicated `claudemd-author` brief existed; they need
corresponding updates (out of scope for run-16).
```

The spec rewrite + the claudemd-author sub-agent dispatch (tranche 3 commits 4-5 + tranche 4 commits 5-6) + the validator reshape (tranche 5 commit 3) land together to form a coherent contract. Tranche 7 commit 1 is where the spec text changes; the brief composer + dispatch wiring + slot-shape refusal in tranches 3-4 produce the runtime behaviour the spec documents.

---

## §16. What this guide deliberately does NOT cover

- **Per-run gap lists for runs > 16** — those live in their own `plans/run-N-readiness.md` documents.
- **Engine code beyond `internal/recipe/` and `internal/tools/deploy_subdomain.go`** — slot-shape refusal lives in `internal/recipe/`; R-15-1 closure lives in `internal/tools/`; CLAUDE.md authoring lives in the `claudemd-author` sub-agent's brief composer (`BuildClaudeMDBrief` in `briefs.go`) + slot-shape refusal (`slot_shape.go`) + finalize validator (`validateCodebaseCLAUDE`) — no engine emit function (`codebase_claude_emit.go` is NEVER created; §6.7 commit 4 retired the engine-emit pivot in favor of the dedicated sub-agent).
- **Runtime behaviour of the new fact pipeline** — implementation determines memory + I/O profile; risks flagged in §12 not benchmarked here.
- **Migration of existing dogfooded artifacts** — run-15 deliverable stays as-is. Run-16+ runs use the new architecture. The run-15 facts.jsonl entry annotated in §9.5 step 5 is the only retroactive change (additive annotation, not deletion).
- **Reference recipe (laravel-jetstream / laravel-showcase) CLAUDE.md updates** — the reference recipes set the wrong-shape precedent that propagated. Updating them is out of scope for run-16; flagged for a future round.
- **v2 archaeology** — lives in [`../zcprecipator2/`](../../zcprecipator2/).

---

## §17. Next steps after triple-verification

1. Fresh instance reports back per §14.10.
2. Address all anomalies / truncation-guard violations / line-cite drift / recommended corrections.
3. **Tranche 0 ships first** — independently of the architecture reshape (R-15-1 forensic closure has no dependency on §§1-8).
4. Tranches 1-7 ship in order, each with its risk gate green before the next starts.
5. Run-16 dogfood after tranche 4 lands (codebase-content + claudemd-author + env-content brief composers wired + slotted IG + yaml comments + CLAUDE.md slot refusal). Use a small recipe shape (single-codebase, no parent) for the first dogfood; expand to showcase shape after tranche 5. **First time `verify-claudemd-zerops-free` (built in tranche 0 commit 3) has session-jsonl data to verify against** — tranches 1-3 ship the gate ahead of any output to verify. Run BOTH dogfood verification gates as part of dogfood sign-off: `make verify-dogfood-no-manual-subdomain-enable` (tranche 0 commit 2, R-15-1 closure) + `make verify-claudemd-zerops-free` (tranche 0 commit 3, §0.6 mechanism verification of the claudemd-author output).
6. Tranche 6 (legacy atom deletion) is conditional on dogfood signal — only ships if dogfood confirms the new atoms produce acceptable output without the legacy atoms. **Specifically, tranche 6 ships only after the showcase-shape dogfood passes, not the small-shape**: a small-recipe success doesn't transfer because the showcase shape exercises 6 tier yamls × ~8 service blocks of env-content authoring that the small shape never touches. If the showcase-shape dogfood reveals a load-bearing legacy atom, tranche 6 delays further until that atom is absorbed into the new atom set.
7. Run-16 readiness sign-off in tranche 7 — spec §Surface 6 rewrite (§15) + CHANGELOG entry + system.md verdict-table updates + spec amendments if any.
