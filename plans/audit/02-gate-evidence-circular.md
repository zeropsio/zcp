# 02 — Bootstrap Gates Fabricate Own Evidence — Circular Validation

## Problém

Workflow systém má 5 phase gates (G0-G4), které mají zajistit že LLM skutečně provedlo kroky před transition. V praxi pro bootstrap (primární workflow) gates generují vlastní evidence z attestačních stringů a pak tuto evidence kontrolují. Cirkulární validace.

## Jak to funguje

### Sekvenční kroky (funguje správně)
- `CompleteStep()` v `internal/workflow/bootstrap.go:125-149` kontroluje že `name` argument matchuje `b.Steps[b.CurrentStep].Name`
- Nelze přeskočit krok — sekvenční enforcement je solidní
- `SkipStep()` v `bootstrap.go:152-182` kontroluje `detail.Skippable` a `validateConditionalSkip()`

### Attestace (honor system)
- LLM volá `action="complete" step="deploy" attestation="deployed successfully"`
- Minimální délka attestace: 10 znaků (`bootstrap.go:136-138`, `minAttestationLen = 10`)
- Žádná sémantická validace — `"aaaaaaaaaa"` projde
- Systém NEMŮŽE ověřit že LLM skutečně zavolalo příslušný tool (MCP protokol limitace)
- `StepDetail.Tools` pole v `bootstrap_steps.go:19-24` listuje doporučené tools — čistě informační

### Auto-complete fabrikuje evidence (cirkulární)
- Když poslední krok doběhne, `autoCompleteBootstrap()` v `internal/workflow/bootstrap_evidence.go:19-83`:
  1. Generuje evidence z attestačních stringů (`attestation: "auto-recorded from bootstrap steps"`)
  2. Zapisuje evidence soubory do `evidence/{sessionID}/{type}.json`
  3. Prochází ALL gates v loop
  4. Gates checkují evidence, kterou systém právě vytvořil
  5. Gates vždy projdou protože evidence byla vytvořena pro ně

### G0 skip loophole
- `internal/workflow/gates.go:68-72`: pokud existuje `discovery.json` < 24h, G0 projde bez `recipe_review` evidence
- Izolováno session ID, takže cross-session leakage je bezpečný
- Ale comment "skip if discovery.json exists and is fresh" je zavádějící

## Proč je to fundamentální

1. **Gates existují aby vynucovaly kvalitu** — ale ve skutečnosti jen kontrolují text, který LLM napsalo
2. **Bootstrap je primární workflow** — většina uživatelů projde bootstrap, ne manuální transition+evidence
3. **Falešný pocit bezpečí** — systém vypadá že má gates a evidence, ale enforcement je ceremoniální

## Kde se podívat dál (neprozkoumáno)

- **`internal/workflow/bootstrap_evidence.go`** — přesný flow jak se evidence fabricate. Kolik evidence typů se generuje? Matchují 1:1 s gate requirements?
- **`internal/workflow/gates.go`** — kompletní gate definice (G0-G4). Co každý gate vyžaduje? Dá se některý gate udělat "reálný" (ověřit systémový stav místo attestace)?
- **`internal/workflow/state.go`** — jak se state ukládá, co se stane při crash mezi evidence write a state save
- **`ValidateEvidence` v `gates.go:119-127`** — kontroluje `Failed > 0` a empty attestation. Je tam víc logiky?
- **Non-bootstrap workflows** — manuální transition+evidence flow. Jsou gates přísnější pro deploy/debug/scale workflows? Nebo je bootstrap výjimka?
- **Reálné LLM chování** — ve skutečnosti LLM POSKYTUJI smysluplné attestace? Pokud ano, cirkulárnost je teoretický problém. Pokud ne (zkrácené kontexty, lazy attestace), je to praktický problém.
- **Alternativa: composite tool actions** — mohl by `action="complete" step="deploy"` interně triggerovat `zerops_deploy` a ověřit výsledek? Nebo by to bylo příliš magické?
- **Alternativa: systémový stav jako evidence** — gate po deploy kroku ověří že service je RUNNING (přes API), ne že LLM napsalo "deployed". Ověřit jestli to je technicky proveditelné (service hostname je v plan).

## Relevantní soubory

```
internal/workflow/bootstrap.go           — CompleteStep(), SkipStep(), minAttestationLen
internal/workflow/bootstrap_evidence.go  — autoCompleteBootstrap(), evidence fabrication
internal/workflow/bootstrap_steps.go     — step definitions, StepDetail.Tools
internal/workflow/gates.go               — CheckGate(), ValidateEvidence(), G0-G4 definitions
internal/workflow/engine.go              — BootstrapComplete(), Transition()
internal/workflow/state.go               — WorkflowState, persistence
internal/workflow/evidence.go            — SaveEvidence(), LoadEvidence()
```
