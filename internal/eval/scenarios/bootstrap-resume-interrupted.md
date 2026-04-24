---
id: bootstrap-resume-interrupted
description: Interrupted bootstrap session left partial state — envelope reports idleScenario=incomplete with resumable services; agent must resume rather than restart
seed: empty
preseedScript: preseed/resume-interrupted.sh
expect:
  mustCallTools:
    - zerops_workflow
  workflowCallsMin: 4
  mustEnterWorkflow:
    - bootstrap
  requiredPatterns:
    # The load-bearing signal is that the agent picked up the ABANDONED
    # session — not fresh-bootstrapped over it. NewEngine auto-claims the
    # dead-PID session before the first tool call, so the planted
    # sessionId threads through whatever path the agent chooses (status
    # response, explicit action=resume, or route=resume at commit).
    #
    # We don't require `"route":"resume"` specifically: under auto-claim
    # the agent may legitimately use `action="resume"` OR simply continue
    # from the active session seen in `status`. The session-ID match is
    # the only signal that reliably separates "agent used planted state"
    # from "agent started fresh".
    - '"sessionId":"sess-abandoned-01"'
  requireAssessment: true
followUp:
  - "Z čeho jsi poznal, že v projektu je přerušený bootstrap? Byl v tom nějaký konkrétní signál (status response, discover output, routeOptions)?"
  - "Proč jsi vybral zrovna resume a ne adopt nebo classic? Co by se stalo, kdybys šel classic?"
  - "Které session ID jsi zvedl a kde v odpovědi discovery jsi ho našel?"
---

# Úkol

Pokračuj v té laravel appce, co jsem včera rozjel.
