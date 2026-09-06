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

**Q1 — yes, with two corrections and one zcp gap.**

| Owner's step                                                     | Verdict                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| ---------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1. "dám naklonovat prostředí"                                    | **Exists.** The group page's "Add dev/stage/…" dialog offers *Clone `<Mate>` (`<environment>`)*, built from the sibling's `GET /project/{id}/export` with its container and every secret block stripped (`createEnvironment.ts`, `recipeExport.ts`). `[code]` `[ledger]`                                                                                                                                                                                                                                                                                                                                                              |
| 2. "vytvoří se prázdnej projekt s zcp (se zkopírovaným tokenem)" | **Exists, minus the copying — and the copying must not happen.** `POST /client/{id}/project` (tags at birth) then `PUT /project/{id}/first-class-recipe/development-container` with `createIntegrationToken: true`; the platform mints a token **per project** (`zcp-<project>`: `NO_ACCESS` on the org, `ADMIN` on exactly that project) `[live]`. A token copied from environment A into B's zcp would make B's zcp operate **A** — zcp binds to the single project the token can see `[code]`. If "token" meant the *agent's* sign-in (Claude), the ledger rule stands: creation must not copy a credential (H-24 is the eval hack) `[ledger]`; the product-shaped alternative is a token seed at import time — `[open]`, §8. |
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

### 2.1 The token: two readings, one answer each

**Reading 1 — zcp's API key.** Not copyable in any useful sense:

- The platform mints one token per dev-container import (`createIntegrationToken: true`), scoped to
  that one project (`NO_ACCESS` org role, `ADMIN` on the project, `canCreateProjects: false`)
  `[live]`. zcp resolves its project from the token: no scope → `ListProjects` → exactly one → that
  project (`internal/auth/auth.go` `discoverProject`) `[code]`. A's token in B's container means B's
  agent operates **A**, silently.
- The one place it *is* copyable from — `GET /project/{id}/export` — returns the project-level
  `vault.ZCP_API_KEY` **in clear** while the zcp service's vault comes back `REDACTED` `[live]`
  `[ledger]`. That is a reason `recipeFromProjectExport` strips every vault block before anything is
  shown or stored, not a channel to reuse.
- Cross-project acts belong to the **user's** token in the client (spec §0: identity is the
  client's), or to zcp's one-time delegation, which exists only to mint a `canCreateProjects` token
  for launch-production (`integration-token/{id}/delegation`).

