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
