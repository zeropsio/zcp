# Friction Audit — 2026-04-24

> Source: externí Claude Code session z 2026-04-22 (log `2026-04-22-202824-vytvor-v-projektu-sluzbu-v-dot-net-ktera-bude-ume.txt`) který postavil .NET upload service + object-storage na eval-zcp. Uživatel shrnul šest friction bodů. Audit mapuje každý bod na kód, měří implementační stav, identifikuje root causes a formuluje konkrétní workstreams.
>
> **Metodologie**: 5 paralelních Explore agentů na kód, live ověření na eval-zcp (SSH + e2e test), čtení plánů (`plans/friction-root-causes.md`, `plans/api-validation-plumbing.md`) pro detekci překryvů.
>
> **Kontext větve**: `friction-git-service-lifecycle`. GLC-1…GLC-6 již ve spec-workflows.md, commity `f12fcfc`, `be1fa64`, `fc99122`. API-validation-plumbing (W1-W8) již shipped před několika dny.

---

## 0. Stavová matice — šest friction bodů z logu

| # | Friction z logu | Plánováno? | Implementováno? | Akce na dnes |
|---|---|---|---|---|
| F1 | `projectImportInvalidParameter` bez jména pole | ✅ `api-validation-plumbing.md` W1-W8 | ✅ **SHIPPED** (d919dfe, 4148a8c, eb1c8bc, 79c9604, 3c2a18a) | Nic — regression watch |
| F2 | SSHFS + git init (root-owned `.git/objects`) | ✅ GLC-1…GLC-6 spec | ✅ **SHIPPED** (e2e `TestE2E_InitServiceGit` live PASS 2026-04-24) | Nic — regression watch |
| F3 | `deployFiles paths not found: ./out` false positive | ❌ **NENÍ v žádném plánu** | ❌ bug nadále přítomen | **W-F3** níže |
| F4 | .NET recipe: `./out` místo `./out/~` + wwwroot gotcha | ✅ `friction-root-causes.md` P2.4 | ⚠️ **PARTIAL** — atom guidance shipped, recipe edit NE | **W-F4** níže |
| F5 | develop-start envelope je hustý (~60 KB) | ✅ `friction-root-causes.md` P3 | ❌ 0/8 items, blokováno na W0 | **W-F5** níže |
| F6 | Recipe hostname collision → forced classic path | ✅ RCO-1…RCO-6 spec | ✅ **SHIPPED 2026-04-24** (viz §12 níže) | Nic — regression watch |

> F1 + F2 = dvě největší stížnosti z logu. Obě už byly vyřešeny na master/této větvi. Regression watch je vše, co zbývá.
> F3 je **genuinely nový bug** — žádný plán ho nepokrývá.
> F4 / F5 / F6 jsou **plánovaná ale nedodělaná** práce různé hloubky.

---

## 1. F1 — Import error surface — **SHIPPED**

### 1.1 Co uživatel viděl
> "zerops_import failnul s `projectImportInvalidParameter`. Hláška neřekla které pole je špatně. Musel jsem si stáhnout `zerops_knowledge scope=infrastructure` a metodou pokus/omyl odhadnout, že kombinace `mode: NON_HA` + `objectStoragePolicy: private` na object-storage API odmítá."

### 1.2 Co je dnes
Všech osm workstreamů `api-validation-plumbing.md` je shipped (agent A status report):

| WS | Co | Evidence |
|---|---|---|
| W1 | `PlatformError.APIMeta` | `internal/platform/errors.go:68`, `zerops_errors.go:56-62` |
| W2 | Per-service `APIError.Meta` | `platform/types.go:146-149`, `zerops_search.go:68` |
| W3 | `convertError` emitting `apiMeta` | `tools/convert.go:60-62` |
| W4 | `Platform.Client.ValidateZeropsYaml` | `platform/zerops_validate.go:25-76` |
| W5 | Deploy flows pre-push validate | `ops/deploy_validate_api.go:27-76`, `deploy_ssh.go:130`, `deploy_local.go:117` |
| W6 | Delete client-side dup validation | `knowledge/versions.go` stub + `ops/import.go` callsites removed |
| W7 | Atom `develop-api-error-meta.md` + hostname format rule | `content/atoms/develop-api-error-meta.md`, `bootstrap-provision-rules.md:10-19` |
| W8 | Contract tests TA-06/TA-07 | `platform/errors_contract_test.go:23-123` |

