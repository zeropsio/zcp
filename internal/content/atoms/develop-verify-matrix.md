---
id: develop-verify-matrix
priority: 4
phases: [develop-active]
title: "Verify matrix"
---

### Per-service verify matrix

Deploy success does not prove user behavior. Use `zerops_discover`:
subdomain URL means web-facing; managed/no HTTP port means non-web.

Run `zerops_verify` first. If any returned check has a `recovery` field,
execute that recovery (`tool` + `action` + `args`) and re-run verify before
any browser/HTTP probe.

| Service shape | Required check |
|---|---|
| Non-web: managed DB/cache/worker/no HTTP port | Run `zerops_verify serviceHostname="{targetHostname}"`. `status=healthy` is enough; nothing to browse. |
| Web-facing: dynamic/static/implicit-webserver with subdomain/port | Run `zerops_verify` for infrastructure, then a verify agent using `agent-browser`. Tool healthy + rendered page proves the service; either failure blocks. |

Fetch the web-agent protocol only when needed:

```
zerops_knowledge query="verify web agent protocol"
```

It has the `Agent(model="sonnet", prompt=...)` template; substitute
`{targetHostname}` and `{runtime}`.

### Verdict protocol

- **VERDICT: PASS** → service verified, proceed.
- **VERDICT: FAIL** → visual/functional issue; iterate from the agent's
  evidence.
- **VERDICT: UNCERTAIN** → fall back to `zerops_verify`; the agent could
  not determine the outcome.
- **Malformed output or timeout** → UNCERTAIN; fall back to `zerops_verify`.
