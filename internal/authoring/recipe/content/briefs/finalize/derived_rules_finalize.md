# Derived rules — finalize-scoped subset

Trimmed copy of `briefs/refinement/derived_rules.md` — rules finalize actors against (root + tier README + tier import.yaml + voice). IG/KB/codebase-yaml rules out of scope. Cite rule id + exact phrase + preserving edit.

## Universal voice

- **V1 — porter-clones-and-runs framing.** Address someone who clones and deploys, not someone learning the framework. Never "we chose", "during scaffold", "the agent owns", "the recipe author".
- **V2 — names what the recipe IS, not what it could be.** Specific framework + features. Never "any Node HTTP framework", "if you use Symfony instead".
- **V3 — link text references porter-recognized concepts**, not internal corpus slugs. **Slug-stem test (HARD)**: link text MUST NOT contain corpus stems (`rolling-deploys`, `init-commands`, `managed-services-*`, `env-var-*`, `subdomain-access`, `object-storage`, `build-from-git`) even with Zerops/managed prefix or `reference`/`guide`/`service` suffix. Forbidden: `[managed NATS service]`, `[Zerops rolling-deploys reference]`. Use porter concept or in-body completion.
- **V4 — porter-actionable phrasings tied to platform signal.** "Feel free to change this", "Configure this to use real SMTP sinks", "Bump … once …".
- **V5 — defer broad platform concepts to docs when inline would sprawl.** Conditional.
- **V6 — no authoring vocabulary on porter-facing surfaces.** Forbidden tokens: `zerops_dev_server`, "the agent", "scaffold", "feature phase", "recipe author", "record-fact".

## Root README — 6-tier entry point, NOT a documentation surface

- **R1 — short** (~28 lines). Intro paragraph + deploy button + cover image + 6-tier link list + catalog punt + Discord.
- **R2 — intro names framework + what it demonstrates.** One sentence.
- **R3 — 6 tier links uniform shape.** `**TIER NAME** [[info]](path) — [[deploy with one click]](url)`. Both for every tier.
- **R4 — trailing punt to recipe catalog**, not in-line teaching.
- **R5 — trailing Discord invite.**
- **R6 — no IG, no KB, no architecture description, no managed-services list.** Zero `## H2` content sections.

## Tier README

- **T1 — short.** Title + one-sentence framing + 2-3 line intro extract.
- **T2 — intro extract names tier's DELTA**, not absolute spec. "Stage uses the same configuration as production, but runs on the lowest scaling settings."
- **T3 — title links back to recipe deploy page**: `[recipe-name (info + deploy)](recipe-url)`.
- **T4 — no tier-promotion narrative.** No "promote to tier 5 when X" or stepping-stone framing.

## Tier import.yaml

- **TY1 — top-of-file comment mirrors tier README intro extract** (2-4 lines).
- **TY2 — per-service comment is 1-2 sentences naming SERVICE ROLE.**
- **TY3 — optional services explicitly marked optional.** "Feel free to remove this service, if you wish to stage-test."
- **TY4 — comments name framework-canonical effects.** "Used by the Laravel app to store data" — not "consumed by api codebase".
- **TY5 — justify non-default priority for databases + object-storage.**

## zerops.yaml — finalize-relevant subset

- **Y8 — no tier-promotion narrative in yaml comments.**