LLM dnes dostane JSON shape (ověřeno v `content/atoms/develop-api-error-meta.md:17-33`):
```json
{ "code": "API_ERROR", "apiCode": "projectImportInvalidParameter",
  "apiMeta": [{"metadata": {"storage.mode": ["mode not supported"]}}] }
```

### 1.3 Root cause (původní) → fix
Dříve: `mapAPIError` nepropagoval `meta[]` z API response; LLM viděl jen top-level text.
Fix: plumbing od `decodeAPIMetaJSON` (plat­form) → `PlatformError.APIMeta` → `convertError` → MCP JSON. Plus instruction atom.

### 1.4 Co zbývá
Nic. Sledovat, jestli agenti opravdu `apiMeta` čtou — zatížit eval scénář s povinnou známkou "agent pochopil první pokus bez trial/error".

---

## 2. F2 — Git lifecycle / SSHFS ownership — **SHIPPED**

### 2.1 Co uživatel viděl
> "`/var/www/uploaddev` byl prázdný mount, napsal jsem kód. Deploy selhal s `fatal: not in a git directory`. Když jsem `git init` udělal z ZCP kontejneru, soubory `.git/` měly rozbité UID mapping přes SSHFS."

### 2.2 Co je dnes
- `internal/ops/service_git_init.go:24-51` — `InitServiceGit` běží `git init` **uvnitř cílového containeru přes SSH exec**, ne z mountu. Identita (`user.email`/`name`) se nastaví ve stejném atomu. Comment lines 12-13 uvádí proč: SFTP MKDIR by vytvořilo root-owned adresáře.
- `internal/ops/deploy_ssh.go:177-194` — GLC-2 safety-net `(test -d .git || (git init -q -b main && git config ...))` s identitou uvnitř OR větve (proti "fatal: empty ident name").
- `docs/spec-workflows.md` GLC-1…GLC-6 invariants (commit `f12fcfc`).
- E2E `TestE2E_InitServiceGit` (`e2e/bootstrap_git_init_test.go:40`) — **live ověřeno 2026-04-24 na `probe` service**, PASS.
- Eval scénář `bootstrap-git-init.md` zakazuje regex `cd /var/www/.+ && git init` + downstream chybové řetězce (`"fatal: empty ident name"`, `"dubious ownership"`, `"insufficient permission for adding an object"`).

### 2.3 Root cause → fix
Původně: bootstrap vytvořil mount, ale nechal `.git/` na agentovi. Agent ho vytvořil z mount-side (ZCP container) kde SFTP MKDIR přes SSHFS vytvoří root-owned `.git/objects/` (platforma "zembed SFTP MKDIR bug"). Fix: `InitServiceGit` post-mount přes SSH exec uvnitř target containeru; deploy safety-net pro migrace.

### 2.4 Co zbývá
Nic. Live e2e pass potvrzuje, že agent na `friction-git-service-lifecycle` branchi tuhle zkušenost neudělá.

---

## 3. F3 — `deployFiles paths not found: ./out` false positive — **NEW GAP**

### 3.1 Co uživatel viděl
> "`.deployignore` jsem napsal defenzivně a zahrnul tam `out/`. Prod setup ale právě `./out` chce deploynout. Výsledek: build proběhl, ale artifact byl prázdný. Varování `deployFiles paths not found: ./out` se objevilo i po opravě, kde deploy reálně uspěl — lehce matoucí (asi falešný pozitiv kontroly)."

### 3.2 Kód — call chain
`internal/ops/deploy_validate.go:86-100` (verifiedováno Read):
```go
// When cherry-picking (not "."), check each path exists.
if len(deployFiles) > 0 && !slices.Contains(deployFiles, ".") && !slices.Contains(deployFiles, "./") {
    var missing []string
    for _, df := range deployFiles {
        p := filepath.Join(workingDir, df)     // ← host filesystem, pre-build
        if _, err := os.Stat(p); err != nil {
            missing = append(missing, df)
        }
    }
    if len(missing) > 0 {
        warnings = append(warnings, fmt.Sprintf(
            "deployFiles paths not found: %s — these will be missing from the deploy artifact",
            strings.Join(missing, ", ")))
    }
}
```
Call sites:
- `internal/ops/deploy_ssh.go:125` — SSH push-dev deploy
- `internal/ops/deploy_local.go:110` — local git-push deploy
- `internal/tools/workflow_checks_deploy.go:96` — workflow pre-flight check

