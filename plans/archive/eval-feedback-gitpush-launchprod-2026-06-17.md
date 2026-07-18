# Plán: oprava nálezů ze dvou launch-production evalů (git-push + přechod do produkce)

> **STAV 2026-06-17** (branch `fix/eval-gitpush-launchprod-feedback`, necommitnuto, `go test ./... -short` + `lint-local` zelené, race-clean):
> - ✅ **F1a** git-push dirty-tree pravda (+ parita, 2 lhoucí TELL stringy, neříká „Push landed") — RED→GREEN testy, **live-ověřeno eval2**
> - ✅ **F1b** recipe shallow repo: detekce + auto-unshallow + fallback blocker + nedestruktivní origin — testy, **live-ověřeno**
> - ✅ **F1c** object-graph/shallow push chyba = vlastní failure class (ne network) — test
> - ✅ **F2** token guidance: single-owner `ghPATScopeRecommendation` helper + 6 Go sites + 2 atomy + `Actions:Read`/`Checks:Read` + settings link + drift test — **scopes live-ověřeny eval2**
> - ✅ **F3** dev_server: probe-success ověří liveness spawnnutého PID (port_in_use) + doc-drift `detectPostSpawnCrash` — testy, **live-ověřeno**
> - ✅ **F4c** prod-ops `enable-subdomain` op (+ ProjectAdminClient metoda + mock) — test
> - ✅ **F5 částečně**: cleanedUpOrphanMetas empty-string guard (test); prod-ops `pipeline.configured` family-aware (#7); doc-drift „restarts it" + „gh auth status" + subdomain „prod opt-in"
> - ⏳ **ZBÝVÁ** (deep launch-composer / bootstrap-render): **F4a** prefer existující `prod` setup; **F4b** `prodDelivery` decouple od source meta (design-call); **F5** durability warning na bootstrap plan-stepu (#4); **F5** DB preview label single+HA (p2 #7)



Zdroj: `.zcp/manual/p.txt` (classic Bun, simple→stage→git-push→prod) + `.zcp/manual/p2.txt` (recipe Laravel, adopt→git-push→prod). Plná analýza: `.zcp/manual/p-analysis.md`. Ověřeno: vlastní grounding + 10-agentní tým + Codex gpt-5.5 + **live repro na eval-zcp kontejneru** (#1, #2, p2#1).

**Sjednocující vzor všech fází:** ZCP na citlivých místech (deploy, dev-server, git-setup, prod setup) **hlásí klidový/úspěšný stav, který neodpovídá realitě** — nebo destruktivně zasáhne stav, který sám nezná. Fix napříč = „nástroj říká pravdu o tom, co reálně udělal, a nesahá nevratně na to, čemu nerozumí."

| Fáze | Téma | Dnes → Po opravě | Závažnost |
|---|---|---|---|
| 1 | git-push říká pravdu + bezpečné recipe repo | „nothing to push" na necommitnutém souboru / smazaný recipe origin → varování + nedestruktivní origin + shallow detekce | 🔴 blokuje reálné uživatele |
| 2 | GitHub token guidance (tvůj request) | rozsypané/driftující scope stringy → jeden owner, kompletní scopes, link | 🔴 + explicitní request |
| 3 | dev_server říká pravdu | running:true přes cizí listener → kontrola liveness spawnnutého PID | 🟠 false-positive health |
| 4 | launch-production korektnost | dev setup do prod / source mutace / nelze zapnout prod URL → prefer prod blok + prodDelivery + enable-subdomain op | 🟠 tichý prod risk |
| 5 | clarity + quick wins | durability warning pozdě / matoucí preview / kosmetika / doc drift | 🟡 |

---

## Fáze 1 — git-push říká pravdu + bezpečné zacházení s recipe repem `[🔴]`

Nejdůležitější: tady reálný uživatel **uvázne** (p2) nebo **tiše nenasadí** (p.txt).

### 1a. NOTHING_TO_PUSH na špinavém working tree (p.txt #1) — LIVE OVĚŘENO
- **Dnes:** agent zapíše soubor, `strategy=git-push` → `"status":"NOTHING_TO_PUSH","message":"remote is up to date"`, soubor mlčky nikam nejde. Live repro: preflight projde (HEAD existuje), `git push` → "Everything up-to-date", `git status --porcelain` → `?? .github/`.
- **Proč to bolí:** nejcitlivější potvrzení deploye („vše venku") lže. Recovery jen ruční SSH `git add/commit`.
- **Po opravě:** před pushem (sloučit do round-tripu `committedCodeCheckCmd`, `deploy_git_push.go:319`) spustit `git status --porcelain`; pokud dirty → varování pojmenující soubory + „git-push přenáší jen committed HEAD; commitni na kontejneru první". Firne **bezpodmínečně** (PUSHED i NOTHING_TO_PUSH). Sdílený helper pro container i local path (dnes jen local varuje, `deploy_local_git.go:217`). Na NOTHING_TO_PUSH neříkat „Push landed" (`deploy_git_push.go:485/554/582`).
- **Trade-off:** NEpoužít self-eval návrh `git add -A` (vzkřísil by schválně zrušený auto-commit, `deploy_git_push.go:311-318`, rozbil `TestBuildGitPushCommand_Basic`). Jen read-only probe.
- **Navíc:** opravit DVĚ lhoucí TELL kopie — atom `setup-git-push-container.md §2` A `launch_source_control_gate.go:658` („the deploy tool refuses to push a dirty tree" — neodmítá).

### 1b. git-push-setup ničí recipe origin + nezná shallow clone (p2 #1) — LIVE OVĚŘENO
- **Dnes:** recipe služba má shallow clone (`.git/shallow`, chybějící delta-base objekt) s origin na recipe remote. `BuildGitOriginSyncCommand` (`git_auth_probe.go:68`) udělá `git remote set-url origin <user repo>` → **přepíše recipe origin**. Live repro: po set-url je recipe remote pryč. Když je clone shallow, zmizí jediná cesta k `--unshallow` → jediný fix = přepsat historii (nevratné). Běžný uživatel uvázne.
- **Proč to bolí:** ZCP nevratně zničí stav, kterému nerozumí. Shallow se **nikde v kódu neřeší** (ověřeno `rg`).
- **Po opravě:** (1) detekovat `.git/shallow` v git-push-setup → buď `git fetch --unshallow` z původního origin PŘED jeho přepsáním, nebo tvrdý blocker s vysvětlením. (2) Neměnit origin destruktivně — buď přidat nový remote (`zerops-origin`), nebo zazálohovat původní URL do meta před set-url.
- **Trade-off:** unshallow může být velký fetch; blocker je bezpečnější default, auto-unshallow lepší DX. → tvoje volba (viz gate).

### 1c. Object-graph/shallow push chyba klasifikovaná jako `network` (p2 #2)
- **Dnes:** push selže na `index-pack failed / did not receive expected object`, ale `failureClassification: network` + suggestions `git pull --rebase` / fresh PAT / restart — vše vedle. Skutečná příčina (lokálně chybějící objekt) v seznamu není.
- **Po opravě:** nová failure třída object-graph/shallow (rozpoznat `index-pack failed`, `did not receive expected object`, `missing object`) → suggestion na `git fsck` + unshallow, ne network recovery. Owner: `deploy_failure_signals.go`.
- **Trade-off:** žádný — dnes posílá diagnostiku špatným směrem.

---

## Fáze 2 — GitHub token guidance (TVŮJ EXPLICITNÍ REQUEST) `[🔴]`

- **Dnes:** scope-doporučení rozsypané v **6 Go sites + 2 atomech, už driftuje** — `ghPatRecommendation` (`build_integration.go:345`) + actions atom říkají jen `Secrets`; jiné `Contents+Secrets+Workflows`. Nikde link, nikde scope na sledování běhu. V transcriptech to spadlo 2×: p.txt na chybějícím `Workflows` (push `.github/workflows/`), p2 na chybějícím scope pro `gh run watch`.
- **Proč to bolí:** porušení „one owner per concept" (CLAUDE.md) → driftuje a podsouvá nedostatečný token. Round-trip s člověkem.
- **Po opravě:** jeden owner `ghPATScopeRecommendation(family, ownerRepo) string` → proroutovat všech 6 Go sites + atomy derivovat z něj + drift test. Obsah (web-ověřeno docs.github.com):
  - **git-push only:** `Contents: Read and write`
  - **actions track:** + `Workflows: Read and write` (push `.github/workflows/`) + `Secrets: Read and write` (`gh secret set`) + `Actions: Read` (`gh run list`) + `Checks: Read` (`gh run watch` anotace — p2 #4)
  - **vždy link:** `https://github.com/settings/personal-access-tokens` (ne `?type=beta`)
  - Primárně dotáhnout `prodCDActionsBlock` (`workflow_launch_production.go:1920-1924`) — nemá scope vůbec, tam to v p.txt spadlo.
- **Trade-off:** „validation set ≠ presentation set" — `deploy_failure_signals.go:638` (repo-not-found) nechat jen Contents (správná failure class), nepřidávat actions scopes.

---

## Fáze 3 — dev_server říká pravdu `[🟠]` — LIVE OVĚŘENO

- **Dnes:** `running:true, healthStatus:200`, ač spawnnutý proces spadl na EADDRINUSE — probe dostal 200 od cizího listeneru. Live repro: cizí listener na :3000 → probe 200, spawnnutý PID DEAD. No-probe větev přitom `checkProcessAlive` (kill -0) dělá.
- **Po opravě:** po probe-success **také** `checkProcessAlive(pidFileFor(logFile))` (`dev_server_start.go:164`); probe prošel + PID mrtvý → `running:false, reason:port_in_use`. Konzistentní s pidfile mechanismem (NE log-scan, který komentář `:287-294` odmítá).
- **Navíc (doc drift):** `tools/dev_server.go:79` + `ops/dev_server.go:94` popisují `detectPostSpawnCrash`, který **neexistuje** → přepsat na „kill -0 na pidfile".
- **Trade-off:** žádný — dnes maskuje reálný crash.

---

## Fáze 4 — launch-production korektnost `[🟠]`

### 4a. Launch defaultuje `dev` setup do produkce (p2 #3)
- **Dnes:** zerops.yaml má `setup: prod`, ale bundle vyjde `setup:"dev"` (`setupProvenance: dev-setup-promoted`), zatímco CI pipeline běží `--setup prod`. `resolveLaunchSetupName` (`launch_promotables.go`) bere setup z meta polí, **nekontroluje existující `prod` blok** v zerops.yaml. Vnitřní nekonzistence; prod by se stavěl dev receptem.
- **Po opravě:** když v zerops.yaml existuje blok jménem `prod` (nebo `ProdSetupNameOverride`), má přednost před dev-promoted; jinak blocker s explicitní volbou, ne warn schovaný v textu.

### 4b. Produkční delivery odvozena ze source meta (p.txt #3)
- **Dnes:** `launchDeliveryFamily` (`workflow_launch_production.go:1780`) bere `meta.BuildIntegration` zdroje → aby agent odemkl prod actions, musí překlopit SOURCE na actions (proti volbě uživatele push-only) + vygeneruje nechtěný source-CI workflow.
- **Po opravě:** launch input `prodDelivery=actions|webhook` řídí prod CD přímo, fallback na source meta jen když unset.
- **Trade-off:** poruší „single-owner-of-delivery-family" princip → **design call (gate)**.

### 4c. Prod HTTP exposure nejde zapnout z workflow (p.txt #5)
- **Dnes:** P-PROD-2 stripuje subdomain; `prod-ops` nemá op; `zerops_subdomain` váže na source projekt → finální smoke test nutně ruční klik v dashboardu.
- **Po opravě:** `prod-ops prodOperation=enable-subdomain` (přidat `GetService`+`EnableSubdomainAccess` na `ProjectAdminClient` — obě už jsou na base clientu). Default OFF zůstává, jen consented opt-in skrz ZCP.
- **Trade-off:** dotýká se záměrného off-by-default → **design call (gate)**, ale loop jinak nelze uzavřít.

### 4d. Prod setup šel „sight-unseen" (p2 #5)
- **Dnes:** gate jen warnuje, že prod setup nebyl ověřen zeleně. **Po opravě:** nabídnout aktivní verify deploy na stage jako součást flow. (Lehčí, lze odložit.)

---

## Fáze 5 — clarity + quick wins `[🟡]`

| Item | Dnes | Fix |
|---|---|---|
| Durability warning (p.txt #4) | simple→standard shodí živou URL, warning až post-deploy | warning v atomu `develop-mode-expansion.md` / na plan-stepu |
| DB preview label (p2 #7) | `type: postgresql:single@18` + `mode:"HA"` zároveň | preview ukázat cílový typ (`postgresql:ha@18`) |
| prod-ops contradiction (p.txt #7) | `pipeline.configured:false` vedle `done:true` | family-aware projekce |
| cleanedUpOrphanMetas (p.txt #10b) | `[""]` empty-string artifact | one-line guard `if m.Hostname==""` |
| Doc drift (Codex) | `detectPostSpawnCrash`, „restarts it", `gh auth status`, subdomain „prod opt-in" | sladit 4 stale stringy |
| Benign warnings (p2 #8) | „grant self ADMIN... expected" jako warning, fstab/etcd noise | filtr/anotace „benign" |
| Async wait idiom (p.txt #9/p2 #9) | jen polling prod-ops | guidance „polling JE idiom" + Actions/Checks scope |

**Backlog (design, ne teď):** no-DB intent recipe ranking (p.txt #10a, negative-intent parsing); stateless re-pass všech inputs (p2 #6, server-side default).

---

## Footer

**Co se zahodilo:**
| Původně (self-eval) | Verdict | Důvod |
|---|---|---|
| #1 fix `git add -A` | zahozeno | vzkřísil by zrušený auto-commit, rozbil test |
| #2 fix „scan logTail" | zahozeno | komentář to odmítá; pidfile liveness je správný mechanismus |
| SSHFS race (p.txt #1) | red herring | i při nulové latenci je soubor untracked → committed-HEAD push ho nepřenese |

**Pořadí + gates:** Fáze 1 (blokuje uživatele) → 2 (tvůj request) → 3 → 4 → 5. Po každé fázi `go test ./... -short` + příslušné pinning testy; #1/#2/p2#1 mají live repro jako acceptance.

**Backward compat:** vše interní (ZCP response shapes, atomy, Go strings). `prodDelivery` input je aditivní (fallback na dnešní chování). Žádný uživatelský on-disk seam se nemění.

**Effort (hrubě):** F1 ~250 LOC (3 sub-fixy + testy), F2 ~150 LOC (helper + 6 sites + drift test + golden regen), F3 ~60 LOC, F4 ~300 LOC (4 sub-fixy, 2 design), F5 ~120 LOC. Celkem ~880 LOC / ~3-4 dny, fázovatelné.

**Design calls pro tebe (gate):**
1. **1b shallow recipe repo:** auto-`unshallow` (lepší DX, velký fetch) vs tvrdý blocker (bezpečnější)?
2. **4b prodDelivery:** decouplovat od source meta (poruší single-owner princip), nebo nechat + jen hlasitě varovat?
3. **4c prod subdomain:** přidat `enable-subdomain` op (otevře prod loop), nebo nechat ruční dashboard klik?
