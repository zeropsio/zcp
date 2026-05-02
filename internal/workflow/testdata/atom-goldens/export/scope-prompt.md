---
id: export/scope-prompt
atomIds: [export-intro, export-scope-prompt]
description: "Export workflow first call, no targetService selected — agent picks from runtimes list."
---
<!-- UNREVIEWED -->

You are exporting a deployed runtime so a fresh Zerops project can reproduce the same infrastructure from a single git repo. The output is one repository at the chosen runtime's `/var/www` containing source code, `zerops.yaml` (build/run/deploy pipeline), and `zerops-project-import.yaml` (project + service definitions with `buildFromGit:` pointing back at the same repo). Re-import on a new project happens via `zcli project project-import zerops-project-import.yaml` or the dashboard.

The export workflow is a three-call narrowing — probe, generate, publish — and `zerops_workflow workflow="export"` carries each call.

## Pick the runtime

If the project has multiple runtime services, the first call returns a `scope-prompt` listing hostnames; pass `targetService=<hostname>` on the next call. For a project with a single runtime, the first call can already include `targetService` and skip this step.

## Pick the variant (pair modes only)

For `mode=standard` and `mode=local-stage` pairs, pick `variant=dev` (packages the dev hostname's tree + zerops.yaml) or `variant=stage` (packages the stage hostname's tree). Both bundle entries emit Zerops scaling `mode=NON_HA` — the destination project's topology Mode is established by ZCP's bootstrap at re-import, not embedded in the bundle.

Single-half source modes (`dev`, `simple`, `local-only`) skip this prompt — the variant is forced.

## What the next calls do

| Call | Inputs you add | Response |
|---|---|---|
| 2 | `targetService` + `variant` (if pair) | Generated bundle + per-env classification table (only env keys; values fetched separately via `zerops_discover` to keep secrets out of the response). |
| 3 | + `envClassifications` map (key → bucket per env) | `publish-ready` body with `importYaml`/`zeropsYaml` contents + `nextSteps` (write yamls, commit, push). |

If `/var/www/zerops.yaml` is missing or git remote is unconfigured, the response chains to `scaffold-zerops-yaml` or `setup-git-push-container` (or `setup-git-push-local` for local-mode runtimes) instead — complete the prereq, then re-call export.

---

You are at `status="scope-prompt"`. The export workflow needs to know which runtime service to package — `targetService` was not supplied on this call, so the response carries the project's `runtimes` list instead of a bundle.

## Pick a hostname from `runtimes`

The `runtimes` array in the response lists every non-managed (non-infrastructure) hostname in the project. Pick the runtime that owns the source repo + zerops.yaml you want to package; managed services (`db`, `redis`, `valkey`, `mongo`, …) come along automatically as bundle dependencies — they are NOT export targets and do NOT appear in `runtimes`.

For a project with a single runtime, you can skip this prompt on the next call by supplying `targetService` directly. For a multi-runtime project (e.g. `app` + `worker`), the choice of `targetService` decides which repo's `zerops.yaml` and `/var/www` tree the bundle captures.

## Re-call with `targetService`

```
zerops_workflow workflow="export" targetService="<hostname-from-runtimes>"
```

If the chosen hostname is a half of a `mode=standard` or `mode=local-stage` pair, the next response is `variant-prompt` (pick `dev` or `stage`). For all other modes, the next response is one of `scaffold-required` / `git-push-setup-required` / `classify-prompt` / `validation-failed` / `publish-ready` depending on which preconditions hold for that runtime.
