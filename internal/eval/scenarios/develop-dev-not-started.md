---
id: develop-dev-not-started
description: Seeded runtime sits at READY_TO_DEPLOY without ZCP meta — LLM must go through bootstrap discovery → adopt, then either deploy code or re-import with startWithoutCode
seed: imported
fixture: fixtures/laravel-minimal.yaml
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_discover
  workflowCallsMin: 4
  # Develop workflow is not required — the scenario explicitly allows the
  # re-import-with-startWithoutCode recovery path, which stays inside
  # bootstrap. Only bootstrap entry is load-bearing here.
  mustEnterWorkflow:
    - bootstrap
  requiredPatterns:
    - '"workflow":"bootstrap"'
    - '"route":"adopt"'
  requireAssessment: true
  # ACTIVE check only — L7 routing can 502 for 30-60s after first enable
  # on dev services; we check that the probe can reach the domain and
  # the service itself is healthy via zerops_verify. Dropping the
  # finalUrlStatus assertion because it flakes on propagation delay.
  finalUrlHostname: appdev
followUp:
  - "V jakém stavu byla služba `appdev` při startu? Jak jsi to zjistil z discovery response?"
  - "Jakou recovery cestu jsi zvolil (deploy existujícího kódu / re-import s startWithoutCode)? Proč?"
  - "Proč SSHFS mount a SSH nefunguje, dokud služba není ACTIVE?"
---

# Úkol

V projektu je naimportovaná jediná Laravel služba `appdev` — vytvořená
mimo ZCP (žádná metadata). Služba **nikdy nebyla deployed** a sedí ve
stavu READY_TO_DEPLOY (import ji vytvořil bez `startWithoutCode: true`).

Tvým úkolem je:

1. **Adoptovat** existující služby do ZCP přes bootstrap discovery:
   - První `zerops_workflow action="start" workflow="bootstrap"` bez
     route — discovery response musí ukazovat adopt option s
     `adoptServices: ["appdev"]`.
   - Commit s `route="adopt"`.
2. Dostat `appdev` do stavu ACTIVE. Můžeš to udělat jakkoli validně:
   - **Nasadit reálný kód** (první deploy z READY_TO_DEPLOY → BUILDING → ACTIVE).
   - **Re-importovat službu** s `startWithoutCode: true` (platforma sama
     vystartuje prázdný kontejner, služba přejde do ACTIVE).

Pravidla:

- Začni `zerops_workflow action="status"` — nezačínej naslepo.
- Bootstrap discovery je **dvoukrokový**: první call bez route = discovery
  response, druhý call s `route=adopt` = commit.
- Po adopci pokračuj v develop flow.
- Na konci ověř, že `appdev` je ACTIVE.

Verify: `zerops_discover service="appdev"` vrací status ACTIVE a HTTP
subdomain endpoint odpovídá (200 nebo 404 stačí — hlavní je, že server
běží).
