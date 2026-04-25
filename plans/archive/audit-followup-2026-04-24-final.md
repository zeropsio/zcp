# Audit followup — 4-layer architecture + topology extraction + doc hygiene

**Date**: 2026-04-24 (initial), 2026-04-25 (rewrite po multi-agent deep dive + dry-run validation)
**Source**: ultra-deep audit zcp repozitáře (18 + 6 + 3 + 4 paralelních agentů, viz git log + konverzace).
**Status**: **FINAL** (post dry-run validation 2026-04-25, plán viability 96-98%)
**Filter**: bez recipe-related věcí (primárně Alešova doména).
**Scope**: 2 fáze (B + C) s celkem 11+ sub-tasky. Phase A (security) **zrušena 2026-04-25** — threat model marginal pro pre-production tool.

> **Validační status**: 4 dry-run agenti (B.0, B.1-B.3, B.4-B.6, Phase C + container) potvrdili 96-98% plan viability. 0 show-stoppers, 9 minor refinements integrated do tohoto finální verze (viz **"Reality check"** boxy v každé phase). Container SSH access ověřen funkční (`ssh zcp` returns Linux 6.18.7 x86_64).

## Status

| Phase | Sub-phases | Priorita | Status |
|-------|-----------|----------|--------|
| Phase B — 4-layer architecture | B.0 (foundation), B.1–B.6 (cleanup + migrations) | HIGH (foundational refactor) | pending |
| Phase C — Doc/plans hygiene | C.I1, C.I2, C.I3, C.I4 | LOW | pending |

> **Phase A — Security (auto-update SHA-256 verification) byla zrušena 2026-04-25.** Threat model je marginal pro pre-production tool: TLS k GitHub releases už integrity řeší; `ZCP_UPDATE_URL` env override vyžaduje attacker se shellem na victim's stroji. Vrátit se před public 1.0 jako součást release-hardening.

## Validation summary (2026-04-25 dry-run pass)

4 agenti ověřili plán **read-only** proti reálnému kódu. **Plan viability: 96-98%**, 0 show-stoppers, 9 minor refinements integrated:

| # | Finding | Phase | Severity | Action |
|---|---------|-------|----------|--------|
| 1 | `gopls` install location PATH issue | B.0.2 | Resolved | gopls v0.21.1 IS installed at `~/go/bin/gopls` (validation agent missed because PATH did not include `~/go/bin`); plus `gofmt -r` + `goimports` (just installed) for AST-aware refactor — better than sed |
| 2 | File count 113 (ne 154) | B.0.2 | Info | Plan was conservative — actual scope smaller |
| 3 | `.golangci.yml` nemá depguard | B.0.3 | Low | Additive add, no conflict |
| 4 | `init_container.go` + `zerops_errors.go` ALREADY FIXED | B.5 | Good | B.5 scope shrunk, only logfetcher remains |
| 5 | `deployStrategyGitPush` má 13 references (ne 1) | B.2 | Medium | Wider scope — per-site sweep |
| 6 | Hostname loops 2 (ne 3) | B.3 | Low | Minor delta |
| 7 | B.3d subdomain helper signature nejasný | B.3 | Low | Specified: `ExtractSubdomainURL(ctx, client, serviceID, rawEnvs)` |
| 8 | F1 `RoleFromMode` symbol nenalezený | C.I4 | Low | Pre-archive verify step added |
| 9 | `TestE2E_Discover` neexistuje | B.0.2, B.6 | Low | Use `TestE2E_Bootstrap_StandardJavaMultiDep` |

**Critical gates pass**:
- ✅ SSH na `zcp` container works (Linux 6.18.7 x86_64)
- ✅ Makefile `linux-amd` target verified
- ✅ `eval/scripts/build-deploy.sh` operational
- ✅ Current build + test state green (baseline)
- ✅ 0 methods on moved types (no circular import risk)
- ✅ All struct fields with moved types have JSON tags (serialization safe)
- ✅ `internal/topology/` collision-free
- ✅ 18 frozen recipe files exact match (mechanical updates, no freeze violation)

## Out of scope (recipe-related)

Vše níže je explicitně **mimo** tento plán:
- `internal/recipe/` (v3 engine), `internal/workflow/recipe_*.go` (frozen v2 cluster — výjimka: B.0.2 dělá mechanical import-path update v 18 frozen souborech, **NE behavior change** → nenarušuje freeze)
- `cmd/zcp/dry_run_recipe.go`, `internal/eval/recipe_create.go`
- `internal/analyze/`, `tools/lint/recipe_atom_lint.go`, `tools/lint/atom_template_vars/`
- `docs/recipes/`, `docs/zcprecipator2/`, `docs/zcprecipator3/`, `docs/zrecipator-archive/`
- `internal/content/atoms/`, `internal/content/workflows/recipe/`
- recipe-affinity logic v `internal/knowledge/`, atom-authoring-contract*.md plans, calibration scripts
- recipe-creation eval scenarios, v2 vocabulary leak

## Pre-conditions (already done in earlier sprints)

- `integration.test` + `integration/CLAUDE.md` smazány z gitu, gitignored
- `internal/server/.zcp/` test leak fix
- `internal/init/init_nginx.go` 0777 → 0755 + chown
- `docs/spec-content-surfaces.md` "six" → "seven"
- `internal/workflow/bootstrap_session.go` zombie smazán; envelope.go doc updated
- `internal/ops/git_status.go` orphan smazán
- 9 dead error codes z `internal/platform/errors.go` smazáno
- CLAUDE.md přepsán z 228 → 95 řádků (invariants-only)
- CLAUDE.local.md má sekci "Problem-Solving Discipline"

---

# Phase B — 4-layer architecture + topology extraction

## Diagnostika (multi-agent deep dive 2026-04-25)

Auditní pass identifikoval **3 propojené root causes** za zdánlivě different symptomy:

### Root #1 — `internal/workflow/` je god-package

Drží **dual responsibility**:
- **Engine + state** (Engine, WorkflowState, StateEnvelope, sessions, atoms, briefing, route logic) — *legitimate*
- **Sdílené typy** (Mode, DeployStrategy, RuntimeClass, PushGitTrigger, PlanMode*, DeployRole*) — *patří do nižší vrstvy*
- **Predikáty** (IsManagedService, ServiceSupportsMode, IsRuntimeType, IsUtilityType) — *patří do nižší vrstvy*

