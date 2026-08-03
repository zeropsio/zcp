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

The recipe's `project.envVariables` (if any) are extracted below as
ready-to-run `zerops_env` pre-steps — key AND value. Run them BEFORE
`zerops_import`. The importer itself accepts `project.envVariables`
inline; it only rejects every OTHER `project.*` key, which is why the
services YAML below never carries a `project:` block.

```
zerops_env action="set" scope="project" key="APP_KEY" value="<@generateRandomString(<32>)>"
```

Preprocessor directives (`<@...>`) evaluate server-side; pass the literal
string, not a pre-rendered value. Repeat for each project env var.

Some recipes carry framework-specific notes about a particular key —
e.g. which prefix format the framework will or won't accept, or whether
a value must be regenerated post-deploy. Check the recipe's gotchas via
`zerops_knowledge recipe="<slug>"` BEFORE pre-setting project env vars;
the gotcha section names the key and the exact value shape that works
for the framework.

2. **Import services.**

Strip `project:`. Submit `services:` verbatim via `zerops_import` — ZCP
already applied plan hostnames and dropped EXISTS-resolved managed
services. Don't edit resource limits, `buildFromGit`, `priority`,
`zeropsSetup`, or `type`.

3. **Wait until every service reaches a running state.** Stage services in standard mode legitimately sit at `READY_TO_DEPLOY` until the first dev → stage cross-deploy; that's acceptable here. Poll:

```
zerops_discover
```

Runtimes must reach a running state (`RUNNING` or `ACTIVE`) before `deploy` — both states are acceptable. Managed deps usually transition first.

4. **Record discovered env vars.**

After services are running, include managed-service env var keys in the provision attestation (e.g. `db: connectionString, port`) for later `run.envVariables` references.
