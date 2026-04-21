# Content surface contracts

Six content surfaces. Each has one reader, one purpose, one single-question test, one canonical shape, and one length range. An item that fails its surface's test is removed, not rewritten. The item fails because it is on the wrong surface; rewriting leaves it on the wrong surface.

Surface contracts are declarative. Classification (atom: classification-taxonomy.md) decides the class of every fact; routing (atom: routing-matrix.md) turns class into surface; these contracts define what each surface accepts once a fact lands.

---

## Surface 1 — Root README

Reader: a developer browsing the recipe page, deciding whether to deploy.

Purpose: name the managed services, list the environment tiers with deploy buttons, point to the recipe category page.

Single-question test: *Can a reader decide in 30 seconds whether this recipe deploys what they need, and pick the right tier?*

Shape: one opening paragraph naming every managed service. One row per environment tier with a deploy button. One link out to the recipe-category page. No debug narrative. No architecture lecture.

Length range: 20 to 30 lines.

Citation requirement: none.

Drop example — a paragraph explaining the database driver choice: that belongs in the env `import.yaml` comments, not here. A reader choosing whether to deploy does not need driver rationale.

---

## Surface 2 — Environment README (`environments/{env}/README.md`)

Reader: someone who already chose to try the recipe and is now picking a tier, or evaluating whether to promote from this tier to the next.

Purpose: teach the tier's audience, scale profile, and what changes relative to the adjacent tier.

Single-question test: *Does this teach me when I would outgrow this tier and what the next tier changes?*

Shape: one section naming the audience (AI-agent iterating, remote dev, local dev, stage reviewer, small-prod operator, HA-prod operator). One section on scale (single replica / multi-replica / HA). One section on what changes relative to the previous tier (or "entry-level tier" if there is none). One section on tier-specific operational concerns (ephemeral stage data, DEDICATED CPU requirement, rolling-deploy verification, and so on).

Length range: 40 to 80 lines of substantive teaching. A 7-line boilerplate fails the test.

Citation requirement: none directly; claim consistency with adjacent env `import.yaml` comments is required (see self-review atom).

Drop example — a service-by-service "why this service exists" enumeration: that belongs in env `import.yaml` comments, one block per service, not in the env README.

---

## Surface 3 — Env `import.yaml` comments (emitted via `env-comment-set` payload)

Reader: someone reading the Zerops-dashboard manifest to understand what the tier runs.

Purpose: explain every per-service decision at this tier — presence, scale, mode — and any tier-promotion context.

Single-question test: *Does each service block explain a decision (why this service exists at this tier, why this scale, why this mode), rather than narrating what the field does?*

Shape: per-service block of ASCII `#` comments. Each block covers: why this service at this tier, why this scale (throughput vs HA rationale for `minContainers`, cost trade rationale for single replica), why this mode (NON_HA durability trade-off, HA failover justification), and what changes on promotion to the next tier.

Length range: roughly 4 to 10 comment lines per service block; the env README summarizes, these comments carry the per-decision rationale.

Citation requirement: when the decision touches a topic on the Citation Map (env-var-model, rolling-deploys, object-storage, and so on), cite the platform topic name.

Drop example — "enables zero-downtime rolling deploys" repeated word-for-word on every service block: each block has its own reasoning. Templated openings fail the test.

---

## Surface 4 — Per-codebase README integration-guide fragment

Reader: a porter bringing their own existing application — a Svelte app they already built, a NestJS API they already wrote. They are not using this recipe as a template. They are extracting the Zerops-specific steps to adapt their own code.

Purpose: enumerate the concrete changes a porter must make in their own codebase to run on Zerops.

Single-question test: *Would a porter who is NOT using this recipe as a template, but bringing their own code, need to copy THIS exact content into their own app?*

Shape: H3 headings inside the `integration-guide` fragment markers, each item standalone. Item 1 is always "Adding `zerops.yaml`" with the full commented YAML read back from disk. Items 2+ are each one platform-forced change: routable bind (`0.0.0.0` instead of `127.0.0.1`), trust-proxy for the L7 balancer, reading env vars from `process.env` directly, `initCommands` with `zsc execOnce` for migrations, `forcePathStyle: true` for Object Storage, `allowedHosts` for bundler dev servers, worker-specific SIGTERM-drain-exit sequences. Each H3 carries an action, a one-sentence reason tied to a Zerops mechanism, and a fenced code block with the minimal diff a porter would apply.

Length range: 3 to 6 H3 items. Beyond 6 and either repo-operations crept in (move to CLAUDE.md) or the author did not choose ruthlessly.

Citation requirement: when the item's mechanism matches the Citation Map, read the guide before writing and reference the guide in the item body.

Drop example — an H3 describing `api.ts`'s content-type check: `api.ts` is recipe scaffold; the porter has no `api.ts`. The underlying principle (bundler dev-server SPA fallback returns `200 text/html`) belongs here as a principle-level item with a code diff; the specific helper's implementation belongs in code comments instead.

