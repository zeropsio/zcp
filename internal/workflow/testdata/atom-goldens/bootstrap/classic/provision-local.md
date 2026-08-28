---
id: bootstrap/classic/provision-local
atomIds: [bootstrap-provision-local, bootstrap-provision-rules, bootstrap-env-var-discovery, bootstrap-wait-active, bootstrap-provision-local-finalize]
description: "Classic route, provision step on a local-machine env (no Zerops container)."
---
=== bootstrap-provision-local ===
### Local-mode provision

Import shape depends on mode:

| Mode | Runtime services | Managed services |
|------|-------------------------------|-----------------|
| Standard | `{name}stage` only; no dev on Zerops | Yes |
| Simple | `{name}` (single service) | Yes |
| Dev / managed-only | None — no runtime on Zerops | Yes |

**Stage properties (standard mode)**:

- Do NOT set `startWithoutCode` — stage waits for first deploy
  (READY_TO_DEPLOY).
- `enableSubdomainAccess: true`.
- No `maxContainers: 1` — use defaults.

**No SSHFS** — `zerops_mount` is unavailable in local mode; files live
on the user's machine.

---

=== bootstrap-provision-rules ===
### Hostname format constraint

API rule: 1–40 chars, **lowercase letters and digits only** (`a-z`,
`0-9`), first char a letter. No dashes, underscores, uppercase, or dots.
Violations fail import with `serviceStackNameInvalid`.

Valid: `appdev`, `app42`, `apistorage`, `workersearch`.
Invalid: `42db`, `my-cache`, `my_app`, `MyApp`, `app.dev`,
`app123456789012345678901234567890123456789`.

### Managed service hostname conventions

Canonical hostnames:

| Hostname | Types |
|---|---|
| `db` | postgresql, mariadb, mysql, mongodb |
| `cache` | valkey, keydb, redis |
| `queue` | nats, kafka, rabbitmq |
| `search` | elasticsearch, meilisearch, typesense |
| `storage` | object-storage, shared-storage |

**Deployment variant + scaling.** HA vs single node is encoded in the
service `type`, not a separate field: `<svc>:single@<ver>` (default) or
`<svc>:ha@<ver>` (3-node cluster, production-grade). Use `:single@` unless
the user asks for production HA. Set `priority: 10` so managed services
initialize before runtime services (default 5).

PostgreSQL and Valkey also take a `profile` (scaling tier — see the
choose-database / choose-cache decisions). Omitting `profile` applies a
default (PostgreSQL single → `oltp-staging`, HA → `oltp-production` =
dedicated CPU + high minima); set it explicitly — dev → PostgreSQL
`oltp-hobby` / Valkey `hobby`, production → `oltp-staging` / `staging`,
higher only on a clear load signal. Other managed types (MariaDB,
ClickHouse, Kafka, …) have no profile — scale them with `verticalAutoscaling`.

**OS variant.** The OS is part of the runtime `type` / `base`, never a
separate field: `<os>/<tech>@<ver>` — `ubuntu/nodejs@22` or
`alpine/nodejs@22`. Every runtime ships both (deno: ubuntu only; docker:
alpine only). Write the prefix explicitly in the import `type` AND in both
`build.base` and `run.base` of zerops.yaml, and keep it identical to the
service type `zerops_discover` reports — `run.base` rewrites the service's
OS on every deploy, and a build/run OS mismatch compiles native modules
against the wrong libc. Changing the prefix invalidates the build cache.

**Legacy forms** — older YAML, recipes and docs still carry these; the
platform accepts them, the schema marks them deprecated, never author them;
read each as its equivalent:

- Legacy: `type: <svc>@<ver>` + `mode: NON_HA` / `mode: HA` ≡
  `<svc>:single@<ver>` / `<svc>:ha@<ver>`
- Legacy: `base: nodejs@22` + `os: ubuntu` ≡ `ubuntu/nodejs@22`
- Legacy: bare `base: nodejs@22` in zerops.yaml ≡ `alpine/nodejs@22`; bare
  `type: nodejs@22` in import ≡ `ubuntu/nodejs@22` (only `static` ≡
  `alpine/static`) — the two legacy defaults differ and `run.base` wins at
  deploy, so a bare import + bare zerops.yaml flips the service to alpine on
  first deploy. When rewriting legacy YAML keep the OS equal to the type
  from `zerops_discover`.

### Runtime service properties

Set these during import-yaml generation:

| Property | Dev service | Stage service | Simple service |
|----------|-----------|---------------|----------------|
| `startWithoutCode` | `true` | omit | `true` |
| `maxContainers` | `1` | omit | omit |
| `enableSubdomainAccess` | `true` | `true` | `true` |
| `verticalAutoscaling.minRam` | `1.0` for compiled runtimes | omit | omit |

`startWithoutCode: true` lets dev/simple reach RUNNING before first
deploy; without it they sit at READY_TO_DEPLOY, blocking SSHFS and SSH.
Stage deliberately omits it and waits at READY_TO_DEPLOY for the first
dev→stage cross-deploy.

