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

Managed defaults: omit mode or set `mode: NON_HA`; set `mode: HA` only
when the user asks for production HA. Use `priority: 10` so managed
services initialize before runtime services (default 5).

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
