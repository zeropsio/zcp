# Adding a Mate by cloning, and Gitea-driven CI/CD — research

2026-09-06. Written for the owner's two questions:

1. Is this flow possible — *clone an environment → an empty project with zcp comes up (with "the
   token copied from the first one") → it receives the command "import project xyz" → it tries the
   import and, when it fails, handles it intelligently (and later fixes the import itself)?*
2. How to set up CI/CD on Gitea and connect Mate's projects to git: one new group on the mock Go app,
   **1 Mate + 1 production**, the Mate pushing to Gitea, Gitea deploying to production — and then
   demonstrate adding a second Mate on top of it.

Nothing was created or changed on the account for this note; every live probe was a read with the
owner's own `zcli` token. Evidence tags:

| Tag        | Meaning                                                                                  |
| ---------- | ---------------------------------------------------------------------------------------- |
| `[live]`   | measured today (2026-09-06) against the Onboarding org, read-only calls                  |
| `[ledger]` | measured earlier and recorded in `../z3/docs/internals/zerops/verified.md`               |
| `[code]`   | read in the mate fork (`../z3`) or in this repo                                          |
| `[docs]`   | vendor documentation (Zerops, Gitea, zcli)                                               |
| `[open]`   | not verified; §8 says how to settle it                                                   |

---

## 0. Answers on one screen

**Q1 — yes: one zcp gap to close, one ledger rule to revisit, and the agent's login carried over as the file it already is.**

| Owner's step                                                     | Verdict                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| ---------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1. "dám naklonovat prostředí"                                    | **Exists.** The group page's "Add dev/stage/…" dialog offers *Clone `<Mate>` (`<environment>`)*, built from the sibling's `GET /project/{id}/export` with its container and every secret block stripped (`createEnvironment.ts`, `recipeExport.ts`). `[code]` `[ledger]`                                                                                                                                                                                                                                                                                                                                                              |
| 2. "vytvoří se prázdnej projekt s zcp (se zkopírovaným tokenem)" | **Exists; the token to carry is the coding agent's sign-in** (owner, 2026-09-06), so the new Mate never asks to authenticate again. The project + zcp part is `POST /client/{id}/project` (tags at birth) then `PUT /project/{id}/first-class-recipe/development-container`. The agent's credential is a file at a known path (`~/.claude/.credentials.json`, mode 0600; Codex `~/.codex/auth.json`) and moving that one file is measured to work (H-24) `[ledger]`. What is missing is the channel between two containers in two projects — the client, connected to both mate servers, is it — and a decision on the one real risk, the shared refresh token (§2.1). Aside: zcp's own API key is *not* what moves — the platform mints one per project (`zcp-<project>`, `NO_ACCESS` on the org, `ADMIN` on that project) `[live]`, and a copied one would bind the new zcp to the old project `[code]`. |
| 3. "dostane command 'naimportuj projekt xyz'"                    | **Half exists.** Today the *client* runs the import itself (accepted in ~1 s) before the agent exists, and the Mate lands with a generic greeting composed into the composer. To have the *Mate* do it: hand the scrubbed services YAML plus the task to its conversation through the first-prompt slot that already exists (`composeZeropsFirstPrompt`; sending it is one `thread.turn.start` dispatch). On the zcp side `zerops_import` needs an open workflow, which an empty project has none of — workable today through `bootstrap route=classic → zerops_import → bootstrap route=adopt`, clean with a small zcp change: a bootstrap route that takes caller-supplied import YAML, which is exactly what `route=recipe` already does with corpus YAML. `[code]` |
| 4. "když to failne, řeší to inteligentně… a fixne import"        | **Real and demonstrable with the mock template.** An export loses `zeropsSetup`, so a cloned `appdev` builds under a setup the repository does not have → `READY_TO_DEPLOY`, `stack.build FAILED` `[ledger]`. zcp already gives the agent structured evidence (`serviceErrors[].meta`, process `failReason`, `zerops_events`), a recovery hint, and the fix path — `zerops_import` with `override: true` + `zeropsSetup: dev` through the diagnose-before-destruct gate — and the repository's `zerops.yaml` is public, so the agent can find the right setup name. `[code]`                                                                                                                                                       |

**Q2 — yes today with hand-written pieces; the primitives exist and are host-agnostic where it
matters.** zcp's `git-push-setup` accepts any HTTPS remote + token; `zcli` authenticates from a
`ZEROPS_TOKEN` env var; the Gitea recipe's runner ships `zcli` and runs jobs in host mode; a
project-scoped deploy token for production can be minted by API. What is GitHub-only is zcp's
*guidance and emission* — `.github/workflows/zerops.yml`, `gh secret set`, PAT scopes, the
launch-production `prodCd` track — a bounded Gitea flavour. Two account-side blockers today: the
live Gitea has **no admin user and no runners** yet, and it sends **no CORS headers**, so the Mate
web client cannot call its API from the browser — automation must run server-side (mate server or
zcp) or the recipe must enable CORS `[live]`.

**Q3 — "how does a new bot come with the Gitea credentials pre-set?" — the recipe can mint them
itself, and the platform already carries them (§3.8).** `recipe-gitea` solves this exact problem
four times over for its own secrets: the container generates them and writes them back as its own
sensitive env with `zsc set-env --sensitive`. The admin user and API token are the same shape, and
`gitea migrate` exists precisely so `admin user create` can run before the server does. The value
then comes *back* out: `GET /service-stack/{id}/user-data` returns sensitive env **in clear** to an
owner token `[live]` — so mate reads the admin credential with the token it already holds, mints a
**per-Mate** Gitea user, and writes that Mate's own token into the new container's `envSecrets`
next to `VSCODE_PASSWORD`. Pre-set at birth, settable afterwards, no human in the loop. And
zcp's credential helper needs no Gitea flavour at all: Gitea resolves the user from the token, never
from the username (§3.8, Q-B1 answered from source).

---

## 1. The model and what exists

### 1.1 Object model (unchanged)

A user's **project** is a *group*: a set of Zerops projects tagged `mate:g:<id>` (+ `mate:role:`,
`mate:name:`). Each Zerops project is one **environment**; a **Mate** is a dev environment that
carries the `mate` marker and a `zcp@1` container (`mate:bot:<name>` names the agent). Stage and
production never have a Mate. **Tools** — Gitea — are singleton projects tagged `mate:tool:gitea`,
outside every group (`groups.ts`, `mateEnvironments.ts`, `tools.ts`). `[code]`

### 1.2 The account today `[live]`

| Project                  | Tags                                                       | Services (status)                                                                                          |
| ------------------------ | ---------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `acme-docs-dev`          | `mate`, `mate:g:6sf11t2b2vga`, `mate:role:dev`, `mate:name:Acme Docs`, `mate:bot:Fen` | `zcp` zcp@1, `app` alpine/nodejs@22, `db` postgresql:single@18 — all ACTIVE                              |
| `Acme Docs - stage`      | `mate:g:…`, `mate:role:stage`, `mate:name:Acme Docs`        | `app` alpine/go@1.22 ACTIVE (built from git on 09-05), `db` postgresql:single@16                          |
| `Acme Docs - production` | `mate:g:…`, `mate:role:prod`, `mate:name:Acme Docs`         | `app` alpine/go@1.22 **READY_TO_DEPLOY** (the 09-05 clone; its `buildFromGit` build failed, nothing since), `db` |
| `mate-gitea`             | `mate:tool:gitea`                                          | `web` ubuntu@26.04 ACTIVE (ports 3000, 2222, subdomain on), `volume` local-storage, `db` postgresql:single@18; **no `runner`** |
| `beviro-crm-{dev,stage,prod}`, `scratch-playground`, `zerops-mate10` | groups / ungrouped                       | mostly a zcp only                                                                                          |

The live Gitea is `https://web-1d76-3000.prg1.zerops.app`: `GET /api/v1/version` → `1.27.2`,
`GET /api/v1/users/search?q=` → `{"ok":true,"data":[]}` (no admin yet), root → 200. (The
`web-926-3000` host in the 09-05 ledger rows was that day's throwaway instance; it now answers a
Zerops 502 page.)

Integration tokens on the org (`GET /client/{id}/integration-token/list`; the bare
`…/integration-token` GET is `405`): seven tokens named `zcp-<project name>` — `zcp-acme-docs-dev`,
`zcp-Acme Docs - stage`, `zcp-beviro-crm-{dev,stage,prod}`, `zcp-scratch-playground`,
`zcp-zerops-mate10` — each `roleCode: NO_ACCESS`, `canCreateProjects: false`, `projects: [{<that
project>, ADMIN}]`, `created` at the same second as the corresponding dev-container import. This is
the token `ZCP_API_KEY` in every zcp container.

### 1.3 What the fork already has for Flow A `[code]`

| Piece                                                                          | Where                                                                                                   |
| ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------- |
| The plan: `create-project` (tags at birth) → `import-container` → `import-recipe` → `await-ready`; with an agent the wait is handed to the provisioning waiter, without one it polls services to `ACTIVE`/`READY_TO_DEPLOY` (10 min cap) | `packages/client-runtime/src/zerops/createEnvironment.ts`, `runEnvironmentCreation.ts`                  |
| The recipe choice per environment: the group's store recipe / a sibling's export scrubbed / nothing yet ("the agent sets the application up") | `EnvironmentRecipeChoice`; dialog logic `apps/web/src/components/zerops/ZeropsEnvironmentCreationDialog.logic.ts` |
| Export → services-only YAML: drops `project:`, every `zcp@` service, every `vault:`/`envSecrets:` block; reports `builtFromGit` services that "will need a deploy" | `recipeExport.ts`, `apps/web/src/zerops/useZeropsCloneSources.ts`                                       |
| The recipe store seam (zcp writes, mate reads) — today a mock seeded only with the showcase group `Go Hello World` (`7k2m9qx4vb1c`), whose dev/stage/prod tiers are the real `zeropsio/recipes` go-hello-world tiers | `recipeStore.ts`, `recipeStoreSeed.ts`, `apps/web/src/zerops/recipeStore.ts`; hacks H-26              |
| The dev-container import document: `zcp@1`, `minRam: 2`, `VSCODE_PASSWORD`, `ZCP_MATE_ENABLED: "1"`, `ZCP_AGENTS`, `install.sh` (latest release) + `zcp init` + nginx; sent with `recipeSource: "zeropsio/zcp"`, `createIntegrationToken: true` | `newProject.ts`, `api.ts` `importDevelopmentContainer`                                                  |
| The first prompt: composed into the composer once per identity-door environment, never sent      | `firstPrompt.ts`, `apps/web/src/zerops/composeFirstPrompt.ts`, `routes/_chat.index.tsx`               |
| Measured: the three writes accepted in < 4 s; tags survive both imports; a clone from an export imports but its `buildFromGit` service does not build (no `zeropsSetup`) | ledger 2026-09-05                                                                                       |

### 1.4 What the fork already has for Flow B `[code]` `[ledger]`

- **"Add Gitea"** in the group tree → `createToolProject` = `POST /client/{id}/project` (tag
  `mate:tool:gitea`) + `POST /project/{id}/service-stack/import` with the recipe's services half
  and `GITEA_DOMAIN` templated to the project's real region (`giteaRecipe.ts`; the published recipe
  hardcodes `app-prg1`, which does not resolve — fixed upstream by PR). Import to a serving Gitea
  took 2 m 02 s.
- **Gitea state** derived from platform reads plus two unauthenticated Gitea endpoints
  (`deriveGiteaState`, `tools.ts`): steps *imported / running / admin user / CI runners / domain +
  SSH*. The probe shell that would fetch the two endpoints from the browser is **not wired** — and
  would be blocked by CORS anyway (§4.5).
- **Runner addon YAML builder** (`buildGiteaRunnerImportYaml`) exists; no UI calls it. Its template
  omits `zeropsSetup: runner` (the published `zerops-runner-import.yaml` has it); it works only
  because the hostname `runner` coincides with the setup name.
- Facts the ledger already holds: admin user and runner token need a shell in `web`;
  `PUT /user-data/{id}` replaces a service's whole env set (never use it for one variable).

### 1.5 What zcp already has `[code]`

