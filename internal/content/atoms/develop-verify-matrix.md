---
id: develop-verify-matrix
priority: 4
phases: [develop-active]
title: "Per-service verify matrix"
---

### Per-service verify matrix

Verify every service in scope after a successful deploy — never assume
deploy success means the app works for end users. Pick the verification
path per service based on what `zerops_discover` reports (subdomain URL
present = web-facing; managed or no HTTP port = non-web).

**Non-web services (managed databases, caches, workers, no subdomain):**

```
zerops_verify serviceHostname="{targetHostname}"
```

Tool returns `status=healthy` once the platform can reach the service.
That's the whole verification — nothing to browse.

**Web-facing services (dynamic/static/implicit-webserver with subdomain
or port):** run `zerops_verify` first for infrastructure baseline, then
spawn a verify agent that drives `agent-browser` end-to-end. A healthy
`zerops_verify` plus a rendered page together prove the service works;
either failing is enough to block.

Spawn the agent per web-facing target — substitute `{targetHostname}`
and `{runtime}` with that service's values:

```
Agent(model="sonnet", prompt="""
Verify Zerops service "{targetHostname}" ({runtime}) works for end users.

## Protocol
1. `zerops_verify serviceHostname="{targetHostname}"` — infrastructure baseline
2. If NOT healthy → VERDICT: FAIL (cite failed checks from zerops_verify response)
3. `zerops_discover service="{targetHostname}"` — get subdomainUrl or connection info
4. Determine reachable URL:
   - subdomainUrl available → use it (public HTTPS)
   - no subdomain, no custom domain → VERDICT: UNCERTAIN (cannot reach from outside)
   - unreachable after timeout → VERDICT: UNCERTAIN
5. `agent-browser open {url}`
6. `agent-browser snapshot` — accessibility tree for AI analysis
7. Evaluate: does the page render meaningful content?
   - Interactive elements (buttons, links, forms)?
   - Text content (headings, paragraphs)?
   - Or empty/broken (empty root div, error page, blank screen)?
8. If concerns: `agent-browser eval "JSON.stringify(Array.from(document.querySelectorAll('script[src]')).map(s=>s.src))"` for loaded scripts
9. For SPAs: `agent-browser eval "window.__errors || []"` AND check if console has errors

## Rules
- zerops_verify unhealthy/degraded → always VERDICT: FAIL (never override infra checks)
- HTTP 401/403 with rendered content (login page, auth challenge) → VERDICT: PASS (auth is working correctly)
- HTTP 401/403 with empty body → VERDICT: UNCERTAIN (cannot determine if intentional)
- zerops_verify healthy + page empty/broken → VERDICT: FAIL (cite what you see)
- zerops_verify healthy + page renders real content → VERDICT: PASS
- agent-browser unavailable or URL unreachable → VERDICT: UNCERTAIN

## Output (mandatory format)
### Infrastructure
zerops_verify status and check summary

### Application
what you observed — DOM content, JS errors, visual state

### Evidence
accessibility tree excerpt or error details

### VERDICT: PASS or FAIL or UNCERTAIN — one-line justification
""")
```

### Verdict protocol

- **VERDICT: PASS** → service verified, proceed.
- **VERDICT: FAIL** → agent found a visual/functional issue; enter the
  iteration loop with the agent's evidence as the diagnosis.
- **VERDICT: UNCERTAIN** → fall back to the `zerops_verify` result (the
  agent could not determine the outcome end-to-end).
- **Malformed agent output or timeout** → treat as UNCERTAIN and fall
  back to `zerops_verify`.