**Kritický detail**: kontroly role (`isDev`, `isStage`) jsou aplikovány řadě dalších warnings (run.start/healthCheck/readinessCheck), **ale NE na tuhle** (řádek 87 nemá `if isDev` ani `if isStage` gate). Takže warning letí i na stage / prod path.

### 3.3 Root cause — tři vrstvy

**Vrstva A — konceptuální neshoda**: `deployFiles` jsou **post-build cesty uvnitř kontejneru**, ne pre-build cesty na hostu. Pro build output artifacts (`dotnet publish -o out`, `vite build` → `./dist`, `go build -o ./bin/app`) cesta na hostu **z principu** neexistuje do té doby, než se build spustí. Kontrola `os.Stat` nad hostovým filesystémem je kategorická chyba.

**Vrstva B — duplicita se Zerops builderem**: Zerops builder sám emituje `WARN: deployFiles paths not found: dist` (vidět v `internal/ops/build_logs_test.go:121` a `internal/tools/deploy_ssh_test.go:243`). Takže máme **dvě nezávislé cesty stejného textu**:
- ZCP pre-flight (pre-build, host FS) → `DeployResult.Warnings`
- Builder post-build (post-build, container FS) → `FetchBuildWarnings` → `DeployResult.BuildLogs`

Plán `deploy-warnings-fresh-only.md` už řeší Vrstvu B (tag scoping I-LOG-2). Vrstva A zůstává nedotčena.

**Vrstva C — warning ulpívá**: `DeployResult.Warnings` se naplní na začátku deploy (`deploy_ssh.go:151`, `deploy_local.go:160`) a **není po úspěšném buildu filtrován**. `deploy_poll.go:39-66` při `status=ACTIVE` volá `FetchBuildWarnings` a zapisuje do `BuildLogs` — `Warnings` nechá na pokoji.

### 3.4 Precedent pro fix

Commit `d961012` (2026-03-03, `fix: suppress false-positive deploy warnings for implicit-webserver runtimes`):
- Zavedl `hasImplicitWebServer(runBase, buildBase)` detector
- Warnings `run.start`/`run.ports` empty se potlačí pokud runtime má built-in web server (php-nginx, nginx, static)
- Pattern: **obalit emisi warningu podmínkou, ne přidávat výjimky do detekce**

### 3.5 Tři fix varianty

**Varianta 1 — parse `buildCommands` pro odvození build outputs** (preferovaná):
```go
// internal/ops/deploy_validate.go - new helper
func inferBuildOutputs(buildCommands []string) []string {
    var outs []string
    patterns := []struct{ flag, re string }{
        {"-o ", `^\S+\s+publish\s+.*-o\s+(\S+)`},         // dotnet publish -o out
        {"--output ", `--output[\s=]+(\S+)`},              // npm/bun build --output
        {"--outdir ", `--outdir[\s=]+(\S+)`},              // bundlers
    }
    // match every command, collect target paths
    ...
}
// then in ValidateZeropsYml:
inferred := inferBuildOutputs(entry.Build.BuildCommands)
for _, df := range deployFiles {
    if slices.Contains(inferred, strings.TrimPrefix(df, "./")) { continue }
    // existing stat check
}
```
Tradeoff: každý nový framework vyžaduje regex; pokrytí není úplné. Ale pokryje 80 % případů (dotnet, node, bun, esbuild).

**Varianta 2 — vypnout check pokud chybí cesta ale existuje `buildCommands`** (simplest):
```go
if len(missing) > 0 && len(entry.Build.BuildCommands) == 0 {
    // emit warning only when no build step plausibly creates the path
    warnings = append(warnings, ...)
}
```
Tradeoff: ztratí se validace pro opravdu chybějící cesty v projektech s `buildCommands`. Ale v praxi: pokud má zerops.yaml `buildCommands`, pravděpodobně ví co dělá; pokud je nemá (statický nebo source deploy), check dává smysl.

**Varianta 3 — filtrovat warnings po úspěšném buildu** (nejslabší):
```go
// deploy_poll.go po status=ACTIVE
result.Warnings = slices.DeleteFunc(result.Warnings, func(w string) bool {
    return strings.Contains(w, "deployFiles paths not found")
})
```
Tradeoff: string-match je křehký; řeší symptom, ne příčinu.

