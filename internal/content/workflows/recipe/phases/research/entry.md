# Research — populate plan.Research and derive the SymbolContract

Research is where the shape of the recipe is decided before any service is provisioned or any file is written. This phase completes when `plan.Research` carries a complete framework profile and `plan.SymbolContract` carries the cross-codebase contract that every later scaffold, feature, and writer dispatch will share.

## What lands on the plan

`plan.Research` is populated with framework identity, pipeline, database, and secret-handling fields:

- Identity: `serviceType`, `packageManager`, `httpPort`.
- Pipeline: `buildCommands`, `deployFiles`, `startCommand`, `cacheStrategy`.
- Database: `dbDriver`, `migrationCmd`, `seedCmd` (when the framework ships a seeder).
- Secrets: `needsAppSecret`, `appSecretKey` (the framework-specific name when a secret is required), `loggingDriver`.
- Showcase-only additions: `cacheLib`, `sessionDriver`, `queueDriver`, `storageDriver`, `searchLib`, `mailLib`.

`plan.Research.Targets` carries one entry per runtime target. Each target declares `type` (`<runtime>@<version>`, chosen from the latest `availableStacks` entry), `hostname` role (`app`, `api`, `worker`, `frontend`), and `sharesCodebaseWith` (empty for separate codebases; set for workers that ride inside a host's codebase). Managed services land as their own target rows (`postgresql`, `valkey`, `nats`, `meilisearch`, `object-storage`, …) with a `typePinReason` when a non-latest version is pinned.

## What this phase decides that downstream steps depend on

- The number of dev mounts provision must create follows directly from the `sharesCodebaseWith` pattern in targets.
- The zerops.yaml setup count per codebase (2 vs 3 setups) follows from worker placement.
- The env-var catalog the generate step consumes is a function of which managed-service targets land here.
- The `SymbolContract` — computed once and frozen for the rest of the run — is derived from the research output.

## How to proceed

Capture the framework profile from the framework's own docs plus the chain recipe when one exists. Record each managed service once with the latest version from `availableStacks` unless a documented compatibility constraint forces an older pin (which must appear in `typePinReason`). When targets and managed services are on the plan, derive `plan.SymbolContract` per the next atom — the contract is a frozen artifact, not something sub-agents reconstruct from prose.

The deliverable of research is the populated `plan.Research` plus the computed `SymbolContract`; the attestation predicate for research-complete is stated in this phase's completion atom.
