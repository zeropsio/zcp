---
id: bootstrap-recipe-close
priority: 1
phases: [bootstrap-active]
routes: [recipe]
steps: [close]
title: "Recipe — auto-adopt on close"
---

### Close the recipe bootstrap

`close` finalizes the bootstrap: the conductor writes a `ServiceMeta` per
runtime service (mode from the plan, strategy left unset for develop to
pick on first use) and appends a reflog entry in `CLAUDE.md`.

You do NOT write metas yourself — the conductor runs
`writeBootstrapOutputs` automatically when you complete this step.

```
zerops_workflow action="complete" step="close" attestation="Recipe {slug} bootstrapped — services active and verified"
```

After close, `zerops_workflow action="status"` will report the transition
message with the two primary follow-up workflows: `develop` (iterate on
the code the recipe provided) and `cicd` (wire git-based deploys).
