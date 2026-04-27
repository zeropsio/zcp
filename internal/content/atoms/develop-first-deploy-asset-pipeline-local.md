---
id: develop-first-deploy-asset-pipeline-local
priority: 5
phases: [develop-active]
modes: [dev, simple, standard]
runtimes: [implicit-webserver]
environments: [local]
deployStates: [never-deployed]
title: "Asset pipeline — local build before verify"
---

### Frontend asset pipeline

Recipes with frontend asset pipelines (Laravel+Vite, Symfony+Encore,
…) intentionally OMIT `npm run build` from dev `buildCommands`. Dev
assumes local Vite HMR and a built artifact for stage.

**Consequence:** after first deploy, stage lacks
`public/build/manifest.json`. Vite helpers (`@vite(...)`,
`<%= vite_* %>`, `{% entry_link_tags %}`) throw HTTP 500
("Vite manifest not found"), so `zerops_verify` fails first.

**Build locally BEFORE `zerops_deploy`:**

```
cd <your-project-dir>
npm install
npm run build
```

The build writes `public/build/manifest.json` locally; `zerops_deploy`
(`zcli push`) ships the working dir, so stage receives the manifest and
next request resolves assets.

**For iterative frontend work, run Vite locally:** `npm run dev`. The
dev server drops `public/build/hot`; helpers route assets to local Vite.
Hot-reload iterates without redeploying. Deploy when stable.

**Do NOT add `npm run build` to dev `buildCommands`.** It defeats the
local-HMR-first dev setup: every `zerops_deploy` rebuilds assets
(~20–30 s penalty).