**Důsledek**: `ops/discover.go:9` a `ops/deploy_validate.go:11` legitimately potřebují predikáty/typy → musí importovat `workflow/` → **opačné vrstvení proti CLAUDE.md** (`tools → ops → platform`).

**Konceptuální vhled**: Tyto typy nejsou "domain types" v DDD smyslu, ani "service types" obecně. Jsou to **ZCP-specifický slovník**, kterým ZCP popisuje Zerops platformu pro LLM a uživatele. Zerops API nezná koncept "Mode=dev/stage" — to je čistě **ZCP organizační princip**. `RuntimeClass` je ZCP klasifikace nad service-stack-type-name. `IsManagedService` je ZCP interpretace.

### Root #2 — Tool-boundary string-decay

Workflow má typed enums. Tools přijímají `string` z MCP a porovnávají přes cast:
```go
string(workflow.TriggerWebhook) == input.Trigger   // workflow_strategy.go:63
switch mode { case "dev", "standard" }              // briefing.go:235
deployStrategyGitPush = "git-push"                  // deploy_ssh.go:18 (local!)
```
**Důsledek**: Type system zbytečný na hranici. Typo v inputu projde silently.

### Root #3 — Helpery jsou organický patchwork

12 inline duplicit napříč boundary:
- `findServiceByHostname` private v ops/helpers.go (3 inline duplicates v tools/eval/dev_server)
- `joinServiceNames` v workflow/adopt_local.go re-implementuje `listHostnames` (3 sites)
- `resolveSubdomainURL` v verify_checks.go ≈ `resolveSubdomainURLForProbe` v eval/probe.go (near-identical 25-line funkce)
- `extractSubdomainURL` (discover) vs `ExtractDomainFromEnv` (subdomain) — same logic, different sig

## CLAUDE.md říká 3-vrstvy, reálná architektura je 4-vrstvá

Současné pravidlo: `tools → ops → platform`. Implicitně neúplné — `workflow/` v něm chybí, sdílený slovník chybí.

### Skutečná 4-vrstvá architektura ZCP

```
┌─────────────────────────────────────────────────────────────┐
│  Layer 4 — ENTRY POINTS                                     │
│  cmd/zcp/, internal/server/, internal/tools/                │
│  - MCP handler boundary, CLI entrypoints                    │
│  - Konvertuje vstupní stringy → typed (z layer 2)           │
└──────────────────────────┬──────────────────────────────────┘
                           ↓
┌─────────────────────────────────────────────────────────────┐
│  Layer 3 — ORCHESTRATION + OPERATIONS (peer layers)         │
│  internal/workflow/  ←/→  internal/ops/                     │
│  - workflow: engine, sessions, atoms, briefing              │
│  - ops: discrete platform operations                        │
│  - PEERS: nesmí se importovat navzájem                      │
│  - Sdílený slovník přes vrstvu níže                         │
└────────────┬────────────────────────────┬───────────────────┘
             ↓                            ↓
        ┌────────────────────────────────────┐
        │  Layer 2 — ZCP TOPOLOGY VOCABULARY │
        │  internal/topology/ (NEW)          │
        │  - Mode, Strategy, RuntimeClass    │
        │  - Predikáty: IsManagedService...  │
        │  - Aliases: PlanMode*, DeployRole* │
        │  - ZERO non-stdlib imports         │
        └────────────┬───────────────────────┘
                     ↓
┌─────────────────────────────────────────────────────────────┐
│  Layer 1 — RAW PLATFORM API                                 │
│  internal/platform/                                         │
│  - Zerops API client, ServiceStack, EnvVar, Process         │
│  - Žádné ZCP-specific koncepty                              │
└─────────────────────────────────────────────────────────────┘
```

**Cross-cutting packages** (peer-stejné-úrovně, ne strict layered):
- `auth/` — pre-engine startup (peer to ops, talks to platform)
- `runtime/` — container detection (utility)
- `knowledge/` — atom corpus (peer to workflow)
- `content/` — atom storage backend
- `recipe/` — v3 engine (peer to workflow, separate scope)
- `eval/` — test/dev tooling (peer to tools)
- `preprocess/`, `schema/`, `catalog/`, `sync/`, `init/`, `update/` — utility/cross-cutting
- `service/` — exec wrappers nginx/vscode (utility, **name collision noted** — proto nový pkg je `topology/`)

### Layering invarianty (pinned testem v B.0.3)

| Pravidlo | Důvod |
|----------|-------|
| `topology/` má jen stdlib imports | Foundational layer |
| `ops/` neimportuje `workflow/`, `tools/`, `recipe/` | Peer/upper |
| `workflow/` neimportuje `ops/`, `tools/`, `recipe/` | Peer/upper |
| `platform/` neimportuje žádný internal/ kromě stdlib | Bottom of stack |
| Cross-cutting packages mohou importovat foundational + peer-stejné-úrovně, ne výš | Strict ze své pozice |

### Proč `topology/` jako jméno

1. **Soubor `workflow/topology.go` už existuje** s touto rolí — má aliases (`PlanMode*`, `DeployRole*`) + dokumentaci o "třech slovnících". Promote-ujeme existující koncept na package.
2. **Reads natural**: `topology.Mode`, `topology.RuntimeClass`, `topology.IsManagedService`. Žádný technicismus.
3. **Reflektuje co to JE**: ZCP modeluje deploy topologii (dev/stage/manual/push-git). Slovník topologie.
4. **Nestane se catchall**: na rozdíl od `domain/` má `topology/` přirozenou hranici.
5. **Vyhýbá se kolizi**: `internal/service/` už existuje (29 LOC, exec wrappers nginx/vscode v containeru) — `topology/` collision-free.

---

## Sub-phase B.0 — Foundation: extract topology + pin layering

**Cíl**: Eliminovat ops→workflow opačné vrstvení, ustanovit explicit 4-layer model, pinnout testem aby drift se nemohl vrátit.

### B.0.1 — Architektonická artikulace (DOC ONLY, ~3 soubory, 30min)

