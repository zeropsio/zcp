---
id: develop-first-deploy-asset-pipeline-container
priority: 5
phases: [develop-active]
modes: [dev, simple, standard]
runtimes: [implicit-webserver]
environments: [container]
deployStates: [never-deployed]
title: "Dev + asset pipeline — build assets over SSH before verify"
---

### Dev/simple + frontend asset pipeline (container)

Recipes whose backend is `php-nginx` / `php-apache` and whose frontend
runs through a build pipeline (Laravel+Vite, Symfony+Encore, …)
intentionally OMIT `npm run build` from the `dev` setup `buildCommands`.
The design assumes iterative HMR via a Vite dev server started over SSH —
not a production asset rebuild on every `zcli push`.

**Consequence:** `public/build/manifest.json` does not exist after the
first deploy. Any view rendering `@vite(...)` / `<%= vite_* %>` /
`{% entry_link_tags %}` throws HTTP 500 with a `Vite manifest not found`
trace. `zerops_verify` will fail for this reason before it reports any
framework-level bug.

**After the first `zerops_deploy` lands, BEFORE `zerops_verify`:**

```
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null {hostname} \
  'cd /var/www && npm run build'
```

The build writes `public/build/manifest.json` directly into the dev
container — SSHFS propagates it back without a redeploy and PHP-FPM picks
it up on the next request.

**For iterative frontend work, start the dev server instead** — it
watches files and survives template edits without repeated manual builds:

```
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null {hostname} \
  'cd /var/www && nohup npm run dev > /tmp/vite.log 2>&1 &'
```

The dev server drops `public/build/hot`; the framework's Vite helper
detects it and routes asset URLs to the running server instead of reading
the manifest. Containers restart on every `zerops_deploy`, so restart the
dev server after each redeploy.

**Do NOT add `npm run build` to dev `buildCommands`.** Every `zcli push`
would then rebuild assets (~20–30 s penalty) and break the HMR-first
design the dev setup was authored around.
