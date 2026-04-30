---
id: bootstrap-classic-node-standard
description: Standard mode (dev+stage pair) via classic route — agent constructs the plan manually (no recipe template) for Node.js + PostgreSQL. Plní gap "today's standard-mode coverage rides on weather-dashboard-* which all use recipes" — exercises classic-route plan construction + cross-deploy + auto-close. Catches H1 (Plan cross-deploy sourceService), H2 (setup="prod" on promote-stage atom), R3 (stageHostname requirement loud-and-clear in bootstrap-mode-prompt) regressions in the manual-plan path.
seed: empty
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_import
    - zerops_deploy
    - zerops_verify
  # Greenfield classic standard-pair = 7-9 workflow calls (start
  # discovery + start route=classic + complete×2-3 + start workflow=develop
  # + close). Calibrated post-first-run.
  workflowCallsMin: 7
  mustEnterWorkflow:
    - bootstrap
    - develop
  requiredPatterns:
    - '"workflow":"bootstrap"'
    # Classic route je celý test — user explicitně odmítá recipe.
    - '"route":"classic"'
    # Standard mode pin — agent musí v plánu poslat bootstrapMode standard
    # + explicit stageHostname (R3 requirement).
    - '"bootstrapMode":"standard"'
    - '"stageHostname":"appstage"'
    # Cross-deploy assertion — H1 fix musí dorazit s sourceService argumentem.
    - '"sourceService":"appdev"'
    - '"targetService":"appstage"'
  forbiddenPatterns:
    # Recipe route by ignoroval explicitní instrukci.
    - '"route":"recipe"'
    # Self-deploy stage (bez sourceService) by porušil DM-2 — H1 fix
    # zajišťuje že Plan emit sourceService.
    - '"targetService":"appstage","setup":"prod"}'
  requireAssessment: true
  finalUrlStatus: 200
  # Standard pair → ResolveProbeHostname auto-pick stage half (po R1+
  # heuristice) ale explicitní pin pro robustnost.
  finalUrlHostname: appstage
followUp:
  - "Submitil jsi plan se `stageHostname: \"appstage\"` explicitně? Co se stane když ho v standard mode vynecháš — viděl jsi tu chybovou hlášku?"
  - "Cross-deploy z appdev na appstage — jakými argumenty (`sourceService`, `targetService`, `setup`)? Proč je to cross-deploy a ne self-deploy stage?"
  - "Auto-close po deploy+verify obou halves — fungoval, nebo jsi musel volat `action=\"close\"` ručně? Co je v atom guidance ke close-mode?"
---

# Úkol

Potřebuju Node.js 22 service na Zeropsu se standard mode (dev + stage
pair) a managed PostgreSQL databází.

**Důležité**: nepoužívej žádný recipe ani template. Chci to naplánovat
ručně — ať mám plnou kontrolu nad hostnames, závislostmi a config blocky.

Aplikace:

- REST API endpoint `GET /todos` vrací JSON pole položek z DB (i prázdné).
- `POST /todos` vytvoří novou položku (body `{"title": "..."}`).
- `DELETE /todos/:id` smaže.
- Tabulka se vytvoří při startu (`CREATE TABLE IF NOT EXISTS`), žádné
  migration tooling.
- Express / Fastify / Hono — co preferuješ.
- Hostname konvence: `appdev` (dev half) + `appstage` (stage half) + `db`
  (postgres). Standard mode plan musí obsahovat **explicitní
  `stageHostname: "appstage"`** — ne hostname-suffix derivation.

Verify chain:

1. `GET /todos` na `appdev` (dev) vrátí 200 + valid JSON array.
2. Cross-deploy dev → stage (`zerops_deploy sourceService=appdev
   targetService=appstage setup=prod`).
3. `GET /todos` na `appstage` (production) vrátí 200 + stejný shape.
