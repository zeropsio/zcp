# Substep: completion

This substep closes the deploy phase. It completes when every deploy substep listed by `zerops_workflow action=status` has been attested and the deploy-step checker's content-surface gate passes against the READMEs authored at `readmes`.

## Precondition

Every substep above is attested:

- `deploy-dev` — every dev target returned `ACTIVE`.
- `start-processes` — every dev-phase runtime process is running.
- `verify-dev` — every HTTP target is healthy on its dev subdomain; every worker target is started in logs.
- `init-commands` — every declared `initCommand` ran and the post-deploy data check passed.
- `subagent` (showcase) — the feature sub-agent returned; every feature in `plan.Features` is implemented.
- `snapshot-dev` (showcase) — re-deploy returned `ACTIVE` for every target with the feature sub-agent's edits baked.
- `feature-sweep-dev` — every api-surface feature returned 2xx `application/json` on dev.
- `browser-walk-dev` (showcase) — every ui-surface feature exercised on both dev and stage subdomains with every per-feature criterion satisfied.
- `cross-deploy-stage` — every stage target returned `ACTIVE`.
- `verify-stage` — every stage target healthy.
- `feature-sweep-stage` — every api-surface feature returned 2xx `application/json` on stage.
- `readmes` — the writer sub-agent returned; every per-codebase `README.md` and `CLAUDE.md` exists on the mount; `ZCP_CONTENT_MANIFEST.json` is present at `/var/www/`.

## Completion call

```
zerops_workflow action="complete" step="deploy" attestation="Dev deployed at {dev_url}, stage deployed at {stage_url}. Both healthy. READMEs narrate debug rounds."
```

Substitute `{dev_url}` and `{stage_url}` with the subdomain URLs recorded at `verify-dev` and `verify-stage`. The attestation string is a structured summary the step-closer logs — keep it brief and observational; narrative belongs in the READMEs.

The deploy-step checker runs at this call. It walks every `README.md` and `CLAUDE.md` against the content-surface contracts (fragment shape, predecessor floors, per-item IG code blocks, worker production-correctness gotchas on separate-codebase worker READMEs, cross-README uniqueness, gotcha-distinct-from-guide, CLAUDE.md depth). If any check fails, the completion call returns the failing check names; iterate on the content and retry. The step closes once every check passes.

## After completion

The `finalize` phase's entry substep loads next. No autonomous export, publish, or follow-up runs from this substep — export and publish are triggered only by an explicit user request in a later phase.