**Doporučení**: Varianta 2 jako první fix (simplest correct solution per CLAUDE.md). Varianta 1 až tehdy, kdyby se ukázaly false negatives.

### 3.6 Test strategie

```go
// internal/ops/deploy_validate_test.go
func TestValidateZeropsYml_DeployFilesMissingButHasBuildCommands(t *testing.T) {
    cases := []struct {
        name          string
        yaml          string
        wantWarning   bool
    }{
        {"dotnet publish + ./out cherry-pick, ./out missing, buildCommands present",
         `zerops:
           - setup: api
             build:
               base: dotnet@9
               buildCommands: [dotnet publish App.csproj -c Release -o out]
               deployFiles: [./out]
             run: {ports: [{port: 8080}], start: dotnet App.dll}`, false},
        {"./out missing AND no buildCommands → warn",
         `zerops:
           - setup: api
             build: {base: static, deployFiles: [./out]}
             run: {base: nginx}`, true},
    }
    // ... assert
}
```

### 3.7 File budget
- `deploy_validate.go`: +3 LOC gate + jedna helper function
- `deploy_validate_test.go`: +60 LOC tests
- Žádné změny v `deploy_ssh.go`/`deploy_local.go`/`deploy_poll.go`

---

## 4. F4 — .NET recipe tilde extraction — **PARTIAL**

### 4.1 Co uživatel viděl
> "Po přidání HTML formuláře: stage vracel 404 na `/`. Příčina: `deployFiles: - ./out` zachovalo adresář, takže `wwwroot` skončilo na `/var/www/out/wwwroot`, ale ASP.NET ContentRootPath byl `/var/www`. Fix byl v znalostní bázi celou dobu — `./out/~` (tilde) extrahuje obsah do `/var/www/`."

### 4.2 Plán P2.4 (friction-root-causes.md:348-367)
1. Edit dotnet-hello-world přes `zcp sync pull/push`:
   - `deployFiles: - ./out` → `deployFiles: - ./out/~`
   - `run.start: dotnet ./out/app/App.dll` → `run.start: dotnet App.dll`
   - Add note: *"Publish output extraction — `./out` preserves the directory (artifacts land at `/var/www/out/`). `./out/~` extracts contents (artifacts land at `/var/www/`). Choose extraction when the runtime expects assets like `wwwroot/` at the application's ContentRootPath."*
2. Add git-init atom guidance (jednověta proti mount-side `git init`).
3. **Explicitně zamítá** nový runtime-specifický atom: "axis model has no runtime-version granularity."

### 4.3 Stav dnes (agent C ověřil)

| Položka | Status | Evidence |
|---|---|---|
| `deployFiles: ./out/~` | ❌ stále `./out` | `internal/knowledge/recipes/dotnet-hello-world.md:31` (po `sync pull`) |
| `run.start: dotnet App.dll` | ❌ stále `dotnet ./out/app/App.dll` | tentýž soubor |
| Tilde extraction note | ❌ chybí | tentýž soubor |
| Git-init mount-side atom guidance | ✅ shipped | `internal/content/atoms/develop-first-deploy-write-app.md:43-50` |

### 4.4 Root cause
Recipe content žije **mimo git** — v Zerops API (Strapi CMS), pulled per `zcp sync pull`. P2.4 tasks na atomu/guidance proběhly (jsou v gitu), ale recipe edit nikdo nedotáhl do Strapi přes `zcp sync push`. Je to bezhlavá práce na cizí systém.

### 4.5 Actionable kroky (pořadí)
```bash
# 1) pull latest
zcp sync pull recipes dotnet-hello-world

# 2) edit internal/knowledge/recipes/dotnet-hello-world.md:
#    - line 31: deployFiles: [./out] → [./out/~]
#    - line 73: start: dotnet ./out/app/App.dll → start: dotnet App.dll
#    - add note block after deployFiles about tilde extraction semantics

# 3) push (creates PR on zerops-recipe-apps repo)
zcp sync push recipes dotnet-hello-world

# 4) after PR merge:
zcp sync cache-clear dotnet-hello-world
zcp sync pull recipes dotnet-hello-world    # verify
```

### 4.6 Optional follow-up
Audit dalších recipe které produkují build output a mají static assets:
- `go-hello-world` (pokud má embed.FS static) — asi není potřeba
- `nextjs-hello-world` (SSR) — zvláště pokud `.next/` má static layer
- `spring-boot`, `quarkus` — pokud JAR má META-INF resources

