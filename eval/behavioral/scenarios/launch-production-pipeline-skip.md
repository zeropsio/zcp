---
id: launch-production-pipeline-skip
description: |
  User explicitly does not want ZCP to configure or check pipeline
  setup during launch — they intend to use manual `zcli push` for
  releases (or will set up GitHub Actions themselves later, or use
  a non-Zerops CI runner). They pass skipPipelineSetup=true on the
  publish call.

  Tests whether the agent:
  - Forwards the skipPipelineSetup flag verbatim instead of stripping
    it as "unknown input".
  - Reads the resulting launched response correctly: no pipeline
    blockers, the launch-pipeline-skipped atom appears, post-checklist
    reflects the no-CD-setup path.
  - Does NOT secretly run dashboard-config guidance the user asked to
    skip.
seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [launch-production, pipeline-extension, skip-flag, manual-zcli-push]
area: launch
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You're launching production but you don't want Zerops auto-building
  on tag push — you have your own CI that runs `zcli push` to prod.
  Tell ZCP to skip pipeline setup. After launch, you expect a clean
  launched response (no pipeline blockers) and confirmation that
  ongoing deploys are your responsibility via zcli.
notableFriction:
  - id: skip-flag-passthrough
    description: |
      The agent must forward skipPipelineSetup=true on the publish
      call. Surfaces whether the agent strips unfamiliar inputs as
      "not in schema" or correctly passes them.
  - id: no-stealth-dashboard-guidance
    description: |
      When the user said skip, the launched response carries no
      pipeline-not-configured blockers and a launch-pipeline-skipped
      atom. Surfaces whether the agent respects the skip OR tries to
      "be helpful" by adding configure-dashboard guidance the user
      didn't ask for.
---

I want to launch production but skip ZCP's pipeline setup — I'll deploy via `zcli push` from my own CI. Set `skipPipelineSetup=true` on the publish call. Same one-shot launch key flow as usual.
