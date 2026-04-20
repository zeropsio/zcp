# Substep: start-processes

This substep completes when every dev-phase runtime process is running on its target container. Processes include the primary server, any asset dev server, and any worker process. All processes start before the verify-dev substep — a verify against a dashboard that needs an asset dev server will 500 if the asset server is not up yet.

## Primary server

- **Server-side apps** (NestJS, Express, NextJS, Go, Django, Rails, Laravel queue consumers, etc.): start via `zerops_dev_server action=start` on the target hostname. The dev-server tool owns the start/stop/status/logs lifecycle and bounds every phase with a tight per-step budget; raw backgrounded SSH commands hit the 120s SSH channel wall because the backgrounded child keeps the channel open.

  ```
  zerops_dev_server action=start hostname=apidev command="npm run start:dev" port=3000 healthPath="/api/health"
  ```

- **Implicit-webserver runtimes** (php-nginx, php-apache, nginx base): skip this step; the runtime's webserver auto-starts on deploy.
- **Static frontends** built into `dist/` or `public/`: skip; Nginx on the dev container serves the built files directly.

## Asset dev server (when the build pipeline uses a secondary runtime)

If `run.prepareCommands` installs a secondary runtime (for example `sudo -E zsc install nodejs@22`) and the scaffold defines a dev server (Vite, Webpack dev server, esbuild --watch), start it alongside the primary server. Before starting, probe whether one is already running — the deploy framework may have started it on first deploy, and a second instance silently falls back to an incremented port that the public subdomain does not route to:

```
ssh appdev "pgrep -f 'vite' || true"
ssh appdev "pgrep -f 'npm run dev' || true"
```

If a process is already running on the expected port, the start is already done. If you need to restart after a config change, stop first then start once using `zerops_dev_server`. Pass the framework's host-binding flag so it listens on `0.0.0.0` (for example `npx vite --host 0.0.0.0`). This step is required even when the primary server auto-starts — the primary handles HTTP, the asset dev server compiles CSS/JS, and templates that reference build-pipeline outputs (Vite manifests, Webpack bundles) depend on it being live.

The asset dev server's lifecycle is HMR-based; the `npm run build` static-asset alternative defeats the purpose of the dev phase.

## Worker dev process (showcase)

- **Shared-codebase worker** (`worker.sharesCodebaseWith` is set on the plan): start the queue consumer on the host target's dev container — both processes run from the same container and the same code tree.

  ```
  zerops_dev_server action=start hostname={host}dev command="{queue_worker_command}" port=0 healthPath=""
  ```

  `{host}` is the target named by `sharesCodebaseWith` — `appdev` for single-app recipes, `apidev` for dual-runtime recipes where the API hosts the worker. Port `0` and empty `healthPath` tell the dev-server tool this process has no HTTP endpoint; it verifies start by pgrep alone.

- **Separate-codebase worker** (`worker.sharesCodebaseWith` is empty, the default for 3-repo shapes): deploy the separate worker codebase to its own dev container, then start the process there.

  ```
  zerops_deploy targetService="workerdev" setup="dev"
  zerops_dev_server action=start hostname=workerdev command="{queue_worker_command}" port=0 healthPath=""
  ```

After any redeploy during iteration, every process above restarts — the previous container is gone. Re-run this substep before re-running verify-dev.
