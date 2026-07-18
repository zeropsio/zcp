# Knowledge-subsystem improvement register — draft for Codex review

Source: 95-agent audit (12 dimensions, per-finding adversarial verification: 70 confirmed / 12 refuted), run wf_63f56038-a63, 2026-07-17.
Full finding details with file:line evidence: audit-digest.md (same directory).

Overall verdict from dimension summaries: the subsystem is structurally healthy — push-channel assembly (Synthesize) is table-driven with strong meta-tests, retrieval honors the tools-only single-fetch spec, launch-token security content is clean, recipes' hello-world families are consistent. The confirmed defects cluster into six themes below, ordered by proposed priority.

## T1 — Agent-facing falsehoods in shipped content (13 findings, 9 HIGH)

Wrong facts delivered to live agents today. Each is a small content fix (S), several also need a golden re-bless; two propagate upstream to zerops-docs / Strapi.

1. `develop-local-workflow`: "build runs from your committed tree" — false; default local deploy ships the dirty working tree (`zcli --no-git`). Secrets-shipping risk. Co-firing atom states the correct fact → contradiction in every local payload.
2. `nestjs-showcase` recipe: inverted Valkey-auth fact (teaches NO password; Valkey enforces requirepass → NOAUTH at deploy). Spec-content-surfaces records this exact inversion as removed 2026-06-14; the recipe still ships it. Fix via Strapi sync push. Sibling `nestjs-minimal`: same-key execOnce collision (seed silently skipped).
3. `choose-database` decision: claims scaling profile "fixed for the service's life" — platform docs say changeable anytime; agents would advise destructive delete+recreate. Also upstream in zerops-docs.
4. `bootstrap-verify`: no `routes:` axis → renders classic-route "don't run zerops_verify / nothing deployed" narrative on recipe close, contradicting bootstrap-close and the handler's own transition message in the same render.
5. `develop-close-mode-auto`: asserts "no configured git remote" while axis-gated on [unconfigured, broken] — false for broken; contradicts develop-git-push-broken 12 lines earlier in the golden.
6. `develop-static-workflow`: offers retired `git-push` close-mode value (folds to auto since the delivery-derivation change); co-firing atoms list only auto/manual.
7. `export-publish-needs-setup`: three stale facts about the pre-probe-first git-push setup flow (incl. project-level GIT_TOKEN instruction vs actual service-scope secret); residue also in export-publish.md and launch-source-control-required.md.
8. `develop-knowledge-pointers`: axis-free (fires on EVERY develop envelope), teaches wrong zerops_knowledge modes (query= for schema/runtime lookups where scope=/runtime= are the contract) and retypes env-key guidance the tool descriptions own.
9. READY_TO_DEPLOY recovery dual-owned with contradictory advice: bootstrap-env-var-discovery (branch on build history, never override on FAILED) vs develop-ready-to-deploy (blanket override=true, "never deployed" premise unguaranteed) — can co-fire; override wipes source (the Wave-1 data-loss class).
10. `launch-classify-platform-envs`: states an auto-filter the code doesn't implement, contradicting sibling atom.
11. `export-publish` step-4: nonexistent zcli commands.
12. `operations.md` theme: "Max 40 cores" vs scaling guide's 8-core cap (demonstrated pull-tier drift).
13. Hostname max-length: atom says 40 (correct, E2E-verified); spec + plan-reject hint still say 25.

## T2 — Reachability / delivery-channel defects (10 findings, 5 HIGH)

Content exists but never reaches agents, or reaches them degraded — the delivery seams, not the content.

