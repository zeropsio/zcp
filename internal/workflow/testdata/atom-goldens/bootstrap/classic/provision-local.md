---
id: bootstrap/classic/provision-local
atomIds: [bootstrap-provision-local, bootstrap-provision-rules, develop-api-error-meta, bootstrap-env-var-discovery, bootstrap-wait-active, bootstrap-provision-local-finalize]
description: "Classic route, provision step on a local-machine env (no Zerops container)."
---
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

---

### Read `apiMeta` on every error response

Any `zerops_*` tool surfacing a Zerops API 4xx may include `apiMeta`.
Missing key = no server detail; present key = exact rejected fields.

Shape:

```json
{
  "code": "API_ERROR",
  "apiCode": "projectImportInvalidParameter",
  "error": "Invalid parameter provided.",
  "suggestion": "Zerops flagged specific fields — see apiMeta for each field's failure reason.",
  "apiMeta": [
    {
      "code": "projectImportInvalidParameter",
      "error": "Invalid parameter provided.",
      "metadata": {
        "storage.mode": ["mode not supported"]
      }
    }
  ]
}
```

Each `apiMeta[].metadata` key is a **field path** (`<host>.mode`,
`build.base`, `parameter`); values list reasons. Fix those YAML fields
and retry — do not guess.

Common `apiCode` shapes:

| `apiCode` | `metadata` key | Meaning |
|---|---|---|
| `projectImportInvalidParameter` | `<host>.mode` | type/mode combination not allowed |
| `projectImportMissingParameter` | `parameter` (value `<host>.mode`) | required field missing |
| `serviceStackTypeNotFound` | `serviceStackTypeVersion` | version string not in platform catalog |
| `zeropsYamlInvalidParameter` | `build.base` etc. | zerops.yaml validator caught the field pre-build |
| `yamlValidationInvalidYaml` | `reason` (with `line N:`) | YAML syntax error |

Per-service import failures use `serviceErrors[].meta` with the same
shape, one entry per failing service-stack.

---

### Discover env vars during provision

Once newly-provisioned (classic) or newly-attached (adopt) services have reached RUNNING / ACTIVE, run discovery so the session records env-var KEYS for every managed service. This is authoritative — do not guess alternative spellings; unknown cross-service references become literal strings at runtime and fail silently.

```
zerops_discover includeEnvs=true
```

Record one row per service in the provision attestation. Keys are enough — values stay redacted; discovery is for cataloguing, not consumption. The develop response covers per-service canonical key names plus cross-service reference syntax (`${hostname_varName}`) when wiring `run.envVariables` at first deploy.

**Pre-first-deploy caveat (classic route)**: classic creates runtime services with `startWithoutCode: true` so they reach RUNNING before any code lands; env vars in such containers live in the project catalogue, not `process.env`, until develop runs the first deploy and references fire. Adopted services are typically already deployed, so this caveat doesn't apply on the adopt route.

---

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

### After services reach RUNNING

1. `zerops_discover includeEnvs=true` — keys only.
2. `zerops_env action="generate-dotenv" serviceHostname=""` —
   writes `.env` resolved from live env vars.
3. Add `.env` to `.gitignore` — it contains secrets.
4. Guide the user to start VPN: `zcli vpn up <projectId>`. Needs
   sudo/admin; ZCP cannot start it. The `local-development` guide
   covers VPN.
