# ZCP Eval Scenarios — Iterativní regresní smyčka

> **Status**: Draft (2026-04-20)
> **Cíl**: Zautomatizovat manuální eval-fix-iterate cyklus. Po každém scénáři reportujeme findings + fix, user schválí, loop.
> **Scope**: `internal/eval/scenarios/*.md`, `internal/eval/seed.go`, `internal/eval/scenario.go`, `cmd/zcp/eval.go`.

---

## 1. Motivace

Dnešní manuální loop (Laravel weather run, 8m 17s, tři bugfixy po dokončení):

1. User spustí agenta s promptem.
2. User mi pošle export.
3. Ručně diagnostikuji regrese → fixy → rebuild binárky → ship do zcp containeru.
4. User re-run.

Funguje, ale:
- Po ztrátě session findings mizí.
- Každý fix mohl rozbít předchozí (žádný regresní štít).
- "Adoption flow" a další fixture-závislé cesty nikdy neotestované proti živému agentovi.

**Cíl**: strojově spustitelné scénáře pokrývající failure surface, který jsme reálně narazili.

---

## 2. Pět prvotních scénářů

Každý scénář = jeden soubor pod `internal/eval/scenarios/*.md`:

```yaml
---
id: scenario-id
description: jednořádkový popis
seed: empty | imported | deployed
fixture: relative/path/to/fixture.yaml    # jen pro imported / deployed
expect:
  mustCallTools: [zerops_workflow, zerops_import, ...]
  workflowCallsMin: 15
  mustEnterWorkflow: [bootstrap, develop]
  finalUrlStatus: 200
  forbiddenPatterns: ["<projectId>"]       # raw substring checks na log
followUp:
  - "Po dokončení odpověz na: Q1? Q2?"
---

# Prompt

<task body>
```

Seed mody (stav projektu před startem agenta):

- `empty` — `CleanupProject()`, pak agent od nuly.
- `imported` — cleanup + `zerops_import <fixture>` + poll ACTIVE (agent vstupuje do projektu s existujícími services, ale prázdným workflow state).
- `deployed` — imported + `zcli push` (runtime má nasazený build).

### 2.1 `greenfield-laravel-weather` [seed: empty]

**Proč**: baseline celé bootstrap cesty. Regresní štít pro 4 bugy, které jsme právě opravili (strategy-unset atom v idle, subdomain pattern, mandatory develop workflow, skip-close transition message).

**Prompt**: "Vytvor Laravel aplikaci s dashboardem počasí (výběr města, zobrazení teploty z Open-Meteo API)."

**Expect**:
- `mustCallTools`: `zerops_workflow` (bootstrap + develop), `zerops_import`, `zerops_verify`
- `workflowCallsMin`: 20
- `mustEnterWorkflow`: `[bootstrap, develop]`
- `finalUrlStatus`: 200 (agent-reported)
- `forbiddenPatterns`: agent nesmí vyprodukovat subdomain s UUID-like stringem (`<projectId>` anti-pattern)

**Follow-up**:
1. "Použil jsi develop workflow pro code changes? Proč ano/ne?"
2. "Jak jsi určil formát URL pro subdomain?"

### 2.2 `greenfield-nodejs-todo` [seed: empty]

**Proč**: dynamic runtime třída — vyžaduje `zsc noop` → SSH start po deployi. Není pokryté scénářem 2.1 (static runtime auto-startuje).

**Prompt**: "Vytvoř REST API pro TODO items s PostgreSQL v Node.js (Express nebo Fastify). Endpointy: GET /todos, POST /todos."

**Expect**:
- `mustCallTools`: `zerops_workflow`, `zerops_import`, `zerops_verify`, SSH commands (přes Bash) pro manual start
- `atomsHit`: `dynamic-runtime-start` (atom navádějící na SSH start po deployi)
- `finalUrlStatus`: 200 na `GET /todos`

