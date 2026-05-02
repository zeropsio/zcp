---
id: export-validate
priority: 3
phases: [export-active]
exportStatus: [validation-failed]
environments: [container]
title: "Read the export bundle's preview + warnings before publishing"
references-fields: [ops.ExportBundle.ImportYAML, ops.ExportBundle.ZeropsYAML, ops.ExportBundle.Warnings, ops.ExportBundle.Errors]
---
The Phase B response carries a generated bundle even when classifications are not yet accepted. Read every field before re-calling with `envClassifications` populated — corrections are cheaper here than after publish.

## What the response carries

| Field | What it contains | Why it matters |
|---|---|---|
| `bundle.importYaml` | The `zerops-project-import.yaml` body. | Inspect the runtime entry's `buildFromGit:`, `zeropsSetup:`, `enableSubdomainAccess:`, and `project.envVariables`. The `services:` list also carries managed deps so `${db_*}`/`${redis_*}` resolve at re-import. |
| `bundle.zeropsYaml` | The repo's live `zerops.yaml` body, verbatim. | Confirm the chosen `setup:` block matches the variant. The `run.envVariables` references must resolve against envs that survived classification. |
| `bundle.warnings` | Per-env hints from the composer. | M4 empty externals, sentinel patterns, unset classifications, and M2 indirect references all surface here. Don't publish with an unresolved warning. |
| `bundle.repoUrl` | Live `git remote get-url origin` from the chosen runtime container. | If wrong (stale remote, accidental fork), fix via `git remote set-url origin <url>` on the runtime container — or re-run `git-push-setup` to refresh the cached `RemoteURL`. |

## Three classes of warning to act on

### M2 — indirect infrastructure reference

```
env "DB_HOST": classified Infrastructure (drops from project.envVariables) but zerops.yaml's run.envVariables references ${DB_HOST} — re-import will fail to resolve. Reclassify as PlainConfig or rewrite zerops.yaml to use managed-service refs (${db_*}/${redis_*}) directly. (plan §3.4 M2)
```

`zerops.yaml` references the project env's name (e.g. `${DB_HOST}`), not the managed-service env's name (`${db_hostname}`). Dropping `DB_HOST` from `project.envVariables` makes the reference unresolvable at re-import. Two fixes:

1. **Reclassify as `plain-config`** — the value `${db_hostname}` stays in the bundle, Zerops applies it at boot, and the runtime sees `DB_HOST=${db_hostname}` which resolves to the managed db's hostname. Preserves the indirection.
2. **Rewrite `zerops.yaml`** so `run.envVariables` references managed-service envs directly: `DB_HOST: ${db_hostname}`. This shortens the resolution chain at the cost of editing the live `zerops.yaml` (which is then bundled with the export).

Pick (1) for quick exports; pick (2) if the new project's owner shouldn't need to know about `DB_HOST` as a separate env.

### M4 — empty / sentinel external secret

```
env "STRIPE_SECRET": empty external secret — review before publish (plan §3.4 M4)
env "STRIPE_KEY": external secret value "sk_test_xyz" matches a known sentinel/test pattern — verify classification (PlainConfig may be more appropriate; plan §3.4 M4)
```

You classified the env `external-secret` but the value is empty or matches a known test/sentinel pattern (`sk_test_*`, `pk_test_*`, `rk_test_*`, `disabled`, `none`, `null`, `false`, `off`, `n/a`, `noop`). Re-import would substitute `<@pickRandom(["REPLACE_ME"])>` for an empty production-like key — likely wrong. Two fixes:

1. **Reclassify as `plain-config`** — carry the empty / sentinel value verbatim. Re-imported services boot with the same disabled / staging shape.
2. **Confirm the bucket and edit the bundle**: if a real key SHOULD be set, bucket `external-secret`, accept the `REPLACE_ME` placeholder, and add a "set this env in dashboard before deploy" step to the new project's runbook.

### Unclassified env

```
env "MYSTERY_VAR": not classified — emitted as plain-config; classify before publish (plan §3.4)
```

You did not send a bucket for this env. The bundle defaults to `plain-config` (emits the value verbatim), which may leak secrets. Re-call with the missing entry classified.

## Schema validation

`bundle.errors` carries blocking JSON-Schema failures from the embedded `import-project-yml-json-schema.json` + `zerops-yml-json-schema.json` (Phase 5). Each entry has `path` (JSON pointer) + `message`. When non-empty, the handler returns `status="validation-failed"` instead of `publish-ready` — fix each error at its source (env classification, zerops.yaml, or service shape) and re-call. Schema drift between the embedded copy and Zerops's current schema is possible; if `zcli project project-import` rejects a bundle that the client validator accepted, the embedded testdata needs a refresh.

**Fixing live `/var/www/zerops.yaml` requires the develop workflow**, not export. Export is stateless — `zerops_mount` returns `WORKFLOW_REQUIRED` during export. To edit the runtime container's zerops.yaml: start `zerops_workflow workflow="develop" scope=[<runtime>]`, mount the service via `zerops_mount`, edit the file, deploy, then re-call export with the same `targetService` + `envClassifications`. The export workflow re-reads zerops.yaml fresh on every invocation, so the fix flows through automatically.

When `publish-ready` fires, spot-check the rendered shape:

- `services[].mode` is `NON_HA` (single-runtime bundles; `HA` requires explicit scaling fields).
- `services[].buildFromGit` resolves to a HTTPS or SSH-form remote URL.
- `services[].zeropsSetup` matches a `setup:` name in the bundled `zerops.yaml`.
- `project.envVariables` keys are not duplicated.
- `#zeropsPreprocessor=on` header is line 1 if any value contains `<@...>`.
