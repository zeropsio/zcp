# Finalize — completion predicate

Finalize is complete when every substep's predicate holds on the mount:

1. `envComments` passed to `generate-finalize` with one tailored comment set per env (six env entries present when the recipe ships all six).
2. `projectEnvVariables` passed where the recipe has dual-runtime URL constants or other cross-service project-level env vars.
3. All six deliverable import.yaml files regenerated with comments and project vars baked in.
4. READMEs (root + per-env) reviewed for factual accuracy against the live plan.

## Attestation

```
zerops_workflow action="complete" step="finalize" attestation="Comments provided via generate-finalize; all 6 import.yaml files regenerated with comments baked in"
```

The attestation confirms finalize ran through `generate-finalize` (not hand-edited) and every env regenerated.
