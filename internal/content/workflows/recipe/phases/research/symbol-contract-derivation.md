# Deriving the SymbolContract

`plan.SymbolContract` is the frozen cross-codebase contract every scaffold, feature, and writer dispatch consumes. It is computed exactly once, at the end of research, from `plan.Research.Targets` plus the managed-service list plus the recipe tier. Every sub-agent receives it as structured JSON interpolated byte-identically into its prompt — the contract is read, never re-derived.

## Shape of the contract

The contract carries six sections, all populated at derivation time:

- **EnvVarsByKind** — for each managed-service kind the recipe provisions (`db`, `cache`, `queue`, `storage`, `search`), the map of the platform-provided env var names the container will see. Keys are the roles (`host`, `port`, `user`, `pass`, `name`, `apiUrl`, `apiHost`, …); values are the exact env var names the platform exposes on the running containers.
- **HTTPRoutes** — declared-once route paths every codebase reads. Producer writes, consumer fetches; both sides read the same string from the contract (`/api/status`, `/api/items`, …).
- **NATSSubjects** and **NATSQueues** — producer publishes to `subjects[name]`; consumer subscribes with `queues[name]`. Both halves must match across codebases, so both halves live in the contract.
- **Hostnames** — one entry per runtime role, carrying `{role, dev, stage}`. `role: "api"` pairs `dev: "apidev"` with `stage: "apistage"`. Writer READMEs reference these; scaffolders embed them in generated code.
- **DTOs** — the interface names shared across producers and consumers (`StatusResponse`, `JobDispatch`, …). Multi-codebase recipes maintain one DTO definition reused by both sides.
- **FixRecurrenceRules** — a positive allow-list of scaffold-phase MUST-DO items, each carrying an `id`, a `positiveForm` sentence, a runnable `preAttestCmd` the scaffold sub-agent executes before returning, and an `appliesTo` list of hostname roles.

## How each section is derived

- **EnvVarsByKind**: read the managed-service kinds present in research targets (`postgresql` → `db`, `valkey` → `cache`, `nats` → `queue`, `object-storage` → `storage`, `meilisearch` → `search`). For each kind, the platform's env catalog shape determines the role map (e.g. `{host: "DB_HOST", port: "DB_PORT", user: "DB_USER", pass: "DB_PASS", name: "DB_NAME"}`). The catalog is the source — research landed the kinds, derivation consults the platform catalog for the exact role-to-key mapping.
- **HTTPRoutes**: dashboard-skeleton routes (`/api/health`, `/api/status`) are fixed; per-feature routes come from the research feature list when the tier is showcase. Minimal-tier routes are a subset keyed by `cacheStrategy`, `queueDriver`, and friends that research recorded.
- **NATSSubjects / NATSQueues**: populated when a `queue` kind is present in research targets. Subject names follow role-pair conventions (`jobs.dispatch`, `jobs.result`); queue names follow consumer-role conventions (`workers`, `notifiers`).
- **Hostnames**: one row per runtime target in `plan.Research.Targets`. `sharesCodebaseWith` does not change hostnames — it changes which zerops.yaml setup count the dev-mount lives under.
- **DTOs**: shared message shapes are listed when the recipe declares a cross-codebase flow. Minimal single-codebase recipes may have zero DTOs; showcase recipes always declare at least the status and feature-list DTOs.
- **FixRecurrenceRules**: a seeded list of 12 rules applies to every recipe with matching roles. The rules close 12 recurrence classes the rule engine recognises.

## The 12 seeded fix-recurrence rules

Each rule names the positive form the scaffold must satisfy and the command the scaffold sub-agent runs before returning:

1. **NATS credentials separate** — pass `user` + `pass` as distinct ConnectionOptions fields; `servers` is `${queue_hostname}:${queue_port}` only. Applies to api, worker.
2. **S3 endpoint is apiUrl** — S3 client `endpoint` reads `process.env.storage_apiUrl` (https). Applies to api.
3. **S3 path-style** — S3 client `forcePathStyle: true`. Applies to api.
4. **Routable bind** — HTTP servers bind `0.0.0.0`. Applies to api, frontend.
5. **Trust proxy** — frameworks with a proxy setting enable trust-proxy for L7 IP forwarding. Applies to api.
6. **Graceful shutdown** — api + worker register SIGTERM drain; Nest apps call `enableShutdownHooks()`. Applies to api, worker.
7. **Queue group** — NATS subscribers declare `queue: '<contract.NATSQueues[role]>'`. Applies to worker.
8. **Env self-shadow absent** — `run.envVariables` carries no `key: ${key}` lines. Applies to any.
9. **Gitignore baseline** — `.gitignore` carries `node_modules`, `dist`, `.env`, `.DS_Store`, plus framework cache dirs. Applies to any.
10. **Env example preserved** — the framework scaffolder's `.env.example` is kept when it ships one. Applies to any.
11. **No scaffold test artifacts** — scaffold-phase self-test scripts are absent from the committed tree. Applies to any.
12. **Scaffolder git skipped** — framework scaffolders run with `--skip-git`, or the scaffolder's `.git/` is removed before scaffold-return. Applies to any.

## When the contract is ready

`plan.SymbolContract` is populated at research-complete. From this moment on it is read-only for the run: provision, generate, deploy, finalize, and close all consume the same contract. Any later mismatch between what the contract declares and what sub-agents emit is a defect the pre-attest commands catch before the scaffold sub-agent returns.
