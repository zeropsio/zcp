---
id: bootstrap-recipe-generate
priority: 1
phases: [bootstrap-active]
routes: [recipe]
steps: [generate]
title: "Recipe — code is already in the repo"
---

### No fresh zerops.yaml needed

The recipe ships with its own `zerops.yaml` and source code, pulled into
the platform at provision time via `buildFromGit`. Writing a new
`zerops.yaml` here would conflict with the one the recipe already deploys.

What to do instead:

- If the user explicitly wants to edit application code, they should clone
  the recipe's GitHub repo locally (URL visible in the recipe's import
  YAML under each service's `buildFromGit:` key), iterate, and redeploy
  via the normal develop flow.
- For bootstrap purposes this step is effectively a no-op. Complete it
  with a short attestation such as `Recipe ships zerops.yaml via
  buildFromGit — no local generation required`.
