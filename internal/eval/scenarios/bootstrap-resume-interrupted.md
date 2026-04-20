---
id: bootstrap-resume-interrupted
description: Interrupted bootstrap session left incomplete ServiceMeta — agent must detect via status and resume rather than restart
seed: empty
preseedScript: preseed/resume-interrupted.sh
expect:
  mustCallTools:
    - zerops_workflow
  workflowCallsMin: 4
  mustEnterWorkflow:
    - bootstrap
  requiredPatterns:
    # route=resume proves the agent saw the resume option in the discovery
    # response and picked it over adopt/classic. sessionId carries the
    # abandoned session — must match the one the preseed planted.
    - '"route":"resume"'
    - '"sessionId":"sess-abandoned-01"'
  # Classic/recipe would start a fresh session and likely collide with the
  # orphan meta. forbiddenPatterns catches the common wrong answers.
  forbiddenPatterns:
    - '"route":"classic"'
    - '"route":"recipe"'
    - '"route":"adopt"'
  requireAssessment: true
followUp:
  - "Z čeho jsi poznal, že v projektu je přerušený bootstrap? Byl v tom nějaký konkrétní signál (status response, discover output, routeOptions)?"
  - "Proč jsi vybral zrovna resume a ne adopt nebo classic? Co by se stalo, kdybys šel classic?"
  - "Které session ID jsi zvedl a kde v odpovědi discovery jsi ho našel?"
---

# Úkol

Pokračuj v té laravel appce, co jsem včera rozjel.
