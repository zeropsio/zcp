# 04 — Error Translation Chain Loses API Error Codes

## Problém

Error řetězec `Zerops API → mapSDKError() → PlatformError → convertError() → MCP JSON` ztrácí informace. API error codes se zahazují, 400-level validation errors se stávají generickým `API_ERROR`, a některé ops funkce obcházejí PlatformError úplně.

## Konkrétní ztráty

### HTTP 400-499 (mimo 401/403/404/429) → generický API_ERROR
- **Soubor**: `internal/platform/zerops_errors.go:77-82`
- **Co se děje**: Neznámé 400-level kódy produkují `NewPlatformError(ErrAPIError, msg, "")` — prázdný suggestion
- **Originální API error code** (`errCode` z řádku 49) se ZAHAZUJE
- **Dopad**: API 422 validation errors (neplatné YAML hodnoty, neplatné porty, konflikty hostname) se stávají `{"code":"API_ERROR","error":"<raw msg>","suggestion":""}`. LLM nerozliší "retry later" od "fix your input".

### HTTP 500+ → "retry later" bez rozlišení
- **Soubor**: `zerops_errors.go:77-82`
- **Co se děje**: `NewPlatformError(ErrAPIError, msg, "Zerops API error -- retry later")`
- **Dopad**: Transientní 500 a persistentní 503 vypadají stejně

### GetProcessStatus přepisuje JAKOUKOLIV chybu na "Process not found"
- **Soubor**: `internal/ops/process.go:44-47`
```go
p, err := client.GetProcess(ctx, processID)
if err != nil {
    return nil, platform.NewPlatformError(platform.ErrProcessNotFound,
        fmt.Sprintf("Process '%s' not found", processID), "Check the process ID")
}
```
- **Dopad**: Network timeout, auth expired, API 500 — vše se stane "Process not found". Zcela zavádějící.
- **Stejný pattern**: `CancelProcess` na řádku 69-72

### `fmt.Errorf` v ops vrstvě obchází PlatformError
Tyto funkce vrací Go error místo PlatformError, takže `convertError` je renderuje jako plain text bez code/suggestion:
- `ops/deploy.go:107` — `fmt.Errorf("list services: %w", err)`
- `ops/mount.go:70` — `fmt.Errorf("list services: %w", err)`
- `ops/mount.go:206` — `fmt.Errorf("list services: %w", err)`
- `ops/events.go` — řádky 121-129, tři `fmt.Errorf` volání
- `ops/progress.go:73` — `fmt.Errorf("poll build for service %s: %w", ...)`
- `ops/progress.go:143` — `fmt.Errorf("poll process %s: %w", ...)`
- `ops/process.go:83` — `fmt.Errorf("cancel process %s: %w", ...)`
- **Pozor**: Pokud wrapped error JE PlatformError, `convertError` ho extrahuje přes `errors.As`. Problém je jen když inner error NENÍ PlatformError (context cancellation, raw network errors co prošly přes mapSDKError).

### Silent env fetch failures v discover
- `internal/ops/discover.go:189` a `199` — `attachEnvs` a `attachProjectEnvs` tiše spolknou errors s `return`
- LLM vidí services bez env vars a neví že měly být přítomné
- Žádný Warnings field v `DiscoverResult`

## Pozitivní příklady (co funguje dobře)

### Deploy SSH error classifier — gold standard
- `internal/ops/deploy_classify.go` mapuje 8 specifických failure patterns na actionable suggestions:
  - OOM → konkrétní doporučení
  - Disk full → konkrétní doporučení
  - zerops.yml missing → konkrétní doporučení
  - Permission denied → konkrétní doporučení
- Tohle je surface-level fix (parsuje stderr místo strukturovaných chyb z API), ale je to EFEKTIVNÍ

### SERVICE_NOT_FOUND includes available services
- `helpers.go:20-26` — `resolveServiceID` připojuje `"Available services: " + listHostnames(services)` do suggestion
- LLM může okamžitě retry se správným hostname

### Env format errors
- `helpers.go:100-121` — `parseEnvPairs` říká LLM přesně jaký formát je očekávaný

## Proč je to fundamentální

1. **LLM rozhoduje na základě error messages** — generický "API_ERROR" bez suggestion nedává LLM dostatek info k self-correction
2. **Misleading errors** (process.go) aktivně škodí — LLM debuguje "process not found" když skutečný problém je auth expired
3. **Inconsistence** — některé tools mají excelentní errors (deploy classifier), jiné generické. LLM nemá konzistentní experience.

## Kde se podívat dál (neprozkoumáno)

- **`internal/platform/zerops_errors.go`** — kompletní mapSDKError() a mapAPIError() flow. Jaké HTTP kódy JSOU mapovány specificky (401, 403, 404, 429)? Jaké nejčastější 400-level kódy Zerops API vrací?
- **Zerops API dokumentace** — jaké error codes API vrací? Existuje seznam? Jsou error codes stabilní? Viz `../zerops-docs/` pro API reference.
- **`internal/platform/errors.go`** — definice všech error code konstant. Kolik jich je? Které se skutečně používají?
- **`internal/tools/convert.go`** — `convertError()` funkce. Jak přesně extrahuje PlatformError z wrapped errors? Edge cases?
- **`internal/platform/client.go`** — jak API client volá SDK a jaké raw errors dostává zpět
- **Deploy classifier patterns** — `deploy_classify.go` kompletní seznam patterns. Dají se rozšířit? Dají se aplikovat na jiné tools?
- **Reálné Zerops API error responses** — E2E test s neplatným hostname, neplatným YAML, neplatným portem. Co API vrací? Je to strukturované JSON s error code, nebo plain text?
- **`DiscoverResult` struct** — existuje Warnings field? Pokud ne, jak ho přidat bez breaking change?

## Relevantní soubory

```
internal/platform/zerops_errors.go  — mapSDKError(), mapAPIError(), error translation
internal/platform/errors.go         — PlatformError, error code constants
internal/platform/client.go         — API client, SDK interaction
internal/tools/convert.go           — convertError(), PlatformError → MCP JSON
internal/ops/process.go             — GetProcessStatus, CancelProcess (misleading errors)
internal/ops/deploy.go              — fmt.Errorf wrapping
internal/ops/deploy_classify.go     — SSH error classifier (gold standard)
internal/ops/discover.go            — silent env fetch failures
internal/ops/progress.go            — fmt.Errorf wrapping
internal/ops/mount.go               — fmt.Errorf wrapping
internal/ops/events.go              — fmt.Errorf wrapping
internal/ops/helpers.go             — resolveServiceID (good pattern), parseEnvPairs (good pattern)
```