1. Four launch atoms unreachable in production (launch-intro incl. token lifecycle overview, launch-ha-assessment, launch-classify-platform-envs, launch-pipeline-configuring): launch delivery is handler-by-atom-ID; no handler references them; scenario manifest masks it by pinning a fixture-only envelope no production code builds. Fix: wire into handlers or add a launchStatus axis; add reachability test.
2. Launch blocker guidance hardcodes `zerops_knowledge uri="zerops://atoms/launch-source-control-required"` but the atom is inline → tool rejects the fetch with a misleading suggestion. Fix: flip to reference:true + lint Go-literal atom URIs.
3. Search-hit recipes steer to uri= fetch which serves raw doc content — recipe= serves universals + mode header. Same doc, two envelopes; discovery path gets the weaker one. Fix: route recipe URIs through GetRecipe in the uri= handler + parity test.
4. `develop-http-diagnostic` gated never-deployed → unreachable exactly when 500s happen (deployed).
5. Container-only zerops_dev_server guidance leaks into local envelopes (first-deploy-verify + dev-server-reason-codes lack environment axes); local installs don't register the tool; co-rendered local atom contradicts it.
6. Decision Hints briefing layer silently dead for composite variant service types (postgresql:single@18 migration fixed the service-card layer, not this one).
7. `runtimeRecipeHints` hand-map drifted: solidstart/vue/angular-ssr invisible to briefings; ~15 dead prefixes; no corpus tie. Fix: derive from recipe frontmatter (matcher already does) or drift test.
8. Matcher taxonomy drift: angular tagged literal placeholder `[framework]` (Angular intent scores 0, word "framework" false-positives 0.95); SvelteKit/SolidStart intents score 0 (tags [svelte]/[solid], no synonyms) → silent degrade to classic bootstrap. Fix at Strapi source + corpus-drift test over embedded store.
9. Query-mode search miss returns bare `[]` — only mode whose miss carries no recovery step.
10. `mode=` parameter false affordance: unused in GetBriefing; advertised common use mode=stage is a silent no-op that strips the adaptation it promises to add.

## T3 — Enforcement-layer blind spots (10 findings, 3 HIGH)

The lint/guard machinery that should prevent T1-class regressions has specific holes; each hole shown live by a current violation.

1. Spec-ID regex stale: P-LP-*/P-PROD-* families unknown → live "per P-PROD-2" in launch-post-checklist ships to agents; handler-behavior verb lists miss "composer strips".
2. Substring membership bug: `strings.Contains("standard simple local-stage local-only", "stage")` → modes:[stage]+configured passes the exact rule built to stop it. Fix: set membership (pattern exists at staleActionViolations).
3. Fires-on-fixture self-test covers 12/16 rule families — the buggy one among the 4 unexercised.
4. 7/23 seed-allowlist entries dead; no liveness enforcement (fixed violations leave ghost entries).
5. references-atoms co-presence contract declared ("same rendered payload") but Rule A checks only inline-ness — live corpus violations (http-diagnostic→platform-rules-local across environment axes; change-drives-deploy→container-only targets; reference:true source carrying references-atoms). Fix: extend Rule A with atomCoRendersWhenever + forbid refs on reference sources.
6. Axis dispatch completeness: new AxisVector field must be hand-added at 4 sites; matcher side fails SILENT (wildcard-broadening). Only guard is a comment. Fix: reflection completeness test — every field classified in exactly one set.
7. Recipe lint skip-happy: truncated nextjs-ssr stub (20 lines, no zerops.yaml — matcher pins it 0.95 for Next.js!) passes green; solidstart yaml before first H2 invisible to extraction; import.yml lint arm unreachable; viability gate dead code with self-skipping calibration tests.
8. Dev-reference lint pattern gaps: plans/ subdir paths, docs/spec-* paths, P-* IDs leak into agent-facing atom text.
9. Markdown-table pipe escaping breaks every alternation grep in export-classify-envs.
10. plan-doc rule misses plans/archive/ paths (the form completed plans actually take).

## T4 — Duplication / single-owner erosion (9 findings, 1 HIGH)

