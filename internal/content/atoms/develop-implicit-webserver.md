---
id: develop-implicit-webserver
priority: 2
phases: [develop-active]
runtimes: [implicit-webserver]
title: "Implicit-webserver runtime"
---

### Implicit-Webserver Runtime (`php-apache`, `php-nginx`)

Apache or nginx is bundled into the runtime image — **no manual `start:` and no `zerops_dev_server` cycling**. After deploy, the web server is already running and serves disk contents; before first deploy the runtime container exists but no web server has been provisioned yet (deploy is the moment that lands files + activates the server). **Do not SSH in to start a server** — there is no `{start-command}` to run.

**`zerops.yaml` differences vs. dynamic runtimes:**

- Omit `run.start` — leave the field out entirely (not even `zsc noop`).
- Omit `run.ports` — port 80 is fixed; Zerops handles it.
- Set `run.documentRoot` to the web-serving subtree. Laravel / Symfony /
  composer apps use `public`; root-serving apps omit it or set `.`.

**Deploy flow (both strategies):**

1. Write or edit application files.
2. Run the strategy-specific deploy (see the active strategy atom).
3. Verify as a web-facing service; see `develop-verify-matrix`.

**When 404/403 follows successful deploy:**

- Wrong `documentRoot` — the web server points at a directory that lacks
  the expected entrypoint.
- `.htaccess` / rewrite rules not shipped — `deployFiles` must include
  the files the web server needs, not just the PHP sources.

`zerops_logs` surfaces Apache / nginx errors for routing / permission
triage; there is no app process to crash.
