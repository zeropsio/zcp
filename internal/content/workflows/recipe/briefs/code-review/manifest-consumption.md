# manifest-consumption

`ZCP_CONTENT_MANIFEST.json` lives at the recipe-output-directory root on the orchestrator side of the mount. Read it once at the start of the review; its entries declare where each recorded fact should have landed.

## Shape

Each entry names a fact by `fact_title` plus three routing fields: `classification`, `routed_to`, `override_reason`. The `routed_to` field is the authoritative claim about which surface the fact was published on. Valid routing destinations are the six content surfaces this review touches — `content_gotcha`, `content_intro`, `content_ig`, `content_env_comment`, `claude_md`, `zerops_yaml_comment` — plus the terminal `discarded` destination.

## Routing honesty verification

For every manifest entry, verify the claim against the actual content on every surface. Walk all six `(routed_to × surface)` pairs:

1. `routed_to=content_gotcha` → the fact's title-tokens (whitespace-split, lowercased, words of length ≥ 4) appear in at least one codebase's knowledge-base fragment.
2. `routed_to=content_intro` → title-tokens appear (paraphrased OK) in at least one intro fragment.
3. `routed_to=content_ig` → title-tokens appear in at least one integration-guide fragment.
4. `routed_to=content_env_comment` → title-tokens appear in the env-comment set the finalize step emits. Advisory at this phase — env comments land after this review; flag only if a later surface echoes the fact.
5. `routed_to=claude_md` → title-tokens appear in at least one CLAUDE.md operational section. Additionally: title-tokens must NOT appear as a bullet in any knowledge-base fragment — a fact routed to `claude_md` but shipped as a gotcha is drift.
6. `routed_to=zerops_yaml_comment` → title-tokens appear in at least one codebase's `zerops.yaml` `#` comments.
7. `routed_to=discarded` → title-tokens must NOT appear as a bullet in any knowledge-base fragment. If a default-discard classification (framework-quirk, library-meta, self-inflicted) was routed to a non-discarded surface, its `override_reason` must be non-empty and justify the override.

## Reporting

Each drift between a manifest claim and actual content placement is a finding under the reporting-taxonomy atom's tiers. The severity depends on user-facing impact: a fact routed to `claude_md` that shipped as a user-facing gotcha stem is higher severity than a metadata-only divergence the porter never reads.
