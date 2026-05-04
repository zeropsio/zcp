---
id: verify-subdomain-recovery-before-browser
description: |
  Existing static appdev service has subdomain access disabled. The agent
  must adopt the runtime, run verify first, follow the structured Recovery
  hint to enable the subdomain, and re-verify instead of redeploying or
  opening the URL first.
seed: deployed
fixture: fixtures/standard-pair-no-subdomain.yaml
tags: [adopt, verify, recovery, subdomain, static-runtime, no-redeploy]
area: recovery
retrospective:
  promptStyle: briefing-future-agent
notableFriction:
  - id: verify-before-browser
    description: |
      The scenario tests whether the agent uses zerops_verify as the first
      diagnostic surface for a public URL problem instead of guessing from
      browser/proxy symptoms.
  - id: recovery-field-followed
    description: |
      The http_root check should carry Recovery pointing at subdomain
      enablement. The agent should execute that precise recovery then
      verify again.
  - id: no-wasted-redeploy
    description: |
      Subdomain access is an infrastructure precondition, not an app build
      problem. A self-deploy would be wasted motion.
---

`appdev` should already be a static site, but its public URL is not reachable for me. Please fix whatever is blocking the public URL and verify the page before you stop.
