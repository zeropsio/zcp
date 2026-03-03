# 03 — Recipes Teach Wrong Pattern (Single-Service vs. Dev/Stage)

## Problém

Všech 30 recipes v `internal/knowledge/recipes/` používá single-service pattern (`hostname: app`). Core reference (`core.md`) vyžaduje dev/stage páry. Recipes učí LLM pattern, který workflow systém pak označí jako NON_CONFORMANT.

## Konkrétní rozpor

### Co říká core.md (řádek ~290)
```
ALWAYS create dev/stage service pairs for runtime services.
```

### Co říkají ALL recipes (příklad laravel-jetstream.md)
```yaml
services:
  - hostname: app
    type: php-nginx@8.4
    enableSubdomainAccess: true
```

### Co by mělo být (podle core.md pravidel)
```yaml
services:
  - hostname: appdev
    type: php-nginx@8.4
    startWithoutCode: true
    maxContainers: 1
    enableSubdomainAccess: true
  - hostname: appstage
    type: php-nginx@8.4
    enableSubdomainAccess: true
```

## Navazující problémy

### Missing `minRam: 1.0` ve všech recipes
- Core.md pravidlo (~řádek 295): "ALWAYS set `verticalAutoscaling.minRam: 1.0` (GB) for runtime services. REASON: 0.5 GB causes OOM during compilation."
- Žádný recipe import.yml toto neobsahuje
- Dopad: OOM při prvním buildu

### Wiring templates nepokazují dev/stage hostname pattern
- `internal/knowledge/themes/services.md` wiring sekce používá sample hostnames `db`, `cache`, `app`
- Text říká "Replace sample hostnames with your actual service hostname"
- Ale pro dev/stage pattern je hostname `appdev`/`appstage`, ne `app`
- Env var wiring templates toto nedemonstrujou

### Knowledge delivery chain zesiluje problém
- LLM typicky volá `zerops_knowledge recipe="laravel-jetstream"` a dostane recipe
- Recipe je nejkonkrétnější, nejkopírovatelnější forma guidance
- Core.md pravidla jsou abstraktní ("ALWAYS do X") vs. recipe je konkrétní YAML
- LLM přirozeně preferuje kopírovat konkrétní YAML před dodržováním abstraktních pravidel

## Proč je to fundamentální

1. **Protiřečící si zdroje pravdy** — recipes a core.md si protiřečí. LLM musí rozhodnout komu věří.
2. **Recipes jsou přirozeně silnější** — LLM kopíruje copy-paste ready YAML, ne abstraktní pravidla.
3. **Problém se řeší na konci** — bootstrap workflow step `generate-import` by měl opravit YAML. Ale je to patch: recipe naučil špatně, workflow opraví. Správně by recipe měl učit správně od začátku.
4. **30 souborů k opravě** — scope je velký ale mechanický.

## Kde se podívat dál (neprozkoumáno)

- **Všech 30 recipes** — `internal/knowledge/recipes/*.md`. Identifikovat které mají runtime services (potřebují dev/stage) vs. jen managed services (databáze — nepotřebují).
- **`recipe_lint_test.go`** — existuje test validující strukturu recipes. Dá se rozšířit o kontrolu dev/stage patternu a minRam?
- **`core.md` přesný řádek** — ověřit přesné znění dev/stage pravidla. Je to MUST nebo SHOULD? Platí vždy nebo jen v bootstrap?
- **Briefing mode** — `internal/knowledge/briefing.go`. Briefing záměrně EXKLUDUJE core reference. LLM dostane recipe + briefing ale NE core.md pravidla. Dvou-volání pattern (briefing + scope) je vyžadován ale ne vynucován (K2 z knowledge auditu).
- **`startWithoutCode: true`** — je to validní import.yml pole? Ověřit v `../zerops-docs/` nebo live API.
- **`maxContainers: 1`** — je to povinné pro dev service? Nebo jenom doporučené?
- **Jak workflow step `generate-import` recipes overriduje** — `internal/content/workflows/bootstrap.md` section `generate-import`. Jak instruuje LLM aby modifikoval recipe output?
- **Dopad na non-bootstrap flow** — pokud LLM nepoužívá workflow a jen se ptá "dej mi zerops.yml pro Laravel", dostane recipe bez dev/stage. Workflow fix se neuplatní.

## Relevantní soubory

```
internal/knowledge/recipes/*.md         — všech 30 recipes
internal/knowledge/themes/core.md       — dev/stage pravidlo, minRam pravidlo
internal/knowledge/themes/services.md   — wiring templates
internal/knowledge/briefing.go          — co briefing vrací (exkluduje core)
internal/knowledge/engine.go            — recipe retrieval flow
internal/knowledge/recipe_lint_test.go  — recipe validation tests
internal/content/workflows/bootstrap.md — section "generate-import"
```