**Follow-up**:
1. "Jak jsi spustil server po deployi?"
2. "Proč dynamic runtime vyžaduje manual start, zatímco php-nginx ne?"

### 2.3 `adopt-existing-nodejs` [seed: imported]

**Proč**: jediný dnešní adoption test je unit-level (`bootstrap_conductor_test.go`). Živý agent proti pre-imported services nikdy neběžel. Známé riziko: agent re-createne existující service místo adopce.

**Fixture** `fixtures/nodejs-only.yaml`:
```yaml
project:
  name: eval-${suiteId}
services:
  - hostname: app
    type: nodejs@22
    mode: NON_HA
```

**Prompt**: "Ve projektu už existuje `app` (nodejs). Přidej PostgreSQL jako `db` a propoj s app přes `DATABASE_URL` env var. Nevytvářej nový app service."

**Expect**:
- discover step adoptuje `app` (ne re-provision)
- provision vytvoří pouze `db`
- žádný duplicitní `app` service
- agent ve follow-up prokáže, že rozpoznal existující service

**Follow-up**:
1. "Jak jsi zjistil, že `app` už existuje?"
2. "Kolik services bylo na konci v projektu?"

### 2.4 `develop-add-endpoint` [seed: deployed]

**Proč**: develop workflow gate, strategy otázka, auto-close. Dnes neotestované s plně-deployed výchozím stavem.

**Fixture**: `fixtures/laravel-minimal.yaml` + zcli push artifact (fully deployed laravel-minimal recipe).

**Prompt**: "Přidej endpoint `GET /api/status` vracející `{ status: 'ok', timestamp: <now-iso> }`."

**Expect**:
- `mustEnterWorkflow`: `[develop]` (NE bootstrap — state už je bootstrapped)
- `atomsHit`: `develop-strategy-question`
- `autoClose`: triggered by successful verify
- zero bootstrap tool calls

**Follow-up**:
1. "Volil jsi push-dev nebo push-git? Proč?"
2. "Kdy se develop session uzavřel?"

### 2.5 `develop-strategy-unset-regression` [seed: deployed, strategy cleared]

**Proč**: přímý regresní štít pro dnes-opravený `develop-strategy-unset` atom. Pokud ten atom přestane v idle phase firovat po budoucím refaktoru, tenhle scénář to chytne.

**Fixture**: deploy laravel-minimal + explicitně vymazat `strategy` field v `.zcp/state.json` (seed pomocí `seedDeployedWithoutStrategy`).

**Prompt**: "Změň titulek na home page na 'Weather Dashboard'."

**Expect**:
- `atomsHit`: `develop-strategy-unset` (právě-opravený atom)
- agent před prvním Edit tool call musí zmínit strategy selection
- strategy atom se objeví v `zerops_workflow status` odpovědi

**Follow-up**:
1. "Viděl jsi hlášku o strategy selection? Kdy?"

---

## 3. Infra rozšíření

### 3.1 `internal/eval/scenario.go` (~80 LOC)
- `ParseScenario(path string) (*Scenario, error)` — parse frontmatter + prompt body
- Validace: seed ∈ {empty, imported, deployed}; fixture existuje pokud ≠ empty; expect.mustCallTools neprázdné
- Unit test: fixtures pod `internal/eval/scenarios/testdata/`

### 3.2 `internal/eval/seed.go` (~120 LOC)
- `seedEmpty(ctx, client, projectID)` — aliases `CleanupProject`
- `seedImported(ctx, client, projectID, fixtureYAML)` — cleanup + `client.ImportProject(yaml)` + `pollUntilActive(services)`
- `seedDeployed(ctx, client, projectID, recipeName)` — imported + invoke `zcli push` over SSH na zcpx
- Všechny idempotentní (každá volá cleanup jako první).

### 3.3 `cmd/zcp/eval.go` (+40 LOC)
- Nový subcommand: `zcp eval scenario <path>`
- Runs: parse scenario → seed → spawn Claude with prompt+followUp → extract → grade