1. Classification protocol ~80% verbatim-duplicated across export-classify-envs (115 l.) and launch-classify-prompt (96 l.), drift already visible ("four-bucket table below" — table has five rows and is above). Fix wrong line now; extract shared protocol into a shared atom (note verifier: references-atoms won't work cross-phase — needs shared inline atom or pointer mechanism).
2. Bootstrap same-render duplication: expected-post-import states ×2, includeEnvs discovery ×2, three "confirm with user", two plan-shape examples; recipe close orders "complete close" (p1) before "verify" (p5) with pre-baked "verified" attestation.
3. develop-close-mode-auto-simple strict subset of workflow-simple, both render in one payload; auto-dev/workflow-dev identical axis vectors + verbatim sentences.
4. Local dev-server guidance triple-duplicated across co-firing local atoms (platform-rules-local family).
5. Env-var precedence/self-shadow facts double-authored across pull tier (guide + atoms).
6. High-fan-out facts re-authored (Cloudflare Full-strict ×7, 50MB subdomain limit ×6) with a one-entry drift-tripwire registry.
7. runtime_resources.go claims SSOT, has zero consumers, conflicts with a provision atom.
8. Two atoms restate most of the reference atom they declare as pointer (pointer-discipline erosion, drift pairs).
9. Steady-state env-guidance gate stale (pre-pointer-render era) — removes all env stubs from steady-state payloads; backlog rationale misstates axes.

## T5 — Spec drift (7 findings, 1 HIGH)

1. spec-knowledge-distribution documents recipe-active phase + StateEnvelope.Recipe field deleted 2026-06-12; §2.1 omits IdleScenario/ExportStatus, wrong Bootstrap type name; atom.go still accepts dead enums recipe-active/orphan (validate-clean, never fire).
2. §11 documents only 7 of the lint's 16 rule families — authors can't learn half the contract without tripping it.
3. spec-workflows §9.3: four classification buckets vs five in code+atoms.
4. spec-content-surfaces citation-map guide IDs mostly not retrievable via zerops_knowledge.
5. Three-way run.start contradiction (spec §1 vs §5 vs fact_owners.go); the uncommitted spec-knowledge-architecture edit is an improvement but half-done (sibling archived-plan cite, stale §8 heading, §5 seed instruction contradiction).
6. Archived-plan cites ×3, stale ComputeEnvelope signature, stale line-range, trailing section map citing deleted recipe-v2 files.
7. §11.6 axis-N doctrine claims platform-rules atoms "always co-fire" — container one is gated+pointer-rendered since P0c.

## T6 — Coverage gaps (3 findings)

1. ES-search-lag (spec-workflows §3.5) has zero agent-facing coverage while atoms steer agents to zerops_events.
2. .git-suffix buildFromGit clone-preflight failure: only in CLAUDE.md/spec/Go comments — no runtime-agent surface.
3. Implicit-webserver standard pairs lose promote/close guidance exactly on close-mode unset→auto transition.

## Audit blind spots (completeness critic — future work, NOT in this register)

Strapi sync round-trip provenance; authoring-side generator (briefs*.go, record_fact loop); cross-call token economics (knowledge_tracker only wired into bootstrap — one develop status call re-emits ~16 atoms/~5250 tokens per call; existing backlog plan); AGENTS.md standing-instruction tier + refresh_agents markerless-stale semantics; inline error-path guidance corpus (next_actions.go, convert.go); eval harness as efficacy loop.

## Questions for Codex

1. Priority order T1→T6 sane? Anything you'd resequence?
2. T2.1 (launch reachability): wire-into-handlers vs launchStatus axis — which shape is right long-term given export already has exportStatus?
3. T3.6 reflection completeness test vs generating dispatch from a single classification table — over-engineering or right call?
4. T4.1 cross-phase shared protocol atom: what mechanism fits (shared inline atom included by both phases / new pull-reference / dedicated shared-fragment tier)?
5. Any theme where the batch-fix should be one flow slice per theme vs per finding?
6. What's missing that these 70 findings + 6 critic gaps still don't cover?
