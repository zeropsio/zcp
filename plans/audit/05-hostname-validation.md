# 05 — Hostname Validation: Wrong, Inconsistent, Only in One Place

## Problém

Hostname validace existuje jen v `mount.go`, je špatná (neodpovídá Zerops pravidlům), a žádný jiný tool hostnames nevaliduje. Neplatné hostnames projdou až k API, které je odmítne s opaque errorem.

## Konkrétní nálezy

### Špatný regex v mount.go
- **Soubor**: `internal/ops/mount.go:332-348`
- **Regex**: `^[a-zA-Z][a-zA-Z0-9_-]{0,62}$`
- **Skutečná Zerops pravidla** (verified via live API, viz MEMORY.md):
  - Only `[a-z0-9]` — NO uppercase, NO hyphens, NO underscores
  - Max 25 chars (ne 63)
  - Immutable po vytvoření
- **Co regex povoluje špatně**: uppercase (`AppDev`), hyphens (`my-app`), underscores (`my_app`), 63 znaků
- **Co regex zakazuje špatně**: hostname začínající číslem (`1app`) — Zerops toto povoluje

### Žádná validace jinde
9 tools volá `resolveServiceID()` v `helpers.go` ale žádný nevaliduje hostname formát:
- `deploy.go` — `resolveServiceID(services, input.TargetService)`
- `manage.go` — `resolveService(client, ctx, input.ServiceHostname)`
- `env.go` — `resolveService(client, ctx, input.ServiceHostname)`
- `subdomain.go` — hostname lookup v services
- `verify.go` — `resolveServiceID(services, input.ServiceHostname)`
- `delete.go` — `resolveServiceID(services, input.ServiceHostname)`
- `logs.go` — `resolveService(client, ctx, input.ServiceHostname)`
- `events.go` — hostname lookup v services
- `scale.go` — `resolveService(client, ctx, input.ServiceHostname)`

### Import YAML hostnames taky nevalidovány
- `internal/ops/import.go` — `extractHostnames()` extrahuje hostnames z import YAML
- Kontroluje hostname konflikty s DELETING services
- ALE nevaliduje hostname formát
- Neplatný hostname v import.yml projde k API

### Error chain pro neplatný hostname
1. LLM vygeneruje `hostname: "my-app"` v import YAML
2. `ops/import.go` extrahuje hostname, nevaliduje formát
3. API dostane YAML, odmítne hostname
4. API error je HTTP 400-499 → `mapAPIError()` → `API_ERROR` bez suggestion (viz problém #04)
5. LLM vidí: `{"code":"API_ERROR","error":"<opaque msg>","suggestion":""}`
6. LLM neví PROČ hostname selhal

## Proč je to fundamentální

1. **Jedna validační funkce by opravila 9+ tools** — vysoký ROI
2. **Validace existuje ale je špatná** — horší než žádná, dává falešný pocit bezpečí
3. **Řetězec selhání** — neplatný hostname → API error → lossy translation → opaque LLM error. Správná validace by dala jasný error s pravidly.

## Kde se podívat dál (neprozkoumáno)

- **`internal/ops/helpers.go`** — `resolveServiceID()` funkce. Přesně jak funguje lookup? Porovnává case-sensitive? Co se stane s hostname co existuje ale má špatný formát (historical service s hyphens)?
- **`internal/ops/mount.go:332-348`** — celá `validateHostname()` funkce. Je volána jen z mount ops nebo i odjinud?
- **Import YAML hostname extraction** — `extractHostnames()` v `ops/import.go`. Co přesně extrahuje? Extrahuje i hostnames z `mount:` field?
- **Zerops API hostname pravidla** — ověřit v `../zerops-docs/` přesný regex/pravidla. Povoluje API hostname začínající číslem? Minimální délka? Reserved names?
- **Historical services** — mohou existovat services s hyphens/underscores z dřívějších verzí Zerops? Pokud ano, validace musí být jen pro NOVÉ hostnames, ne pro existující.
- **`workflow/bootstrap.go`** — `validateConditionalSkip` pracuje s plan, který má hostnames. Validuje plan hostnames?
- **`internal/tools/workflow.go`** — plan parameter pro bootstrap step. Schema říká "Validates hostnames and types" — kde se ta validace děje?

## Relevantní soubory

```
internal/ops/mount.go       — validateHostname() se špatným regexem
internal/ops/helpers.go     — resolveServiceID(), resolveService() — bez hostname validace
internal/ops/import.go      — extractHostnames(), žádná format validace
internal/platform/zerops_errors.go — jak API hostname error dorazí k LLM
internal/tools/workflow.go  — plan hostname validation claim
```