### 3.4 `internal/eval/grade.go` (~80 LOC)
- `GradeResult(scenario *Scenario, log string, tools []ToolCall) *Grade`
- Assertions: tool calls contain `mustCallTools`, count ≥ `workflowCallsMin`, `finalUrlStatus` matches, no `forbiddenPatterns`
- Returns `Grade{Passed bool, Failures []Failure}` pro reportování

### 3.5 Scénářové soubory (~5 × 30 LOC)
- `internal/eval/scenarios/greenfield-laravel-weather.md`
- `internal/eval/scenarios/greenfield-nodejs-todo.md`
- `internal/eval/scenarios/adopt-existing-nodejs.md`
- `internal/eval/scenarios/develop-add-endpoint.md`
- `internal/eval/scenarios/develop-strategy-unset-regression.md`

### 3.6 Fixtures (~3 × 15 LOC)
- `internal/eval/scenarios/fixtures/nodejs-only.yaml`
- `internal/eval/scenarios/fixtures/laravel-minimal.yaml` (zcli push target)
- Validace proti živému import schématu v `internal/schema` (unit test).

**Celkem**: ~450 LOC produkční + ~150 LOC test.

---

## 4. Iterativní loop protokol

Pro každý scénář (1 → 5):

1. **Run**: `zcp eval scenario internal/eval/scenarios/<id>.md`
2. **Capture**: `log.jsonl` + `assessment.md` + `tool-calls.json` + `follow-up-answers.md` pod `internal/eval/results/<suiteId>/<id>/`
3. **Auto-grade**: scenario expectations vs. extracted evidence → `grade.json`
4. **Já reportuju**:
   - PASS/FAIL + která assertion selhala
   - Root-cause hypotéza (code path, atom, template)
   - Navrhovaný fix (konkrétní soubor + změna)
5. **User**: schvaluje fix, upravuje scénář, nebo odkládá
6. **Apply fix**: code change + unit/integration test (RED-GREEN) + rebuild linux binárky + scp na zcpx
7. **Re-run scénář** → musí PASS před postupem na scénář N+1

**Pravidlo**: nepřeskakuju na scénář N+1, dokud N nepasuje. Zabraňuje stohování nesouvisejících fixů.

---

## 5. Pořadí exekuce

Nezávislé (každý cleanupuje). Pořadí reflektuje confidence + risk:

1. **greenfield-laravel-weather** — nejvyšší pokrytí, nejčerstvější regresní surface (baseline).
2. **develop-strategy-unset-regression** — úzký, validuje právě shipnutý fix.
3. **greenfield-nodejs-todo** — nová runtime třída.
4. **adopt-existing-nodejs** — netknutá cesta; očekávám findings.
5. **develop-add-endpoint** — závisí na funkčním `seedDeployed`; poslední.

---

## 6. Success kritéria

- Všech 5 scénářů má zaznamenaný PASS v `internal/eval/results/`.
- Každý failure během iterace má fix + regresní test (Go unit nebo integration).
- Tento plán přesunutý do `plans/archive/eval-scenarios.md` s post-mortem tabulkou (scénář × nalezené bugy × fix commit).

---

## 7. Otevřené otázky

- **Follow-up Q&A**: batch mode (Q v prompt appendixu) vs. session resume (`claude --resume`). MVP: batch. Pokud agent v batch nedokáže odpovědět věrohodně (protože už nepamatuje tool calls), překlopit na resume.
- **Adoption fixture drift**: když se schéma verzí (`nodejs@22` → `nodejs@24`), fixture se rozbije. Vyřešit `catalog.Latest()` substitucí při seed, nebo ručním bumpováním + linterem.
- **`seedDeployed` cost**: každý run dělá full `zcli push` (~60 s build). Zvážit snapshot cache, pokud testy začneme pouštět v CI.