**Reading 2 — the agent's sign-in.** A fresh container's agent starts unauthenticated; the ledger
rule from the 09-05 creation run is explicit: *creation does not, and must not, copy a credential in
(H-24 is the eval hack, not the product)*. Each new Mate goes through the S7 authorization flow (a
device login, about a minute). The product-shaped shortcut would be a **token seed**: zcp already
reads `ZCP_AGENT_TOKEN_<SUFFIX>` flags (spec MA-7) and names a `setup-token` verb (S7-5); whether a
long-lived Claude token can be carried in the dev-container import's `envSecrets` so the Mate is
signed in from birth is `[open]` (Q-A3). For the demo, plan one sign-in per Mate.

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
| A-5 | both | Recipe store written by zcp's `export`, read by mate (H-26) — the real fix for lossy clones.                                                                                                                                                                              | L    |

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
| 2  | Admin user                                                                                       | **user, shell in `web`** (`zcli vpn up && ssh web`, or the GUI Remote Web Terminal) | `gitea admin user create --config /etc/gitea/app.ini --admin --username admin --email … --password '…' --must-change-password=false`                                                                                                                       | **not done** `[live]`; cannot be automated from mate (another project's container) |
| 3  | An admin API token for automation                                                                | same shell, or basic auth                 | `gitea admin user generate-access-token --config /etc/gitea/app.ini --username admin --token-name mate --scopes all` — or `POST /api/v1/users/admin/tokens {"name":"mate","scopes":["all"]}` with basic auth (`gitea admin user create --access-token` mints a scope-less, useless token — Gitea #33474) `[docs]` | manual once                                |
| 4  | Runners                                                                                          | shell (token) + mate client (import)      | `gitea actions generate-runner-token --config /etc/gitea/app.ini` → import `zerops-runner-import.yaml` with the token (`buildGiteaRunnerImportYaml` exists; no button yet). 3 containers register as `ubuntu-latest:host`, `ubuntu-26.04:host`; each ships `git`, `nodejs`, `zcli` `[docs]` | **not done**; needs the button (B-1)       |
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

```yaml
# .gitea/workflows/deploy-prod.yaml  — in the go-hello-world-app repository
name: deploy-prod
on:
  push:
    tags: ["v*"]                       # the release act tags vX.Y.Z; branch pushes never deploy
jobs:
  deploy:
    runs-on: ubuntu-latest             # label ubuntu-latest:host → the job runs on the runner container itself
    steps:
      - uses: actions/checkout@v4      # JS action; the runner ships nodejs + git for exactly this
      - name: Deploy to production
        env:
          ZEROPS_TOKEN: ${{ secrets.ZEROPS_TOKEN }}                 # project-scoped token for the prod project (step 11)
        run: |
          zcli push --service-id "${{ secrets.ZEROPS_PROD_SERVICE_ID }}" --setup prod --version-name "${GITHUB_REF_NAME}"
```

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
| B-5 | recipe-gitea | `[cors] ENABLED = true` with the mate origins allowed, if the browser client is to talk to Gitea directly (else B-1/B-2 go through the mate server). Fix the fork's runner template (`zeropsSetup: runner`).               | S    |
| B-6 | fork        | A pipeline-first prod recipe for the seed group (`startWithoutCode: true`, no `buildFromGit`) — the seed's prod tier pulls from GitHub, which is the wrong first build once Gitea owns the code.                             | S    |

---

## 4. Platform facts measured today `[live]`

| Fact                                                                                                                                                                                                                                                                                    | Evidence                                                                           |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| Every dev-container import mints its own integration token, `zcp-<project name>`: `roleCode: NO_ACCESS`, `canCreateProjects: false`, `ADMIN` on exactly one project; `created` matches the environment creation to the second. Seven such tokens on the org.                        | `GET /client/{id}/integration-token/list` (the bare `…/integration-token` GET → 405) |
| The export of `acme-docs-dev` returns the project-level `vault.ZCP_API_KEY` in clear; the zcp service's vault entries come back `REDACTED` with `sensitive: true`, except the non-sensitive flag `ZCP_AGENT_OAUTH_CLAUDE_CODE`, also in clear.                                        | `GET /project/Mx0EAKnDTm2tTnA40uwpFw/export`, key names and redaction flags only   |
| The live Gitea (`web-1d76-3000.prg1.zerops.app`) is `1.27.2`, has **no user** and **no runner service**; its API answers with no `Access-Control-Allow-Origin` (preflight → 405), so a browser on another origin cannot call it.                                                        | `/api/v1/version`, `/api/v1/users/search?q=`, an `Origin:` probe, service-stack read |
| `Acme Docs - production` still holds `app` at `READY_TO_DEPLOY` (the 09-05 clone's failed build); stage's `app` was built from git on 09-05.                                                                                                                                              | service-stack reads                                                                |
| `RequestClientIntegrationToken`: `name`, `roleCode` ∈ {OWNER, ADMIN, BASIC_USER, READ_ONLY, NO_ACCESS} (default NO_ACCESS), `canCreateProjects`, `canViewFinances`, `canEditFinances`, `projects[] {projectId, roleCode}`; also `PUT …/integration-token/{id}/regenerate`, `DELETE …/{id}`. | `swagger/openapi.yml` (890 KB, the Swagger UI's `url`); zerops-go SDK v1.0.20 paths |
| `RequestFirstClassRecipeDevelopmentContainer`: `serviceImportYaml` (required), `recipeSource`, `recipeSourceUrl`, `createIntegrationToken`.                                                                                                                                             | `swagger/openapi.yml`                                                              |
| Project import in one call exists: `POST /client/{id}/project/import` (yaml with `project:` block incl. `tags`); the fork uses `POST /client/{id}/project` + service import instead so it owns the tags and can insert the container.                                                         | SDK `PostClientProjectImport`; zcp `CreateAndImportProject`; Zerops import docs   |

---

## 5. Open questions

| Id   | Question                                                                                                                                                                | How to answer                                                                                                                                                                                                                                             |
| ---- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Q-A1 | Exact shape of a go-hello-world dev environment's export (does `verticalAutoscaling` survive, does `db` keep `profile`?)                                               | Create the dev tier once (or use zcp `route=recipe`), `GET /project/{id}/export`, diff against the recipe. Read-only after the creation.                                                                                                                   |
| Q-A2 | Does `zerops_import` inside a `route=classic` bootstrap session leave that session recoverable (`close`/`reset`) before `route=adopt`?                                    | Run the sequence on a scratch Mate; record the session state file transitions. Decides whether A-3 is needed for the demo or only for the product.                                                                                                        |
| Q-A3 | Can a coding agent be signed in from birth by a token seed in the dev-container import (`ZCP_AGENT_TOKEN_<SUFFIX>` / `setup-token`), and is that acceptable under S7?    | Read `internal/init/adapters/*` for the token consumer; test on a throwaway container with a `claude setup-token` value; decide with the owner — it trades one sign-in per Mate for a shared long-lived credential.                                          |
| Q-B1 | Does Gitea accept zcp's credential helper (`username=oauth2`, token as password) or require the username to match the token owner?                                        | `GIT_TERMINAL_PROMPT=0 git ls-remote https://oauth2:<token>@web-1d76-3000.prg1.zerops.app/admin/repo.git HEAD` after step 2/3. One command; decides whether `git-push-setup` works on Gitea unchanged.                                                    |
| Q-B2 | Is `BASIC_USER` on the prod project enough for `zcli push`, or does it need `ADMIN`?                                                                                     | Mint two tokens with `POST /client/{id}/integration-token`, push with each to a scratch service, revoke both.                                                                                                                                              |
| Q-B3 | Does Gitea 1.27 on the recipe read `.github/workflows/` as well as `.gitea/workflows/`?                                                                                   | Push a trivial workflow under each path to a test repo once runners exist; look at Actions.                                                                                                                                                               |
| Q-B4 | Does the platform's `buildFromGit` accept a public repository on a Gitea host (`https://web-1d76-3000.prg1.zerops.app/admin/go-hello-world-app`)?                        | Import one `golang@1.22` service with that URL into `scratch-playground`; watch `stack.build`; delete the service. Decides §3.5's first bullet.                                                                                                             |
| Q-B5 | How long does the runner addon take from import to three idle runners, and does the `zcli` download in `prepareCommands` survive GitHub rate limits?                      | Time the import once; `Site administration → Actions → Runners`.                                                                                                                                                                                          |

---

## 6. Recommended order for the demo

1. **Gitea ready** (user, 10 min): admin user (step 2), admin token (3), runner token → import the
   runner addon (4; by `zcli project service-import` until B-1 lands), migrate the mock repository
   (5). Settle Q-B1 and Q-B3 the same afternoon with one probe each.
2. **The group** (mate client, exists): "Go Hello World" → "Add dev" with an agent from the dev
   tier; sign the agent in; `route=adopt`; `git-push-setup` against the Gitea repository; first
   push. Then "Add production" from a pipeline-first prod tier (B-6 is a two-line edit of the seed).
3. **Prod token + secrets + workflow** (script or hand, 5 min): steps 11–13. The Mate writes
   `.gitea/workflows/deploy-prod.yaml` and pushes it; the user sets the two secrets.
4. **Release** from the Mate → production `ACTIVE` → URL. This is the CI/CD demonstration.
5. **Second Mate**: A-1 (clone-specific prompt) is the one fork change worth making before the
   demo — with it the A2 flow (Mate imports, fails, fixes) is a prompt plus today's zcp tools; A-3
   makes it clean. Everything else in §2.5/§3.7 is product work after the demo.

The single most valuable fact to carry into the design: **the token never moves.** The platform
mints one per project, the export leaks the old one, and zcp would follow a copied token to the
wrong project. What moves between environments is *shape* (services YAML) and *intent* (the first
prompt) — and the Mate's job is to turn shape into a running application, which is exactly the
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
