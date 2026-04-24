# Fact recording

Schema (all required):

- `topic` — short kebab-case
- `symptom` — observable failure (status + quoted error)
- `mechanism` — why (platform-side; both sides if intersection)
- `surfaceHint` — one of: `root-overview`, `tier-promotion`,
  `tier-decision`, `porter-change`, `platform-trap`, `operational`,
  `scaffold-decision`, `browser-verification`
- `citation` — `zerops_knowledge` guide id or published-recipe URL.
  Required for every `platform-trap` / `porter-change` fact.

Self-inflicted findings (code bugs fixed during scaffold) are NOT
facts — discard.
