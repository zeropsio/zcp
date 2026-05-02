---
id: bootstrap-classic-plan-static
priority: 2
phases: [bootstrap-active]
routes: [classic]
steps: [discover]
title: "Classic bootstrap — static runtime plan"
---

### Static runtime plan

If the plan you're about to submit includes a static-runtime container (`nginx`, `static`), apply this section. (Dynamic-runtime planning lives in the sibling `bootstrap-classic-plan-dynamic`.) Static-runtime containers come up serving an empty document root after bootstrap. The first build artifact lands in develop via `zerops_deploy`; bootstrap creates the empty container and stops there.

Before submitting the plan, confirm with the user:

- the chosen runtime hostname (`appdev` is the standard convention)
- whether a stage pair is wanted (`standard` mode) or a single container (`simple` / `dev` mode)

Close-mode, git-push capability, and the actual `zerops.yaml` (including `deployFiles` shape) are decided in develop after the first deploy lands — not here.