---

## Surface 5 — Per-codebase README knowledge-base fragment

Reader: a developer hitting a confusing failure on Zerops and searching for what is wrong.

Purpose: surface platform traps that are non-obvious even to someone who read the platform docs and the framework docs.

Single-question test: *Would a developer who read the Zerops docs AND the relevant framework docs STILL be surprised by this?*

If the answer is "no, the platform docs cover it" — remove; it is not a gotcha, it is a pointer to docs.
If the answer is "no, the framework docs cover it" — remove; framework quirks belong in framework docs.
If the answer is "yes, it surprises you even after reading both" — this is a gotcha.

Shape: an H3 `### Gotchas` section with 3 to 6 bullets inside the `knowledge-base` fragment markers. Each bullet:

```
- **<concrete observable symptom>** — <mechanism>. <evidence or 1-2 sentence explanation>.
```

The stem names an HTTP status, a quoted error string, a measurable wrong state — not "it breaks". The body names the platform mechanism and, when the topic matches the Citation Map, references the platform topic.

Length range: 3 to 6 bullets.

Citation requirement: every gotcha whose topic matches the Citation Map MUST reference the cited platform topic in the body. A gotcha in a matching-topic area without a citation is folk-doctrine shipping.

Drop examples:

- Self-inflicted — "our seed script silently exited 0 and the execOnce key recorded success". The seed script was buggy; execOnce honored its contract. This is a code fix, not a gotcha.
- Framework-only — `setGlobalPrefix('api')` colliding with `@Controller('api/...')` decorators. Pure framework fact; no Zerops involvement.
- Tooling-metadata — peer-dep version mismatches from the package registry.
- Scaffold-code rationale — "our helper catches the SPA fallback class of bug". The helper is recipe scaffold; the underlying principle belongs in the integration guide.
- Restatement of an integration-guide item — if the IG teaches `forcePathStyle`, the gotcha must add value beyond the fix (the symptom, the error string, the quiet failure mode).

---

## Surface 6 — Per-codebase CLAUDE.md

Reader: someone (human or Claude Code) with this specific repo checked out locally, working on the codebase.

Purpose: operational guide for running the dev loop, iterating on the repo, exercising features by hand.

Single-question test: *Is this useful for operating THIS repo specifically — not for deploying it, not for porting it to other code?*

Shape: plain markdown, no fragments, no extraction rules. A template skeleton with four base sections (Dev Loop, Migrations, Container Traps, Testing) plus at least two custom sections chosen for what the codebase actually needs (Resetting dev state, Log tailing, Adding a managed service, Driving a test endpoint, Recovering from a burned execOnce key, and so on).

Length range: a substantive floor of 1200 bytes and at least 2 custom sections beyond the template.

Citation requirement: none; CLAUDE.md is repo-local and not published.

Drop example — deploy instructions: those belong in integration-guide items and in `zerops.yaml` comments. Framework basics: the operator already knows the framework; CLAUDE.md is not a tutorial.

---

## Surface summary table

| # | Surface | Reader | Canonical length |
|---|---|---|---|
| 1 | Root README | Recipe-page browser | 20–30 lines |
| 2 | Env README | Tier chooser | 40–80 lines |
| 3 | Env `import.yaml` comments | Dashboard-manifest reader | 4–10 lines per service block |
| 4 | README integration-guide fragment | Porter with own app | 3–6 H3 items |
| 5 | README knowledge-base fragment | Dev hitting platform failure | 3–6 gotcha bullets |
| 6 | Per-codebase CLAUDE.md | Repo operator | ≥1200 bytes, ≥2 custom sections |

Cross-surface discipline: each fact lives on exactly one surface. Other surfaces that benefit from the fact cross-reference — "See apidev/README.md §Gotchas for NATS credential format" — they do not re-author. The routing-matrix atom enforces this at the manifest level; the self-review atom enforces it at the published-content level.

---

## Showcase tier supplements

When the plan tier is showcase and the codebase list includes a separate-codebase worker:

- The worker README knowledge-base fragment MUST contain one gotcha covering queue-group semantics under multi-replica deployment. The stem names the broker, "queue group" (or the equivalent library term), and "minContainers" or "per replica" or "exactly once". The body shows the exact client-library option that sets the group.
- The worker README MUST contain one gotcha covering graceful SIGTERM shutdown with an in-flight-drain sequence. The stem names SIGTERM or "drain" or "graceful shutdown". The body carries a fenced code block showing the concrete call sequence (catch SIGTERM → drain → exit).

Both items cite the rolling-deploys platform topic.

---

## Self-referential decoration prohibition (positive form)

Every item you publish must make sense to its reader without them having read the rest of the recipe's code. If an item requires the reader to know the recipe's own helper file names, helper-class names, or scaffold-specific symbols, the item is self-referential. Rephrase at the principle level (what Zerops does, what a porter should do) or move the implementation detail to code comments in the scaffold source.
