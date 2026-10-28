---
id: export-compose-ready
priority: 3
phases: [export-active]
exportStatus: [compose-ready]
title: "Export compose-ready — the bundle is yours to commit"
references-fields: []
---

### Export compose-ready — the bundle is yours to commit

The bundle composed clean (schema-valid `zerops-project-import.yaml` + the repo's `zerops.yaml`), and
ZCP has no push to chain for this service — no probe-proven git-push capability, or no checkout of its
own to push from — so it hands the files over instead. This is the standalone recipe-repo outcome: a
repo that re-imports the whole stack.

What to do with it:

1. Write the files into the repository root (the response carries the full bodies) — where
   `nextSteps` names a lease, in that lease's checkout. When `bundle.zeropsYamlSource` is
   `origin`, the `zerops.yaml` was read from the repository's origin and is already there: write
   only `zerops-project-import.yaml`.
2. Commit + push with your own git credentials, or from the lease.
3. Re-import anywhere via `zcli project project-import zerops-project-import.yaml` — `buildFromGit`
   points back at this same repo, so a fresh project clones + builds it.

Optional follow-on, when `nextSteps` offers it: wire probe-proven push capability first
(`zerops_workflow action="git-push-setup" service="<hostname>"`) and re-call export — the workflow
then advances to `publish-ready` with prefilled commit+push commands instead of stopping here.
