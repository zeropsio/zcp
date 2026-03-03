# 07 — Meta-Pattern: Rules Enforced by Text Instead of Code

## Problém

Systém definuje pravidla v textových instrukcích (tool descriptions, workflow content, knowledge docs) a spoléhá že LLM je bude dodržovat. Když nedodržuje, přidá se další instrukce ("MUST", "NEVER", "ALWAYS") místo strukturálního enforcement v kódu.

## Katalog instrukčních pravidel bez kódového enforcement

### "NEVER call zerops_import directly"
- **Kde**: Server instructions (`instructions.go:15-25`)
- **Co brání**: `requireWorkflow()` guard v `tools/import.go:29` a `tools/deploy.go:40`
- **Ale**: Guard je soft — `guard.go:12-14`: `if engine == nil { return nil }`. Když engine je nil (getwd failure), guard se přeskočí.
- **A**: Jen import a deploy jsou guarded. `zerops_manage`, `zerops_scale`, `zerops_env`, `zerops_delete`, `zerops_subdomain`, `zerops_mount` nemají guard vůbec.
- **A**: Guard checkuje "existuje session?" ale ne "je správná fáze?". Import v VERIFY fázi projde.

### "you MUST reload after env set"
- **Kde**: Tool description `tools/env.go:23`
- **Enforcement**: `nextActions` hint v response. Žádný kód.
- **Alternativy**: Auto-reload po env set (s opt-out), `reloadRequired: true` field v response, workflow step enforcement.

### "ALWAYS create dev/stage pairs"
- **Kde**: `core.md:290`
- **Enforcement**: Žádný. Recipes učí opak (viz #03). Import tool nevaliduje.
- **Workflow step `generate-import`** by měl instruovat LLM, ale je to instrukce (markdown), ne validace.

### "You MUST have explicit user approval to delete"
- **Kde**: Tool description `tools/delete.go:22-23`
- **Structural gate**: `confirm=true` boolean
- **Problém**: LLM si `confirm=true` nastaví samo. Neexistuje user-interaction gate.
- **Alternativa**: Confirmation token z user interakce, MCP `sampling` pro user approval.

### "REQUIRES active workflow session"
- **Kde**: Tool descriptions pro import a deploy
- **Enforcement**: `requireWorkflow()` — ale viz výše, bypass při nil engine.

### "load knowledge BEFORE generating YAML"
- **Kde**: Bootstrap step ordering (step 3 = load-knowledge, step 4 = generate-import)
- **Enforcement**: Step ordering je sekvenční (funguje). Ale attestace je honor system — LLM může "complete" step bez skutečného volání `zerops_knowledge`.
- **KnowledgeTracker**: `ops/knowledge_tracker.go` trackuje jestli briefing+scope byly loaded. Ale nic s tím nedělá — je to info pro bootstrap conductor hint, ne blocker.

### Annotation hints (idempotent, destructive) jsou per-tool, ne per-action
- `zerops_manage`: `IdempotentHint: true` a `DestructiveHint: true` — ale `stop` je destructive, `start` ne, `restart` je destructive
- `zerops_subdomain`: `DestructiveHint: false` — ale `disable` JE destructive (kills live traffic)
- MCP protocol limitace: annotations jsou per-tool, ne per-action. Ale systém by mohl mít per-action descriptions.

## Pattern: Kde se problém "fixuje na konci"

| Krok | Co se děje |
|------|-----------|
| 1. Recipe naučí špatný pattern | single-service, no minRam |
| 2. LLM vygeneruje import YAML z recipe | špatný YAML |
| 3. Bootstrap step `generate-import` instruuje opravu | instrukce v markdown |
| 4. Pokud LLM neopraví, import projde | API může odmítnout, nebo přijmout špatný config |
| 5. Projekt je NON_CONFORMANT | server instructions řeknou "use bootstrap" |
| 6. Cycle se opakuje | |

Správný přístup: recipe od začátku učí správný pattern. Validace v import tool odmítne špatný pattern. Error message vysvětlí pravidla.

## Spektrum enforcement

Od nejslabšího po nejsilnější:

1. **Text v knowledge docs** — LLM musí přečíst a zapamatovat (nejslabší)
2. **Text v tool description** — LLM vidí při každém tool callu
3. **Text v response (nextActions)** — LLM vidí po tool callu
4. **Workflow step ordering** — kód vynucuje sekvenci
5. **Guard check (soft)** — kód blokuje ale má bypass cesty
6. **Input validation v ops** — kód odmítne špatný input s jasným errorem
7. **JSON schema enum/constraint** — MCP klient odmítne před odesláním (nejsilnější)

Většina pravidel v ZCP je na úrovni 1-3. Jen málo dosahuje úrovně 5-7.

## Kde se podívat dál (neprozkoumáno)

- **MCP protocol capabilities** — podporuje MCP `sampling` (user-in-the-loop) pro destructive operations? Mohlo by `zerops_delete` vyžadovat MCP sampling confirmation?
- **JSON schema constraints** — kolik tool parametrů má `enum` constraints vs. free text? Např. `action` v env/manage/subdomain — mají enum?
- **`internal/tools/*.go` schema definitions** — projít všechny `InputSchema` definice. Kde by enum/pattern/min/max pomohl?
- **Composite tool pattern** — mohl by `zerops_env action="set"` automaticky vrátit `reloadRequired: true` a MCP klient by mohl auto-trigger reload? Nebo je to mimo scope MCP?
- **Per-action tool splitting** — místo jednoho `zerops_manage` s 6 akcemi, mít `zerops_start`, `zerops_stop`, `zerops_restart` s individuálními annotations. Trade-off: víc tools vs. přesnější metadata.
- **Workflow phase-aware guards** — `requireWorkflow()` kontroluje existenci session. Mohl by kontrolovat i fázi? Např. import jen v DEVELOP fázi, deploy jen v DEPLOY fázi?
- **Reálné LLM compliance** — jak často LLM skutečně dodržuje textové instrukce? Závisí na modelu (Claude vs GPT vs Gemini), context length, instruction position. Testovat na reálných scénářích.

## Relevantní soubory

```
internal/tools/guard.go         — requireWorkflow(), nil engine bypass
internal/tools/env.go           — "MUST reload" instruction, "get" action surface patch
internal/tools/delete.go        — "MUST have user approval" instruction
internal/tools/manage.go        — per-tool annotations pro multi-action tool
internal/tools/import.go        — workflow guard
internal/tools/deploy.go        — workflow guard
internal/tools/subdomain.go     — destructive hint mismatch
internal/server/instructions.go — "NEVER call directly" instruction
internal/ops/knowledge_tracker.go — tracks but doesn't enforce
internal/knowledge/themes/core.md — "ALWAYS" rules
internal/content/workflows/bootstrap.md — step instructions
```
