# 01 — Phantom Parameters: MCP Host Descriptions vs. Skutečné Schema

> **RESOLVED** (2026-03-02): Removed phantom `mode` parameter from documentation (~15 occurrences) and MEMORY.md ("4 Modes" line). Deleted `docs/` directory (bootstrap-flow.md, knowledge-flow.md) and `design/` directory (zcp-prd.md, flow-orchestration.md, context-first-delivery.md, workflow-gaps.md) — stale narrative docs were the primary source of phantom parameters. Updated CLAUDE.md and test headers to remove design doc references. The actual tool schemas in Go code (`WorkflowInput`, `DeployInput`, `ScaleInput`) were already correct.

## Problém

MCP host descriptions (to co LLM vidí jako tool descriptions) obsahují parametry, které v kódu neexistují. LLM je posílá, JSON schema je tiše ignoruje, žádný warning ani error.

## Konkrétní phantom parametry

### `mode` v zerops_workflow
- **MCP host description**: `"mode": {"description": "Session mode for start action: full (all phases), dev_only (skip deploy/verify), hotfix (skip discover), or quick (no gates)."}`
- **Server instructions** (`internal/server/instructions.go:18-25`): routingInstructions říká `workflow="bootstrap" mode="full"`
- **Skutečný kód**: `WorkflowInput` struct v `internal/tools/workflow.go` nemá žádné `Mode` pole
- **`handleStart()`** v `workflow.go:133` čte `input.Intent` ale nikdy `input.Mode`
- **`engine.Start()` a `engine.BootstrapStart()`** nemají mode parametr
- **MEMORY.md** dokumentuje "4 Modes: full, dev_only, hotfix, quick" — to je taky špatně, modes neexistují v kódu
- **Dopad**: LLM si myslí že `mode="quick"` přeskočí gates. Nepřeskočí. LLM si myslí že `mode="dev_only"` přeskočí deploy/verify. Nepřeskočí. Vše vždy běží jako "full".

### `freshGit` v zerops_deploy
- **MCP host description**: `"freshGit": {"description": "Remove existing .git and reinitialize before push..."}`
- **Skutečný kód**: `DeployInput` v `internal/tools/deploy.go` nemá `FreshGit` pole
- **Co se děje místo toho**: `buildSSHCommand()` v `internal/ops/deploy.go:183` vždy auto-detekuje: `test -d .git || (git init...)`
- **Dopad**: LLM posílá `freshGit=true` — nic se neděje, chování je vždy auto-detect

### `setup` v zerops_deploy
- **MCP host description**: `"setup": {"description": "SSH mode only: custom shell command to run before push..."}`
- **Skutečný kód**: `DeployInput` nemá `Setup` pole
- **Dopad**: LLM posílá `setup="npm install"` — příkaz se nikdy nespustí

### `startContainers` v zerops_scale
- **MCP host description**: `"startContainers": {"description": "Initial number of containers on service start."}`
- **Skutečný kód**: `ScaleInput` má jen `MinContainers` a `MaxContainers`
- **Dopad**: LLM posílá `startContainers=2` — ignorováno

### "local zcli push" v zerops_deploy
- **MCP host description**: `"Deploy code to a Zerops service via SSH (cross-service) or local zcli push"`
- **Skutečný tool description** v `tools/deploy.go:34`: `"Deploy code to a Zerops service via SSH"` (jen SSH)
- **Kód**: `ops/deploy.go:67-73` vrací `ErrNotImplemented` pokud sshDeployer je nil, žádná local deploy cesta ve v2
- **Dopad**: LLM se může pokusit o local deploy (omit sourceService) — ve v2 to nefunguje

## Proč je to fundamentální

1. **Tiché selhání** — žádný error, žádný warning. LLM si myslí že parametr funguje.
2. **Systémová desynchronizace** — MCP host descriptions byly pravděpodobně psány pro main branch nebo plánované API, ne pro v2 skutečnost.
3. **Nedetekovatelné** — LLM nemá způsob jak zjistit že parametr nemá efekt. Chování vypadá "normálně" protože default fallback funguje.

## Kde se podívat dál (neprozkoumáno)

- **Odkud se MCP host descriptions generují?** — Jsou v nějakém config souboru, nebo je LLM host (Claude Desktop, Cursor) generuje z JSON schema? Pokud jsou v config souboru, je to prostý file edit. Pokud se generují z JSON schema, pak schema je zdrojem pravdy a popisy by se měly synchronizovat.
- **Jsou phantom parametry záměrné placeholdery?** — Možná existuje plan implementovat `mode` v budoucnu. Pokud ano, descriptions předbíhají implementaci. Pokud ne, jsou to relikty z jiné verze.
- **Jak se chová MCP protokol s extra fields?** — JSON schema validace v MCP: strict vs. lenient. Pokud MCP klient dělá strict validaci, phantom parametry by způsobily client-side error (lepší). Pokud lenient (pravděpodobné), tiše se zahodí (horší).
- **`internal/server/instructions.go` dynamic project summary** — řádky 109-121 mění instrukce podle project state. Obsahují tyto phantom parametry v dynamickém textu? Zkontrolovat celý soubor.
- **MEMORY.md zmínka o 4 modes** — je memory stale? Existoval mode v dřívější verzi kódu? Git history `workflow.go` by ukázala jestli mode field existoval a byl odstraněn.

## Relevantní soubory

```
internal/tools/workflow.go      — WorkflowInput struct, handleStart()
internal/tools/deploy.go        — DeployInput struct
internal/tools/scale.go         — ScaleInput struct (ověřit)
internal/ops/deploy.go          — buildSSHCommand(), auto-detect git
internal/ops/workflow.go        — engine interface
internal/workflow/engine.go     — Start(), BootstrapStart()
internal/server/instructions.go — routingInstructions, project summary builder
```