Matice recipes × "má static assets" × "potřebuje tilde" by měla být v `plans/friction-root-causes.md` P2.4 jako šířka pokrytí. Dnes je jen .NET explicitně.

---

## 5. F5 — Adaptive guidance envelope — **NOT STARTED**

### 5.1 Co uživatel viděl
> "Init odpověď develop workflow je obří — hromada 'atom' sekcí (env var channels, diagnostika, SSHFS pravidla, první deploy branch, verifikační matice…). Užitečné, ale hutné."

### 5.2 Plán P3 (friction-root-causes.md:426-510)
Adaptivní envelope:
- Add `ServiceMaturity` (first-run / edit-loop) do `ServiceSnapshot`.
- Derive z `WorkSession.Verifies[hostname]` (last-verify passed → edit-loop).
- Axis `maturity: [first-run, edit-loop]` v `AxisVector` + filter v `atomMatches`.
- Migrace `develop-verify-matrix.md` na `maturity: [first-run]`; pointer atom pro edit-loop.
- Mode 5 v `zerops_knowledge`: `key=<atom-id>` pro on-demand full body.
- Target: first-run ≤ 20 KB, edit-loop ≤ 14 KB.

### 5.3 Stav dnes (agent B)

**0 z 8 items shipped** — žádné reference na `ServiceMaturity`, `MaturityFirstRun`, `MaturityEditLoop` v kódu. `AxisVector` má 10 axes (Phases, Modes, Environments, Strategies, Triggers, Runtimes, Routes, Steps, IdleScenarios, DeployStates), **ne `maturity`**. Žádný `develop-verify-matrix-pointer.md`. `knowledge.go` má 4 modes (query, runtime/services briefing, scope=infrastructure, recipe), **ne mode 5**. Žádný `brief_size_test.go`. Žádný scénář `develop-verified-service-shorter-brief.md`.

### 5.4 Baseline měření (ověřeno dnes)
- 46 develop-* atomů v `internal/content/atoms/`
- Celková velikost develop-* atomů: **60 744 B ≈ 60 KB**
- 4 top "velké" atomy:
  - `develop-verify-matrix.md` — 3 506 B (primární kandidát na maturity split)
  - `develop-platform-rules-container.md` — 1 962 B (sekundární kandidát)
  - `develop-platform-rules-common.md` — 1 391 B
  - `develop-env-var-channels.md` — 1 261 B

**Pozor**: 60 KB je celá corpus velikost, ne velikost jednoho envelope. Envelope se filtruje přes `atomMatches` podle axes. Baseline z plánu (~18 KB per scénář) nicméně konzistentně překračuje target (14 KB edit-loop).

### 5.5 Root cause
`compute_envelope.go` a `synthesize.go` jsou čistě axis-driven — k sadě atomů které matchují axis kombinaci **neexistuje second-layer trim** podle toho, jestli agent už něco verifikoval. Každý develop-active turn dostane stejnou payload velikost bez ohledu na historii. `WorkSession.Verifies[]` se sbírá (`compute_envelope.go:369-372`), ale nikdo z ní ještě neodvozuje maturity.

### 5.6 Blokéři
- **W0 eval framework size assertion support** — plan říká "P3 blocked on W0". W0 potřebuje scénář-level `MaxBriefBytes` field + runner check. Bez toho nelze P3 `brief_size_test.go` gate.
- Atom corpus invariants (`corpus_coverage_test.go:207-209`) require `MustContain` pro některé atomy; P3 musí je respektovat nebo upravit.

### 5.7 Priority odhad
P3 je největší work-package z friction-root-causes.md. Správně sekvencovaně (W0 → P3). **Nezabývat se bez slotu na plný workstream.** Pro uživatelský payoff (20-25 % redukce) je to dobrá investice, ale ne urgent.

---

## 6. F6 — Recipe hostname collision → rename offer — **SHIPPED 2026-04-24**

### 6.1 Co uživatel viděl
> "Recipe má kolizi s existujícími hostname. Jdu classic route s vlastním plánem — .NET dev+stage pár a S3 object storage."

Ztrátu recipe (import YAML + metadata + cross-service wiring) přešel bez návrhu alternativy. Agent pracoval kolem klasickou cestou.

