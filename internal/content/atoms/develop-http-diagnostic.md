---
id: develop-http-diagnostic
priority: 2
phases: [develop-active]
title: "HTTP diagnostics — priority order"
---

### HTTP diagnostics

When the app returns 500 / 502 / empty body, follow this order. Stop at
whichever step resolves the error — do **not** default to
`ssh {hostname} curl localhost` for diagnosis.

1. **`zerops_verify serviceHostname="{hostname}"`** — runs the canonical
   health probe (HEAD + `/health`/`/status`) and returns a structured
   diagnosis. This is always the first step.
2. **Subdomain URL** — format is
   `https://{hostname}-${zeropsSubdomainHost}.prg1.zerops.app/` for static
   / implicit-webserver runtimes (php-nginx, nginx), `-{port}` appended
   for dynamic runtimes. `${zeropsSubdomainHost}` is a project-scope env
   var (numeric, not the projectId) injected into every container. Read
   it on the host with `env | grep zeropsSubdomainHost`, or call
   `zerops_discover` which returns the resolved URL directly. Do not
   guess a UUID-shaped string.
3. **`zerops_logs severity="error" since="5m"`** — surfaces recent error-
   level platform logs (nginx errors, crash traces, deploy failures)
   without opening a shell.
4. **Framework log file on the mount** — read directly via the `Read`
   tool (e.g. `/var/www/{hostname}/storage/logs/laravel.log`,
   `var/log/...`). Do NOT `ssh {hostname} tail …` — the mount exposes the
   same file, cheaper and without quote-escaping hazards.
5. **Last resort: SSH + curl localhost** — only when the above miss
   something container-local (e.g. worker-only service with no HTTP
   entrypoint; service bound to a non-default interface). Even then,
   `zerops_verify` usually already encodes the check.
