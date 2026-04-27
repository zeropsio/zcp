---
id: develop-http-diagnostic
priority: 2
phases: [develop-active]
title: "HTTP diagnostics — priority order"
references-atoms: [develop-platform-rules-container]
---

### HTTP diagnostics

For 500 / 502 / empty body, stop at the first useful signal; do **not**
default to
`ssh {hostname} curl localhost` for diagnosis.

1. **`zerops_verify serviceHostname="{hostname}"`** — start with the
   canonical health probe and structured diagnosis; see
   `develop-verify-matrix` for the full verify path.
2. **Subdomain URL** — static / implicit-webserver:
   `https://{hostname}-${zeropsSubdomainHost}.prg1.zerops.app/`; dynamic
   adds `-{port}`. `${zeropsSubdomainHost}` is numeric and project-scope,
   not the projectId. Read it with `env | grep zeropsSubdomainHost`, or
   use `zerops_discover` for the resolved URL. Do not guess a UUID.
3. **`zerops_logs severity="error" since="5m"`** — recent platform errors
   (nginx, crash traces, deploy failures) without opening a shell.
4. **Framework log file on the mount** — read via Read tool
   (`/var/www/{hostname}/storage/logs/laravel.log`, `var/log/...`). See
   `develop-platform-rules-container` for the mount-vs-SSH split.
5. **Last resort: SSH + curl localhost** — only when earlier checks miss
   container-local state (worker-only service, non-default bind). Even
   then, `zerops_verify` usually already encodes the check.
