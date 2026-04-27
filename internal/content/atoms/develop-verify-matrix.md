---
id: develop-verify-matrix
priority: 4
phases: [develop-active]
title: "Per-service verify matrix"
---

### Per-service verify matrix

Deploy success does not prove the app works for end users. Pick the
verification path per service based on what `zerops_discover` reports:
subdomain URL present means web-facing; managed or no HTTP port means
non-web.

**Non-web services (managed databases, caches, workers, no subdomain):**

```
zerops_verify serviceHostname="{targetHostname}"
```

Tool returns `status=healthy` once Zerops can reach the service.
That's the whole verification — nothing to browse.

**Web-facing services (dynamic/static/implicit-webserver with subdomain
or port):** run `zerops_verify` first for infrastructure baseline, then
spawn a verify agent that drives `agent-browser` end-to-end. A healthy
`zerops_verify` plus a rendered page together prove the service works;
either failing is enough to block.

Per web-facing target, fetch the sub-agent dispatch protocol on demand:

```
zerops_knowledge query="verify web agent protocol"
```

The protocol carries the full `Agent(model="sonnet", prompt=...)`
template — substitute `{targetHostname}` and `{runtime}` per service
when dispatching.

### Verdict protocol

- **VERDICT: PASS** → service verified, proceed.
- **VERDICT: FAIL** → agent found a visual/functional issue; enter the
  iteration loop with the agent's evidence as the diagnosis.
- **VERDICT: UNCERTAIN** → fall back to the `zerops_verify` result (the
  agent could not determine the outcome end-to-end).
- **Malformed agent output or timeout** → treat as UNCERTAIN and fall
  back to `zerops_verify`.
