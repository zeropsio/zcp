---
id: export/compose-ready
atomIds: [export-intro, export-compose-ready]
description: "Export workflow, bundle composed clean but no probe-proven push capability — bundle handed over for the user to commit (the standalone recipe-repo terminal)."
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

=== export-compose-ready ===
### Export compose-ready — the bundle is yours to commit

The bundle composed clean (schema-valid `zerops-project-import.yaml` + the repo's `zerops.yaml`), and
no probe-proven git-push capability exists for this service — so ZCP hands the files over instead of
chaining a push. This is the standalone recipe-repo outcome: a repo that re-imports the whole stack.

What to do with it:

1. Write both files into the repository root (the response carries the full bodies).
2. Commit + push with your own git credentials.
3. Re-import anywhere via `zcli project project-import zerops-project-import.yaml` — `buildFromGit`
   points back at this same repo, so a fresh project clones + builds it.

Optional follow-on: wire probe-proven push capability first (`zerops_workflow
action="git-push-setup" service="<hostname>"`) and re-call export — the workflow then advances to
`publish-ready` with prefilled commit+push commands instead of stopping here.
