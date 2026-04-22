# Content surface contracts

Four content surfaces. Each has one reader, one purpose, one single-question test, one canonical shape, and one length range. An item that fails its surface's test is removed, not rewritten. The item fails because it is on the wrong surface; rewriting leaves it on the wrong surface.

Surface contracts are declarative. Classification + routing decisions are documented in the `classification-pointer` atom (above) and backed by the runtime classify lookup; these contracts define what each surface accepts once a fact lands. The "Pre-loaded input" section below carries annotated pass/fail examples per surface — pattern-match against them before publishing.

---

## Surface 1 — Per-codebase README integration-guide fragment

Reader: a porter bringing their own existing application — a Svelte app they already built, a NestJS API they already wrote. They are not using this recipe as a template. They are extracting the Zerops-specific steps to adapt their own code.

Purpose: enumerate the concrete changes a porter must make in their own codebase to run on Zerops.

Single-question test: *Would a porter who is NOT using this recipe as a template, but bringing their own code, need to copy THIS exact content into their own app?*

Shape: H3 headings inside the `integration-guide` fragment markers, each item standalone. Item 1 is always "Adding `zerops.yaml`" with the full commented YAML read back from disk. Items 2+ are each one platform-forced change: routable bind (`0.0.0.0` instead of `127.0.0.1`), trust-proxy for the L7 balancer, reading env vars from `process.env` directly, `initCommands` with `zsc execOnce` for migrations, `forcePathStyle: true` for Object Storage, `allowedHosts` for bundler dev servers, worker-specific SIGTERM-drain-exit sequences. Each H3 carries an action, a one-sentence reason tied to a Zerops mechanism, and a fenced code block with the minimal diff a porter would apply.

Length range: 3 to 6 H3 items. Beyond 6 and either repo-operations crept in (move to CLAUDE.md) or the author did not choose ruthlessly.

Citation requirement: when the item's mechanism matches the Citation Map, read the guide before writing and reference the guide in the item body. Every IG item whose manifest entry is routed to `content_ig` must carry at least one `citations` entry with a non-empty `guide_fetched_at` timestamp — the completion gate at `complete substep=readmes` refuses entries missing it.

---

## Surface 2 — Per-codebase README knowledge-base fragment

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

Citation requirement: every gotcha whose topic matches the Citation Map MUST reference the cited platform topic in the body. Every gotcha manifest entry (`content_gotcha`) must also carry at least one `citations` entry with a non-empty `guide_fetched_at` timestamp — the completion gate at `complete substep=readmes` refuses entries missing it. A gotcha without a citation is folk-doctrine shipping. Concrete drop patterns (self-inflicted / framework-only / tooling-metadata / scaffold-code rationale / IG-restatement) are shipped as FAIL examples in the "Pre-loaded input" section — pattern-match against them, don't re-derive the list.

---

## Surface 3 — Per-codebase CLAUDE.md

Reader: someone (human or Claude Code) with this specific repo checked out locally, working on the codebase.

Purpose: operational guide for running the dev loop, iterating on the repo, exercising features by hand.

Single-question test: *Is this useful for operating THIS repo specifically — not for deploying it, not for porting it to other code?*

Shape: plain markdown, no fragments, no extraction rules. A template skeleton with four base sections (Dev Loop, Migrations, Container Traps, Testing) plus at least two custom sections chosen for what the codebase actually needs (Resetting dev state, Log tailing, Adding a managed service, Driving a test endpoint, Recovering from a burned execOnce key, and so on).

Length range: a substantive floor of 1200 bytes and at least 2 custom sections beyond the template.

Citation requirement: none; CLAUDE.md is repo-local and not published.

---

## Surface 4 — Env `import.yaml` comments (emitted via `env-comment-set` payload)

Reader: someone reading the Zerops-dashboard manifest to understand what the tier runs.

Purpose: explain every per-service decision at this tier — presence, scale, mode — and any tier-promotion context.

Single-question test: *Does each service block explain a decision (why this service exists at this tier, why this scale, why this mode), rather than narrating what the field does?*

Shape: per-service block of ASCII `#` comments. Each block covers: why this service at this tier, why this scale (throughput vs HA rationale for `minContainers`, cost trade rationale for single replica), why this mode (NON_HA durability trade-off, HA failover justification), and what changes on promotion to the next tier.

Length range: roughly 4 to 10 comment lines per service block; the env comment set is a payload — you do NOT write the `import.yaml` files themselves.

Citation requirement: when the decision touches a topic on the Citation Map (env-var-model, rolling-deploys, object-storage, and so on), cite the platform topic name.

**Factuality rule**: any number in your comment must match the adjacent YAML field exactly. Use qualitative phrasing ("single-replica", "HA mode", "modest quota") when the YAML has no number to match — never invent a number from memory. The check enforces this: a numeric claim that contradicts the adjacent YAML fails with a detail of the form `comment claims "N <unit>" but adjacent YAML has <key>: M`. Subjunctive phrasing ("bump to 50 GB when usage grows") bypasses the check — use it for tier-promotion guidance, not for current-configuration assertions. Default to qualitative phrasing; earn the number by matching the YAML.

---

## Surface summary table

| # | Surface | Reader | Canonical length |
|---|---|---|---|
| 1 | README integration-guide fragment | Porter with own app | 3–6 H3 items |
| 2 | README knowledge-base fragment | Dev hitting platform failure | 3–6 gotcha bullets |
| 3 | Per-codebase CLAUDE.md | Repo operator | ≥1200 bytes, ≥2 custom sections |
| 4 | Env `import.yaml` comments (payload) | Dashboard-manifest reader | 4–10 lines per service block |

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