### 6.2 Klíčové zjištění (live-ověřeno na eval-zcp)

Zerops import YAML odděluje `hostname` od `zeropsSetup`:

```yaml
services:
  - hostname: appdev          # LIBOVOLNÉ — lze přejmenovat
    type: dotnet@9            # IMMUTABLE — binds recipe k app repu
    zeropsSetup: dev          # IMMUTABLE — label do repo's zerops.yaml
    buildFromGit: https://... # IMMUTABLE — repo URL
```

Runtime service lze přejmenovat bez cascading edits, protože repo's `zerops.yaml` referencuje `setup: dev`/`setup: prod` labely (NE hostnames). Managed service (`db`) ale přejmenovat nelze — repo's `${db_*}` env refs by přestaly resolvovat.

Empirické ověření (eval-zcp, 2026-04-24): platforma přijala `hostname: renametest` + `zeropsSetup: prod` + recipe `buildFromGit`; `stack.create` a `stack.build` proběhly (2. import s kolidujícím hostname vrátil `serviceStackNameUnavailable` s `meta.name: [renametest]`). Rename je tedy platform-native mechanismus, nepotřebujeme nic nového.

### 6.3 Root cause (původně)

**Dvojité selhání**:
1. **Architektonicky**: neexistoval hostname-substitution layer v provision pipeline — recipe YAML šel do `zerops_import` verbatim.
2. **Kontentem**: atom `bootstrap-recipe-match.md:22` říkal *"never rename"* zatímco `runtimeCollisionError()` nabízel *"or rename the target"*. Kontradikce paralyzovala LLM.

### 6.4 Implementované řešení

**Single-path design — žádné feature flagy, žádná backward-compat větev** (per CLAUDE.local.md):

| Vrstva | Co se změnilo | Soubor(y) |
|---|---|---|
| **Rewrite funkce** | Nová `RewriteRecipeImportYAML(recipe, plan)` — bere recipe YAML + submitted plan, mapuje runtime služby podle `(type, zeropsSetup→role)` na `target.DevHostname`/`target.ExplicitStage`, managed služby matchuje podle `type` na `Dependency.Hostname` (musí sedět s recipe), a při `Resolution=EXISTS` drop-uje managed entry z výstupu. | `internal/workflow/recipe_override.go` (220 LOC) + test (200 LOC) |
| **Plan-submit pre-flight** | `BootstrapCompletePlan` volá rewrite jako probe po `ValidateBootstrapTargets`. Jakýkoliv problém (managed rename, runtime type mismatch, YAML parse) zmaří plan s přesnou chybou. | `internal/workflow/engine.go:547-560` |
| **Provision guide injection** | `buildGuide(StepProvision)` injektuje rewrittenní YAML; `StepDiscover` stále verbatim (plan ještě neexistuje). | `internal/workflow/bootstrap_guide_assembly.go:58-79` |
| **Atom guidance** | `bootstrap-recipe-match.md` plně přepsán — vysvětluje hostname vs zeropsSetup model, happy path (žádné kolize), 2 recovery cesty (rename runtime, adopt managed), a co je immutable. | `internal/content/atoms/bootstrap-recipe-match.md` |
| **Doprovodné atomy** | `bootstrap-recipe-import.md` zmíní, že YAML už je rewritten (nenabízí agentovi další edit). `bootstrap-route-options.md` popisuje rename+adopt jako recovery místo "ask the user". | dva atomy |
| **Error string** | `runtimeCollisionError()` sjednoceno — nabízí adopt+rename bez zmiňování nepodporovaného mechanismu. | `internal/workflow/validate.go:313-330` |
| **Eval scénáře** | `bootstrap-recipe-collision.md` tighten na hard-gate (`forbiddenPatterns: serviceStackNameUnavailable`). Nový `bootstrap-recipe-collision-rename.md` pro happy rename path s fixture `runtime-hostname-collision.yaml`. | 2 + 1 |
| **Spec invariants** | RCO-1…RCO-6 v `docs/spec-workflows.md §8` — plan-submit pre-flight, immutable fields, managed immutability, EXISTS drop, discover-verbatim-provision-rewritten, slot matching. | `docs/spec-workflows.md:1137-1152` |

**TDD proof**: 11 cases v `TestRewriteRecipeImportYAML` + 3 wiring cases v `bootstrap_guide_assembly_test.go` + 2 engine cases v `engine_test.go` → všechny green. `go test ./... -count=1 -race` a `make lint-local` plně čisté.

