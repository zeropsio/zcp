# Multi-runtime weather-dashboard audit — follow-up plan

> **Status**: 6 of 18 fixes shipped (host→hostname + 4 atom-axis tightenings + RecordExternalDeploy + startup_detected delete). 12 remaining across Tier 2-5. Validation re-run not yet executed.
> **Origin**: 11-runtime weather-dashboard eval sweep ran 2026-04-25 against eval-zcp project. Audit report at `eval/results/audit-multi-weather-20260425_012145.md` (11-scenario data, agent self-evals + bucket classification).
> **Owner**: LLM-only execution. Each session starts with §0.

---

## 0. Restart Protocol — READ FIRST

60 seconds of attention. Every session. No exceptions.

1. `git log --grep='audit\|atoms(\|verify:\|workflow:' --oneline --max-count=20` — see what's been shipped from this plan.
2. Read §6 "Phase Status" — find first unchecked `[ ]`.
3. Read that fix's full spec in §4.
4. Run `go test ./... -count=1 -short` — baseline green?
5. Execute the next commit per spec.
6. Tick the `[x]` in §6 after the commit lands.
7. Never re-litigate decisions documented in §5. They are final.

**Post-compaction first actions**:
- Read this file's §0, §3 (what's done), §6 (next unchecked).
- Recommended next: **Sprint 1 closure** = ship Fix #6 (per-scenario timeout) + run validation re-run (Fix #1-#4 effectiveness).
- Or proceed directly to Sprint 3 (knowledge gaps, fixes #7-#13).

---

## 1. Context

Cross-runtime weather-dashboard eval sweep (11 scenarios: PHP-Laravel, Node.js, Python, Go, Next.js SSR, Ruby, Bun, Java, Deno + 2 legit fails — .NET ASP.NET, Rust). 9/11 PASS. Each agent produced a structured `## EVAL REPORT` with **Atom Bucket Classification** (A=load-bearing / B=useful awareness / C=pure noise) for every atom in its dispatch brief.

**Cross-runtime patterns confirmed (≥2 runtimes flagged C):**
- `develop-dev-server-triage` — C in 3/4 dynamic runtimes (PHP/Python/Go) — **shipped Fix #2**
- `develop-dynamic-runtime-start-container` — C in 3/3 dynamic runtimes when first-deploy flow doesn't manually start dev server — partial (no fix yet)
- `develop-dev-server-reason-codes` — C in 4/8 (Python/Go/Next/Rust) — **shipped Fix #4**
- `develop-ready-to-deploy` — C in 4/8 (PHP/Node/Next/Rust) — **shipped Fix #3**
- `develop-env-var-channels` — C in 3/8 — partial signal, no fix yet
- `develop-knowledge-on-demand` — C in 2/8 — likely hallucinated atom ID, real corpus has `develop-knowledge-pointers`

**Knowledge bug confirmed (3/4 batch-1 runtimes):**
- PostgreSQL env var key `host` vs reality `hostname` — **shipped Fix #1** (extended to all 13 managed types via platform-verifier agent verification).

**Tool-side bugs found:**
- `zerops_verify::startup_detected` — broken-by-design from c3493c1 (Feb 2026) — **shipped Fix #5** (deleted entirely, not pattern-extended).

**Friction with weak/no fix yet:**
- `npm ci` vs `npm install` for fresh scaffolds (2/2 Node runtimes hit) — Fix #8
- TypeScript `moduleResolution: node10` deprecation (1× Next) — Fix #9
- Python `pip install --target=./vendor` PATH (1× Python) — Fix #10
- Go heavy compile in initCommands (1× Go) — Fix #11
- Ruby `BUNDLE_PATH` interactive (1× Ruby) — Fix #12
- `zerops_dev_server` env-prefix `VAR=val cmd` (1× Ruby) — Fix #13
- Project-level env vars via `${VAR}` syntax (1× Laravel/PHP, universal pattern) — Fix #7

---

## 2. Empirical data sources

| Artifact | Path | Use |
|---|---|---|
| Final audit report | `eval/results/audit-multi-weather-20260425_012145.md` | 11-runtime bucket matrix + friction table |
| Per-scenario raw | `eval/results/scenario-*/2026-04-2*/weather-dashboard-*/` | result.json + log.jsonl + tool-calls.json + assessment |
| Aggregator | `eval/scripts/aggregate-weather-audit.py` | Re-run after edits to refresh report |
| Sweep wrapper (batch 1) | `eval/scripts/run-multi-runtime-weather.sh` | 4 runtimes sequential |
| Autonomous driver | `eval/scripts/autonomous-weather-sweep.py` | batch 2 (7 runtimes) with retry policy |
| Follow-up batch 3 | `eval/scripts/run-batch3-followup.sh` | bun/java/deno (after main driver budget) |
| Scenarios | `internal/eval/scenarios/weather-dashboard-{php-laravel,nodejs,python,go,nextjs-ssr,dotnet,ruby,rust,bun,java,deno}.md` | 11 scenario specs with `## EVAL REPORT` + bucket-classification self-eval contract |
| Driver decision log | `eval/results/autonomous-driver-20260424_205511.log` | What ran, retries, hard-budget exit reason |

**Validation re-run command** (after Fix #1-#5 shipped, not yet executed):
```bash
./eval/scripts/build-deploy.sh                                         # ship latest binary to zcp container
./eval/scripts/run-scenario.sh internal/eval/scenarios/weather-dashboard-python.md  # representative dynamic
./eval/scripts/run-scenario.sh internal/eval/scenarios/weather-dashboard-nextjs-ssr.md  # build pipeline
./eval/scripts/run-scenario.sh internal/eval/scenarios/weather-dashboard-php-laravel.md  # implicit-webserver
./eval/scripts/run-scenario.sh internal/eval/scenarios/weather-dashboard-ruby.md  # bundler
python3 eval/scripts/aggregate-weather-audit.py                        # generate post-fix audit
diff <pre-audit> <post-audit>                                          # confirm C-count drop
```

Expected: dispatch-brief size for first-deploy envelope drops from ~750 → ~550 lines (~25%). C-count on `develop-dev-server-triage`, `develop-dev-server-reason-codes`, `develop-ready-to-deploy` drops to 0 across all 4 re-runs.

---

## 3. Already shipped this session (commit anchors)

| Commit | Fix | Files |
|---|---|---|
| `d448325` | #1 — `host` → `hostname` + verified all 13 managed service types via platform-verifier agent | `bootstrap-env-var-discovery.md`, `develop-first-deploy-env-vars.md` |
| `7b8c3fd` | #2 — `develop-dev-server-triage` axes: `runtimes:[dynamic]` + `deployStates:[deployed]` | atom frontmatter |
| `c78182f` | #3 — new `serviceStatus` axis on AxisVector + ParseAtom + synthesize.go matcher; ready-to-deploy atom gated on `serviceStatus:[READY_TO_DEPLOY]` | atom.go, synthesize.go, develop-ready-to-deploy.md, spec-knowledge-distribution.md §3.9-3.10 |
| `bea7955` | #4 — `develop-dev-server-reason-codes` axis: `deployStates:[deployed]` | atom frontmatter |
| `46325f0` | bonus — `RecordExternalDeploy(stateDir, hostname)` helper + `zerops_workflow action="record-deploy"` MCP action; bridges manual zcli/CI deployers to MCP-tracked Deployed state | service_meta.go, workflow.go, workflow_record_deploy.go (new) |
| `ef10d27` | gofmt fixup after Fix #3's struct-field reformat | atom.go |
| `8188b95` | #5 — DELETE `startup_detected` check (broken-by-design since Feb 2026: `Search` is substring not OR; `"listening\|started\|ready"` literal never matched any service) | verify_checks.go, verify.go, verify_test.go (×2 files), deploy_poll.go, spec-workflows.md |

**Net delta**: 35 lines added, 90 lines removed, 1 new MCP action, 1 new axis on AxisVector. Full test suite + lint clean at session end.

---

## 4. Remaining fix specs (verbatim, ordered by sprint)

### Tier 2 — Code change with biggest impact

#### Fix #6 — Per-scenario timeout for heavy compile runtimes

**Stav teď:** V `internal/eval/runner.go:48` je hardcoded `config.Timeout = 30 * time.Minute` pro každý scenario eval. Plus `--max-turns 60` pro Claude CLI subprocess. Pro .NET (ASP.NET) a Rust to nestačí: cargo build cold cache trvá 5-10 min, ASP.NET má ContentRootPath gotcha co vyžaduje 2-3 deploy iterace + agent's natural exploration. V auditu obě uvízly: dotnet exit-1 + 30m timeout, rust 30m timeout × 2 retries.

**Proč to vadí:** Zdravý agent dělá pomalu velký krok zatímco timeout vyprší. Eval fail-uje na infrastructure (CLI process killed) ne na agent inability.

**Co se změní:** Přidat do scenario YAML frontmatteru novou volitelnou hodnotu `timeoutMinutes: 60`. V `internal/eval/scenario.go::Scenario` struct přidat `TimeoutMinutes int` field. V `RunScenario` použít `time.Duration(sc.TimeoutMinutes) * time.Minute` pokud je >0, jinak fallback na global `r.config.Timeout`. Pro `weather-dashboard-dotnet.md` a `weather-dashboard-rust.md` nastavit `timeoutMinutes: 60`. ~10 řádků.

**Co bude jinak:** Heavy-compile runtimes dostanou férovou šanci. Retest obou by pravděpodobně prošel. Universal cap zůstane.

---

### Tier 3 — Knowledge gaps (chybí explicitní guidance)

#### Fix #7 — New atom: project-level env vars are auto-injected

**Stav teď:** Žádný atom nepopisuje, že project-level env vars (nastavené přes `zerops_env action="set" project=true`) se automaticky injectují do všech kontejnerů. Agent při psaní `zerops.yaml` napíše do `run.envVariables` třeba `APP_KEY: ${APP_KEY}` myslíc že tím odkazuje project-scope. Reálně `${...}` syntax je striktně cross-SERVICE (`${hostname_KEY}`), takže `${APP_KEY}` resolveru hledá službu `APP` s key `KEY`, nenajde, ponechá literal string. App pak crashne (Laravel: „Unsupported cipher or incorrect key length").

**Proč to vadí:** Hit Laravel/PHP eval — wasted ~2 min. Hit by každý framework s project-secrets (Rails SECRET_KEY_BASE, Django DJANGO_SECRET_KEY, Next NEXTAUTH_SECRET, Symfony APP_SECRET). Universal pattern.

**Co se změní:** Vytvořit `internal/content/atoms/develop-project-env-vars.md` se 3 sekcemi: (1) project-scope vars jsou auto-available v každém containeru — netřeba je redeklarovat v `run.envVariables`, (2) `${...}` syntax JE EXKLUZIVNĚ pro cross-service refs ve formátu `${hostname_KEY}`, (3) pro ověření: `ssh <hostname> printenv | grep YOUR_KEY` v containeru ukáže auto-injected hodnotu. Frontmatter: `phases: [develop-active], deployStates: [never-deployed]` (relevantní hlavně při scaffoldingu).

**Co bude jinak:** Eliminuje celou třídu „proč moje project env nefunguje v zerops.yaml" friction.

---

#### Fix #8 — Node: `npm install` for fresh, `npm ci` for existing

**Stav teď:** Žádná guidance ohledně volby `npm install` vs `npm ci` v `buildCommands`. Agenti defaultují na `npm ci` (CI standard). Fresh scaffold ale nemá `package-lock.json` (agent zapsal jen `package.json` ručně), takže `npm ci` selže s „can only install with an existing package-lock.json".

**Proč to vadí:** Hit oba Node-based scenarios (Node Express + Next.js SSR). Konzistentní pattern, dvě deploy iterace pryč.

**Co se změní:** Doplnit do atomu `develop-first-deploy-write-app.md` (nebo nového `develop-nodejs-buildcommands.md`) note: pro fresh scaffold bez `package-lock.json` použij `npm install`, který lockfile vytvoří. `npm ci` má smysl pouze pro existující projekty s checked-in lockfilem (deterministic install). Stejná logika platí pro `bun install` (always works) vs `bun install --frozen-lockfile`. ~10 řádků.

**Co bude jinak:** Šetří 1 deploy-fail iteration na každý Node/Next scaffold.

---

#### Fix #9 — Next.js: TypeScript moduleResolution deprecation

**Stav teď:** Next.js 15+ při prvním buildu auto-generuje `tsconfig.json` s `moduleResolution: "node10"` (legacy). TypeScript 6.x (na Zerops platform via npm install) odmítá s „Option 'moduleResolution=node10' is deprecated and will stop functioning in TypeScript 7.0". Build crashne.

**Proč to vadí:** Hit Next.js eval, 1 deploy iteration. Pravděpodobně i jiných Next/TS variant.

**Co se změní:** Doplnit do scaffold atomu nebo nového Next-specific atomu krátkou poznámku: pro Next.js project ručně vytvoř `tsconfig.json` PŘED prvním buildem s `moduleResolution: "bundler"` (nebo `"NodeNext"`). Default Next-generated config nemusí projít s TypeScript 6+. ~5 řádků.

**Co bude jinak:** Šetří 1 deploy iteration na Next scaffolds.

---

#### Fix #10 — Python: `--target` install path

**Stav teď:** Recipes/atomy pro Python doporučují `pip install --target=./vendor` (instalace deps lokálně do projektu). Agent pak napíše `start: gunicorn --bind 0.0.0.0:8080 app:app` s předpokladem, že `gunicorn` je na PATH. Reálně `--target=./vendor` umístí binárky do `./vendor/bin/`, což na PATH NENÍ. App crashne s „gunicorn: command not found".

**Proč to vadí:** Hit Python eval, ~3 min pátrání + 1 redeploy. Universal pro Flask + FastAPI + Django + jakýkoliv framework potřebující standalone binary.

**Co se změní:** Doplnit do atomu (scaffold nebo Python-specific) note: `pip install --target=./vendor` umístí scripts/binárky do `./vendor/bin/`, ne na PATH. Pro start command použij explicit path `start: /var/www/vendor/bin/gunicorn ...` NEBO `start: python -m gunicorn ...` (standardní Python module-execution syntax). ~5 řádků.

**Co bude jinak:** Šetří iteraci u Python deploys. Funguje pro všechny standalone Python servery (gunicorn, uvicorn, hypercorn, daphne).

---

#### Fix #11 — Go: heavy compile in `buildCommands`, not `initCommands`

**Stav teď:** Existující Go guidance/recipes ukazují `initCommands: go run cmd/migrate/main.go` (pro migration script). Agent extends pattern na `initCommands: go build -o app .` (full project compile). `initCommands` ale běží v RUNTIME containeru s constrained CPU + bez deps cache, build trvá 3-5× déle a často timeout-uje (DEPLOY_FAILED).

**Proč to vadí:** Hit Go eval, 1 redeploy + 2 min waste. Stejný pattern by hit Rust (`cargo build`), .NET (`dotnet publish`).

**Co se změní:** Explicit note v Go-specific atomu nebo obecném scaffold atomu: heavy compile (`go build`, `cargo build`, `dotnet publish`, `mvn package`) patří do `build.buildCommands`, NIKDY do `run.initCommands`. Důvody: (1) build container má dependency cache + faster CPU, (2) `initCommands` se spouští každý cold start runtime containeru. Migrations/setup scripts jsou OK v `initCommands` (rychlé, idempotent). ~10 řádků.

**Co bude jinak:** Šetří iteraci u všech compiled languages (Go, Rust, .NET, Java).

---

#### Fix #12 — Ruby: `BUNDLE_PATH` for interactive bundle install

**Stav teď:** Default `bundle install` zapisuje do system gem dirs (`/usr/local/lib/ruby/gems/...`). User `zerops` na containerech nemá write permission do system path. Build-time deploy v `buildCommands` má auto-nastavený `BUNDLE_PATH=vendor/bundle` (Zerops Ruby base image), ale interaktivní SSH session ne. Agent pak narazí na permission denied když SSHne na container a spustí `bundle install` ručně (např. pro generování `Gemfile.lock` před prvním deployem).

**Proč to vadí:** Hit Ruby eval, ~3 min pátrání + workaround.

**Co se změní:** Doplnit do Ruby-specific atomu note: pro interaktivní `bundle install` v dev containeru přes SSH použij `BUNDLE_PATH=vendor/bundle bundle install`. Build-time bundle install v `zerops.yaml::buildCommands` je auto-konfigurován, žádný override netřeba. ~5 řádků.

**Co bude jinak:** Šetří Ruby deployment iteration na první projekt.

---

#### Fix #13 — `zerops_dev_server`: env-prefix syntax

**Stav teď:** Tool `zerops_dev_server` při akci `start`/`restart` interně volá `exec` na shell pro spuštění start command. POSIX `exec` builtin nepodporuje shell-style inline env-prefix `VAR=value cmd` (to je bash feature). Když agent zavolá `command="BUNDLE_PATH=vendor/bundle bundle exec puma ..."`, exec interpretuje `BUNDLE_PATH=vendor/bundle` jako neexistující program a vrátí `sh: exec: BUNDLE_PATH=vendor/bundle: not found`.

**Proč to vadí:** Hit Ruby eval. Universally affects každý framework potřebující env var prefix v start command (Ruby BUNDLE_PATH, Node NODE_ENV, Python PYTHONPATH).

**Co se změní:** Dvě možnosti, lépe obě:

A) Tool fix v `internal/ops/dev_server.go`: detect inline env-prefix v command stringu (regex `^[A-Z_][A-Z0-9_]*=\S+\s+`), automaticky wrapnout do `/usr/bin/env VAR=val cmd` formátu. ~20 řádků.

B) Docs: do tool description (nebo dev-server atomu) přidat: pro env-prefixed commands použij `env VAR=val cmd` (POSIX env utility), ne shell syntax `VAR=val cmd`. ~5 řádků.

**Co bude jinak:** Tool podporuje běžnou konvenci. Ruby a další env-prefix-needing frameworks fungují first-try.

---

### Tier 4 — Tool API friction

#### Fix #14 — Auto-derive `setup=` parameter for cross-deploy

**Stav teď:** `zerops_deploy targetService=appstage` vyžaduje explicit `setup=prod`, který odkazuje na blok `zerops:` v zerops.yaml co používat (typicky pojmenovaný podle target service role). Help text říká „setup names target's zerops.yaml block", ale agenti to často přečtou jako „setup my source" a předají `setup=dev`.

**Proč to vadí:** UX confusion. User feedback to flagnul.

**Co se změní:** Při bootstrap importu zachovat `zeropsSetup` atribut z import YAML do persistent ServiceMeta na disk. Při `zerops_deploy` cross-deploy bez explicit `setup=`, načíst target's `zeropsSetup` z meta a použít jako default. Agent může explicitně overridovat parametrem. Kód: ~30 řádků (rozšíření ServiceMeta + read v deploy handler).

**Co bude jinak:** Cross-deploy příkaz `zerops_deploy targetService=appstage` funguje napoprvé bez paměti agenta jak se zerops.yaml block jmenuje.

---

#### Fix #15 — `zerops_import` accepts whole YAML with `project:` block

**Stav teď:** Tool `zerops_import` akceptuje pouze sekci `services:`. Pokud YAML obsahuje `project:` blok (env vars, preprocessor directives, scaling config), tool error-uje. Agent musí YAML manuálně rozdělit: (1) extract `project.envVariables` → call `zerops_env action="set" project=true variables=[...]`, (2) submit zbývající (services-only) YAML do `zerops_import`. Dva kroky, dvě místa kde zapomenout.

**Proč to vadí:** Friction class. User feedback flagnul.

**Co se změní:** Tool internally splits YAML: pokud má `project:` blok, parsne ho, pro každý `project.envVariables` entry zavolá interní env-set, pak ImportServices se zbytkem. Agent posílá jeden volání, dostane jeden response shrnující obě části. Kód: ~50 řádků v handleru tool.

**Co bude jinak:** One tool call místo dvou. Eliminuje „zapomněl jsem env-set před importem" failure class.

---

#### Fix #16 — Better `zerops_env set project=true` message when 0 services

**Stav teď:** `internal/tools/env.go:178` má hardcoded response: „No ACTIVE services needed restart. The new env value will be injected when a service starts or deploys." Když je voláno za bootstrap (před vytvořením služeb), zní jako „nic se nestalo, možná chyba".

**Proč to vadí:** Confusing messaging.

**Co se změní:** Rozlišit dvě cesty:
- `project=true` + 0 services: „Project env var stored. No consumer services exist yet — value will inject into containers as they're created."
- `project=true` + N services restartováno: stávající message.

V env.go přidat if-branch ~5 řádků.

**Co bude jinak:** Méně matoucí message během bootstrap-time project env setupu.

---

#### Fix #17 — `zerops_discover` env-key bucketing

**Stav teď:** Response z `zerops_discover` vrací flat seznam env vars per service. Není structurální distinkce mezi: (a) project-level vars (auto-injected, user-set), (b) service-level vars (user-set přes env-set), (c) platform-injected (auto-set Zeropsem podle service typu — `hostname`, `port`, `connectionString`, `password` u DB, atd.). Agent musí mentálně kategorizovat při čtení.

**Proč to vadí:** Slow reading, user feedback flagnul.

**Co se změní:** Restructure response shape:
```json
{
  "services": [{
    "hostname": "db",
    "envs": {
      "platformInjected": {"hostname": "...", "port": "5432", "connectionString": "...", ...},
      "userServiceLevel": {"CUSTOM_KEY": "..."}
    }
  }],
  "projectEnvs": {"APP_KEY": "...", "GIT_TOKEN": "..."}
}
```

V `internal/ops/discover.go` rozšířit `DiscoverResult` struct, distinguish keys per source. Klasifikace zdroje keys: porovnat proti hardcoded set platform-injected names per service type. ~50 řádků refactor + může breaknout existing consumery shape.

**Co bude jinak:** Agent vidí strukturu „odkud key přichází" instantly. User-facing schema-breaking change ale pre-prod takže OK.

---

### Tier 5 — Atom ID consolidation

#### Fix #18 — Render dispatch brief with atom-ID headers

**Stav teď:** Agenti v self-eval reportech zapisují atom IDčka, která NEEXISTUJÍ v reálném korpusu (`develop-strategy-pick`, `develop-knowledge-on-demand`, `develop-apiMeta-errors`). Realita: atom file je `develop-strategy-awareness`, `develop-knowledge-pointers`, `develop-api-error-meta`. Agenti si ID rekonstruují z paměti podle obsahu protože v dispatch briefu vidí jen atom BODY (markdown content), žádné explicit ID labely.

**Proč to vadí:** Cross-runtime aggregator nemůže mechanically mapovat „atom X je C v 5/11 runtimes" když 5 různých agentů popisuje stejný atom 5 různými fake IDčky. Audit-data fuzzy-match místo exact-match.

**Co se změní:** V `internal/workflow/render.go` (nebo wherever renderuje synthesized atoms) přidat per-atom hlavičku s real ID:

```
=== develop-first-deploy-intro ===
[atom body content here]

=== develop-first-deploy-scaffold-yaml ===
[atom body content here]
```

Agent pak v self-eval reportu používá canonický ID který vidí přímo v dispatch briefu, ne paměť. ~20 řádků v render funkci.

**Co bude jinak:** Cross-runtime aggregation se stane mechanical exact-match. Future audity dají čistší signál.

---

## 5. Locked architectural decisions (do NOT re-litigate)

Tyto rozhodnutí byly v této session učiněné s plným kontextem. Pokud chceš změnit, prosím doložit nová data.

### D1 — startup_detected SMAZÁN, ne pattern-extended (Fix #5)

**Důvod:** `Search` v `platform.LogFetchParams` je dokumentovaný jako "case-sensitive substring match on Message". `"listening|started|ready"` se hledalo jako literál, žádná aplikace tohle nikdy nelogla. **Check byl rozbitý od ship-time `c3493c1` (Feb 2026)** a nikdy nic nematchoval. Adding more patterns by paper-overoval architectural mistake — log-substring match je špatná abstrakce pro "did the app start". HTTP probe (proof-of-life via 2xx/3xx/4xx) + service_running + error_logs.Detail dohromady pokrývají všechny failure modes.

### D2 — `serviceStatus` jako 5. service-scoped axis (Fix #3)

**Důvod:** Konsistentní s existing service-scoped axes (Modes/Strategies/Triggers/Runtimes/DeployStates). Joins conjunction rule (§3.10 spec-knowledge-distribution): jedna služba musí satisfy VŠECHNY declared service-scoped axes. Univerzálně použitelná osa pro budoucí stavové atomy.

### D3 — `RecordExternalDeploy` přes `zerops_workflow action="record-deploy"` ne dedicated tool (bonus)

**Důvod:** Workflow tool je tracking-side concern. Action je workflow-less (routed before engine != nil guard). Idempotent. Žádný nový MCP nástroj, jen rozšířený action enum. Cleaner než nový top-level tool.

### D4 — Agent ID hallucination NENÍ aggregator bug, je to render-side gap (Fix #18 budoucí)

**Důvod:** Agenti nemají přístup k raw atom file names — vidí jen body content. Real fix je render-side (přidat ID headers do dispatch briefu), ne aggregator-side fuzzy matching. Dokud Fix #18 neproběhne, audit data má noise — ale signál (top atoms by C-count, real corpus filter) drží.

### D5 — eval-zcp je shared playground, vždy uklidit po sobě

**Důvod:** Per CLAUDE.local.md memory. Platform-verifier agent v session úspěšně provisionoval+deletoval 14 test services. Pattern: provision via zerops_import → discover → use → cleanup pomocí `zerops_delete` per hostname.

### D6 — error_logs status `info` (advisory) je vědomý design choice

**Důvod:** Per komentář v `verify.go::CheckInfo`: "advisory — LLM sees the data but aggregateStatus ignores it". Pokud chceš crashloop catch v aggregate status, správný next step je threshold-driven `error_logs` (pass do N errors, fail nad N), NE re-introducing log-pattern matching. Test `TestVerify_RuntimeCrashLoop` documentuje tradeoff.

---

## 6. Phase Status (tick as you go)

**Sprint 1 — Quick wins (DONE except validation re-run)**
- [x] Fix #1 — host→hostname (`d448325`)
- [x] Fix #2 — dev-server-triage axes (`7b8c3fd`)
- [x] Fix #3 — serviceStatus axis + ready-to-deploy (`c78182f`)
- [x] Fix #4 — reason-codes deployStates (`bea7955`)
- [x] Fix #5 — startup_detected DELETE (`8188b95`)
- [x] Bonus — RecordExternalDeploy + record-deploy action (`46325f0`)
- [ ] Validation re-run — rebuild zcp, redeploy na eval-zcp, re-run 4 representative runtimes (PHP/Python/Next/Ruby), aggregate, diff vs pre-fix audit. Confirm C-count drop on `develop-dev-server-triage`, `develop-dev-server-reason-codes`, `develop-ready-to-deploy`.

**Sprint 2 — Eval infra**
- [ ] Fix #6 — Per-scenario timeoutMinutes in scenario YAML

**Sprint 3 — Knowledge gaps (~7 small atom edits / new atoms)**
- [ ] Fix #7 — `develop-project-env-vars.md` new atom
- [ ] Fix #8 — Node `npm install` vs `npm ci` note
- [ ] Fix #9 — Next.js TypeScript moduleResolution note
- [ ] Fix #10 — Python `--target` PATH note
- [ ] Fix #11 — Go heavy-compile-in-buildCommands note
- [ ] Fix #12 — Ruby `BUNDLE_PATH` interactive note
- [ ] Fix #13 — `zerops_dev_server` env-prefix (tool fix A + docs B)

**Sprint 4 — Tool API**
- [ ] Fix #14 — auto-derive `setup=` from target's `zeropsSetup`
- [ ] Fix #15 — `zerops_import` accepts whole YAML with `project:` block
- [ ] Fix #16 — better `zerops_env set project=true` 0-services message
- [ ] Fix #17 — `zerops_discover` env-key bucketing (project/service/platform-injected)

**Sprint 5 — Render-side**
- [ ] Fix #18 — atom-ID headers in dispatch brief render

---

## 7. Validation strategy

After each Sprint commit lands:
1. `go test ./... -count=1 -short` — basic correctness
2. For atom-axis changes: write inline test that asserts envelope X hides/shows atom Y (pattern from Fix #2/#3/#4 commits)
3. For tool-API changes: extend appropriate `internal/tools/*_test.go` with new action handler test
4. For knowledge-gap atoms (Fix #7-#12): `go test ./internal/eval/... -run TestScenarios_LiveFilesParse` confirms parse

End of each Sprint:
1. Build + ship: `./eval/scripts/build-deploy.sh`
2. Run validation re-run per §2 (4 representative scenarios)
3. Diff aggregator output vs prior sprint's audit
4. Commit findings to this file's §3 (with new commit hashes)

End of plan:
- All Sprint 1-5 ticked
- Final eval re-sweep (11 runtimes) confirms dispatch-brief reduction
- This plan file moves to `plans/archive/multi-runtime-audit-followup.md` with final commit

---

## 8. Critical facts to preserve verbatim

These got established empirically this session and inform future decisions:

- **eval-zcp project ID**: managed by user; agents authenticate via `ZCP_API_KEY` from `.mcp.json`. Pinned in zcli config on zcp container.
- **Real managed service env keys** (live-verified 2026-04-25, see commit `d448325`): postgresql, mariadb, valkey, keydb, nats, kafka, clickhouse, elasticsearch, meilisearch, typesense, qdrant, object-storage, shared-storage. RabbitMQ (rabbitmq@3.9) returns `serviceStackTypeVersionIsNotActive` — currently un-provisionable.
- **Service typy NEEXISTUJÍ v platform schema enum**: `mysql`, `redis`, `mongodb` — atom guidance je dříve uváděla mylně, opraveno commitem `d448325`.
- **`platform.LogFetchParams.Search` semantics**: case-sensitive SUBSTRING match (NOT regex, NOT OR). Documented v `internal/platform/types.go:35-37`.
- **`ServiceSnapshot.Deployed` source**: 3 OR-composed signals via `compute_envelope.go::DeriveDeployed`. Truth lives in `.zcp/state/services/<hostname>.json::FirstDeployedAt` + `.zcp/state/work-session/<pid>.json::Deploys[]` ephemeral mirror + `IsAdopted() && Status==ACTIVE` fallback. Stamping locations: `RecordDeployAttempt` from 4 deploy tools + `adopt_local.go:123` + `bootstrap_outputs.go:126`.
- **`RecordExternalDeploy` bridge**: workflow-less stamp for manual zcli/CI deployers. Available at `zerops_workflow action="record-deploy" targetService=<hostname>`. Idempotent. Per pair-keyed invariant § E8, stage hostname stamps dev-keyed pair meta.
- **Atom ID hallucination risk**: agents in self-eval reports use atom IDs from memory not from corpus. Real corpus check: `ls internal/content/atoms/`. Fuzzy-match aggregator handles it but Fix #18 (render-side ID headers) is the proper resolve.
- **Audit re-run cost**: ~12-20 min/scenario for 4 representative dynamic runtimes = ~60-80 min. Heavy-compile (.NET/Rust) needs Fix #6 first or skip.
