---
id: bootstrap-recipe-import
priority: 1
phases: [bootstrap-active]
routes: [recipe]
steps: [provision]
title: "Recipe import"
---

### Provision recipe services

Procedure is fixed; do NOT rewrite or reorder.

1. **Project-level env vars (if any).**

If the YAML begins with a `project:` block containing `envVariables:`, set
them at project scope BEFORE `zerops_import`; the import tool rejects
project-level blocks.

```
zerops_env action="set" scope="project" key="APP_KEY" value="<@generateRandomString(<32>)>"
```

Preprocessor directives (`<@...>`) evaluate server-side; pass the literal
string, not a pre-rendered value. Repeat for each project env var.

2. **Import services.**

Strip `project:`. Submit `services:` verbatim via `zerops_import` — ZCP
already applied plan hostnames and dropped EXISTS-resolved managed
services. Don't edit resource limits, `buildFromGit`, `priority`,
`zeropsSetup`, or `type`.

3. **Wait until every service reports `ACTIVE`.** Poll:

```
zerops_discover
```

Every runtime must reach `status: ACTIVE` before `deploy`; managed deps
usually transition first.

4. **Record discovered env vars.**

After ACTIVE, include managed-service env var keys in the provision
attestation (e.g. `db: connectionString, port`) for later
`run.envVariables` references.
