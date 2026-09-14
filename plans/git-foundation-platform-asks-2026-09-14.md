# Git foundation — požadavky na platformu (pro Aleše)

Kontext: zcp staví git jako základ každé cesty kódu, provider-agnosticky (GitHub, GitLab,
self-hosted Gitea, cokoli). Živě ověřeno 2026-09-14 v projektu `eval`
(`.claude/agent-memory/platform-verifier/verified-facts.md`, sekce z 2026-09-14). Tři
limity platformy dnes vynucují obcházení na straně zcp; každý splněný bod níže maže kód,
ne přidává. Seřazeno podle páky.

## 1. `buildFromGit` jen pro github.com / gitlab.com

Ověřeno: import s `buildFromGit: https://<gitea-na-zeropsu>/org/repo` — `ls-remote` preflight
projde (Gitea access log ukazuje `GET /info/refs … 200` z 10.17.52.4), build spadne 36–40 ms
poté s `internalServerError`. Codeberg stejně. Docs říkají „A URL of a Github or Gitlab
repository".

Požadavek: povolit libovolný HTTPS git host (nebo aspoň allowlist rozšířit o self-hosted
Gitea / Forgejo), včetně privátního repa s tokenem v URL nebo v samostatném poli.

Co to odemkne: import a export projektu z/do jakéhokoli originu bez zcp-side push.

## 2. Autentizovaný build trigger s refem

Ověřeno: webhook receivery existují jen jako `/service-stack/{id}/github-webhook` a
`/gitlab-webhook`, vázané na OAuth účtu (`githubAuthorizationRequired`). Generický
autentizovaný trigger neexistuje; `PUT /service-stack/{id}/trigger-pipeline` bere
`buildFromGit` + `zeropsSetup`, ale kvůli bodu 1 jen pro GH/GL.

Požadavek: `PUT /service-stack/{id}/trigger-pipeline` (nebo nový endpoint) přijímající
`{ref | sha, zeropsYamlSetup}` pro repo, které služba už zná, autentizovaný integračním
tokenem; volitelně HMAC-podepsaný webhook s konfigurovatelným secretem pro cizí forge.

Co to odemkne: CI-on-push z Gitey a z uživatelova vlastního CI bez `zcli push` z runneru.

## 3. Commit sha na appVersion

Ověřeno: `--version-name` z `zcli push` se ukládá jen do ES search DTO
(`SearchAppVersions.name`); přímé GET DTO `name` nemají; sha/branch není nikde na appVersion,
ani u `source: GIT` (`publicGitSource` nese jen `gitUrl` + `branchName`).

Požadavek: pole `commitSha` (a `ref`) na appVersion DTO — pro platformní buildy vyplněné
z checkoutu, pro `zcli push` z parametru; dostupné i v přímém GET.

Co to odemkne: „co běží kde" přímo z platformy; zcp pak nemusí držet evidenci v gitu.

## 4. TTL / jednorázovost integračního tokenu

Ověřeno: `POST /client/{clientId}/integration-token` má `projects:[{projectId, roleCode}]`,
regenerate, delete — žádné `expiresAt`, žádné single-use.

Požadavek: volitelné `expiresAt` (a/nebo `maxUses`) při mintu.

Co to odemkne: token pro CI runner s omezenou životností místo trvalého; launch delegace
bez ručního mazání.

## Co zcp udělá bez ohledu na odpověď

- Origin na jakémkoli HTTPS hostu přes `git-push-setup` (funguje už dnes, PAT + credential helper).
- Evidence deployů jako anotované git tagy `zcp/deploy/<projekt>/<služba>/<appVersionId>`.
- Gitea Actions (act_runner) + `zcli push` jako CI pro Giteu — živě se ověřuje; pokud projde,
  žádný zcp relay nevznikne.