### 6.5 Co mohou agenti dělat teď

Při kolizi (běžný scénář: existující Python weather app s `appdev`/`appstage`, user chce přidat .NET upload službu):

```json
// Plan submission:
{
  "plan": [{
    "runtime": {
      "devHostname": "uploaddev",          // nekolidující
      "stageHostname": "uploadstage",       // nekolidující
      "type": "dotnet@9",                   // recipe verbatim
      "bootstrapMode": "standard"           // recipe verbatim
    },
    "dependencies": [
      {"hostname": "storage",               // NE "db" — recipe má object-storage
       "type": "object-storage",
       "resolution": "CREATE"}
    ]
  }]
}
```

ZCP rewrituje recipe YAML na:
```yaml
services:
  - hostname: uploaddev                     # <- přepsáno z appdev
    type: dotnet@9                          # <- recipe verbatim
    zeropsSetup: dev                        # <- recipe verbatim
    buildFromGit: https://...               # <- recipe verbatim
  - hostname: uploadstage                   # <- přepsáno z appstage
    type: dotnet@9
    zeropsSetup: prod
    buildFromGit: https://...
  # managed services dle plan.Dependencies
```

Agent vidí rewrittenní YAML v provision guidance a kopíruje ho do `zerops_import`. Žádná editace v hlavě, žádné collision error, žádný pivot na classic route.

### 6.6 Limity (dokumentováno v RCO-3)

Managed-service rename není podporován — vyžadoval by fork recipe app repo a edit jeho `zerops.yaml`. Pokud kolize je na managed jiného typu než recipe potřebuje (např. existující `db: mariadb` vs recipe chce `postgresql`), použije se `route="classic"`. Tento případ má vlastní error path přes RCO-1 pre-flight.

---

## 7. Prioritizace — next actions

| # | Akce | Složitost | Payoff | Pořadí |
|---|---|---|---|---|
| W-F3 | deployFiles false-positive fix (Varianta 2: check buildCommands) | S (+3 LOC + test) | H — ruší shmajchlovaný warning každému .NET/Node/Go prod deploy | **1** |
| W-F4 | Dotnet recipe sync push (`./out/~` + tilde note) | XS (edit + sync push + cache-clear) | H — eliminuje exact friction bod z logu | **2** |
| ~~W-F6~~ | ~~Recipe hostname collision fix~~ | ~~M~~ | ~~H~~ | ✅ **SHIPPED 2026-04-24** — viz §6 |
| W-F5 | Adaptive envelope (P3 + W0) | L (multi-file plan) | M — 20-25 % redukce baseline velikosti | **3 (slot work)** |
| — | F1, F2 regression watch | — | — | automatické |

### 7.1 Co z tohoto dělat **v jednom commit-session**
W-F3 + W-F4 + W-F6L1. Všechny tři jsou orthogonal, malé, s jasným testem. Celkově ~1-2 hodiny práce včetně test writing a e2e smoke.

---

## 8. Root cause patterns napříč friction points

Dva vzorce, které se opakují:

**Pattern 1 — Check neverifikuje správný filesystem/lifecycle fázi**:
- F2: `git init` z ZCP mountu (špatný filesystem — SSHFS, ne container native) → fix = exec v target container přes SSH
- F3: `deployFiles paths` check na hostu (špatná fáze — pre-build, ne post-build) → fix = parse `buildCommands` nebo gate podle jejich existence

Generalizace: **validace musí běžet na stejném filesystému a ve stejné lifecycle fázi jako produkční kontext**. Jakmile validace předběhne nebo šilně zanede, false positives jsou nevyhnutelné.

**Pattern 2 — Guidance a validace si odporují**:
- F6: atom "never rename" vs. `runtimeCollisionError()` "or rename the target"
- F4: knowledge má `./out/~` pro static runtime, ale dotnet recipe používá `./out` bez vysvětlení
- F1 (historická): atoms tvrdily "tool warns you about invalid mode" ale API-deferred validator by ten warning nikdy neemitoval (W7.2 `grep "Invalid parameter\|mode required"` audit)

Generalizace: **každá claim v atomu musí mít odpovídající test/invariant v kódu**. `api-validation-plumbing.md` W8 TA-06/TA-07 je dobrý krok; `friction-root-causes.md` P2.5 atom ↔ code contract framework by zobecnil tenhle problém napříč celou corpus.

