---
id: export/scope-prompt
atomIds: [export-intro, export-scope-prompt]
description: "Export workflow first call, no targetService selected — agent picks from runtimes list."
---
=== export-intro ===
You are exporting a deployed runtime so a fresh Zerops project can reproduce the same infrastructure from a single git repo. The output is one repository at the chosen runtime's `/var/www` containing source code, `zerops.yaml` (build/run/deploy pipeline), and `zerops-project-import.yaml` (project + service definitions with `buildFromGit:` pointing back at the same repo). Re-import on a new project happens via `zcli project project-import zerops-project-import.yaml` or the dashboard.

The export workflow is a three-call narrowing — probe, generate, publish — and `zerops_workflow workflow="export"` carries each call. Some companion atoms refer to these as **Phase A** (probe — scope prompt), **Phase B** (generate — classify/validate), and **Phase C** (publish — bundle + push).

## Pick the runtime

If the project has multiple runtime services, the first call returns a `scope-prompt` listing hostnames; pass `targetService=<hostname>` on the next call. For a project with a single runtime, the first call can already include `targetService` and skip this step. For a pair, the dev and stage halves are distinct hostnames in that list — the hostname you pass alone selects which half is packaged (`appdev` → dev tree, `appstage` → stage tree).

## What the next calls do

| Call | Inputs you add | Returns `status=` |
|---|---|---|
| 2 | `targetService` | `classify-prompt` |
| 3 | + `envClassifications` map (key → bucket per env) | `publish-ready` (push-capable source), `compose-ready` (no probe-proven push capability — bundle handed over to commit yourself), or `validation-failed` |

The status-specific section of the response carries content + commands; this table is a call-shape map, not a content cheatsheet.

If `/var/www/zerops.yaml` is missing or git remote is unconfigured, the response carries a status that walks the prereq (zerops.yaml scaffold or `git-push-setup`) instead — complete the prereq, then re-call export.

---

=== export-scope-prompt ===
You are at `status="scope-prompt"`. The export workflow needs to know which runtime service to package — `targetService` was not supplied on this call, so the response carries the project's `runtimes` list instead of a bundle.

## Pick a hostname from `runtimes`

The `runtimes` array in the response lists every non-managed (non-infrastructure) hostname in the project. Pick the runtime that owns the source repo + zerops.yaml you want to package; managed services (`db`, `redis`, `valkey`, `mongo`, …) come along automatically as bundle dependencies — they are NOT export targets and do NOT appear in `runtimes`.

For a project with a single runtime, you can skip this prompt on the next call by supplying `targetService` directly. For a multi-runtime project (e.g. `app` + `worker`), the choice of `targetService` decides which repo's `zerops.yaml` and `/var/www` tree the bundle captures.

## Re-call with `targetService`

```
zerops_workflow workflow="export" targetService="<hostname-from-runtimes>"
```

The chosen hostname alone determines which half of a pair is packaged (`appdev` → dev half, `appstage` → stage half) — there is no separate dev/stage choice. The next response is one of `scaffold-required` / `git-push-setup-required` / `classify-prompt` / `validation-failed` / `publish-ready` depending on which preconditions hold for that runtime.
