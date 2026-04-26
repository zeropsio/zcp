---
id: develop-implicit-webserver
priority: 2
phases: [develop-active]
runtimes: [implicit-webserver]
title: "Implicit-webserver runtime — no manual start, serve from documentRoot"
---

### Implicit-Webserver Runtime (`php-apache`, `php-nginx`)

Apache or nginx is already running inside the container and auto-serves
whatever's on disk. **Do not SSH in to start a server** — there is no
`{start-command}` to run. Deploy lands the files; the web server picks
them up immediately.

**`zerops.yaml` shape differences vs. dynamic runtimes:**

- Omit `run.start` — leave the field out entirely (not even `zsc noop`).
- Omit `run.ports` — port 80 is fixed; the platform handles it.
- Set `run.documentRoot` to the web-serving subtree. For Laravel / Symfony
  / composer-based apps that's `public` (not the repo root). For apps that
  serve from the root, omit `documentRoot` or set it to `.`.

**Deploy flow (both strategies):**

1. Write or edit files at `/var/www/<hostname>/`.
2. Run the strategy-specific deploy (see the active strategy atom).
3. Verify by fetching a URL, not by checking process state:
   `zerops_verify serviceHostname="<hostname>"` or `zerops_discover service="<hostname>"` to read the current `subdomainUrl` + curl.

**When the page 404s or 403s after a successful deploy:**

- Wrong `documentRoot` — the web server points at a directory that lacks
  the expected `index.php` / `index.html`.
- Missing `index.php` entrypoint — composer-based apps often need
  `public/index.php` but a scaffolded project may start without it.
- `.htaccess` / rewrite rules not shipped — `deployFiles` must include
  the files the web server needs, not just the PHP sources.

`zerops_logs` surfaces Apache / nginx errors directly — use them for
routing / permission triage (there's no app process to crash).
