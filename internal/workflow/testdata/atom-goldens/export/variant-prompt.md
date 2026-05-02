---
id: export/variant-prompt
atomIds: [export-intro, export-variant-prompt]
description: "Export workflow, targetService picked but Variant unset on a mode=standard pair — agent picks dev or stage half."
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

You are at `status="variant-prompt"`. The chosen `targetService` is part of a `mode=standard` (or `mode=local-stage`) pair, and the export workflow needs to know which half — `dev` or `stage` — to package into the bundle.

## What the variant decides

`variant="dev"` packages the dev hostname's working tree + the live `zerops.yaml` from `/var/www`. `variant="stage"` packages the stage hostname's tree. Each variant produces a different bundle because the two halves have different deploy histories, env values, and (sometimes) different `zerops.yaml` `setup:` blocks.

Both bundles emit Zerops scaling `mode=NON_HA` for the runtime entry — single-runtime imports collapse to standalone scaling regardless of source variant. The destination project's topology mode (the `dev` / `stage` / `simple` / `local-stage` / `local-only` decision) is established by ZCP's bootstrap on the destination side at re-import, not embedded in the bundle.

## Re-call with `variant`

```
zerops_workflow workflow="export" targetService="appdev" variant="dev"
```

Substitute `variant="stage"` for the stage half. The `targetService` hostname must match the variant: the dev hostname goes with `variant="dev"`, the stage hostname with `variant="stage"` — passing the dev hostname with `variant="stage"` is a mismatch and the handler errors.

After the next call, the response advances to the next status (`scaffold-required` / `git-push-setup-required` / `classify-prompt` / `validation-failed` / `publish-ready`) depending on which preconditions hold for the chosen half.