**Co**: Aktualizovat CLAUDE.md aby odrážel reálnou 4-layer architekturu.

**Konkrétní změny**:
1. **`CLAUDE.md`** — nová sekce `## Architecture` (dnes je tam jen layering rule jako jednořádkový bullet):
   - 4-layer ASCII diagram (zjednodušený)
   - Per-layer popis
   - Dependency rule pinning
   - Cross-cutting note
2. **`docs/spec-architecture.md`** (nový) — detailnější popis pro code reviewers / nové přispěvatele:
   - Per-package mapping na vrstvy
   - Examples co je / není OK
   - Promotion rule: nový sdílený typ → topology/ first, ne workflow/
3. **CLAUDE.md** existující bullet "tools → ops → platform" → smazat / nahradit odkazem na novou sekci

**Verifikace**: review oka, žádný kód neměním. `make lint-local` clean (markdown linter pokud existuje).

**Commit**: `arch: document 4-layer architecture (topology + workflow/ops + platform)`

### B.0.2 — Topology extraction (CODE, ~154 souborů, atomic commit, ~2h)

**Co**: Vytvořit `internal/topology/` + přesunout typy + aktualizovat všechny imports.

**Symboly přesunované do `internal/topology/`**:

**Types** (~80-120 LOC):
- `Mode` + konstanty `ModeDev/Standard/Stage/Simple`
- `DeployStrategy` + konstanty `StrategyUnset/PushDev/PushGit/Manual`
- `RuntimeClass` + konstanty `RuntimeDynamic/Static/ImplicitWeb/Managed/Unknown`
- `PushGitTrigger` + konstanty `TriggerUnset/Webhook/Actions`

**Predikáty**:
- `IsManagedService(typeVersionName string) bool`
- `ServiceSupportsMode(typeVersionName string) bool`
- `IsRuntimeType(typeVersionName string) bool`
- `IsUtilityType(typeVersionName string) bool` (+ supporting maps)

**Aliases**:
- `PlanMode*` (PlanModeStandard/Dev/Stage/Simple/LocalStage/LocalOnly) — všechny jsou aliases pro Mode constants
- `DeployRole*` (DeployRoleDev/Stage/Simple) — aliases

**Co ZŮSTÁVÁ v `workflow/`** (engine concerns):
- `Phase` + konstanty (PhaseIdle, PhaseBootstrapActive, etc.) — engine lifecycle
- `IdleScenario`, `BootstrapRoute`, `DeployState` — engine concepts
- `StateEnvelope`, `ServiceSnapshot`, `ServiceMeta` — engine output struktury (jejich `Mode` field bude typu `topology.Mode`)
- `Engine`, sessions, atoms, briefing, route logic

**Implementační kroky** (před commitem, lokálně):

1. **Vytvořit nový package** (5 souborů):
   ```
   internal/topology/
   ├── doc.go           — package-level docstring (role + invariant)
   ├── types.go         — Mode, DeployStrategy, RuntimeClass, PushGitTrigger + constants
   ├── predicates.go    — IsManagedService, ServiceSupportsMode, IsRuntimeType, IsUtilityType
   ├── aliases.go       — PlanMode*, DeployRole* (move from workflow/topology.go)
   └── types_test.go    — testy moved from workflow/
   ```

