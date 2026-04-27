---
id: bootstrap-recipe-import
priority: 1
phases: [bootstrap-active]
routes: [recipe]
steps: [provision]
title: "Recipe — import services, wait ACTIVE"
---

### Provision recipe services

Procedure is fixed; do NOT rewrite or reorder these steps.

**1. Project-level env vars (if any).**

If the YAML begins with a `project:` block containing `envVariables:`, set
those at the project scope BEFORE calling `zerops_import` — the import tool
rejects project-level blocks, so these vars cannot travel in the same call.

```
zerops_env action="set" scope="project" key="APP_KEY" value="<@generateRandomString(<32>)>"
```

Zerops Preprocessor directives (the `<@...>` forms) are evaluated
server-side; pass the literal string, not a pre-rendered value. Repeat for
every project-level env var in the YAML.

**2. Import services.**

Strip `project:`. Submit `services:` verbatim via `zerops_import` — ZCP already applied your plan's hostnames and dropped EXISTS-resolved managed services. Don't edit resource limits, `buildFromGit`, `priority`, `zeropsSetup`, or `type`.

**3. Wait until every service reports `ACTIVE`.** Poll with:

```
zerops_discover
```

Every runtime service must reach `status: ACTIVE` before `deploy`. Managed
dependencies (postgresql, valkey, etc.) typically transition first.

**4. Record discovered env vars.**

After services reach ACTIVE, include a summary of managed-service env var
keys in the provision attestation (e.g. `db: connectionString, port`).
These keys are needed for cross-service references in `run.envVariables`.