| Capability                            | Tool / action                                                    | Notes                                                                                                                                                                                                                       |
| ------------------------------------- | ---------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Import services from YAML             | `zerops_import content|filePath [override] [confirmDestructive]` | Requires an open workflow (`requireWorkflowContext`: bootstrap session, develop work session, or recipe-authoring session). Only `project.envVariables` is allowed in a `project:` block. Returns `serviceErrors[]` with the API's field-level `meta`, per-process `FINISHED/FAILED` + `failReason`, warnings, the envelope. `override` on a service with failed history refuses first with `DIAGNOSIS_REQUIRED` + `wouldDestroy` + a pre-filled retry call. |
| Bootstrap routes                      | `zerops_workflow start bootstrap [route=resume|adopt|classic|recipe]` | `recipe` derives a complete plan from the matched recipe's `ImportYAML` (`BootstrapCompleteRecipePlan`) and provision imports it, mounts dev services, writes `ServiceMeta`. The corpus has `go-hello-world` (`internal/knowledge/recipes/go-hello-world.import.yml`: `appdev` dev + `appstage` prod + `db`). `adopt` writes `ServiceMeta` for services that already exist. |
| Git push from the container           | `zerops_workflow action=git-push-setup service remoteUrl gitToken` → `zerops_deploy strategy=git-push` | Probe-first (`git push --dry-run`), then `GIT_TOKEN` as a service-scope secret on the push source, origin rewritten, inline credential helper `username=oauth2` / `password=$GIT_TOKEN`. HTTPS only, no credential in the URL. Host-agnostic in code; the *guidance* is GitHub-flavoured. |
| CI shape after a push                 | `zerops_workflow action=build-integration integration=actions|webhook|none` | `actions` emits `.github/workflows/zerops.yml` + two `gh secret set` commands (`ZEROPS_TOKEN` = the container's own `ZCP_API_KEY`, `ZEROPS_SERVICE_ID`). GitHub-only by construction.                                                            |
| Release                               | `zerops_workflow action=release service`                          | Verifies clean tree + HEAD pushed, derives the next `vX.Y.Z`, tags and pushes the tag. Host-agnostic.                                                                                                                       |
| Launch production                     | `zerops_workflow start launch-production`                         | Pipeline-first: production import carries no `buildFromGit`, runtimes start with `startWithoutCode: true`, the first release deploys through the pipeline. Token by one-time delegation or an explicit `launchKey`; `prodCd` track emits `.github/workflows/zerops-prod.yml` + `ZEROPS_TOKEN_PROD` via `gh secret set`. GitHub-only in the CD half. |
| Export as recipe                      | `zerops_workflow start export`                                    | Single-repo `zerops-project-import.yaml` + `zerops.yaml` bundle; the natural writer of the recipe store mate reads (H-26 "still open").                                                                                                |

---

## 2. Flow A — adding a Mate by cloning

### 2.1 The token: the agent's authorization moves to the new Mate

The owner's "token copied from the first one" is the coding agent's sign-in: a new Mate should come
up already authorized, no second device login. The facts that bound the design:

- **Where it lives.** Claude Code stores a `/login` credential in `~/.claude/.credentials.json`
  (mode 0600 on Linux; macOS uses the Keychain and falls back to the same file when the Keychain is
  locked). It holds an access token that lives ~8 h and the refresh token behind it; the login
  itself has a lifetime — Claude Code warns 3 days before it expires and `/login` renews it. Codex
  keeps `~/.codex/auth.json` and refreshes it during runs. `[docs]` `[ledger]`
- **Moving the file works.** H-24 measured it on 2026-08-28: `~/.claude/.credentials.json` alone,
  no `~/.claude.json` merge, and `claude -p` answers, the zcp MCP works under the copied login, and
  two containers used the same copy concurrently within the 8 h window with no conflict. The
  agent-auth feed sees a restored file within ~0.5 s and re-verifies with `claude auth status`
  (~2 s), then `zcp agent mark-oauth` writes the platform flag. `[ledger]`
- **What the product does today.** Each Mate signs in through S7 (the device flow relayed to the
  client); zcp's welcome/bootstrap also know an `authorized-token` state keyed on
  `ZCP_AGENT_TOKEN_<SUFFIX>` (a GUI-written env), and zcp's Claude adapter pre-approves an
  `ANTHROPIC_API_KEY` from the env. Nothing in zcp or mate knows `CLAUDE_CODE_OAUTH_TOKEN`. The
  ledger rule written on 09-05 — *creation does not, and must not, copy a credential in (H-24 is the
  eval hack, not the product)* — is the thing this ask overturns; it should be revisited with the
  risk below named, not silently. `[code]` `[ledger]`
- **The one real risk: one refresh token in two containers.** Both copies refresh the same login.
  H-24's operating note is "a token refresh in one container can invalidate the copy in another —
  re-stash from the one that still works"; whether a refresh actually rotates and invalidates the
  sibling's token is unmeasured (`questions.md` Q-12: leave two containers past `expiresAt`, diff
  `expiresAt`/mtime, never contents). Inside the 8 h window nothing happens; the question is the
  first refresh after the copy. `[ledger]`

Three mechanisms, in the order they can ship:

| Mechanism                                                   | How it works                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   | Verdict                                                                                                                                                                                                                                                              |
| ----------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **M1 — transfer the credential file** (the owner's ask)     | The two containers cannot reach each other (different projects, no shared network); the **client** is connected to both mate servers and is the carrier. Source Mate: a new member-only RPC hands the file's bytes to the client (never logged, never in a feed; the same trust surface as the terminal RPC the login already uses). New Mate: once its mate server answers the identity door, a second RPC writes the file (0600) and the existing feed does the rest — verify, `mark-oauth`, the face turns authorized. The client holds the bytes in memory for the seconds between; nothing is stored. A variant that needs no wait for the second server: put the bytes into the new container's import as an `envSecrets` value and let zcp init (or mate's boot) materialize the file — but that also parks the credential in the platform's env store, which every project member with API access can read verbatim. The file is project-wide either way (spec §8.3), so the difference is exposure to the API, not to the project. Codex: the same with `~/.codex/auth.json`. | **Do this for the demo.** It is literally the ask, it works today (a manual `scp` over the project VPN reproduces H-24 in a minute), and the product shape is two RPCs plus a client step. Measure Q-12 alongside; if a refresh does invalidate the sibling, the client — connected to both — can re-copy from the Mate whose `providerAuth` is still `authenticated` when another turns `unauthenticated`. |
| **M2 — one long-lived token for all Mates**                 | `claude setup-token` mints a one-year OAuth token (`sk-ant-oat01…`; Pro, Max, Team, Enterprise) that Claude Code reads from `CLAUDE_CODE_OAUTH_TOKEN`, ranked **above** the `/login` credential in its precedence list; it can only make model requests (no Remote Control, no claude.ai connectors — locally configured MCP servers such as zcp's still work). Minted once through the same login walker (the command prints the token at the end — it must go straight into the platform env, never into the feed), stored as `ZCP_AGENT_TOKEN_CLAUDE_CODE` (sensitive) on the source zcp, read back by the client at creation (an OWNER token sees sensitive values verbatim on `GET /service-stack/{id}/env`) and seeded into the new container's `envSecrets`; the mate server or zcp's Claude adapter exports it into the agent process env. No file, no refresh, no race, one credential to revoke. Needs the `setup-token` zcp verb the spec already names (S7-5), the env→process bridge, and the client copy. Codex has no equivalent. `[docs]` `[code]` | **The product mechanism for Claude.** Same "authorized at birth" result as M1 with none of the refresh coupling; a year of validity matches how long a group lives.                                                                                                                       |
| **M3 — API keys** (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`)   | Already consumed by zcp's adapters and by the GUI's `token` auth type; seedable via `envSecrets` today.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         | Not the product path: API billing instead of the subscription, which is the point of the fork (H-24). Keep as the escape hatch.                                                                                                                                       |

Two details either mechanism needs: the credential belongs to whoever signed in (the
`ZeropsAgentAuthorizers` record), so the new Mate's record should say "copied from `<Mate>` by
`<user>`" rather than nothing; and the copy should be offered in the creation dialog as a
checkbox ("sign the agent in with Fen's login"), on by default when the source Mate's agent is
authorized, so the person sees what is being carried.

### 2.2 "Import project xyz" — who imports, and what the agent needs

Three designs, all with the client (user token) reading the source's export — the agent's token
cannot see another project:

| Design                                                 | What happens                                                                                                                                                                                                                                                                                                                                                 | Verdict                                                                                                                                                                                              |
| ------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **A1 — today**: client imports, Mate inherits the aftermath | `import-recipe` runs before the agent exists; the Mate lands and the first prompt is generic. Only change: a clone-specific prompt carrying the outcome (`undeployed` names the services whose build failed) and the task "get them running".                                                                                                                  | Smallest change; the agent's job starts at *diagnose the failed build*, not at *import*. Good enough to demonstrate "handles it intelligently".                                                       |
| **A2 — the owner's flow**: client hands over, Mate imports | The client creates project + container only (`recipe: {kind: "none"}`), and puts the scrubbed services YAML plus "import this into your project; if it fails, diagnose and fix" into the Mate's first prompt. The agent runs `zerops_import`, reads `serviceErrors`/processes/events, fixes, re-imports, then adopts. | Demonstrates zcp's validation plumbing end to end. Needs the zcp gap below closed (or the classic→import→adopt dance scripted into the prompt). Recommended direction.                                  |
| **A3**: Mate reads the source project itself            | Impossible with the container's token (single-project scope) and against spec §0; delegation is one-time and mints a *create* token, not a *read* one.                                                                                                                                                                                                       | Drop.                                                                                                                                                                                                |

**The zcp gap.** `zerops_import` refuses without an open workflow. On an empty project the agent
must open one: `zerops_workflow start bootstrap` (first call returns the route options;
`route=classic` opens a session at `discover`) → `zerops_import content=…` (gate satisfied) → the
bootstrap session now sits on a plan it never got, so close/reset it → `bootstrap route=adopt` to
write `ServiceMeta` for the imported runtimes (mounts, git init). Workable, but three detours the
prompt has to teach. The clean form reuses what `route=recipe` already does: a route that takes
**caller-supplied import YAML** (`importYaml=` on the start call), derives the complete plan from it
(`ParseRecipeImportShape` → `BootstrapCompleteRecipePlan`), imports at `provision`, mounts and
writes `ServiceMeta`. Size: small — a new `BootstrapRoute` value, one input field, and the
recipe-route derivation pointed at the supplied YAML instead of `RecipeMatch.ImportYAML`.

**Compose or send?** The first-prompt slot is *composed, never sent* by rule (spec §4.8 MC
invariants: the person reads what will be said before it costs a turn). Auto-sending the clone
command is technically one dispatch of the same `thread.turn.start` command the composer sends,
but it would be the first place mate speaks into a conversation on the user's behalf. Recommendation:
compose it, and let the person press Enter — for the demo the moment they do is the moment the Mate
starts working, which reads better than a Mate that was already talking when the page opened.

**Sign-in gate.** Whatever is composed waits on the agent's authorization (§2.1). The prompt survives
that: it sits in the composer until sent.

### 2.3 What fails, what the agent sees, what it can fix — on the mock template

Source: a Mate created from the seed's dev tier (or zcp's `route=recipe` on `go-hello-world`): `appdev`
(golang@1.22, `zeropsSetup: dev`, `buildFromGit` go-hello-world-app), `appstage` (`zeropsSetup:
prod`), `db` (postgresql:single@16, `profile: oltp-staging`, `priority: 10`).

**The export loses exactly what makes the build work** `[ledger]`: no `zeropsSetup`, no `priority`,
no `profile`; `buildFromGit` gains `@main`. Expected scrubbed clone (shape extrapolated from the
Acme Docs stage export — Q-A1 verifies it on a go-hello-world environment):

```yaml
services:
  - hostname: appdev
    type: golang@1.22
    buildFromGit: https://github.com/zerops-recipe-apps/go-hello-world-app@main
    enableSubdomainAccess: true
    verticalAutoscaling:
      minRam: 0.5
  - hostname: appstage
    type: golang@1.22
    buildFromGit: https://github.com/zerops-recipe-apps/go-hello-world-app@main
    enableSubdomainAccess: true
    verticalAutoscaling:
      minRam: 0.5
  - hostname: db
    type: postgresql:single@16
```

**What happens on import:** accepted in ~1 s; `db` `ACTIVE` in ~40 s; both runtimes build under
the setup *named after the hostname* (`appdev`, `appstage`), which the repository's `zerops.yaml`
does not define (it has `prod` and `dev`) → `stack.build FAILED` at about +1 min, service left at
`READY_TO_DEPLOY` `[ledger]`. The `zerops_import` call itself reports the import processes, not the
later build — so the agent's evidence is `zerops_events serviceHostname=appdev` (the build failure
timeline), `zerops_discover` (status `READY_TO_DEPLOY`, `publicGitSource.explicitSetup: null`), and
zcp's failure classification (`LatestFailedAppVersionContext` → class `build`).

**The fix the agent can make today:** read the repository's `zerops.yaml` (public), then

```yaml
services:
  - hostname: appdev
    type: golang@1.22
    zeropsSetup: dev
    buildFromGit: https://github.com/zerops-recipe-apps/go-hello-world-app
    enableSubdomainAccess: true
    verticalAutoscaling:
      minRam: 0.5
```

with `override: true`. Because `appdev` has failed history, the first call refuses with
`DIAGNOSIS_REQUIRED` and a `wouldDestroy` payload naming the target, the env keys that would be
lost and a pre-filled retry (`confirmDestructive: {operation: "import-override",
acknowledgedTargets: ["appdev"]}`, plus a `startWithoutCode: true` hint where the target has no
active version). The second call replaces the service and the build runs. Same for `appstage` with
`zeropsSetup: prod`. Then `bootstrap route=adopt` so the pair has `ServiceMeta`, and the develop
loop is open. That sequence — *import, watch it fail, read the repo, re-import with the right setup,
adopt* — is the "fixes the import" the owner wants to see, and every tool in it exists.

**Two side notes for the demo script.**

- The client can *sometimes* avoid the failure: the store recipe carries `zeropsSetup`; the export
  never does, and the platform does not store the setup it built with either
  (`explicitSetup: null`) — so on the clone path only the agent (reading the repo) can know the
  setup. That is the honest reason the failure belongs in the demo rather than being patched away.
- A control case: "set up go-hello-world" on an empty Mate → `bootstrap route=recipe` imports the
  canonical YAML with `zeropsSetup` and nothing fails. Showing both makes the point that a clone is
  a *shape*, a recipe is the *truth* — which is why the recipe store (zcp's `export` workflow as
  writer) is the long-term answer to cloning.

### 2.4 Timings to expect `[ledger]`

| Step                                                     | Measured                                   |
| -------------------------------------------------------- | ------------------------------------------ |
| `POST project` + container import + services import      | all accepted in < 4 s                      |
| Container up, mate serving, identity connect             | ~2 min (the provisioning checklist)        |
| Managed service (`db`) `ACTIVE` after import             | ~40 s                                      |
| `buildFromGit` build → `ACTIVE` or `FAILED`              | ~1 min                                     |
| Agent sign-in (S7 device flow)                           | ~1 min of the user's time                  |

### 2.5 Work items for Flow A

| # | Where | Item                                                                                                                                                                                                                                                                     | Size |
| - | ----- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ---- |
| A-1 | fork | Clone-specific first prompt: the environment's role and source, the scrubbed services YAML (A2) or the `undeployed` list (A1), and the task. Composed through the existing marker (`ZEROPS_FIRST_PROMPT_STORAGE_KEY`), keyed on the new environment id.             | S    |
| A-2 | fork | Creation dialog: a "let the Mate import it" choice next to "clone now" (plan `recipe: none` + carry the YAML to the prompt). Show the two-minute checklist as today.                                                                                                     | S    |
| A-3 | zcp  | Bootstrap route that takes caller-supplied import YAML (see §2.2); `zerops_import` stays gated. Alternatively (interim): an atom teaching the classic→import→adopt sequence for an empty project.                                                                          | S–M  |
| A-4 | zcp  | After a `buildFromGit` build fails on an imported service, the envelope/next-action should name `zeropsSetup` as the first suspect when `publicGitSource.explicitSetup` is null — today the agent has to infer it.                                                            | S    |
| A-5 | fork | Carry the agent's login (§2.1 M1): a member-only RPC on the source mate server that hands over `~/.claude/.credentials.json` / `~/.codex/auth.json`, a second on the new one that writes it (0600) and lets the feed verify; the client copies between them after the identity door; the creation dialog offers it as a checkbox; the authorizer record says "copied from `<Mate>`". Revise the 09-05 ledger rule that forbade it. | M    |
| A-6 | both | The one-year token path (§2.1 M2): zcp `agent setup-token` verb (S7-5) that runs `claude setup-token` and stores the result as `ZCP_AGENT_TOKEN_CLAUDE_CODE` (sensitive); the mate server exports it as `CLAUDE_CODE_OAUTH_TOKEN` to the agent process; the creation flow reads it from the source zcp's env and seeds the new container's `envSecrets`. | M    |
| A-7 | both | Recipe store written by zcp's `export`, read by mate (H-26) — the real fix for lossy clones.                                                                                                                                                                              | L    |

---

## 3. Flow B — Gitea CI/CD with one Mate and one production

### 3.1 Topology

```
Onboarding org
├── mate-gitea  (mate:tool:gitea)                       group "Go Hello World"  (mate:g:<id>)
│   ├── web    ubuntu@26.04  Gitea 1.27.2  :3000 https, :2222 ssh   ├── go-hello-world-dev   (mate, mate:role:dev, mate:bot:<name>)
│   ├── db     postgresql                                            │   ├── zcp      the Mate; mounts /var/www/appdev, /var/www/appstage
│   ├── volume local-storage                                         │   ├── appdev   golang@1.22  setup dev   (push source)
│   └── runner ubuntu@26.04 ×3  host mode, git+node+zcli  ◄──────┐   │   ├── appstage golang@1.22  setup prod  (build target of the pair)
│                                                                │   │   └── db       postgresql:single@16
│        git push https + GIT_TOKEN  ─────────────────────────────┼───┘
│        Gitea Actions job: zcli push --setup prod  ──────────────┼──►└── go-hello-world-prod  (mate:role:prod, no Mate)
│                                       ZEROPS_TOKEN = prod-scoped│        ├── app  golang@1.22  startWithoutCode, minContainers 2
└────────────────────────────────────────────────────────────────┘        └── db   postgresql:single@16  oltp-production
                                  Zerops API  ◄── user token (mate client), zcp-<project> tokens (each zcp), prod token (Gitea secret)
```

Everything crosses on public HTTPS: the Mate pushes to Gitea's `zerops.app` subdomain; the runner
calls the Zerops API. No VPN is involved in the steady state.

### 3.2 Setup, step by step — who does it, with what

| #  | Step                                                                                             | Actor                                     | Call / command                                                                                                                                                                                                                                                                    | Status                                     |
| -- | ------------------------------------------------------------------------------------------------ | ----------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------ |
| 1  | Gitea project                                                                                    | mate client (user token)                  | "Add Gitea" — exists; `mate-gitea` is already up `[live]`                                                                                                                                                                                                                         | done                                       |
| 2  | Admin user                                                                                       | **user, shell in `web`** (`zcli vpn up && ssh web`, or the GUI Remote Web Terminal) | `gitea admin user create --config /etc/gitea/app.ini --admin --username admin --email … --password '…' --must-change-password=false`                                                                                                                       | **not done** `[live]`; not automatable *from mate* — but the recipe can do it to itself (§3.8) |
| 3  | An admin API token for automation                                                                | same shell, or basic auth                 | `gitea admin user generate-access-token --config /etc/gitea/app.ini --username admin --token-name mate --scopes all` — or `POST /api/v1/users/admin/tokens {"name":"mate","scopes":["all"]}` with basic auth (`gitea admin user create --access-token` mints a scope-less, useless token — Gitea #33474) `[docs]` | manual once; same recipe block mints it (§3.8) |
| 4  | Runners                                                                                          | shell (token) + mate client (import)      | `gitea actions generate-runner-token --config /etc/gitea/app.ini` → import `zerops-runner-import.yaml` with the token (`buildGiteaRunnerImportYaml` exists; no button yet). 3 containers register as `ubuntu-latest:host`, `ubuntu-26.04:host`; each ships `git`, `nodejs`, `zcli` `[docs]` | **not done**; token can be a project var the addon inherits (§3.8) |
| 5  | The repository                                                                                   | Gitea API (admin token)                   | `POST /api/v1/repos/migrate {"clone_addr":"https://github.com/zerops-recipe-apps/go-hello-world-app","repo_name":"go-hello-world-app","service":"github","private":false}` → 201 `[docs]`. Or `POST /user/repos` and let the Mate push the tree it already has.                     | one call                                   |
| 6  | The group and the Mate                                                                           | mate client                               | New group "Go Hello World"; "Add dev" with an agent. Recipe: the dev tier (`appdev` dev + `appstage` prod + `db`). A live group has no store record (H-26) — for the demo either seed the mock store under the demo group id, or let the Mate import the tier (`route=recipe`, corpus has it). | exists                                      |
| 7  | Adopt                                                                                            | Mate (agent)                              | `zerops_workflow start bootstrap route=adopt` — imported services have no `ServiceMeta`; adoption mounts, `git init`s `/var/www` on `appdev`, pairs `appdev`/`appstage`.                                                                                                             | exists                                      |
| 8  | Connect the Mate to Gitea                                                                        | Mate (agent) with a Gitea token the user supplies (scope `write:repository`) | `zerops_workflow action=git-push-setup service=appdev remoteUrl=https://web-1d76-3000.prg1.zerops.app/admin/go-hello-world-app.git gitToken=<gitea token>` → dry-run probe → `GIT_TOKEN` secret on `appdev` → origin rewritten → `meta.GitPushState=configured`. The source's clone came from GitHub via `buildFromGit`; `shallowCloneGuard` unshallows it first. | exists; Q-B1 (username `oauth2` on Gitea) |
| 9  | First push                                                                                       | Mate (agent)                              | `ssh appdev "cd /var/www && git add -A && git commit -m …"` → `zerops_deploy targetService=appdev setup=dev strategy=git-push`. With `buildIntegration=none` the push is archived at the remote and the deploy call says so.                                                             | exists                                      |
| 10 | Production project                                                                               | mate client                               | "Add production" (no agent) with a pipeline-first prod recipe: `app golang@1.22, zeropsSetup: prod, startWithoutCode: true, enableSubdomainAccess: true, minContainers: 2`; `db postgresql:single@16, profile: oltp-production, priority: 10`. **No `buildFromGit`** — the first build comes from Gitea. | exists (the seed's prod tier needs the two edits) |
| 11 | Production deploy token                                                                          | mate client (user token; Owner/Admin)     | `POST /client/{clientId}/integration-token {"name":"gitea-deploy-go-hello-world-prod","roleCode":"NO_ACCESS","canCreateProjects":false,"canViewFinances":false,"canEditFinances":false,"projects":[{"projectId":"<prod>","roleCode":"ADMIN"}]}` → the token value is returned once `[docs: openapi]`. Q-B2: does `BASIC_USER` suffice for `zcli push`? | one call; not wired in mate                 |
| 12 | Repository secrets                                                                               | Gitea API (admin token) — **server-side** (§4.5) | `PUT /api/v1/repos/admin/go-hello-world-app/actions/secrets/ZEROPS_TOKEN {"data":"<prod token>"}` and `…/ZEROPS_PROD_SERVICE_ID {"data":"<prod app service id>"}` `[docs]`                                                                                                        | two calls                                   |
| 13 | The workflow file                                                                                | Mate (agent) writes it in `/var/www/appdev` and pushes (`Contents` via `git`), or the client through Gitea's contents API | see §3.3                                                                                                                                                                                                                                                | hand-written today; zcp Gitea flavour later (B-3) |
| 14 | Release                                                                                          | Mate (agent)                              | `zerops_workflow action=release service=appdev` → `v1.0.0` tagged at HEAD and pushed → Gitea Actions job on a runner → `zcli push --setup prod` → production build (readiness check on `/`) → `app` `ACTIVE`.                                                                          | exists                                      |
| 15 | Watch it land                                                                                    | mate client                               | The production environment's row/service map reads the prod project with the user's token (service map is a client projection) — `app` goes `READY_TO_DEPLOY → building → ACTIVE`, URL appears.                                                                                       | exists                                      |

### 3.3 The workflow file

This is the file as it actually ran, in both demo repositories (§3.10):

```yaml
# .gitea/workflows/deploy-prod.yaml
name: deploy to production
on:
  push:
    tags: ["v*"]                       # the release act tags vX.Y.Z; branch pushes never deploy
jobs:
  deploy:
    runs-on: ubuntu-latest             # label ubuntu-latest:host → the job runs on the runner container itself
    steps:
      - uses: actions/checkout@v4      # JS action; the runner ships nodejs + git for exactly this
      # The runner recipe ships zcli, so this step is optional on our own runners —
      # it is here so the workflow does not depend on the runner image at all, which
      # is how Zerops' own GitLab template does it.
      - name: Install zcli
        run: curl -L https://zerops.io/zcli/install.sh | sh
      - name: Push to production
        env:
          ZEROPS_TOKEN: ${{ secrets.ZEROPS_TOKEN }}                 # ADMIN on the prod project, NO_ACCESS elsewhere (step 11)
        run: |
          "$HOME/.local/bin/zcli" push \
            --service-id "${{ secrets.ZEROPS_PROD_SERVICE_ID }}" \
            --setup prod \
            --version-name "${GITHUB_REF_NAME}"
```

Both shapes were run live and both deploy: with the install step and `$HOME/.local/bin/zcli`, and
without it relying on the `zcli` the runner recipe already installs on `PATH`. Prefer the install
step — it costs a few seconds and makes the file portable to any runner. Note `$HOME`, not
`/root`: jobs run in host mode as the `zerops` user, not as root in a container, so a `image:` key
and `/root/.local/bin` from a GitLab-style template would both be wrong here.

Facts behind it:

- `zcli` authenticates from `ZEROPS_TOKEN` and the env var takes precedence over a stored login
  (zcli CHANGELOG, `src/cmdBuilder/createRunFunc.go`) `[docs]` — no `zcli login` step, nothing
  persisted on the shared runner.
- `zcli push --service-id … --setup … --version-name …` are the documented flags; `--setup` is why
  zcp's own Actions template installs zcli instead of using `zeropsio/actions` (that action has no
  setup input) `[docs]` `[code]`.
- Gitea Actions resolves `uses: actions/checkout@v4` from github.com by default
  (`DEFAULT_ACTIONS_URL=github`); absolute URLs work too; `runs-on` must be a single label or a
  list of labels; `jobs.<id>.environment` is ignored; secrets may not start with `GITHUB_` `[docs]`.
- Workflow files live under `.gitea/workflows/`. Gitea's source also accepts `.github/workflows/`
  — Q-B3 confirms it on 1.27; if it holds, zcp's emitted path works on Gitea unchanged.
- Host mode: `docker://` actions, `container:` jobs and `services:` blocks do not work; jobs run as
  the `zerops` user on the runner container; anyone who can push a workflow runs code on the runners
  and can read their env — treat the runner project as shared by everyone with push access
  (recipe README) `[docs]`.
- Tag events: the `release` action pushes an annotated tag; Gitea fires `push` with the tag ref;
  `${GITHUB_REF_NAME}` is set for GitHub compatibility.

### 3.4 zcp's GitHub-only spots and the Gitea equivalents

| Where in zcp                                                                                  | GitHub-specific today                                                                                                                      | Gitea equivalent                                                                                                                                                                                          | Impact            |
| --------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------- |
| `git-push-setup` walkthrough (`inputsRequired`, atoms `setup-git-push-container`)             | PAT guidance names github.com fine-grained scopes; "recommendedIntegration: actions (for GitHub)"                                          | Token scope `write:repository` (`read:repository` for the probe alone); token created in Gitea UI or `POST /users/{u}/tokens`. Code path is host-agnostic (`validateRemoteURL`, `parseGitHost`, credential helper). | text only          |
| Credential helper `username=oauth2`                                                           | GitHub ignores the username; GitLab expects `oauth2`                                                                                        | Gitea accepts a token as the basic-auth password; whether the username must match the token's owner is Q-B1                                                                                              | verify            |
| Git identity (`DeriveGitHubIdentity`, `IsGitHubRemote`)                                       | Reads the GitHub user behind the PAT to attribute commits                                                                                  | Falls back to the robot identity; a Gitea variant reads `GET /api/v1/user`                                                                                                                                | cosmetic           |
| `build-integration=actions`                                                                   | Emits `.github/workflows/zerops.yml`; `gh secret set -R owner/repo`; reuses `ZCP_API_KEY` as `ZEROPS_TOKEN` (the container's own project token) | Path `.gitea/workflows/…` (or Q-B3); secrets via `PUT /repos/{o}/{r}/actions/secrets/{name}` with a Gitea token; `owner/repo` parsing already host-agnostic (`ParseGitRemoteOwnerRepo`)                    | new flavour        |
| `launch-production` `prodCd` (actions family)                                                 | `.github/workflows/zerops-prod.yml`, `ZEROPS_TOKEN_PROD` via `gh secret set` reading the staged `ZCP_LAUNCH_TOKEN`                        | Same file under `.gitea/workflows/`, secret via API; the delegated-mint and staging protocol is host-agnostic                                                                                                | new flavour        |
| `webhook` integration                                                                         | Zerops dashboard OAuth pull — GitHub/GitLab only (platform)                                                                                | None: the platform has no Gitea pull integration `[docs]`; Gitea → Zerops is always *push* (zcli from a runner). A Gitea webhook has nothing to call on the Zerops side.                                    | n/a                |

Nothing above blocks the demo: the agent can write the workflow file itself and the user (or a
script) sets the two secrets; `git-push-setup`, `deploy strategy=git-push` and `release` work as
they are, pending Q-B1.

### 3.5 Adding the second Mate on top

Flow A on the Gitea-backed group: "Add dev" → *Clone `<Mate>` (go-hello-world-dev)* or the
agent-import variant → a second Mate with its own `appdev`/`appstage`/`db`. Two things differ from
the plain clone:

- **Where its code comes from.** The clone's `buildFromGit` still points at GitHub (the export
  carries the source's URL). Pointing it at the Gitea repository requires the platform's
  `buildFromGit` to accept a non-GitHub/GitLab host — the docs say "a GitHub or GitLab repository";
  Q-B4 tests a public Gitea URL. If it does not, the second Mate imports without `buildFromGit`
  (`startWithoutCode: true`) and its agent clones from Gitea with `GIT_TOKEN` after
  `git-push-setup` — one extra step, no blocker.
- **Two Mates, one repository.** zcp pushes `main` by default (`branch` is a parameter of the
  git-push deploy). Production deploys only on tags (`release`), so two Mates on `main` cannot ship
  by accident; a branch per Mate plus a Gitea pull request is the natural collaboration shape and
  needs no zcp change beyond passing `branch`.

### 3.6 Timings to expect

| Step                                                   | Expected                                                        |
| ------------------------------------------------------ | --------------------------------------------------------------- |
| Gitea import → serving                                 | ~2 min `[ledger]`                                               |
| Runner addon import → 3 runners idle                   | build of the `runner` setup ~2–3 min (apt + downloads) `[open]` |
| Group dev environment with agent                       | ~2 min container + ~1 min per `buildFromGit` build              |
| Production project (pipeline-first)                    | services `ACTIVE` in < 1 min (nothing to build)                 |
| Release → Gitea job → `zcli push` → prod `ACTIVE`      | Go build ~1–2 min                                               |

A live end-to-end demo from an empty org is 15–20 minutes of wall clock; with Gitea and runners
prepared, about 8.

### 3.7 Work items for Flow B

| #   | Where       | Item                                                                                                                                                                                                                      | Size |
| --- | ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---- |
| B-1 | fork        | Gitea tool card: wire the two probes (through the mate server or a CORS-enabled recipe — §4.5), the "Add runners" import with a token field, and the copyable admin/runner-token commands the ledger already words.        | M    |
| B-2 | fork        | "Connect to Gitea" on a Mate's environment: create/migrate the repository, mint the prod deploy token (`POST /client/{id}/integration-token`), set the repository secrets, hand the Gitea token to the Mate's `git-push-setup` prompt. Gitea calls must run server-side. | M–L  |
| B-3 | zcp         | Gitea flavour for `build-integration` and `launch-production` prodCd: workflow path, secret conveyance via the Gitea API instead of `gh`, token-scope wording. Detect the host from `meta.RemoteURL`.                        | M    |
| B-4 | zcp         | Gitea identity derivation (`/api/v1/user`) and a Gitea row in the token-scope table of `setup-git-push-container`.                                                                                                        | S    |
| B-11 | zcp        | **Stop letting the git-push stamp block (§3.12, feasibility in §3.13):** no gate refuses on `GitPushState` alone — it confirms against the container first, and a failed or impossible confirmation falls back to the cache rather than to a new refusal. The stamp stays as the envelope's cheap cache. Predicate already exists in `adopt_gitpush_reconcile.go`; two decision sites to change. | M    |
| B-5 | recipe-gitea | **Self-bootstrapping admin (§3.8):** `admin-init.sh` + the `start.sh` guard — `gitea migrate`, `admin user create --random-password --access-token`, publish `GITEA_ADMIN_USER/PASSWORD/TOKEN` with `zsc set-env --sensitive`; same for the runner token as a project var. Removes the human from §3.2 steps 2–4. Also fix the fork's runner template (`zeropsSetup: runner`). Also `[cors] ENABLED = true` for the mate origins, which B-8 needs. | S    |
| B-6 | fork        | A pipeline-first prod recipe for the seed group (`startWithoutCode: true`, no `buildFromGit`) — the seed's prod tier pulls from GitHub, which is the wrong first build once Gitea owns the code.                             | S    |
| B-7 | fork        | **A Mate born connected (§3.8 layer 3):** `buildZcpServiceImportYaml` takes optional `giteaUrl`/`giteaToken` into `envSecrets` — never `run.envVariables`, which would make B-8 impossible; creation mints a per-Mate Gitea user (admin token) + its token (admin password, basic auth, since tokens cannot mint tokens); the first prompt names `$GITEA_URL`/`$GITEA_TOKEN` so the agent passes its own env to `git-push-setup`. | S–M  |
| B-8 | fork        | **The reconcile and its button (§3.9):** one function taking any Mate to "has a live credential" — read the service env + one `GET /users/{u}/tokens`, then the eight-row table; a Mate roster on the Gitea tool card driving it, restarting only Mates that are running. Depends on B-5 for CORS. | M    |
| B-9 | fork        | **`provision-git` as a soft creation step (§3.9):** a `planEnvironmentCreation` step before `import-container` that mints `mate/<bot>` and seeds `GITEA_URL`/`GITEA_TOKEN` into `envSecrets` — skipped without comment when there is no Gitea, when it is not `ACTIVE`, or when minting fails. Creation must never fail on the git host. | S    |
| B-10 | fork       | **Spawn the agent from the container's live env** rather than the mate server's own process environment, so a credential written after boot is picked up by the next turn. Removes the restart from B-8 and from anything else we later hand a running Mate. | S    |

---

### 3.8 Making it automatic — the recipe mints, the platform carries, the bot is born with it

The setup table above has three rows where a human sits in a terminal (admin user, admin token,
runner token), and a fourth problem behind them: **every new Mate would need its Gitea credential
pasted in by hand.** That is not a demo blocker, it is a product blocker — "add a Mate" has to be
one click, and a click cannot paste a token.

It does not have to be that way, and the recipe itself says so.

#### The pattern is already in the recipe

`recipe-gitea` has this exact problem four times over — `JWT_SECRET`, `LFS_JWT_SECRET`,
`SECRET_KEY`, `INTERNAL_TOKEN` are secrets only Gitea can generate — and it already solves it
without a human. `init.sh`, on the first boot:

```sh
value="$(/usr/local/bin/gitea generate secret "$secret")"
printf '%s' "$value" | zsc setEnv --sensitive "$secret" -
```

The container mints the secret and **writes it back to the platform as its own sensitive service
env**. `start.sh` refuses to start until the four arrive, Zerops restarts it with them present, and
the README can honestly say "there is nothing to prepare by hand". Gitea's admin user and its API
token are the same shape of secret: they can only be made from inside, and today they are the only
two the recipe does not make for itself.

So the answer to "how do we do this automatically" is not a new mechanism. It is the recipe's own
mechanism, applied to two more values, and then two layers above it.

| Layer       | Who does it                    | What it does                                                                                                                              | Cost                    |
| ----------- | ------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------- | ----------------------- |
| 1. **Mint** | the `web` container, once ever | `gitea migrate`, then `gitea admin user create --admin --random-password --access-token`; publish user, password and token as sensitive env | ~25 lines in the recipe |
| 2. **Carry**| the platform                   | `GET /service-stack/{web}/user-data` with the **user's own token** returns those values in clear                                            | already true `[live]`   |
| 3. **Seed** | mate, at environment creation  | mint a **per-Mate** Gitea user + token with the admin credential; write it into the new zcp's `envSecrets` beside `VSCODE_PASSWORD`         | B-2, plus a small B-7   |

#### Layer 1 — the container mints its own admin

Two facts make this work, both read out of Gitea 1.27.2's source today:

- `gitea migrate` exists for exactly this purpose. Its own description: *"This is a command for
  migrating the database, so that you can run `gitea admin create user` before starting the
  server."* `initDB` (used by every other admin command) calls `db.InitEngine`, which does **not**
  migrate; `migrate` calls `InitEngineWithMigration`. So the schema has to be created before the
  first admin command, and there is a command for it.
- `gitea admin user create` mints the API token in the same call: `--access-token`,
  `--access-token-name`, `--access-token-scopes` (**default `all`**, and 1.27.2 rejects a token
  with no permission scope — the scope-less-token bug of #33474 is not reachable here). With
  `--random-password` the command prints both values and nothing else has to be invented:

  ```
  generated random password is '<password>'
  New user '<name>' has been successfully created!
  Access token was successfully created... <token>
  ```

Captured into a shell variable, that output never reaches the container log. The block belongs in
`start.sh` — after `zsc envReplace` renders `app.ini` (the admin commands need it) and before
`gitea web` takes the foreground:

```sh
# start.sh, between rendering app.ini and starting gitea
if [ -z "${GITEA_ADMIN_TOKEN:-}" ]; then
  zsc exec-once gitea-admin-user -- ./admin-init.sh
  echo "start.sh: admin credentials published, restarting to pick them up ..."
  exit 1
fi
```

```sh
#!/usr/bin/env bash
# admin-init.sh — mints the first admin and an automation token, once ever.
set -euo pipefail

: "${GITEA_ADMIN_USER:=mate}"
: "${GITEA_ADMIN_EMAIL:=mate@localhost}"

# --random-password and --access-token both print their value; capture, never echo.
out="$(gitea migrate --config /etc/gitea/app.ini >/dev/null && \
  gitea admin user create \
    --config /etc/gitea/app.ini \
    --admin \
    --username "$GITEA_ADMIN_USER" \
    --email "$GITEA_ADMIN_EMAIL" \
    --random-password \
    --must-change-password=false \
    --access-token \
    --access-token-name mate \
    --access-token-scopes all)"

password="$(printf '%s' "$out" | sed -n "s/^generated random password is '\(.*\)'$/\1/p")"
token="$(printf '%s' "$out" | sed -n 's/^Access token was successfully created\.\.\. //p')"
[ -n "$password" ] && [ -n "$token" ] || { echo "admin-init.sh: could not read the generated credentials"; exit 1; }

printf '%s' "$GITEA_ADMIN_USER" | zsc set-env GITEA_ADMIN_USER -
printf '%s' "$password"         | zsc set-env --sensitive GITEA_ADMIN_PASSWORD -
printf '%s' "$token"            | zsc set-env --sensitive GITEA_ADMIN_TOKEN -
echo "admin-init.sh: done"
```

Four properties worth keeping:

- **Nothing is printed.** The values move from stdout into shell variables into `zsc` on stdin —
  the same stdin form `init.sh` already uses, because a generated value can begin with `-`.
- **The password never touches `argv`,** so it is not visible to `ps` even inside the container.
  That is the reason for `--random-password` over a self-generated `--password`.
- **It runs once, and says why it is restarting.** `zsc exec-once` guards the cluster case
  (`maxContainers: 1` today, but the guard is free) and the `GITEA_ADMIN_TOKEN` test guards
  everything else, since env only refreshes at boot. The deliberate `exit 1` is the pattern the
  recipe already uses for the four secrets — the container comes back with the values present.
- **Rotation is a delete.** Remove `GITEA_ADMIN_TOKEN` in the GUI and restart; the block runs again
  (`exec-once` needs a fresh key — use `${GITEA_ADMIN_USER}` in the key, or drop `exec-once` and
  rely on the env guard alone, which is what a single-container service actually needs).

The runner token looks like the same move one call further (`gitea actions generate-runner-token`).
**It is not, and trying it is what broke the first live run** — that command posts to the running
server, which no boot-time script ever sees. Ask the API for one instead, with the token above:
`POST /api/v1/admin/actions/runners/registration-token`. See §3.10.

#### Layer 2 — the platform is the channel, and it hands the value back

This is the fact that makes the whole thing possible, and it was measured today:
`GET /service-stack/{id}/user-data` **returns sensitive values in clear** to a token with the
owner's role. On the live Gitea's `web` service, all five secrets came back readable —
`SECRET_KEY`, `INTERNAL_TOKEN`, `JWT_SECRET`, `LFS_JWT_SECRET`, `DB_PASSWORD` — flagged
`sensitive: true` and unredacted. `[live]`

So a credential minted inside the container is readable by the account that owns it, with the token
mate already holds. No terminal, no copy-paste, no second storage system: **the platform's env
store is the handoff.** (Note the contrast with `GET /project/{id}/export`, which *does* redact
service vault entries — §4. The export is not the read to use; `user-data` is.)

One caveat to state plainly: this means the account owner can read the Gitea admin token from the
API, which is correct — it is their Gitea — but it is also why layer 3 exists. The *bot* should
never hold that token.

#### Layer 3 — a new Mate is born with its own Gitea identity

Not the admin token. Each Mate gets its own Gitea user, so pushes are attributable, and revoking one
Mate is deleting one user rather than rotating the credential every Mate shares.

Two calls, both server-side, both with credentials mate read in layer 2:

| # | Call                                                                                            | Auth                                    | Why that auth                                                                                                                                       |
| - | ----------------------------------------------------------------------------------------------- | --------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1 | `POST /api/v1/admin/users` `{username: "mate-<bot>", email, password, must_change_password:false}` | admin **token**                         | the `/admin` group is `tokenRequiresScopes(admin) + reqToken() + reqSiteAdmin()` — an `all`-scoped admin token passes                                  |
| 2 | `POST /api/v1/users/mate-<bot>/tokens` `{name, scopes:["write:repository"]}`                       | admin **password** (HTTP basic)         | this route is `reqSelfOrAdmin() + reqBasicOrRevProxyAuth()` — **a token cannot mint a token**; basic auth as a site admin can mint for any user         |

That second guard is the reason layer 1 publishes the password and not just the token. It is not an
oversight in Gitea; it is a deliberate "tokens do not breed tokens" rule, and it costs us one
sensitive env var.

The bot's token then lands where the fork already puts per-container secrets — the `envSecrets`
block of `buildZcpServiceImportYaml`, beside `VSCODE_PASSWORD` and the `ZCP_AGENT_AUTH_TYPE_*`
flags:

```yaml
    envSecrets:
      VSCODE_PASSWORD: "…"
      GITEA_URL: "https://web-1d76-3000.prg1.zerops.app"
      GITEA_TOKEN: "…"          # this Mate's own, write:repository
```

**Pre-set**, because it is in the import body the container is created from — the Mate has it before
its first boot, exactly like its VS Code password. **Settable**, because it is ordinary service env
afterwards: rotation is the delete-then-create upsert `enableZeropsMate` already implements, and
zcp's `git-push-setup` writes its own `GIT_TOKEN` per push-source service, so a Mate can be pointed
at a different host later without touching the birth values.

And the agent never has to ask the user for it. `git-push-setup` takes the token as an argument, so
the first prompt can say *"your Gitea is at `$GITEA_URL`, your token is in `$GITEA_TOKEN`"* and the
agent passes its own env through.

#### Two write points, one store — seeded at creation, filled by a button

The owner's shape (2026-09-06): *"add the credentials as we create the project, and have a physical
button to fill it where not already set."* That is right, and one platform detail is what makes it
safe rather than a collision.

**The two writes land in the same store.** `envSecrets:` in a *service import* YAML creates service
userData records, which is exactly what `POST /service-stack/{id}/user-data` creates. So the
credential seeded at creation and the credential the button writes are the same kind of record on
the same key, and the button can rewrite what creation wrote.

This would not be true of the other place a value can go. `zerops.yaml run.envVariables` is baked
into the app version, sits *above* service userData in the precedence chain, and **owns the key
namespace**: a userData write on a key that already exists in the yaml is rejected outright with
`userDataDuplicateKey` 400, *"key not unique in service stack frame of reference"*. A credential
put there could only ever be changed by editing the yaml and redeploying — the button would be
structurally impossible, not merely awkward. `[zcp spec-zerops-env-lifecycle §2]`

So: `GITEA_URL` and `GITEA_TOKEN` go in `envSecrets`, never in `run.envVariables`.

**"Where not already set" already exists, twice over.** `enableZeropsMate` is the same problem
solved: read the service env, leave an already-correct value **completely alone**, and only
delete-then-create when it is absent or wrong (the platform offers create and delete for a key and
no update). The projects page already renders that as a per-environment button with an `enable`
state beside `wait`, `starting` and `pending`. The Gitea button is that button with a different key
and one extra step in front of it.

| Moment          | How the value arrives                                                 | Restart? |
| --------------- | --------------------------------------------------------------------- | -------- |
| At creation     | in the import body, before the container's first boot                 | none     |
| Button, later   | `POST …/user-data` on the existing container, then restart            | yes      |

**The restart is the honest cost of the second one, and it is not avoidable by being clever.** A
service userData write reaches the container in about six seconds with no restart, but *new
processes only*. The Mate's agent is spawned by the long-running mate server, so it inherits that
server's boot-time environment and will not see a value written after it started. Fresh SSH sessions
do see it, which is why zcp's `git-push-setup` needs no restart and this does. Seeding at creation
avoids the restart entirely; the button pays it, exactly as the Mate-enable button already does.

**Where the button belongs.** On the Gitea tool card, as a roster of the account's Mates with their
connection state, because "which of my Mates can push?" is a question about Gitea, not about any one
environment. One button fills every Mate that is missing it. A per-environment `…` entry is the
fallback if the roster is too much for a first cut.

**This brings CORS back as a real dependency.** Minting the per-Mate Gitea user and its token are
Gitea API calls, and a button in the browser is the thing making them. Either the recipe enables
CORS for the mate origins (B-5) and the button stays a plain client action, or every press routes
through some mate server, which raises the question of *which* Mate's server provisions a different
Mate. Enabling CORS is the smaller change and the better shape.

**One assumption to prove before relying on rotation.** The delete-then-create branch has probably
never run against a key that came from `envSecrets`: `enableZeropsMate` leaves an already-correct
flag alone, so its delete path only ever executes on containers that never had the key. Deleting one
`envSecrets`-created key on a scratch container settles it, and it is a minute of work. If it turns
out those records refuse deletion, the fallback is to *not* seed at creation and let the button be
the only writer.

#### Q-B1 is answered, from source: the credential helper needs no change

zcp's helper emits `username=oauth2` and the token as the password
(`internal/ops/git_credential.go`). Gitea 1.27.2's basic auth, in `services/auth/basic.go`:

```go
isUsernameToken := len(passwd) == 0 || passwd == "x-oauth-basic"
authToken := uname
if !isUsernameToken {
    authToken = passwd          // a non-empty password IS the token
}
```

and `VerifyAuthToken` then resolves the user from `token.UID` — **the username is never compared
against the token's owner.** It is only used by the password-login fallback further down, which a
valid token never reaches. So `oauth2` (or any string) works, and `git-push-setup` runs against
Gitea unchanged. The live probe is now a confirmation, not a decision. `[source]`

#### What this changes in the plan

- §3.2 steps 2, 3 and 4 stop being manual: the recipe change removes the human from all three, and
  the runner token becomes a project variable the addon inherits.
- **B-5 grows** from "enable CORS" into the real item: the self-bootstrapping admin block above.
  CORS then matters less, not more — every call in layer 3 is server-side by necessity, so the
  browser never needs Gitea's API at all.
- **B-7 (new, S–M):** `buildZcpServiceImportYaml` takes optional `giteaUrl` / `giteaToken` and emits
  them into `envSecrets`; the creation dialog offers "connect this Mate to Gitea" when the account
  has a Gitea tool, the way it already offers a recipe choice.
- **B-8 (new, M):** the fill-it-in button for Mates that already exist, on the Gitea tool card.
- **CORS (B-5) is a dependency again,** not an optional extra: the button is a browser making Gitea
  API calls. Enabling it in the recipe is smaller than routing every press through a mate server.
- For the demo this is optional — one terminal, ten minutes, and the account has an admin. For the
  product it is the difference between a tool a user configures and a tool that configures itself.

---

### 3.9 The credential flow, designed for every case

§3.8 established that the credentials can exist without a human. This section is the flow itself:
what happens for a Mate created after Gitea, a Mate created before it, a Mate whose token was
revoked, a Mate that is asleep, and a Gitea that was rebuilt from scratch.

#### Three decisions, and why

**1. One Gitea user, one token per Mate.** Not a Gitea user per Mate, and not one token shared by
all of them.

The argument for per-Mate *users* is attribution, and it is wrong: a commit's author comes from
`git config user.name/email`, which `git-push-setup` already sets per service, not from the
credential that pushes. Per-Mate users would buy only push-log granularity, and they would cost a
permission model — a fresh Gitea user cannot push to a repository owned by someone else, so every
repository would need per-user collaborator grants or the whole thing would have to move into an
organization with a team. That is a lot of machinery for a log line.

Per-Mate *tokens* on one user give what actually matters. Gitea lets a user hold many named tokens,
so revoking one Mate is deleting one token, rotation is per Mate, and there is no permission matrix
because every token belongs to the user that owns the repositories. The scope is
`write:repository`, which is real (`models/auth/access_token_scope.go`) and confines the token to
repositories even though the user it belongs to is a site admin — the admin routes are gated by
`tokenRequiresScopes(admin)`, which such a token fails before `reqSiteAdmin()` is ever consulted.

The upgrade path stays open: if per-Mate repository permissions are ever wanted, the same flow mints
users instead of tokens, and everything below is unchanged.

**2. The token's name is the Mate's identity.** `mate/<bot name>`, deterministic, never random.
This is what makes the whole flow idempotent, because Gitea's
`DELETE /users/{u}/tokens/{token}` **accepts the name** when the path segment is not numeric
(`routers/api/v1/user/app.go`). So "give this Mate a working credential" is always the same two
calls — delete by name, create by name — whatever state it was in, including states we did not
anticipate. A token value is returned once and never readable again, so a lost value is always
replaced, never recovered.

**3. It is one reconcile, not two features.** There is a desired state — *every Mate has a live
credential for the account's Gitea* — and one function that moves a Mate to it. Creating a Mate is
not a separate mechanism; it is the case where the container does not exist yet and the write is
therefore free. This matters because the common timeline is the opposite of the flattering one: most
users will have Mates **before** they have Gitea, so the fill-it-in path is the general case and
seeding at creation is the special one. Designing the special case first is how you end up with two
half-mechanisms that disagree.

#### The reconcile

Inputs it reads, all cheap: the Mate's service env (does `GITEA_TOKEN` exist), and one Gitea call
listing the owner's tokens by name (`GET /users/{u}/tokens`, basic auth). One list covers the whole
roster, so the account view costs one call and not one per Mate.

| Observed                                        | Meaning                                       | Action                                                                        |
| ----------------------------------------------- | --------------------------------------------- | ----------------------------------------------------------------------------- |
| env set, token of that name exists in Gitea     | connected                                     | **nothing** — never rewrite a working value                                    |
| env set, no such token in Gitea                 | revoked upstream, or Gitea was rebuilt         | mint by name, write, restart if running                                        |
| env absent, token of that name exists           | interrupted run, or the container was replaced | delete by name, mint, write, restart if running                                |
| env absent, no token                            | never connected                               | mint, write, restart if running                                                |
| Mate exists, no Gitea tool on the account       | nothing to connect to                          | not offered; the card offers *Add Gitea* instead                               |
| Gitea present but not `ACTIVE`                  | too early                                      | leave pending, say so, reconcile when it comes up                              |
| Gitea `ACTIVE` but no admin credential in env   | old recipe, or the bootstrap block not landed  | fall back to today's copyable command on the tool card (§3.2 step 2)           |
| token exists, named for a Mate that is gone     | orphan                                         | offer cleanup; never delete silently                                           |

Two properties fall out of this table rather than being designed in. A Gitea whose volume is lost
puts **every** Mate in row 2 and one press repairs the account. And an interrupted run is always row
3, which is self-healing, because the deterministic name means a half-finished attempt leaves
something the next attempt recognises instead of an orphan it cannot see.

#### Where it runs, and what it costs

**At creation — free, and it must never block.** The credential goes into `envSecrets` in the import
body, so the Mate boots with it and there is no restart. That needs one new step in
`planEnvironmentCreation`, before `import-container` because the value has to be in that body:

```
create-project → provision-git? → import-container → import-recipe → await-ready
```

`provision-git` **fails soft**. If Gitea is down, or minting fails, or the account has no Gitea, the
step is skipped and creation continues; the Mate lands in row 4 and the button fixes it later. A git
host having a bad minute must never be able to stop someone making an environment.

**Afterwards — the button, and a restart only when one is owed.** `POST /service-stack/{id}/user-data`
lands in about six seconds and is seen by *new processes*, so a container that is running needs a
restart for its agent to see the value. A Mate that is **asleep does not**: it will read the value at
its next boot, so the reconcile writes the env and skips the restart. Sleeping Mates are repaired for
free, and nothing is woken up to be told something it could have read on its own.

That restart is not permanent, though, and it is worth removing rather than living with. Its whole
cause is that the agent inherits a long-lived parent's environment. If the mate server read the
container's live env source at agent-spawn time instead of its own process environment, every
credential written after boot would be picked up by the next turn, with no restart at all — for this
and for anything else we ever want to hand a running Mate. That is a small change in one place and it
is the better fix.

#### What the account looks like

- One Gitea user, `mate`, site admin, owns the repositories. Created by the recipe (§3.8), password
  and token published as its own sensitive env.
- One token per Mate, named `mate/<bot name>`, scope `write:repository`.
- On each Mate's zcp service: `GITEA_URL` and `GITEA_TOKEN` as `envSecrets`.
- Not to be confused with `GIT_TOKEN`, which zcp's `git-push-setup` writes on the *runtime* service
  that pushes. `GITEA_TOKEN` is what the Mate **has**; `GIT_TOKEN` is what it **installs** on
  `appdev` once it knows which repository that service pushes to. Different services, different keys,
  different lifetimes.
- Production environments have no agent and never take part: the runner deploys to prod with a
  **Zerops** integration token, not a Gitea one (§3.2 step 11). The roster is Mates only.

#### What this depends on

- **CORS in the recipe (B-5).** The button is a browser making Gitea API calls. The alternative is
  routing every press through a mate server, which forces the question of *which* Mate's server
  provisions a different Mate, and would send the admin credential to every container instead of
  holding it briefly in one tab. CORS is both smaller and safer.
- **The admin password, not just the token** — `GET`/`POST`/`DELETE` on `/users/{u}/tokens` are all
  guarded by `reqBasicOrRevProxyAuth()`. Tokens do not breed tokens in Gitea (§3.8).
- **One unproven assumption:** that a key created through `envSecrets` can later be deleted, since
  `enableZeropsMate`'s delete branch has only ever run on containers that never had the key. If it
  cannot, drop the creation-time seed and let the button be the only writer — the reconcile is
  unchanged, every Mate simply starts in row 4.


---

### 3.10 Built and run, 2026-09-06 — what held and what did not

Everything above was built on the live account rather than argued. The recipe change is
`zeropsio/recipe-gitea` branch `admin-bootstrap`; the Gitea it produced is `mate-gitea`; the
production it deploys to is `hello-go - production`. Measured facts are in the fork's ledger.

| Step                                            | Result                                                                                                |
| ----------------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| Import the patched recipe                       | **174 s** to a Gitea with an admin user, an `all`-scoped token and both published as sensitive env      |
| Read them back as the client would              | token authenticates as `mate`, `is_admin: true`; password authenticates the basic-auth-only routes      |
| Reconcile four existing Mates                   | all four read "never connected", each got `mate/<bot>`; a second run rewrote nothing                    |
| A Mate's own credential                         | clones and pushes; refused by `/user` and `/admin/...` with 403                                         |
| Q-B1, live                                      | `oauth2`, the owner's name and `nobody` all authenticate — the username is ignored                      |
| Runner addon                                    | import to one online runner in **100 s**                                                                |
| `git push origin v1.0.0`                        | Gitea Actions run #1 → `zcli push --setup prod` → production `ACTIVE` in **67 s**, serving over HTTPS    |
| Rotation                                        | deleting `GITEA_ADMIN_TOKEN` and restarting re-mints; both new values authenticate                      |

#### The Star Wars TODO group, wired the same way

Asked directly: was any of this set up for Fen's group? It was not — the run above built its own
group. So it was done afterwards, on `Acme Docs - production`, and that turned out to be worth more
than a demo: **that production had never deployed at all.** It had sat at `READY_TO_DEPLOY` since the
failed clone of 09-05.

| Piece                     | What was done                                                                                  |
| ------------------------- | ------------------------------------------------------------------------------------------------ |
| Repository                | `mate/acme-docs` in the account's Gitea, one repository per group                                 |
| Deploy token              | a second integration token, `ADMIN` on `Acme Docs - production` only, `NO_ACCESS` on the org       |
| Repository secrets        | `ZEROPS_TOKEN` and `ZEROPS_PROD_SERVICE_ID` on that repository                                     |
| Workflow                  | the file above, on `v*` tags                                                                       |
| Result                    | `v1.0.0` → `READY_TO_DEPLOY` → `ACTIVE`; `v1.0.1` re-deployed through the self-installing variant   |

Production answers on its own subdomain, database included. Public access had to be turned on
afterwards (`PUT /service-stack/{id}/enable-subdomain-access`): the call is rejected with 400 while
a service still has no deployed code, so it is a post-deploy step, not an import-time one.

**What is still not wired, and cannot be from outside the container:** Fen itself pushing to that
repository. `git-push-setup` runs inside the Mate and writes `GIT_TOKEN` on the service that pushes;
it is a zcp tool call in Fen's own conversation, not a platform call. Fen has the credential
(`GITEA_URL`, `GITEA_TOKEN` from the reconcile) and the repository exists, so the remaining step is
one instruction to Fen.

**And a real problem the group has, which the wiring exposed rather than caused:** its environments
are not the same application. `acme-docs-dev` runs `alpine/nodejs@22` with no git source, while stage
and production are `alpine/go@1.22` built from `go-hello-world-app`. That is the lossy-clone failure
of §2.3 sitting in the account: the dev environment a Mate works in has drifted from what production
runs. Deploying production from Gitea is correct and now works, but until Fen's own tree is what
feeds that repository, the group has a Mate developing one app and a pipeline shipping another.

#### Three things the design got wrong

**1. The runner token cannot come from the recipe.** §3.8 said minting it was "the same move one
call further". It is not: `gitea actions generate-runner-token` is not a database command, it posts
to the running server on `localhost:3000`, and nothing in the boot path runs with the server up. It
failed with `connection refused` on every boot. Worse, it sat *before* the publishing step under
`set -e`, so it took an admin password and token that already existed in the database down with it
and left a Gitea nobody held the credentials for. Two lessons, both now in the recipe: **publish a
credential the moment it exists**, and get registration tokens from
`POST /api/v1/admin/actions/runners/registration-token` with the admin token instead.

**2. Init commands were the wrong home, but not for the reason first recorded.** Init commands *do*
run on every container start — `zsc execOnce` is what makes one of them run once. What does not
re-run them is a **failing start command**, which the platform retries on its own. That is exactly
the first boot: `start.sh` exits until the secrets `init.sh` just wrote have propagated, so a script
in `initCommands` gets one look at an empty environment and is never reached again. Hanging it off
the start command is what made it work, and a later restart of a live service confirmed the rule by
running the init commands again with `execOnce` short-circuiting `init.sh`.

**3. Env propagation is slower than the ledger said.** Not ~6 s but ~15 s to reach a newly started
process. The recipe absorbs it in start-command retries, which is why they exist.

#### Smaller corrections

- `zeropsSetup` without `buildFromGit` is rejected outright (`projectImportInvalidParameter`,
  *"parameter is required for use of pipelineConfig"*). B-6's pipeline-first production service takes
  `startWithoutCode: true` and **no** setup name; the setup arrives with the first `zcli push --setup`.
- `enableSubdomainAccess: true` does not take on a `startWithoutCode` service — it came up
  `subdomainAccess: false` and answered 502 until `PUT /service-stack/{id}/enable-subdomain-access`.
- The service env read returns cross-service references **unresolved** (`GITEA_DOMAIN` comes back as
  the literal `web-${zeropsSubdomainHost}-3000...`), so a client builds public URLs from the
  project's `zeropsSubdomainHost` and `publicZone`, never from the variable.
- Re-minting leaves the previous token valid: Gitea's CLI cannot delete one. Recovery is fine;
  rotation after a leak needs the old entries revoked by hand.

### 3.11 Two-way sync with GitHub — what is actually on offer

Asked while the above was running. Gitea has two mirror mechanisms and neither is bidirectional, so
the honest answer is "one direction automatically, both directions only by ordinary git".

| Mechanism                | Direction      | Repo stays writable in Gitea? | How                                                                                      |
| ------------------------ | -------------- | ----------------------------- | ------------------------------------------------------------------------------------------ |
| **Push mirror**          | Gitea → GitHub | **yes**                       | `POST /repos/{o}/{r}/push_mirrors` — `remote_address`, `remote_username`, `remote_password`, `interval`, `sync_on_commit` |
| **Pull mirror**          | GitHub → Gitea | **no** — read-only            | `mirror: true` + `mirror_interval` at migrate time; Gitea force-updates from the remote      |
| Plain second remote      | both           | yes                           | no Gitea feature at all — the agent pushes and pulls both remotes, git resolves conflicts    |

The useful one for Mate is the **push mirror**, and it fits the product exactly: Mates work in the
account's own Gitea, and every push is copied to GitHub within seconds (`sync_on_commit: true`) so
the team keeps its usual home, its reviews and its badge. It needs a GitHub token stored in Gitea per
repository, which the same reconcile could set.

What cannot be done is making both sides authoritative and having Gitea reconcile them. A pull mirror
is a force-update: anything committed on the Gitea side between syncs is discarded, and Gitea marks
such a repo read-only precisely so nobody tries. Combining a pull mirror with a push mirror on one
repository is a loop with a data-loss branch, not a sync.

If genuine two-way is ever wanted, the answer is the third row: no mirroring, GitHub as a second git
remote, and a merge instead of a force-update. That is a Mate task rather than a Gitea setting, and
`git-push-setup` already models one remote per service, so it would need a second.


---

### 3.12 Why `git-push-setup` is in the way, and what should replace it

Asked after the group was wired: why does any of this depend on `git-push-setup` — shouldn't zcp be
more flexible? It should. The dependency is not technical necessity, it is a modelling choice, and
today's two Gitea results are the second and third time it has cost something.

**It is not there because the agent cannot do git.** The agent has a shell on the container, and
`zerops_env action=set serviceHostname=… ` already writes a sensitive service-scope variable — the
durable home a credential needs, since `/var/www` is replaced on redeploy. Remote, credential helper
and push are three commands it can write itself.

**It is there because zcp models delivery as a ladder and made this rung one.** `GitPushState`
is a stamp on `ServiceMeta`, and everything above reads it: build integration is refused unless it is
`configured` (`topology/delivery.go`), delivery state is derived from it (`delivery_state.go`), and a
production launch is gated on it (`launch_source_control_gate.go`). So the tool is not a convenience
an agent may skip; it is the only door into the rest of the system.

Three costs, all visible in the tree rather than hypothetical:

- **The stamp is history, not truth.** It records that one probe passed once. The truth lives in
  `/var/www/.git/config` and the service env, and it drifts — `types.go` names "manual rewrite or
  recipe-template carryover" as a cause. The proof is the repair machinery that exists to chase it:
  a `broken` state, `gitPushReconstruct`, and a whole `adopt_gitpush_reconcile.go`. That is a lot of
  code maintaining a cache of something one SSH command could answer on demand.
- **One remote per service.** `meta.RemoteURL` is a single string. The GitHub push-mirror question of
  §3.11 is therefore not merely unimplemented, it is unrepresentable — a service cannot have two
  remotes in this model no matter what the agent does.
- **The wrapper's assumptions are the ceiling.** HTTPS only, SCP-form SSH rejected outright.
  Identity derivation is GitHub-only. The credential helper hardcodes `username=oauth2`, which works
  on Gitea purely because Gitea ignores the username (§3.8). Every host the wrapper did not
  anticipate needs a zcp release rather than a different command from the agent — which is exactly
  the shape of work items B-3 and B-4.

#### The fix is one inversion

**Gates should test the world, not the history.** Replace "did you run our tool?" with "can this
service push?", answered when it is asked: read `.git/config` and check the credential resolves, or
just run the probe that `git-push-setup` already runs. It is one SSH round trip, it is never stale,
and it deletes the `broken` state, the reconcile and the reconstruction along with the cache they
repair.

With that inversion the rest falls out:

- `git-push-setup` stays, demoted to **sugar**: one call that does the safe common case well — probe
  the token, write it at service scope with `sensitive: true`, sync origin, seed an identity. Most
  agents should still call it, and its structured errors are genuinely good.
- Nothing downstream requires that *it* was what configured the service. An agent that wired a
  deploy key, a second remote for mirroring, or a monorepo subtree passes the same empirical check.
- The credential's durable home stays a service-scope secret, because that is a platform fact rather
  than a preference. `zerops_env` already exposes it; the agent should be told that is where it goes.

What is worth keeping from the current design is the *discipline*, not the *stamp*: probe before you
trust, and give the agent structured errors it can act on. Moving that probe from setup time to use
time keeps both and gives up nothing.


---

### 3.13 Can B-11 be executed cleanly? — a read of the actual tree

Short answer: **yes, and more cheaply than §3.12 implied — but not as "delete the stamp".** The change
that is clean is narrower and better: *the stamp stays as a cache and stops being allowed to block.*

#### What the tree already contains

The inversion is not a new idea in zcp. It is written, three times, at three different moments.

| Where                                   | What it already does                                                                                       |
| --------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| `tools/adopt_gitpush_reconcile.go`      | derives push capability **from the world** — live `origin` **and** a `GIT_TOKEN` service secret — and stamps it |
| `tools/launch_source_control_gate.go`   | reads `LiveRemoteURL` at gate time and compares it with the record (its "Check 3")                            |
| `tools/workflow_export_probe.go`        | `refreshRemoteURLCache` compares live against cached and warns on drift                                        |

`ServiceMeta` itself already says the quiet part: `RemoteURL string // cache; runtime source of truth =
git remote get-url origin`. So one of the two fields is *documented* as a cache, and the code around it
behaves accordingly. `GitPushState` is the field that never got the same treatment.

Better still, `adopt_gitpush_reconcile.go`'s own comment is the argument for B-11, written by someone
who hit the bug: re-asserting `unconfigured` on an already push-capable service *"makes the launch
source-control gate force a needless git-push-setup re-run … the only normal path into the
credential-rotation branch that can DESTROY the working token"*. They fixed it at one entry point.
B-11 is that same fix applied where the question is asked instead of where a service happens to enter.

**The predicate is therefore already specified and proven in production code:**

> push-capable ⟺ a live `origin` exists **and** the `GIT_TOKEN` service secret exists (presence only)

using two helpers that already exist: `readGitRemoteURL` (SSH) and `ops.EnvHasServiceKey` (platform,
never reads the value).

#### Why the blast radius is smaller than the file count suggests

Thirty non-test files mention the state and forty-four test files assert on it — 220 assertions. That
number is misleading, because **the decision logic is pure and takes the state as an input**:

```go
func RecommendDelivery(in DeliveryInputs) DeliveryDecision   // topology/delivery.go
func DeriveDeliveryState(gitPush GitPushState, hasProdLaunches bool) DeliveryState
```

Neither cares whether the value arrived from a stamp or a probe. So the topology layer and its tests
do not change at all. What changes is **provenance at a handful of boundaries** — and there are far
fewer than thirty:

| Boundary                              | Today                             | After                                                          |
| ------------------------------------- | --------------------------------- | ---------------------------------------------------------------- |
| `topology/delivery.go` via its caller | refuses on the stamp alone        | confirm live before refusing                                     |
| `launch_source_control_gate.go`       | already live                      | unchanged                                                        |
| `deploy_repo_delivery.go`             | reports state from the stamp      | report from the cache, label it as such                          |
| `compute_envelope.go`                 | stamp into every snapshot         | **unchanged** — see the hot-path constraint below                |
| `deploy_git_push.go`                  | stamps `broken` on failure        | report the failure; stop making it sticky                        |
| `adopt_gitpush_reconcile.go`          | reconciles at adopt               | redundant as a correctness fix; keep only as cache warming       |

Only **two** places construct the decision inputs (`workflow_git_push_setup.go:1174` and
`deploy_repo_delivery.go:52`). That is the whole surface that decides anything.

#### The four things that make "just delete the stamp" wrong

1. **The envelope is a hot path.** `ComputeEnvelope` runs on every tool call from three call sites and
   builds a snapshot **per service**, with `GitPushState` feeding atom matching — which next-actions the
   agent is even shown. A live check there is one SSH round trip plus one API call per service per tool
   call. Not acceptable. The cache has to stay for display and atom selection.
2. **Services go offline.** A stopped or sleeping service cannot be reached over SSH. If the live check
   is authoritative in both directions, an asleep service becomes "not push-capable" and the agent is
   sent to re-run setup — reintroducing the exact token-destroying path the adopt reconcile was written
   to avoid. **The live check must only ever be able to unblock, never to newly block.**
3. **Local mode has no `GIT_TOKEN`.** Outside a container, auth is the user's own credential helper, and
   `adopt_gitpush_reconcile` bails on `!rt.InContainer` for precisely this reason. The predicate has to
   be mode-aware: container is origin + secret; local is origin + a successful `git ls-remote`.
4. **`broken` is sticky, and under an empirical model it should not be.** It is written when a push
   fails and then persists as a state. Derived, "broken" is just "the last push failed", which belongs in
   the response, not in the record.

#### The change that is actually clean

> **No gate may refuse an action on the stamp alone. A gate that would refuse confirms against the
> container first, and a failed or impossible confirmation falls back to the cached answer — never to a
> new refusal.**

That keeps every property worth keeping and drops the one that hurts:

- the envelope stays cheap, because the cache still drives display and atoms;
- an agent that wired its own remote — deploy key, second remote for a GitHub mirror, monorepo subtree —
  passes the gate, because the gate asks the container rather than the history;
- an offline service degrades to today's behaviour exactly;
- the adopt-time reconcile stops being load-bearing;
- `RemoteURL` and `GitPushState` finally have the same, already-documented status: a cache.

#### Shape of the work

| Step | Work                                                                                                                              | Size |
| ---- | ----------------------------------------------------------------------------------------------------------------------------------- | ---- |
| 1    | Extract the predicate from `adopt_gitpush_reconcile.go` into one mode-aware `DerivePushCapability(ctx, …)` with its own table tests | S    |
| 2    | Make the two `DeliveryInputs` construction sites confirm-before-refuse, unblock-only                                              | S    |
| 3    | Stop stamping `broken`; report the push failure instead                                                                            | S    |
| 4    | Demote `adopt_gitpush_reconcile` to cache warming, or delete it once step 2 covers its case                                       | S    |
| 5    | Tests: the 220 pure-layer assertions stand; rework the boundary tests (adopt reconcile, export probe, deploy_repo_delivery)       | M    |

Realistically **M, not L** — one new predicate, two call sites, one deletion, and a test pass over
roughly half a dozen files. The risk is concentrated in step 2's fallback direction, and that is exactly
the kind of thing a table test pins down: *offline ⇒ cached answer, never a new block.*

The one thing worth deciding before starting: whether `git-push-setup` should also stop being the only
writer of the credential, i.e. whether the agent is *told* that `zerops_env action=set` on `GIT_TOKEN`
plus its own `git remote add` is a supported path. B-11 makes that work; documenting it is what makes
agents actually use it.


### 3.14 How the client actually gets the credential — settled 2026-09-06

Everything in §3.8/§3.9 assumed the client could read what the recipe publishes. Measured on a cold
account, it cannot: **`GET /service-stack/{id}/user-data` answers the literal string `REDACTED` for
every `sensitive: true` entry when the caller holds a user access token** — the token
`POST /auth/login` returns, which is exactly what a browser client has. The earlier "sensitive values
come back in clear" reading was taken with a zcli token. An **integration token** reads them in clear.

That leaves three ways to give a Mate a Gitea token, and only one survives.

| Path | Verdict |
| --- | --- |
| The client keeps an org-wide integration token | No. A browser holding `ADMIN` over the whole organization is a much larger key than the job needs. |
| The client generates the admin password itself and passes it in at import | No. It then has to keep it — durably, across devices — and the recipe stops being self-sufficient. |
| **The client mints a token when it needs one, uses it, and revokes it** | Yes. |

The third works because an integration token is *derivable on demand*: minting one needs nothing but
the user session the client already has, so there is no secret at rest anywhere.

```
POST   /client/{clientId}/integration-token
       {name, roleCode: "NO_ACCESS", canCreateProjects: false,
        projects: [{projectId: <the Gitea project>, roleCode: "ADMIN"}]}   → 200, token once
GET    /service-stack/{gitea web}/user-data                               → the three values, in clear
DELETE /client/{clientId}/integration-token/{tokenId}                     → 200
```

Both ends measured: the scoped token reads `GITEA_ADMIN_PASSWORD` (12 chars) and `GITEA_ADMIN_TOKEN`
(40 chars) in clear and nothing outside that project, and the revoke answers 200
(`DELETE /integration-token/{id}` without the client prefix is a 404).

**The second gate is CORS.** `fetch('https://web-….zerops.app/api/v1/version')` from a page on another
origin fails with `Failed to fetch` — so a browser cannot mint a `mate/<bot>` token, and cannot run the
probe `tools.ts` documents either. `zeropsio/recipe-gitea` PR #3 enables `[cors]` with
`ALLOW_CREDENTIALS = false`; nothing else in the client half can be built until an instance is rebuilt
on it.

So B-8 becomes, in order: rebuild Gitea on the CORS change → mint-read-revoke for the admin password →
`POST /users/{admin}/tokens` with basic auth for `mate/<bot>` → write `GITEA_URL`/`GITEA_TOKEN`/
`GITEA_REPO` onto the Mate's zcp → restart it. B-9 is the same sequence run from
`planEnvironmentCreation` as one more step, so a Mate created into an account that has a Gitea is born
with its credential.

---

## 4. Platform facts measured today `[live]`

| Fact                                                                                                                                                                                                                                                                                    | Evidence                                                                           |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| Every dev-container import mints its own integration token, `zcp-<project name>`: `roleCode: NO_ACCESS`, `canCreateProjects: false`, `ADMIN` on exactly one project; `created` matches the environment creation to the second. Seven such tokens on the org.                        | `GET /client/{id}/integration-token/list` (the bare `…/integration-token` GET → 405) |
| The export of `acme-docs-dev` returns the project-level `vault.ZCP_API_KEY` in clear; the zcp service's vault entries come back `REDACTED` with `sensitive: true`, except the non-sensitive flag `ZCP_AGENT_OAUTH_CLAUDE_CODE`, also in clear.                                        | `GET /project/Mx0EAKnDTm2tTnA40uwpFw/export`, key names and redaction flags only   |
| The live Gitea (`web-1d76-3000.prg1.zerops.app`) is `1.27.2`, has **no user** and **no runner service**; its API answers with no `Access-Control-Allow-Origin` (preflight → 405), so a browser on another origin cannot call it.                                                        | `/api/v1/version`, `/api/v1/users/search?q=`, an `Origin:` probe, service-stack read |
| `Acme Docs - production` still holds `app` at `READY_TO_DEPLOY` (the 09-05 clone's failed build); stage's `app` was built from git on 09-05.                                                                                                                                              | service-stack reads                                                                |
| `RequestClientIntegrationToken`: `name`, `roleCode` ∈ {OWNER, ADMIN, BASIC_USER, READ_ONLY, NO_ACCESS} (default NO_ACCESS), `canCreateProjects`, `canViewFinances`, `canEditFinances`, `projects[] {projectId, roleCode}`; also `PUT …/integration-token/{id}/regenerate`, `DELETE …/{id}`. | `swagger/openapi.yml` (890 KB, the Swagger UI's `url`); zerops-go SDK v1.0.20 paths |
| **`GET /service-stack/{id}/user-data` returns sensitive values in clear** to an owner-role token. On Gitea's `web`: `SECRET_KEY`, `INTERNAL_TOKEN`, `JWT_SECRET`, `LFS_JWT_SECRET` and `DB_PASSWORD` all came back readable, each flagged `sensitive: true`. This is the channel a container-minted credential travels back out on (§3.8) — and the contrast with the export, which *does* redact service vault entries. | `GET /service-stack/YI8dOA9KSTqQu4mNBADwYQ/user-data?limit=40`, 35 entries |
| Gitea 1.27.2 can bootstrap its own admin: `gitea migrate` exists so `admin user create` can run before the server (`initDB` uses `db.InitEngine`, no migration; `migrate` uses `InitEngineWithMigration`), and `admin user create --random-password --access-token` prints password and token and defaults `--access-token-scopes` to `all` (a scope-less token is rejected outright, so #33474 is not reachable). `[source]` | `cmd/migrate.go`, `cmd/admin_user_create.go` at tag `v1.27.2` |
| **Gitea ignores the username in HTTP basic auth when a token is the password.** `parseAuthBasic` sets `authToken = passwd` for any non-empty password; `VerifyAuthToken` resolves the user from `token.UID` and never compares the username. So zcp's `username=oauth2` credential helper authenticates as the token's owner, unchanged. `[source]` | `services/auth/basic.go` at `v1.27.2`; answers Q-B1 |
| Minting a Gitea token for another user needs **basic auth, not a token**: `/users/{u}/tokens` is guarded by `reqSelfOrAdmin() + reqBasicOrRevProxyAuth()`, while `/admin/users` accepts an `all`-scoped admin token (`reqSiteAdmin()`). A per-Mate identity therefore needs the admin password published beside the admin token. `[source]` | `routers/api/v1/api.go` lines 1080–1088, 1802–1854 |
| `RequestFirstClassRecipeDevelopmentContainer`: `serviceImportYaml` (required), `recipeSource`, `recipeSourceUrl`, `createIntegrationToken`.                                                                                                                                             | `swagger/openapi.yml`                                                              |
| Project import in one call exists: `POST /client/{id}/project/import` (yaml with `project:` block incl. `tags`); the fork uses `POST /client/{id}/project` + service import instead so it owns the tags and can insert the container.                                                         | SDK `PostClientProjectImport`; zcp `CreateAndImportProject`; Zerops import docs   |

---

## 5. Open questions

| Id   | Question                                                                                                                                                                | How to answer                                                                                                                                                                                                                                             |
| ---- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Q-A1 | Exact shape of a go-hello-world dev environment's export (does `verticalAutoscaling` survive, does `db` keep `profile`?)                                               | Create the dev tier once (or use zcp `route=recipe`), `GET /project/{id}/export`, diff against the recipe. Read-only after the creation.                                                                                                                   |
| Q-A2 | Does `zerops_import` inside a `route=classic` bootstrap session leave that session recoverable (`close`/`reset`) before `route=adopt`?                                    | Run the sequence on a scratch Mate; record the session state file transitions. Decides whether A-3 is needed for the demo or only for the product.                                                                                                        |
| Q-A3 | Does a token refresh in one container invalidate a copied `~/.claude/.credentials.json` in another (`questions.md` Q-12)? Decides whether M1 needs the re-copy loop or is simply fine.    | Copy the file into a second throwaway container, leave both past `expiresAt`, run `claude -p` on each, diff `expiresAt` and mtime only — never contents. One evening, two containers.                                          |
| Q-A4 | Does `claude setup-token` run through mate's login walker inside the container (it prints the token at the end instead of "Login successful"), and does `CLAUDE_CODE_OAUTH_TOKEN` in the agent's env win over a stale credential file as the precedence list says? | Run it once in a throwaway Mate's login terminal; then start the agent with the variable set and a deliberately expired file present; `/status` names the active method. |
| Q-B1 | ~~Does Gitea accept zcp's credential helper (`username=oauth2`)?~~ **Closed — source and live (§3.8, §3.10).** `git ls-remote` succeeded with `oauth2`, with the owner's own name and with `nobody`; the username is never compared to the token's owner. `git-push-setup` needs no Gitea flavour. | done |
| Q-B2 | Is `BASIC_USER` on the prod project enough for `zcli push`, or does it need `ADMIN`? (`ADMIN` is confirmed working, §3.10 — the cheaper role is still untested.)          | Mint a second token with `roleCode: BASIC_USER` on the prod project, re-run the same workflow, revoke it.                                                                                                                                                  |
| Q-B3 | Does Gitea 1.27 on the recipe read `.github/workflows/` as well as `.gitea/workflows/`?                                                                                   | Push a trivial workflow under each path to a test repo once runners exist; look at Actions.                                                                                                                                                               |
| Q-B4 | Does the platform's `buildFromGit` accept a public repository on a Gitea host (`https://web-1d76-3000.prg1.zerops.app/admin/go-hello-world-app`)?                        | Import one `golang@1.22` service with that URL into `scratch-playground`; watch `stack.build`; delete the service. Decides §3.5's first bullet.                                                                                                             |
| Q-B5 | ~~How long does the runner addon take from import to idle runners?~~ **Closed (§3.10):** 100 s from import to one online runner, `zcli` download included. | done |

---

## 6. Recommended order for the demo

> **The executable version of this section is `z3-gitea-demo-runbook-2026-09-06.md`** — every call,
> every YAML, every measured timing and every trap, written to be run from an empty account without
> re-deriving any of it.


> **Steps 1, 3 and 4 are done** (§3.10, §3.11): the account has a Gitea that minted its own
> credentials, a runner, two repositories, and two groups deploying to production on a tag. What
> remains of this list is step 2 — a Mate that pushes its own work — and step 5.

1. ~~**Gitea ready**~~ — **done, and it is no longer a user step.** The recipe mints the admin user
   and its token on first boot (merged into `zeropsio/recipe-gitea`); the runner token comes from
   `POST /api/v1/admin/actions/runners/registration-token`. Q-B1 and Q-B5 are closed.
2. **The group** (mate client, exists): "Go Hello World" → "Add dev" with an agent from the dev
   tier; sign the agent in; `route=adopt`; `git-push-setup` against the Gitea repository; first
   push. Then "Add production" from a pipeline-first prod tier (B-6 is a two-line edit of the seed).
3. ~~**Prod token + secrets + workflow**~~ — **done for both groups**, by API rather than by hand.
   Product work is B-2 (the client doing it) and B-9 (seeding at creation).
4. ~~**Release** → production `ACTIVE` → URL~~ — **done twice.** `hello-go - production` and
   `Acme Docs - production` both deploy on a `v*` tag through Gitea Actions.
5. **Second Mate**: A-1 (clone-specific prompt) is the one fork change worth making before the
   demo — with it the A2 flow (Mate imports, fails, fixes) is a prompt plus today's zcp tools; A-3
   makes it clean. The agent's login travels with the clone: for the demo, copy
   `~/.claude/.credentials.json` from the source Mate into the new one over the project VPN (H-24's
   recipe, one `scp`); A-5 is the product form. Everything else in §2.5/§3.7 is product work after
   the demo.

Two tokens, two fates. **zcp's API key never moves**: the platform mints one per project, the
export leaks the old one, and zcp would follow a copied one to the wrong project. **The agent's
login does move** — as the file it already is today, as a one-year token once S7-5 lands. Beside
it, what moves between environments is *shape* (services YAML) and *intent* (the first prompt) —
and the Mate's job is to turn shape into a running application, which is exactly the
failure-handling the owner wants to show.

---

## 7. Sources

Fork (`../z3`): `packages/client-runtime/src/zerops/{createEnvironment,runEnvironmentCreation,recipeExport,recipeStore,recipeStoreSeed,giteaRecipe,tools,groups,newProject,api,firstPrompt,handover}.ts`;
`apps/web/src/components/zerops/{ZeropsEnvironmentCreationDialog.logic.ts,ZeropsProjectsPage.tsx,ZeropsGroupTree.tsx}`;
`apps/web/src/zerops/{useZeropsCloneSources,composeFirstPrompt,recipeStore}.ts`; `apps/web/src/routes/_chat.index.tsx`;
`apps/server/src/zerops/ZeropsAgentAuth.ts`; `docs/internals/zerops/{verified,hacks,questions,map}.md` (H-24, H-26, the 2026-09-05 sections).

zcp (this repo): `internal/tools/{import,guard,workflow_bootstrap,workflow_git_push_setup,workflow_build_integration,workflow_release,workflow_launch_production,launch_existing,launch_delegation,launch_pipeline,deploy_git_push,workflow_export}.go`;
`internal/ops/{import,git_credential,deploy_git_push,service_git_init,github_user}.go`; `internal/workflow/{route,engine}.go`;
`internal/platform/{zerops_delegation,project_admin}.go`; `internal/auth/auth.go`; `internal/content/atoms/{setup-build-integration-actions,setup-git-push-container,develop-git-push-delivery,launch-post-checklist}.md`;
`internal/knowledge/recipes/go-hello-world.{md,import.yml}`; `docs/spec-mate.md` §0, §4.7–4.8, §6; `docs/spec-workflows.md` §9–10; `docs/spec-launch-production-platform-spike.md` A.10.

Platform: `https://api.app-prg1.zerops.io/api/rest/public/swagger/openapi.yml`; zerops-go SDK v1.0.20 (`sdk/*.go` paths); Zerops docs — import/export reference, build & deploy pipeline, zcli commands and configuration, RBAC, GitHub integration, REST API; zcli CHANGELOG (`ZEROPS_TOKEN`).

Gitea: `zeropsio/recipe-gitea` (`README.md`, `zerops.yaml`, `zerops-project-import.yaml`, `zerops-runner-import.yaml`, `runner-init.sh`, `app.ini`); Gitea docs — Actions overview/quickstart/comparison/FAQ, runner (`act_runner`), API usage, command line (`admin user create`, `generate-access-token`, `actions generate-runner-token`), API operations `repoMigrate`, `createCurrentUserRepo`, `repoCreateHook`, repository action secrets (`PUT /repos/{owner}/{repo}/actions/secrets/{secretname}`); Gitea issues #33474 (scope-less CLI token).