2. **Smazat z workflow/**:
   - `workflow/topology.go` — celý (obsah moved)
   - `workflow/managed_types.go` — predikáty moved (verify co tam zůstává)
   - Z `workflow/envelope.go`: smazat `type Mode string`, `type DeployStrategy string`, `type RuntimeClass string`, `type PushGitTrigger string` definice + jejich konstanty (struktury jako ServiceSnapshot zůstávají, jen pole `Mode topology.Mode`)
   - Z `workflow/service_meta.go`: stejně
   - `_test.go` testy moved-typů přesunout do `topology/types_test.go`

3. **Mechanical rename napříč ~113 soubory** (validated 2026-04-25 — plan estimate 154 was conservative):
   - workflow/* (sami sebe musí update, většina pole/konstrukcí používá moved typy)
   - tools/*
   - ops/* (TADY se ELIMINUJE LAYERING VIOLATION — verified 18 imports of workflow currently)
   - eval/*
   - test files
   - 18 frozen recipe_*.go (verified count exactly matches; mechanical, ne freeze violation per CLAUDE.md "exempt until deletion" — chrání behavior, ne import paths)
   
   **Tooling (validated 2026-04-25)**:
   - `~/go/bin/gopls v0.21.1` — AST-aware identifier rename (within-package)
   - `gofmt -r` — built-in AST-aware expression rewrite (cross-package)
   - `~/go/bin/goimports` — auto-fix imports (just-installed)
   - Claude Code `LSP` tool — `findReferences`, `goToDefinition` for verification
   
   **PATH setup pro session**:
   ```bash
   export PATH="$HOME/go/bin:$PATH"
   ```
   
   **Rename strategy — `gofmt -r` + `goimports`** (AST-safe, ne raw sed):
   ```bash
   # Pro každý moved symbol — gofmt -r dělá AST-aware rewrite
   # (won't match in strings/comments na rozdíl od sed):
   
   # Types
   gofmt -r 'workflow.Mode -> topology.Mode' -w internal/ cmd/
   gofmt -r 'workflow.DeployStrategy -> topology.DeployStrategy' -w internal/ cmd/
   gofmt -r 'workflow.RuntimeClass -> topology.RuntimeClass' -w internal/ cmd/
   gofmt -r 'workflow.PushGitTrigger -> topology.PushGitTrigger' -w internal/ cmd/
   
   # Type constants (per type)
   gofmt -r 'workflow.ModeDev -> topology.ModeDev' -w internal/ cmd/
   gofmt -r 'workflow.ModeStandard -> topology.ModeStandard' -w internal/ cmd/
   gofmt -r 'workflow.ModeStage -> topology.ModeStage' -w internal/ cmd/
   gofmt -r 'workflow.ModeSimple -> topology.ModeSimple' -w internal/ cmd/
   gofmt -r 'workflow.StrategyUnset -> topology.StrategyUnset' -w internal/ cmd/
   gofmt -r 'workflow.StrategyPushDev -> topology.StrategyPushDev' -w internal/ cmd/
   gofmt -r 'workflow.StrategyPushGit -> topology.StrategyPushGit' -w internal/ cmd/
   gofmt -r 'workflow.StrategyManual -> topology.StrategyManual' -w internal/ cmd/
   gofmt -r 'workflow.RuntimeDynamic -> topology.RuntimeDynamic' -w internal/ cmd/
   gofmt -r 'workflow.RuntimeStatic -> topology.RuntimeStatic' -w internal/ cmd/
   gofmt -r 'workflow.RuntimeImplicitWeb -> topology.RuntimeImplicitWeb' -w internal/ cmd/
   gofmt -r 'workflow.RuntimeManaged -> topology.RuntimeManaged' -w internal/ cmd/
   gofmt -r 'workflow.RuntimeUnknown -> topology.RuntimeUnknown' -w internal/ cmd/
   gofmt -r 'workflow.TriggerUnset -> topology.TriggerUnset' -w internal/ cmd/
   gofmt -r 'workflow.TriggerWebhook -> topology.TriggerWebhook' -w internal/ cmd/
   gofmt -r 'workflow.TriggerActions -> topology.TriggerActions' -w internal/ cmd/
   
   # Aliases
   gofmt -r 'workflow.PlanModeStandard -> topology.PlanModeStandard' -w internal/ cmd/
   gofmt -r 'workflow.PlanModeDev -> topology.PlanModeDev' -w internal/ cmd/
   gofmt -r 'workflow.PlanModeStage -> topology.PlanModeStage' -w internal/ cmd/
   gofmt -r 'workflow.PlanModeSimple -> topology.PlanModeSimple' -w internal/ cmd/
   gofmt -r 'workflow.PlanModeLocalStage -> topology.PlanModeLocalStage' -w internal/ cmd/
   gofmt -r 'workflow.PlanModeLocalOnly -> topology.PlanModeLocalOnly' -w internal/ cmd/
   gofmt -r 'workflow.DeployRoleDev -> topology.DeployRoleDev' -w internal/ cmd/
   gofmt -r 'workflow.DeployRoleStage -> topology.DeployRoleStage' -w internal/ cmd/
   gofmt -r 'workflow.DeployRoleSimple -> topology.DeployRoleSimple' -w internal/ cmd/
   
   # Predicates (call expressions — gofmt -r needs func arg pattern)
   gofmt -r 'workflow.IsManagedService(x) -> topology.IsManagedService(x)' -w internal/ cmd/
   gofmt -r 'workflow.ServiceSupportsMode(x) -> topology.ServiceSupportsMode(x)' -w internal/ cmd/
   gofmt -r 'workflow.IsRuntimeType(x) -> topology.IsRuntimeType(x)' -w internal/ cmd/
   gofmt -r 'workflow.IsUtilityType(x) -> topology.IsUtilityType(x)' -w internal/ cmd/
   ```
   
   **Auto-fix imports**:
   ```bash
   ~/go/bin/goimports -w internal/ cmd/
   # Adds "github.com/zeropsio/zcp/internal/topology" where needed
   # Removes "github.com/zeropsio/zcp/internal/workflow" where unused (some files only used moved symbols)
   ```
   
   **Verification po rewrite**:
   ```bash
   # Negative greps (musí být všechny 0):
   grep -rn 'workflow\.\(Mode\|DeployStrategy\|RuntimeClass\|PushGitTrigger\|PlanMode\|DeployRole\|IsManagedService\|ServiceSupportsMode\|IsRuntimeType\|IsUtilityType\)' \
     --include="*.go" internal/ cmd/
   # Use Claude Code LSP tool pro spot-check verification:
   # LSP findReferences(internal/topology/types.go, line of Mode definition) → 
   #   should show all callers across the codebase
   ```

4. **Update imports**:
   ```bash
   goimports -w internal/ cmd/
   ```

5. **Build + iterate** (lokálně):
   ```bash
   go build ./... && go test ./... -short -count=1 -race && make lint-local
   ```

6. **Container verifikace** (viz "Container testing strategy" níže):
   ```bash
   ./eval/scripts/build-deploy.sh    # build linux-amd64 + SCP na zcp host
   ssh zcp "/usr/local/bin/zcp serve --help"    # smoke test
   ```

7. **Spot-check e2e** proti eval-zcp projektu:
   ```bash
   # Z lokálu nebo z containeru — VPN funguje
   # NOTE: TestE2E_Discover NEEXISTUJE per dry-run validation;
   # use TestE2E_Bootstrap_StandardJavaMultiDep místo
   go test ./e2e/... -tags e2e -run TestE2E_Bootstrap_StandardJavaMultiDep -count=1
   ```

>> **Reality check (post-validation)**:
> - All 13 moved symbols verified at correct file:line locations: `envelope.go:95-142`, `managed_types.go:35`, `recipe_service_types.go:13-48`, `topology.go:44-62`
> - **0 methods on moved types** confirmed (no circular import risk) — types jsou pure string aliases
> - All struct fields with moved types have JSON tags → serialization invariant preserved
> - `internal/topology/` collision-free
> - 18 frozen recipe_*.go files use moved symbols (count exact match s plánem)
> - Current build + test state: **green** (baseline established)
> - File count: 113 actual vs 154 estimated (plán byl conservative — actual scope smaller)
> - **Tooling resolved**: gopls v0.21.1 + gofmt -r + goimports + LSP tool (Claude Code) všechno k dispozici → AST-safe refactor, ne raw sed
> - Per Agent A, B.0 je ready to execute, plan accuracy 96%

**Commit**: `arch: extract topology package — Mode/Strategy/RuntimeClass + predicates`

### B.0.3 — Layering pin (1 commit, 2-4 nové soubory, 1h)

**Co**: Pinnout layering invariant testem aby drift se nemohl vrátit.

**Dvě komplementární strategie**:

#### A. depguard rule (lint-time, fast feedback)

**Pre-flight** (validated 2026-04-25): `.golangci.yml` **NEMÁ depguard enabled** — plánujeme additive add. Reference grep:
```bash
grep -A 30 "depguard" /Users/macbook/Documents/Zerops-MCP/zcp/.golangci.yml
# výsledek: žádný hit → přidat enable + rules
```

Aktualizovat `.golangci.yml`:
```yaml
linters-settings:
  depguard:
    rules:
      topology-foundation:
        files:
          - "**/internal/topology/**"
        deny:
          - pkg: "github.com/zeropsio/zcp/internal/workflow"
            desc: "topology is below workflow — would create reverse import"
          - pkg: "github.com/zeropsio/zcp/internal/ops"
            desc: "topology is below ops"
          - pkg: "github.com/zeropsio/zcp/internal/tools"
            desc: "topology is foundational"
          - pkg: "github.com/zeropsio/zcp/internal/recipe"
            desc: "topology is foundational"
      
      ops-no-workflow:
        files:
          - "**/internal/ops/**"
        deny:
          - pkg: "github.com/zeropsio/zcp/internal/workflow"
            desc: "ops and workflow are peer layers — share types via topology"
          - pkg: "github.com/zeropsio/zcp/internal/tools"
            desc: "ops is below tools"
      
      workflow-no-ops:
        files:
          - "**/internal/workflow/**"
        deny:
          - pkg: "github.com/zeropsio/zcp/internal/ops"
            desc: "workflow and ops are peer layers"
          - pkg: "github.com/zeropsio/zcp/internal/tools"
            desc: "workflow is below tools"
      
      platform-foundational:
        files:
          - "**/internal/platform/**"
        deny:
          - pkg: "github.com/zeropsio/zcp/internal/workflow"
          - pkg: "github.com/zeropsio/zcp/internal/ops"
          - pkg: "github.com/zeropsio/zcp/internal/tools"
          - pkg: "github.com/zeropsio/zcp/internal/topology"
            desc: "platform is below topology — raw API only"
```

#### B. AST-based test (test-time, robust)

`internal/architecture_test.go` (root-level test):
```go
// Package architecture_test pins ZCP's layering rules. Runs as part of
// `go test ./...` so violations surface in CI and local test runs.
package architecture_test

import (
    "go/build"
    "strings"
    "testing"
)

// layerRules: package → set of allowed imports.
// Empty slice means "only stdlib imports allowed".
var layerRules = map[string][]string{
    "internal/topology": {},
    "internal/platform": {},
    "internal/ops":      {"internal/platform", "internal/topology"},
    "internal/workflow": {"internal/platform", "internal/topology"},
    // tools/, server/, cmd/zcp/ can import everything below
}

func TestLayering_NoForbiddenImports(t *testing.T) {
    // Walk packages via build.Default.Import, parse imports, validate.
    // ...
}
```

Toto je výhodnější než depguard protože:
- Selže celé `go test ./...` (depguard selže jen `make lint-local`)
- Self-documenting (test = spec)
- Jeden zdroj pravdy v Go kódu, ne v YAML

**Commit**: `arch: pin layering invariant via depguard + architecture_test`

**Verifikace**: zavedeme úmyslně violation (např. `import "github.com/zeropsio/zcp/internal/workflow"` v ops/), spustíme test → fail; revert + ověřit že fail message obsahuje název pravidla.

### B.0.4 — Spec/doc sweep (1 commit, 5-10 souborů, 30min)

**Co**: Najít a aktualizovat všechny dokumenty co odkazují na starou layering rule nebo na `workflow.Mode` etc.

**Search**:
```bash
grep -rn "tools → ops → platform\|workflow\.Mode\|workflow\.IsManagedService\|workflow\.DeployStrategy\|workflow\.RuntimeClass" \
  --include="*.md" docs/ plans/ CLAUDE.md
```

**Pravděpodobné editace** (per Agent 3 audit, předběžně 0 nálezů — verifikace dry-run):
- `docs/spec-workflows.md` — pokud zmiňuje layering, update na 4-layer
- `docs/spec-knowledge-distribution.md` — totéž
- `plans/audit-followup-2026-04-24.md` — phase B aktualizovat (po-completion)

**Commit**: `docs: update specs to reflect 4-layer architecture + topology package`

### B.0 DoD

- B.0.1 → B.0.4 všechny commitnuté
- `internal/topology/` package existuje, ZERO non-stdlib imports
- `grep -rn '"github.com/zeropsio/zcp/internal/workflow"' internal/ops/` → 0 lines
- `go build ./...` clean
- `go test ./... -count=1 -race` pass (full suite)
- `make lint-local` pass (s novou depguard rule)
- `architecture_test.go` passing
- `make linux-amd` produkuje binary
- `./eval/scripts/build-deploy.sh` úspěšně nasadí na container
- `ssh zcp "/usr/local/bin/zcp serve --help"` smoke test pass
- 1 e2e spot-check pass

---

## Sub-phase B.1 — Layer cleanup beyond topology (1-2 commits, ~5-8 souborů, 1-2h)

**Co**: Opravit zbývající cross-package porušení po B.0.

Po B.0 zůstávají:
- **`tools/workflow_strategy.go:143`** — konstruuje `workflow.StateEnvelope` inline (bypass Engine API). Změnit na call přes Engine. Zachovat semantic.
- **`internal/auth/`** dělá direct `platform.Client` calls (`auth.go:60, 146, 154` — GetUserInfo, GetProject, ListProjects). **Decision**: auth je pre-engine startup, tyto calls jsou OK direct. Dokumentovat v auth/doc.go.
- **`internal/eval/`** dělá direct `client.X` calls. **Decision**: eval je test/dev tooling, peer to tools layer. **Migration is part of B.4** (pojede přes ops.LookupService etc.)

**Commit**: `arch(tools): route StateEnvelope construction through Engine API`
**Commit**: `arch(auth): document direct-client justification (pre-engine startup)`

### B.1 DoD

- `tools/workflow_strategy.go` neukončuje StateEnvelope inline
- `auth/doc.go` má sekci o pre-engine layering exception
- Build + test + lint clean

---

## Sub-phase B.2 — Tool boundary normalization (3-4 commits, ~10-12 souborů, 2-3h)

**Co**: Eliminovat tool-boundary string-decay. Každý tool entry parses string → typed enum jednou při vstupu.

**Konkrétní sites** (per `/tmp/audit-stringly-typed.md`):

1. **`tools/workflow_strategy.go:63`** — `string(workflow.TriggerWebhook) == input.Trigger` → parse input.Trigger → `topology.PushGitTrigger` na entry, comparison typed
2. **`tools/workflow.go:708`** — `string(workflow.BootstrapRouteResume)` cast → parse input.Route → typed `workflow.BootstrapRoute`
3. **`tools/workflow_adopt_local.go:132`** — `string(workflow.PlanModeLocalStage)` pro JSON output → použít json marshaler
4. **`internal/knowledge/briefing.go:235-242`** — `switch mode { case "dev", "standard", "simple" }` → typed switch on `topology.Mode`
5. **`internal/knowledge/briefing.go:26`** — `prependModeAdaptation(mode string)` → `(mode topology.Mode)`
6. **`tools/knowledge.go:35,60`** — `KnowledgeInput.Mode string` → `topology.Mode` (s JSON validation na decode)
7. **`tools/deploy_ssh.go:18`** — local `deployStrategyGitPush = "git-push"` → smazat, použít `topology.StrategyPushGit`. **Reality check (validation 2026-04-25)**: tato konstanta má **13 references** napříč tools/ ne 1, jak plán naznačoval. Per-site sweep potřebný — větší B.2 scope než původně.
8. **`tools/convert.go:31`** — `statusFinished = "FINISHED"` → centralizovat (existuje v ops/process.go nebo platform/types.go)
9. **`internal/ops/deploy_validate.go:106-107`** — `string(workflow.DeployRoleDev)` cast pro comparison → accept typed `topology.DeployRole`

**Strategy per site**: validate při entry → typed throughout pipeline.

**Phasing**:
- B.2a — workflow tooling sites (3 files)
- B.2b — knowledge briefing (2 files)
- B.2c — knowledge tools input + status constants (3 files)
- B.2d — ops deploy_validate (1 file, dependent on B.0 since uses topology.DeployRole)

### B.2 DoD

- `grep -rn "string(workflow\." internal/tools/ internal/ops/` → 0 lines (currently: 11; žádné cast-pro-comparison)
- `grep -rn "deployStrategyGitPush\|deployStrategyPushDev\|deployStrategyManual" internal/tools/` → 0 (currently: 13 references; žádné local const duplicates)
- Tool inputs typed-validated při decode
- 11 test files updated for typed enum values (+30min vs original estimate per dry-run)
- Build + test + lint clean

---

## Sub-phase B.3 — Helper consolidation (2-3 commits, ~8-10 souborů, 1-2h)

**Co**: Consolidate inline duplicates do canonical helpers.

**Konkrétní sites** (per `/tmp/audit-helpers.md`):

### B.3a — Hostname lookup (3 sites)
- Promote `ops/helpers.go::resolveServiceID` → `ops.FindService` (public)
- Migrate inline:
  - `tools/deploy_local_git.go:32-36`
  - `eval/probe.go:47-51`
  - `ops/dev_server.go:251-255`

### B.3b — Hostname listing (3 sites)
- Canonical: `ops/helpers.go::listHostnames`
- Migrate:
  - `workflow/adopt_local.go:198-199` (`joinServiceNames` re-implementation → smazat, použít `ops.ListHostnames`)
  - `ops/export.go:125-132` (ServiceHostnames method → use canonical)
  - `workflow/render.go:157-161` (renderServices → use canonical)

### B.3c — Subdomain URL resolution (2 sites)
- `ops/verify_checks.go:144-172` (resolveSubdomainURL) ≈ `eval/probe.go:109-129` (resolveSubdomainURLForProbe)
- Promote canonical → `ops.ResolveSubdomainURL`
- Smazat duplicate v eval

### B.3d — Subdomain env extraction (3 sites)
- `ops/discover.go:270-288` (extractSubdomainURL)
- `ops/subdomain.go:179-184` (inline check)
- `ops/verify_checks.go:158-171` (inline check)
- **Konsolidovat do jednoho helperu** — proposed signature (clarified post-validation):
  ```go
  // ExtractSubdomainURL returns the subdomain URL from envs, or empty string
  // when zeropsSubdomain key is absent. Optional rawEnvs avoids re-fetching
  // when caller already has them (e.g., discover.go already iterates envs).
  // Pass nil rawEnvs to fetch via client.GetServiceEnv(ctx, serviceID).
  func ExtractSubdomainURL(ctx context.Context, client platform.Client, serviceID string, rawEnvs []platform.EnvVar) string
  ```
  Single entry point covers both cached (rawEnvs != nil) and API-fetch (nil) paths.

### B.3e — "Service not found" error construction (2 sites)
- `ops/dev_server.go:257-259` (inline `*PlatformError`)
- `eval/probe.go:54` (inline `fmt.Sprintf`)
- Migrate to `ops.FindService` (returns canonical error)

### B.3 DoD

- `joinServiceNames` smazaný z workflow
- `resolveSubdomainURLForProbe` smazaný z eval
- Inline `for/if/break` na hostname lookup → 0 sites in tools/eval (currently: 2 — plán předtím říkal 3, jeden už opravený)
- Subdomain helpers konsolidované do jednoho `ExtractSubdomainURL` entry pointu
- Build + test + lint clean

---

## Sub-phase B.4 — ListServices/GetServiceEnv layer migration (3-5 commits, ~13 souborů, 3-4h)

**Co**: Eliminovat 19 sites tools/workflow/eval co volají `client.ListServices` / `client.GetServiceEnv` přímo.

**Nový API v `internal/ops/lookup.go`** (z původního Phase B):
```go
func ListProjectServices(ctx context.Context, client platform.Client, projectID string) ([]platform.ServiceStack, error)
func LookupService(ctx context.Context, client platform.Client, projectID, hostname string) (*platform.ServiceStack, error)
```

**Po B.3 už máme `ops.FindService`** — `LookupService` = `ListProjectServices` + `FindService`.

**19 sites** (původní Phase B per-site mapping zachován):
- 11 sites v tools/ (deploy_local_git, deploy_preflight, env, workflow, workflow_adopt_local, workflow_checks, workflow_develop, workflow_strategy)
- 2 sites v workflow/ (compute_envelope, adopt_local)
- 5 sites v eval/ (probe ×2, cleanup ×2, seed)
- 1 site v env.go RestartService (ponechat raw — single site, no helper added)

**Phasing**:
- B.4a — Add `ops/lookup.go` + tests (1 commit, 2 files)
- B.4b — Migrate tools/ (3 commits, 8 files split per CLAUDE.md max-5-files)
- B.4c — Migrate workflow/ (1 commit, 2 files)
- B.4d — Migrate eval/ (1 commit, 3 files)

### B.4 DoD

- `grep -rn "client\.ListServices\|client\.GetServiceEnv" --include="*.go" internal/ cmd/ | grep -v "internal/ops/\|internal/platform/\|_test.go"` → 0 lines
- 1 e2e spot-check pass

---

## Sub-phase B.5 — Errors cleanup (1 commit, 1-2 soubory, 15-20min)

**Co**: Opravit drobnosti v error handling per `/tmp/audit-errors.md`.

> **Reality check (post-validation)**: Per Agent C dry-run, tyto sites byly **ALREADY FIXED** v current code:
> - `internal/platform/zerops.go:154` — already wraps with context
> - `internal/init/init_container.go:50, 68` — already wrapped
>
> **Skutečný B.5 scope (validated)** je menší než původně plánovaný:

- `internal/platform/logfetcher.go` lines 99, 110, 140, 150, 162, 167 — přidat Suggestion fields (6 sites)
- `internal/platform/zerops_errors.go:53` + `logfetcher.go` — disambiguate `ErrAPIError` vs `ErrNetworkError` pro clearly-network errors

### B.5 DoD

- 6 logfetcher.go Suggestion fields populated
- ErrAPIError vs ErrNetworkError disambiguation per error path
- Build + test + lint clean

---

## Sub-phase B.6 — Sweep + verification (1 commit, ~1-2 soubory, 1h)

**Co**: Final sweep + comprehensive verification.

**Negative greps** (musí všechny vrátit 0):
```bash
# No reverse-layering
grep -rn '"github.com/zeropsio/zcp/internal/workflow"' internal/ops/

# No client.X bypass
grep -rn "client\.ListServices\|client\.GetServiceEnv" --include="*.go" internal/ cmd/ \
  | grep -v "internal/ops/\|internal/platform/\|_test.go"

# No string casts pro comparison
grep -rn "string(workflow\." internal/tools/ internal/ops/

# No local strategy const duplicates
grep -rn "deployStrategyGitPush\|deployStrategyPushDev" internal/tools/

# No inline hostname lookup loop
grep -rn "if services\[i\]\.Name == hostname" internal/tools/ internal/eval/
```

**Positive verification**:
- `go build ./...`
- `go test ./... -count=1 -race`
- `make lint-local`
- `make linux-amd` cross-build
- Container deploy + smoke test
- E2E spot-check (TestE2E_Discover) pass

**CLAUDE.md invariant pin** (nový bullet):
```markdown
- **4-layer architecture pinned** — `internal/topology/` is foundational vocabulary;
  `internal/ops/` and `internal/workflow/` are peer layers (no cross-import);
  `internal/platform/` is raw API. Pinned by `architecture_test.go` +
  `.golangci.yml::depguard`. Spec: `docs/spec-architecture.md`.
```

### B.6 DoD

- Všechny negative greps → 0
- Všechny positive checks → pass
- CLAUDE.md má novou invariant entry
- Phase B kompletně commitnuté

---

# Cross-cutting: Container testing strategy

> **Reality check (post-validation)**: SSH na `zcp` host **OVĚŘENO funkční** (Agent D dry-run): `ssh zcp` returns Linux kernel 6.18.7 x86_64. VPN up, container reachable. Container testing strategy unblocked.

ZCP testovací realita má **tři vrstvy**, každá s jinými constraints:

## Layer 1 — Local unit + integration tests

**Kdy**: Default pro každou změnu kódu. Běží na localu.
**Příkaz**: `go test ./... -count=1 -race -short`
**Co testuje**: 
- Unit testy v `internal/<pkg>/_test.go` — pure Go logic
- Integration tests v `integration/` — používají `platform.Mock` (žádné real API)
**Latence**: ~30s pro full suite

**Pro B.0**: Tohle pochytá většinu regressions z topology rename. Mock signatures se nezměnily, test fixtures stejné, JSON serialization invariant.

## Layer 2 — Local + cross-build verification

**Kdy**: Před commitem každé sub-phase.
**Příkazy**:
```bash
go build ./...                  # native build (darwin-arm64 typically)
make lint-local                 # full golangci-lint pass
make linux-amd                  # cross-build linux-amd64 (target Zerops container)
```
**Co testuje**: 
- Native compile correctness
- Linter rules (depguard incl.)
- Cross-platform compile (catch platform-specific bugs)

**Pro B.0**: Cross-build je důležitý — Zerops services běží na linux-amd64. Lokální build na M-series Mac chyby nemusí pochytat.

## Layer 3 — Container deployment + e2e tests

**Kdy**: Pro phases co dotýkají platform interaction (B.0 final verification, B.4 ListServices migration).

**Pattern (per `eval/scripts/build-deploy.sh`)**:
```bash
# 1. Cross-build linux-amd64
make linux-amd

# 2. Deploy to container
./eval/scripts/build-deploy.sh
# Default EVAL_REMOTE_HOST=zcp; SCPs builds/zcp-linux-amd64 → /usr/local/bin/zcp
# Used SSH opts: -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null

# 3. Smoke test on container
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null zcp \
  "/usr/local/bin/zcp serve --help"
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null zcp \
  "echo '{}' | /usr/local/bin/zcp serve"   # MCP server smoke

# 4. Run e2e tests on container (or locally with VPN)
go test ./e2e/... -tags e2e -run TestE2E_Bootstrap_StandardJavaMultiDep -count=1
```

**`zcp` host**:
- Service v eval-zcp projektu
- VPN poskytuje routing
- `/usr/local/bin/zcp` canonical install (root-owned, sudo passwordless on `zcp`)
- `~/.local/bin/zcp` symlink
- `/var/www/{hostname}/` SSHFS mounts to live services
- `zcli` authenticated, scope pinned to eval-zcp

**Pro B.0**: 
- B.0.1 (doc only) — nepotřebuje container
- B.0.2 (rename) — container deploy + smoke test po success
- B.0.3 (layering pin) — depguard se ověří `make lint-local` lokálně; architecture_test.go běží `go test`
- B.0.4 (doc sweep) — nepotřebuje container

**Pro pozdější phases (B.4 zejména)**:
- Po každé migraci ListServices, e2e spot-check (TestE2E_Discover) běží proti eval-zcp
- Verifikuje že real Zerops behavior se nezměnilo

## SSH access — ULTRA MANDATORY (CLAUDE.local.md)

```bash
# Always disable host key checking — keys rotate per deploy
SSH_OPTS="-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null"
ssh $SSH_OPTS zcp "<command>"
ssh $SSH_OPTS nodejsdev "<command>"   # přímo do služby přes mount
```

## Co se NESMÍ dělat

- **NIKDY** spouštět `zcp eval scenario` na localu (`/var/www/` paths jsou hardcoded SSHFS mounts)
- **NIKDY** používat `ssh <host>` bez SSH_OPTS (host keys rotují)
- **NIKDY** předpokládat lokální VPN bez ověření (`ssh zcp "echo ok"`)

---

## Phase B verifikační matice

| Sub-phase | Local unit/integ | Cross-build | Container deploy | E2E spot-check |
|-----------|:----------------:|:-----------:|:----------------:|:--------------:|
| B.0.1 (doc) | ✓ | — | — | — |
| B.0.2 (rename) | ✓ | ✓ | ✓ | ✓ |
| B.0.3 (pin) | ✓ | ✓ | — | — |
| B.0.4 (sweep) | ✓ | — | — | — |
| B.1 (cleanup) | ✓ | ✓ | ✓ | ✓ (1 test) |
| B.2 (boundary) | ✓ | ✓ | — | — |
| B.3 (helpers) | ✓ | ✓ | — | — |
| B.4 (migration) | ✓ | ✓ | ✓ | ✓ (full discover suite) |
| B.5 (errors) | ✓ | — | — | — |
| B.6 (sweep) | ✓ | ✓ | ✓ | ✓ |

---

# Phase C — Doc / plans hygiene (4 nálezy, beze změny od pre-final)

## C.I1 — Promote `instruction-delivery-rewrite.md` from plans to spec

**Problem**: `plans/instruction-delivery-rewrite.md` (1335 LOC, 72 KB) self-deklaruje "Status: Landed" ale je citován jako autoritativní spec ze 4 míst:
- `docs/spec-workflows.md:60`, `:260`, `:858`
- `internal/workflow/scenarios_test.go:2`

**Akce**:
1. `git mv plans/instruction-delivery-rewrite.md docs/spec-instruction-delivery.md`
2. Smazat / přepsat "Status: Landed" banner
3. Sweep 4 citace → `docs/spec-instruction-delivery.md`
4. Verify: `grep -rn "plans/instruction-delivery-rewrite" --include="*.md" --include="*.go" .` returns 0

**Verifikace**: `go test ./internal/workflow/... -run "Scenarios" -count=1` passes.

## C.I2 — Archive 3 done plans

3 plans s "Status: Implemented" — `git mv` → `plans/archive/`:
- `plans/deploy-config-central-point.md` (256 LOC)
- `plans/export-workflow.md` (166 LOC)
- `plans/api-validation-plumbing.md` (722 LOC)

Banner: `> **Archived 2026-04-24** — implementation shipped per status header. Retained for git history traceback.`

## C.I3 — friction-root-causes.md zombie

`plans/friction-root-causes.md` (969 LOC) — 5/6 workstreamů SUPERSEDED. Extract P2.2 + W0 do nového `plans/subdomain-and-eval-size.md`. P2.4 + P3 jsou recipe-adjacent (skip per scope). Original archivovat s bannerem.

## C.I4 — Archive stale audit findings + team plan

`docs/audit-bootstrap-develop-findings.md` + team-plan → `docs/archive/`.

> **Reality check (post-validation)**: Per Agent D dry-run, **F1 (`RoleFromMode`) symbol NEEXISTUJE v current code** — buď již opravené, nebo audit findings status outdated. **Pre-archive verification step**: před banner přidáním ověřit:
> ```bash
> grep -rn "RoleFromMode" --include="*.go" /Users/macbook/Documents/Zerops-MCP/zcp/
> # výsledek 0 → F1 resolved
> ```
> Pokud F1 a F3 confirmed-resolved, banner zachovat ("F1, F3 verified-resolved"). Pokud F1 ne-resolved, update banner accordingly.

## Phase C DoD

- 0 dead citations
- Build + tests stay green (jen path-string updates v Go test komentáři)
- `find plans -maxdepth 1 -name "*.md" | wc -l` se sníží z 14 na 10
- `find docs -maxdepth 1 -name "*.md" | wc -l` se sníží o 1 (-2 archive +1 nový)

---

# Doporučená sekvence

| Pořadí | Phase | Reason |
|-------:|-------|--------|
| 1 | **Phase C — Doc/plans hygiene** | Žádné kódové změny (kromě C.I1 path updates v Go test komentáři + spec-workflows.md). Trvá ~1h, low risk, immediately viditelné že se vyčistilo. |
| 2 | **Phase B.0 — Foundation** | Foundational refactor — všechny B.1–B.6 na něm staví. ~3-4h spread přes 4 sub-phases. |
| 3 | **Phase B.1–B.6 — Cleanup + migrations** | Po B.0 můžeme jet sériově. ~6-8h celkem. |

**Total estimate**: ~10-13h work, rozprostřeno do ~12-15 commitů.

---

# Definition of Done — celý plán

- Všechny phases (C, B.0–B.6) mají DoD splněný
- `git status` clean (vše commitnuto)
- `go test ./... -count=1 -race` pass
- `make lint-local` pass (s novou depguard rule)
- `architecture_test.go` pass
- `make linux-amd` produkuje binary
- `./eval/scripts/build-deploy.sh` deploys cleanly
- Container smoke test pass
- E2E spot-check pass
- CLAUDE.md má 4-layer architecture sekci + invariant pin
- `docs/spec-architecture.md` exists
- Tento plán archivován: `git mv plans/audit-followup-2026-04-24.md plans/archive/audit-followup-2026-04-24.md` s "Completed YYYY-MM-DD" bannerem