---

## 9. Evidence index

### Code anchors (ověřené 2026-04-24)
- `internal/ops/deploy_validate.go:77-99` — deployFiles check, role gates
- `internal/ops/service_git_init.go:24-51` — container-side git init
- `internal/ops/deploy_ssh.go:177-194` — GLC-2 safety-net
- `internal/ops/deploy_ssh.go:130` + `deploy_local.go:117` — W5 pre-push validate
- `internal/platform/zerops_errors.go:56-62,94-101` — W1 APIMeta
- `internal/platform/zerops_validate.go:25-76` — W4 ValidateZeropsYaml
- `internal/tools/convert.go:60-62` — W3 apiMeta surface
- `internal/content/atoms/develop-api-error-meta.md` — W7 atom
- `internal/content/atoms/bootstrap-provision-rules.md:10-19` — W7 hostname format
- `internal/content/atoms/bootstrap-recipe-match.md:22` — F6 contradiction
- `internal/content/atoms/develop-first-deploy-write-app.md:43-50` — F4 git-init guidance
- `internal/platform/errors_contract_test.go:23-123` — W8 contract tests
- `internal/workflow/route.go:260` — recipe collision detection
- `internal/workflow/validate.go:219,313` — collision → force rename/adopt
- `internal/knowledge/recipes/dotnet-hello-world.md:31,73` — still `./out` + `dotnet ./out/app/App.dll`

### Live evidence
- `TestE2E_InitServiceGit` — PASS 2026-04-24 na `probe` service v eval-zcp (ZCP_E2E_GIT_INIT_SERVICE=probe, 0.88s)
- `go test ./internal/ops/... -run TestValidateZeropsYml` — PASS (existing suite bez coverage nového scénáře)
- `zcp sync pull recipes dotnet-hello-world` — recipe pulled, P2.4 recipe edits nejsou ve Strapi/API
- 46 develop-* atoms; 60 744 B total

### Measurement baselines
- Dotnet: `deployFiles: [./out]` → host `/var/www/uploaddev/out` neexistuje do `dotnet publish -o out` → warning fires
- Recipe collision: blocks plan submit via `runtimeCollisionError()`; eval `bootstrap-recipe-collision.md` treats soft-goal
- Atom corpus: 46 develop-* atoms totaling ~60 KB; no maturity axis

---

## 10. Open questions

1. **F3 varianta**: Chceme parse `buildCommands` (Varianta 1, přesnější) nebo gate na jejich existenci (Varianta 2, simpler)? Doporučení: Varianta 2 first-cut; Varianta 1 jako evoluce když Varianta 2 bude produkovat false negatives.
2. **F4 scope**: Audit všech recipes pro "má static assets + build output" (beyond dotnet) — udělat samostatný pass nebo nechat na "come back when it bites"?
3. **F5 sequencing**: Kdy pustit W0 + P3? Závislosti podle friction-root-causes.md mapují P3 jako druhý (po P1 + P2 konsolidaci). W0 eval size framework je nutný předpoklad.
4. **F6 L2 poptávka**: Je collision opravdu častý blocker? Pokud ano, L2 rename mechanism stojí za investici. Pokud ne, L1 content fix stačí.
5. **Cross-friction generalizace**: Implementovat "atom ↔ code contract" framework z P2.5 jako preventivní krok proti dalším F1/F4/F6 typům contradictions?

---

## 11. Appendix — tools použité pro audit

- 5× `Agent subagent_type=Explore` pro paralelní deep-dive (api-validation status, P3 status, dotnet recipe status, deployFiles root cause, recipe collision design)
- `zcp sync pull recipes dotnet-hello-world` — potvrdit stav Strapi recipe
- `TestE2E_InitServiceGit` — live git lifecycle ověření na `probe@eval-zcp`
- `go test ./internal/ops/...` — baseline test suite stav
- `wc -c internal/content/atoms/develop-*.md` — atom corpus sizing
- `grep -rn "deployFiles paths not found"` — find all warning emission sites
- `git log` + `git show d961012` — pattern precedent (implicit-webserver suppression)
- Čtení: `plans/friction-root-causes.md` (950 LOC), `plans/api-validation-plumbing.md` (722 LOC), `plans/develop-flow-enhancements.md` (85 LOC)
