# Provision — attestation predicate

Provision is complete when all of the following hold:

## Predicate

1. Every dev service is RUNNING; every stage service is READY_TO_DEPLOY; every managed service is RUNNING.
2. Every dev mount the plan required is present. The mount count matches the number of `sharesCodebaseWith` groups in `plan.Research.Targets`.
3. Every dev mount has a container-side `.git/` owned by `zerops`, with at least one commit on `main`. The single-SSH-call sequence (config + init + initial commit) has run once per mount.
4. `zerops_discover includeEnvs=true` has run, and the env var catalog for every managed service is recorded on the attestation — unless the plan provisions no managed services, in which case discovery is skipped.
5. Dual-runtime recipes have `DEV_*` and `STAGE_*` URL constants set at the project level with correct port suffixes. Single-runtime recipes have no URL constants to set.
6. Framework secrets are on the project (shared keys) or on `envSecrets` (per-service keys) per the `needsAppSecret` decision in research.

## Attestation call

```
zerops_workflow action="complete" step="provision" attestation="Services created: {list}. Env vars cataloged for zerops.yaml wiring (not yet active as OS vars — activate after deploy): {list}. Dev mounts: {list every dev mount path — one per codebase in your plan}"
```

The attestation lists every service by hostname, every managed service's env var catalog, and every dev mount path. Generate consumes this list as its starting state: RUNNING dev containers, mounted filesystems, git-initialised repos, and the env var catalog for zerops.yaml authoring.
