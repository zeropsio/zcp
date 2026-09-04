---
id: develop-verify-matrix
priority: 4
phases: [develop-active]
title: "Verify matrix"
references-fields: [ops.VerifyResult.Status, ops.VerifyResult.Checks, ops.CheckResult.Status, ops.CheckResult.Detail, ops.CheckResult.HTTPStatus, ops.CheckResult.Recovery, ops.CheckResult.BodyText, ops.CheckResult.ConsoleErrors]
---

### Per-service verify matrix

Verify every service after deploy — deploy success ≠ working app. Shape from
`zerops_discover`: subdomain URL = web-facing; managed / no HTTP port = non-web.
Run `zerops_verify` first; a check with a `recovery` field → run it, re-verify,
before any browser probe.

| Shape | Check |
|---|---|
| non-web (managed / worker / no HTTP port) | `zerops_verify` → `status=healthy` is the whole check |
| web (dynamic / static / implicit-webserver) | `zerops_verify` → judge `http_root`: `httpStatus` + `bodyText` + `consoleErrors`; healthy + a real body (not a blank shell / error page, no fatal console error) proves it |

When `bodyText`/`consoleErrors` are missing, truncated, or the page needs
interaction / SPA routes / non-root / auth, drive the browser **inline** with
`zerops_browser` — inner commands cover click/fill/find/get/is/wait plus
`set viewport`/`set device`/`set media` for responsive and dark-mode checks;
pass `screenshot: true` for visual evidence; failed/4xx/5xx network requests
are always reported alongside errors/console, no flag needed. Never spawn a
sub-agent, call raw `agent-browser`, or use `eval`.
Internal-only service (no public subdomain) → `zerops_subdomain action="disable"` after deploy.

- **VERDICT: PASS** — healthy + real rendered content; proceed.
- **VERDICT: FAIL** — healthy infra but blank/broken/error page, or a failing check; iterate from the check's `detail` + render evidence.
- **VERDICT: UNCERTAIN** — no render data + URL unreachable; fall back to `zerops_verify`.
