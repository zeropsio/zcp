# Decision recording — record `porter_change` + `field_rationale` facts

For every non-obvious decision you make at scaffold, record a
structured fact via `record-fact`. Codebase-content phase reads these
to synthesize IG/KB/yaml comments. Recording at densest context
(the moment of the decision) keeps the why preserved.

## Two kinds at scaffold scope

| Kind | Required fields |
|---|---|
| `porter_change` | `topic`, `why`, `candidateClass`, `candidateSurface` |
| `field_rationale` | `topic`, `fieldPath`, `why` |

The full `Fact` schema (every kind, every field, every accepted enum
value) is in the `zerops_recipe` action description for `record-fact`.

## Skip rule

Skip recording if `candidateClass ∈ {framework-quirk, library-metadata,
operational, self-inflicted}` — those have no porter-facing surface.
Record only when `candidateClass ∈ {platform-invariant, intersection,
scaffold-decision}`.

## Git hygiene

Before first deploy:

```
ssh <hostname>dev "git config --global user.name 'zerops-recipe-agent' && git config --global user.email 'recipe-agent@zerops.io'"
```

Then `git init && git add -A && git commit -m 'scaffold'`.
