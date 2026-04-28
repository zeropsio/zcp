---
id: export-intro
priority: 1
phases: [export-active]
environments: [container]
title: "Export — turn a deployed runtime into a re-importable git repo"
---
You are exporting a deployed runtime so a fresh Zerops project can reproduce the same infrastructure from a single git repo. The output is one repository at the chosen runtime's `/var/www` containing source code, `zerops.yaml` (build/run/deploy pipeline), and `zerops-project-import.yaml` (project + service definitions with `buildFromGit:` pointing back at the same repo). Re-import on a new project happens via `zcli project project-import zerops-project-import.yaml` or the dashboard.

The export workflow is a three-call narrowing — probe, generate, publish — and `zerops_workflow workflow="export"` carries each call.

## Pick the runtime

If the project has multiple runtime services, the first call returns a `scope-prompt` listing hostnames; pass `targetService=<hostname>` on the next call. For a project with a single runtime, the first call can already include `targetService` and skip this step.

## Pick the variant (pair modes only)

For `mode=standard` and `mode=local-stage` pairs, the response prompts for `variant=dev` or `variant=stage`:

| Picked variant | Re-imports as | Why |
|---|---|---|
| `dev` | `mode=dev` | Preserves "this was our dev environment" intent. |
| `stage` | `mode=simple` | The stage half of a pair has no dev to cross-deploy from in the new project — collapses cleanly to a standalone runtime. |

<!-- axis-k-keep: signal-#3 -->
Single-half source modes (`dev`, `simple`, `local-only`) skip this prompt — the variant is forced.

## What the next calls do

| Call | Inputs you add | Response |
|---|---|---|
| 2 | `targetService` + `variant` (if pair) | Generated bundle + per-env classification table (only env keys; values fetched separately via `zerops_discover` to keep secrets out of the response). |
| 3 | + `envClassifications` map (key → bucket per env) | `publish-ready` body with `importYaml`/`zeropsYaml` contents + `nextSteps` (write yamls, commit, push). |

If `/var/www/zerops.yaml` is missing or git remote is unconfigured, the response chains to `scaffold-zerops-yaml` or `setup-git-push-container` (or `setup-git-push-local` for local-mode runtimes) instead — complete the prereq, then re-call export.
