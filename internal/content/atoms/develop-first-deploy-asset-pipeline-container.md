---
id: develop-first-deploy-asset-pipeline-container
priority: 5
phases: [develop-active]
modes: [dev, simple, standard]
runtimes: [implicit-webserver]
environments: [container]
deployStates: [never-deployed]
title: "Asset pipeline — SSH build before verify"
---

### Frontend asset pipeline

`php-nginx` / `php-apache` services with a frontend build pipeline
(Laravel+Vite, Symfony+Encore, …) typically OMIT `npm run build` from dev
`buildCommands`. Dev assumes HMR via Vite over SSH, not a production asset
rebuild on every `zerops_deploy`.

**Consequence:** after first deploy, `public/build/manifest.json` is
missing. Vite helpers throw HTTP 500 ("Vite manifest not found"), so
`zerops_verify` fails before any framework bug.

**After the first `zerops_deploy` lands, BEFORE `zerops_verify`:**

```
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null {hostname} \
  'cd /var/www && npm run build'
```

The build writes `public/build/manifest.json` in the dev container;
SSHFS propagates it without redeploy. PHP-FPM reads it on next request —
no restart needed.

**For iterative frontend work, start Vite via the dev-server primitive** (one durable lifecycle per service — survives this MCP call, restarts cleanly with `action=status` / `action=restart`):

```
zerops_dev_server action=start hostname="{hostname}" command="npm run dev" port=5173 healthPath="/"
```

Vite drops `public/build/hot`; helpers route assets through it. The dev-server primitive tracks the process via the runtime container's lifecycle, so backgrounding is not your concern. New containers start on every `zerops_deploy` — re-run `action=start` after each redeploy.

**Do NOT add `npm run build` to dev `buildCommands`.** It defeats
HMR-first dev setup: every push rebuilds assets (~20–30 s penalty).
