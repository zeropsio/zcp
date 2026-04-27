---
id: develop-first-deploy-asset-pipeline-local
priority: 5
phases: [develop-active]
modes: [dev, simple, standard]
runtimes: [implicit-webserver]
environments: [local]
deployStates: [never-deployed]
title: "Asset pipeline — build assets locally before verify"
---

### Frontend asset pipeline

Recipes that ship a frontend asset pipeline
(Laravel+Vite, Symfony+Encore, …) intentionally OMIT `npm run build`
from the `dev` setup `buildCommands`. The design assumes you run Vite
HMR locally and deploy a built artifact to stage.

**Consequence:** `public/build/manifest.json` does not exist on the
stage runtime container after the first deploy. Any view rendering
`@vite(...)` / `<%= vite_* %>` / `{% entry_link_tags %}` throws HTTP
500 with a `Vite manifest not found` trace. `zerops_verify` will fail
for this reason before it reports any framework-level bug.

**Build locally BEFORE `zerops_deploy`:**

```
cd <your-project-dir>
npm install
npm run build
```

The build writes `public/build/manifest.json` into your working
directory; `zerops_deploy` (`zcli push`) ships the entire working dir,
so the manifest lands on the stage runtime container and the next request
resolves asset URLs through it.

**For iterative frontend work, run Vite locally:** `npm run dev`. The
dev server drops `public/build/hot` with localhost URLs; the
framework's Vite helper routes asset URLs to your local Vite server.
Hot-reload iterates without redeploying. Deploy when stable.

**Do NOT add `npm run build` to dev `buildCommands`.** It defeats the
local-HMR-first dev setup (every `zerops_deploy` rebuilds assets,
~20–30 s penalty).