**Never put `mode`, `:ha`/`:single`, or HA in any form on a runtime
service** — runtimes have no deployment variant. The `mode` field (legacy
`NON_HA`/`HA`) belongs only to managed services, and even there the variant
`type` has replaced it. For runtimes, the dev/standard/simple distinction is
committed in the discover-step plan via `bootstrapMode`, which composes the
runtime properties (`startWithoutCode`, `maxContainers`, paired stage entry)
into the import yaml at provision time. Adding `mode: DEV` (or similar
invented values) silently fails.

Expected post-import states: Dev/Simple → RUNNING, Stage →
READY_TO_DEPLOY, Managed → RUNNING/ACTIVE.

### Import YAML — `project:` block dichotomy

`zerops_import` operates within an EXISTING project (the one ZCP is
attached to) and **rejects YAML containing a top-level `project:`
block** with `IMPORT_HAS_PROJECT`. The block is only valid for the
`zcli project project-import` create-new-project flow.

If the YAML you constructed (or copied from a recipe template, or saw
in a Zerops doc) starts with `project:` → strip that block before
calling `zerops_import`. If it carried project-level env vars, set
them at project scope FIRST via:

```
zerops_env action="set" scope="project" key="<KEY>" value="<value-or-preprocessor-directive>"
```

Preprocessor directives (e.g. `<@generateRandomString(<32>)>`)
evaluate server-side; pass the literal string, not a pre-rendered
value. After all project-level keys are set, submit `services:`
verbatim to `zerops_import`.

---

=== bootstrap-env-var-discovery ===
### Discover env vars during provision

Once newly-provisioned (classic) or newly-attached (adopt) services have reached RUNNING / ACTIVE, run discovery so the session records env-var KEYS for every managed service. This is authoritative — do not guess alternative spellings; unknown cross-service references become literal strings at runtime and fail silently.

```
zerops_discover includeEnvs=true
```

Record one row per service in the provision attestation. Keys are enough — values stay redacted; discovery is for cataloguing, not consumption. The develop response covers per-service canonical key names plus cross-service reference syntax (`${hostname_varName}`) when wiring `run.envVariables` at first deploy.

**Adopt route — skip when no new wiring:** adopted services already carry their env wiring in the running app, so this discovery is only needed if THIS task adds NEW cross-service references. For a code-only change to an already-wired app (edit / redesign / bugfix), skip it and fetch keys lazily at wiring time — running it now is a no-op round-trip.

**Pre-first-deploy caveat (classic route)**: classic creates runtime services with `startWithoutCode: true` so they reach RUNNING before any code lands; env vars in such containers live in the project catalogue, not `process.env`, until develop runs the first deploy and references fire. Adopted services are usually ACTIVE.

When `zerops_discover` shows a runtime stuck at `status=READY_TO_DEPLOY`, branch on whether it ever tried to build (check `zerops_events`):

- **Never built** (created without `startWithoutCode: true`, no failed build in the timeline): re-import with `startWithoutCode: true` + `override: true` to reach ACTIVE. Safe — there is no deployed code to lose.
- **Build FAILED** (the timeline shows a failed build / prior deploy attempt): the service still holds the buildFromGit code that failed to build. DIAGNOSE first — `zerops_events` then `zerops_logs` — fix the cause (e.g. add the missing managed dependency the build needed), then re-deploy. Do **NOT** `override`: it REPLACES the service stack and wipes the very source you need to fix. (`override=true` on a service with deploy history returns `DIAGNOSIS_REQUIRED`; acknowledging `confirmDestructive` still wipes — only do it if the code lives elsewhere, e.g. git.)

---

=== bootstrap-wait-active ===
### Wait until services are running

After `zerops_import` completes, the Zerops engine provisions runtime containers
asynchronously. Subsequent deploy or verify calls against a service that is
still `CREATING` / `STARTING` will fail with a retryable error.

Poll service state:

```
zerops_discover
```

Repeat until every service reports a running status. Expected transitions: dev / simple runtimes → `RUNNING` (with `startWithoutCode: true`) or `ACTIVE` once a deploy lands; stage runtimes → `READY_TO_DEPLOY` (waiting for the first dev → stage cross-deploy); managed services → `RUNNING` / `ACTIVE`. The readiness predicate accepts BOTH `RUNNING` and `ACTIVE` as the operational state — do not block on a specific string. `READY_TO_DEPLOY` is acceptable for stage services in standard mode at this step.

---

=== bootstrap-provision-local-finalize ===
### After services reach RUNNING

1. `zerops_discover includeEnvs=true` — keys only.
2. `zerops_env action="generate-dotenv" serviceHostname=""` —
   writes `.env` resolved from live env vars.
3. Add `.env` to `.gitignore` — it contains secrets.
4. Guide the user to start VPN: `zcli vpn up <projectId>`. Needs
   sudo/admin; ZCP cannot start it. The `local-development` guide
   covers VPN.
