# ZCP Workflow System

## How it works

Bootstrap vytvoří infrastrukturu a zapíše service metas (typ, mode, stage, deps, strategie). Všechny další workflows čtou tyto metas. Knowledge (runtime guides, YAML schémata, service cards) je embedded v binárce a automaticky se injektuje do step guides.

```mermaid
flowchart TD
    FRESH(["Nový projekt"]) --> BOOT

    BOOT["BOOTSTRAP — 6 kroků\n1 discover: klasifikace + plán\n2 provision: import.yml + create services\n3 generate: zerops.yml + app kód\n4 deploy: mode-aware push\n5 verify: health check\n6 strategy: push-dev / ci-cd / manual"]
    BOOT -->|"zapíše metas"| READY(["Projekt ready"])

    READY -->|"přidej feature / fixni bug"| DEPLOY
    DEPLOY["DEPLOY — 3 kroky\n1 prepare: načti kontext, uprav kód\n2 deploy: push per mode\n3 verify: health check"] -->|done| READY

    READY -->|"nastav CI/CD"| CICD["CICD — 3 kroky\nchoose → configure → verify"]
    CICD --> READY

    READY -->|"něco je rozbitý"| DEBUG["DEBUG — stateless\nservice context + diagnostic guidance"]
    READY -->|"je to pomalý"| SCALE["SCALE — stateless"]
    READY -->|"změň config"| CONFIG["CONFIGURE — stateless"]

    DEBUG -->|"potřebuje kód fix"| DEPLOY
```

---

## Bootstrap detailně

Každý krok má **checker** — automatickou validaci proti live API. Checker běží PŘED tím než krok postoupí. Když selže, step zůstane `in_progress` a agent dostane `CheckResult.checks` s konkrétními chybami. Může opravit a zkusit `complete` znovu, nebo zavolat `iterate` (reset kroků 2-4, návrat na generate).

```mermaid
flowchart TD
    D["1 DISCOVER\nklasifikace + identifikace služeb + volba modu\nagent prezentuje plán uživateli"]
    D -->|"complete s plan=[...]"| DV{"ValidateBootstrapTargets\nhostnames, types, modes, resolutions"}
    DV -->|fail| D
    DV -->|pass| P

    P["2 PROVISION + import.yml Schema knowledge\nwrite import.yml → zerops_import\nzerops_mount → zerops_discover includeEnvs"]
    P -->|complete| PV{"checkProvision\nservices RUNNING? env vars?"}
    PV -->|fail| P
    PV -->|"pass (uloží env vars)"| G

    G["3 GENERATE + runtime guide + service cards + env vars + yml schema\nGuide filtrován podle modu: standard/dev/simple\nwrite zerops.yml + app code"]
    G -->|complete| GV{"checkGenerate\nyml valid? env refs? ports? deployFiles?"}
    GV -->|fail| G
    GV -->|pass| DEP

    DEP["4 DEPLOY + Schema Rules + env vars\nGuide filtrován podle modu\nstandard: dev→stage | dev: dev only | simple: direct"]
    DEP -->|complete| DEPV{"checkDeploy\nall RUNNING? subdomains?"}
    DEPV -->|fail| DEP
    DEPV -->|pass| V

    V["5 VERIFY\nzerops_verify all targets"]
    V -->|complete| VV{"checkVerify\nall healthy?"}
    VV -->|pass| S["6 STRATEGY\npush-dev / ci-cd / manual"]
    VV -->|fail| CHOICE{"opravit a retry\nnebo iterate?"}
    CHOICE -->|retry| V
    CHOICE -->|"iterate: reset 2-4\nescalace: diagnose→systematic→stop"| G

    S --> DONE(["Bootstrap hotový, metas zapsány"])
```

**Co každý krok dostane za knowledge:**

| Krok | Injektovaná knowledge | Zdroj |
|------|----------------------|-------|
| discover | nic (plán neexistuje) | — |
| provision | import.yml Schema + Preprocessor Functions | core.md |
| generate | runtime guide + service cards + wiring + env vars + zerops.yml Schema + Rules & Pitfalls | core.md + runtimes/*.md + services.md + session |
| deploy | Schema Rules + env vars | core.md + session |
| verify, strategy | nic | — |

**Escalace při iteraci:** 1-2 = diagnose z logů, 3-4 = systematický 6-bodový checklist, 5+ = stop a ptej se uživatele.

---

## Deploy detailně

Primární post-bootstrap workflow. Při startu načte service metas → sestaví targets (dev před stage) a ServiceContext (runtime type, dependency types) pro knowledge injection.

```mermaid
flowchart TD
    START(["start deploy"]) --> LOAD["Načti metas → targets + ServiceContext"]

    LOAD --> PREP["1 PREPARE\n+ runtime briefing + service wiring + yml schema\nzkontroluj config, uprav kód"]
    PREP -->|complete| DEP

    DEP{"2 DEPLOY podle modu"}
    DEP -->|standard| STD["deploy dev → SSH start → verify\ncross-deploy stage → verify"]
    DEP -->|dev| DONLY["deploy dev → SSH start → verify"]
    DEP -->|simple| SIMP["deploy → auto-start → verify"]
    STD --> VER
    DONLY --> VER
    SIMP --> VER

    VER["3 VERIFY — zerops_verify"]
    VER -->|healthy| FIN(["Hotovo"])
    VER -->|unhealthy| FIX["fix → retry nebo iterate"]
    FIX --> DEP
```

---

## Mody

| | Standard | Dev | Simple |
|---|---|---|---|
| Services | dev + stage + managed | dev + managed | 1 runtime + managed |
| zerops.yml start | `zsc noop --silent` | `zsc noop --silent` | real command |
| healthCheck | ne (v dev) | ne | ano |
| Server start | agent přes SSH | agent přes SSH | auto po deploy |
| Deploy | dev → stage | dev only | direct |
| Iterace | edit na SSHFS → SSH restart | stejné | edit → redeploy |

---

## Stateless workflows

Debug, scale, configure — bez session. Dostanou service context (seznam služeb s typy, mody, strategiemi) prepended k guidance. Po skončení nabídnou přechod na jiný workflow.

---

## Router

Rozhoduje podle: project state (FRESH/CONFORMANT/NON_CONFORMANT) + strategy z metas + intent keywords v user zprávě.

| Stav | Nabídne |
|------|---------|
| FRESH | bootstrap |
| CONFORMANT + push-dev | deploy |
| CONFORMANT + ci-cd | cicd + deploy |
| NON_CONFORMANT | bootstrap + deploy |

Intent boost: "broken" → debug, "deploy" → deploy, "add service" → bootstrap, "slow" → scale.

---

## Context recovery

Všechny zdroje jsou vždy dostupné: markdown content (embedded), knowledge store (embedded), session state (disk). `action="status"` sestaví identický guide. Žádný tracking state, žádný dedup.

---

## Persistence

```
.zcp/state/
  sessions/{id}.json    ← WorkflowState (Bootstrap | Deploy | CICD)
  services/{host}.json  ← ServiceMeta (přežije smazání session)
```

---

## Container vs Local

**Teď: container-only.** SSHFS mount, SSH deploy, SSH start.

**Local (Wave 4-5, neimplementováno):** zcli push, lokální soubory, real start vždy (i pro dev). Architektura připravena (Environment type), content a tooling chybí. Detaily v `plans/wave4-5-local-flow.md`.
