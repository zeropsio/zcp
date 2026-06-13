---
id: bootstrap-provision-rules
priority: 2
phases: [bootstrap-active]
routes: [classic]
steps: [provision]
title: "Provision rules (classic route — import-yaml construction)"
---

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

**Legacy form** you may still see: `type: <svc>@<ver>` + a separate `mode:`
field. `mode: NON_HA` ≡ `:single`, `mode: HA` ≡ `:ha`. It still imports
(remapped to the variant), but `mode` is deprecated and ignored by
validation — author new YAML in the variant form, and read a discovered
`mode:` as its equivalent variant.

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
