# What zcp builds for the auth backbone

2026-09-16 · The zcp-side extraction of `zerops-auth-backbone-implementation-2026-09-15.md` (the
guide): everything that lands in the `zcp` repository or the Gitea recipe, with the guide's section
beside each item. The guide decides; this lists. Measured facts are in the fork's ledger
(`z3/docs/internals/zerops/verified.md`, the 2026-09-15 and 2026-09-16 sections).

## A. zcp inside a Mate

| # | Requirement | Guide | Today | Accept when |
|---|---|---|---|---|
| A1 | **Git per dev pair, as early as possible.** Right after bootstrap creates a dev/stage pair: read `GITEA_URL` and `GITEA_TOKEN` from the live env — the app writes them onto the `zcp` service when the Mate is made; at sign-up they can land minutes after the Mate is up, so wait with backoff — ask the broker for the pair's service repo (`POST /mate/repository {name}`, authenticated with that token), run `git-push-setup` against the returned canonical URL, push | 2.1, 1.5 | zcp creates no remotes; `git-push-setup` needs a user-supplied URL and token; `GITEA_URL` only toggles a paragraph of agent context; nothing reads `GITEA_TOKEN` | the bootstrap hook table passes (Gitea up / not yet / refused); one repository per pair; the token is never persisted in the app's dev container (A6) |
| A2 | **The import file in the group repo, proposed and kept current.** As soon as the services exist, propose the recipe to the group repo as a pull request from the Mate's branch; update the same pull request on every service change (a new service, different scaling). Content: the published recipe layout (`internal/recipe/layout.go`) — tier directories, each a whole-project `import.yaml` with a README; the AI Agent tier from the Mate's own project (its dev/stage pairs and managed services); stage and production from the production transform (`bundle.BuildLaunch`: rename, `startWithoutCode`, HA, `minContainers` ≥ 2); each runtime's `buildFromGit` names its service repo and `zeropsSetup` its setup; secrets generated or `REPLACE_ME` (`export`'s classification) | 2.2, D13 | the import the agent writes in bootstrap is never persisted; `export` covers one runtime building from its own origin; the composer has no caller (`7b5bf673`) | a group-wide export exists; a pull request opens and updates on change; a merged recipe imports through 4.3's conversion (`buildFromGit` + `zeropsSetup` → `startWithoutCode`, source map kept) |
| A3 | **Gitea as a forge kind.** Matched on the `GITEA_URL` host beside github/gitlab/unknown (`internal/topology/git_pat.go:42-46`); forge-aware delivery (`topology/delivery.go:42-49`); `.gitea/workflows` emission for tests and orchestration; commit identity = the Mate's bot; `agents_git_host.md` and the git atoms reconciled | 2.3 | three forge kinds, none of them Gitea | a Gitea remote is classified and delivered as one |
| A4 | **Joining from the recipe.** A Mate created from the recipe adopts the imported services (`route=adopt`), clones each runtime's service repo per the recipe's `buildFromGit`, syncs it to `main` (atom `develop-git-push-start-from-remote`, measured with two Mates on one repository), deploys each with its setup, runs migrations and seeds; from then on it works on a branch and lands through pull requests. Fix the adopt-time git-push reconcile that skips services whose state is not complete yet (`adopt_gitpush_reconcile.go:51`) | 2.4 | a no-op on a first adoption | a second Mate reaches a running dev pair from the recipe with no human step but the agent sign-in |
| A5 | **Never push `main`.** Every code repository's `main` is protected; the bot lands through pull requests | 1.5 | — | the git atoms never target `main` directly |
| A6 | **Stop handing the key out.** Remove `ZEROPS_TOKEN = ZCP_API_KEY` from the GitHub build-integration secrets (`internal/tools/workflow_build_integration.go:331-343`); mask `GITEA_TOKEN` (`internal/ops/env.go:57-61`); self-deploy passes the key to the one `zcli push` without `zcli login` persisting it in the app's container (`internal/ops/deploy_ssh.go:236`) | 0.9 | all three leak | no `ZCP_API_KEY` in build-integration output; no `zcli login` in the SSH command; the masking table |
| A7 | **The key as a service variable, the project isolated.** The app moves `ZCP_API_KEY` from the project onto the `zcp` service and sets `envIsolation: service` (measured: zcp boots unchanged; the platform does not write the project entry back). zcp keeps reading the key from its process env (`internal/auth/auth.go:122`) and keeps every sibling read on the API — nothing may assume `none` or a project-level key | 0.10 | zcp's own spec already forbids `none` dependencies (resolved 2026-05-28) | a Mate on `service` isolation with a service-level key passes 0.1's operation list |
| A8 | **Deploys through the broker, never a Zerops key in CI.** Emitted workflows deploy with the action `POST /deploy {environment, service, repository}` using `github.token`; workflows keep default permissions (or add `actions: read`) so the broker can verify the job | 5.4 | `zcli push` with a `ZEROPS_TOKEN` secret (the demo) | no Zerops token in any emitted workflow or secret |
| A9 | **Delegated launch goes for group Mates.** Delegations are deleted at creation (0.4); `launch-production` answers with its manual `launchKey` fallback (`internal/tools/launch_delegation.go`) | 0.4, Phase 7 | every Mate can create one project through its delegation | a Mate with no delegation reports the fallback, not an error |

## B. The broker — `zcp gitea broker`

One small stateless service in the Gitea project, built from zcp (reusing its Zerops client, the
role function and the recipe code), 2–3k lines plus tests.

| # | Requirement | Guide |
|---|---|---|
| B1 | **Credentials:** one Zerops token — org `READ_ONLY`, `BASIC_USER` on the Gitea project and on every group stage and production (grants added in place as environments are created); `GITEA_ADMIN_TOKEN` / `GITEA_ADMIN_PASSWORD` referenced from `web`; the OIDC signing key in its vault. Its process trusts nothing about the network it sits in | 1.2, 1.3, 1.6 |
| B2 | **Five endpoints, none taking a Zerops key:** `POST /mate/credential` (a throwaway proves the person; `ensure` returns nothing new when a token is live, `rotate` mints generation *n+1* as `mate/{bot}/{n+1}` and older generations are revoked only once the newest is ten minutes old); `POST /mate/repository` (the bot's Gitea token, resolved against Gitea; creates the repo in the bot's org, adds it as collaborator, returns the canonical URL); `POST /deploy` and `GET /deploy/{id}` (the job token verified by `GET /user` → task id, then `GET /repos/{claimed}/actions/jobs/{taskId}` → `200` only for the true repo); `POST /hooks/gitea` (HMAC). No open poke | 1.3, 1.5, 5.4 |
| B3 | **The OIDC provider for the org's Gitea:** discovery, `/authorize`, `/token`, `/userinfo`, JWKS; the consent step is a throwaway named for that Gitea; claims from the role function (`org:owner`, `g:{group}:read\|write\|release`); codes in memory; `preferred_username` derived from the Zerops user id | 3.6 |
| B4 | **The loop** — every few minutes and on webhooks: rights mirror (a Gitea org, teams read/write/release and the group repo per registered group; team membership and admin flags; departed people disabled and their tokens deleted; one bot per Mate, a reader with write on its own repositories); releasers = the org's owners and admins until the group has a production project; the registry read from the Gitea project's tags; **a bad or partial read writes nothing**, and a pass that would change more than a cap's worth stops and reports; catch-up deploys (desired head vs the sha in the deployed version's name); recipe deltas imported into each environment on merge; runner services per group — imported on a group's first workflow, started on `workflow_job: queued`, stopped after a quiet spell, removed with the group | 1.4, 1.6, 5.3 |
| B5 | **Deploys from protected state only:** an environment deploys the head of its source ref; production the commits listed by the newest tag the broker *approved* (`mate/release: approved` on the tagged commit; `refused` never deploys, not after a restart); the caller picks the environment, never a commit or a ref; mixed sources merged into `env/{name}` that only the broker writes; promote a stage build to production only when both setups share their `build` section, otherwise Gitea's archive (`PREFIX_ARCHIVE_FILES=false`); versions named by sha, production's also by release and tagger; a queue per environment, newest wins; results as commit statuses | 5.1, 5.3, 5.5, 5.6 |
| B6 | **The rules it must keep:** it never executes repository code; it reads only its own variables; being inside the project proves nothing to it; if it is down, Gitea serves what exists and the next pass catches up | 1.3, 1.6 |
| B7 | **Proofs it ships with:** a cross-org `runs-on` stays queued · broker down, two pushes, broker up → one deploy of the second sha · a refused tag stays refused after a restart and a `/deploy` · two rotations started together → one live token after the grace period · a truncated member list → no write | 1.3–1.6, 5.5 |

## C. The shared role function

A pure module in both repos — `packages/shared/src/zeropsRoles.ts` in the fork, its Go twin in zcp —
mapping a person's effective role per project to Mate scopes, Gitea teams and the release right; one
fixture file copied into both repositories and asserted equal. Fixtures include an override lowering
an org `OWNER`, a project created by a *can create projects* member (its `OWNER`), and a group with no
production project yet. (Guide 1.4.)

## D. The recipe — `zeropsio/recipe-gitea`

`app.ini`: `REQUIRE_SIGNIN_VIEW=true`, `DEFAULT_PRIVATE=private`, `USER_MAX_CREATION_LIMIT=0`,
`DEFAULT_ALLOW_CREATE_ORGANIZATION=false`, `ALLOW_ONLY_EXTERNAL_REGISTRATION=true`,
`ENABLE_PASSWORD_SIGNIN_FORM=false`, no reverse-proxy auth, SSH off, bounded `SESSION_LIFE_TIME`,
`[oauth2_client]` with `ENABLE_AUTO_REGISTRATION=true`, `USERNAME=preferred_username`,
`ACCOUNT_LINKING=disabled`, `[cors]` listing every app origin literally with `Authorization` in
`HEADERS`, `[repository] PREFIX_ARCHIVE_FILES=false`, `[actions] ABANDONED_JOB_TIMEOUT=2h`.
`admin-init.sh`: the site admin renamed away from `mate`, the OIDC source added from env pointing at
the broker. Services: Postgres `postgresql@18` `NON_HA`; the broker as a second service; one
`runner-{group}` service per group at `minContainers: 1`, `maxContainers: 3`, registered at Gitea org
scope, its registration token in its own vault; the project left on the default `envIsolation`; a
scheduled `gitea dump` to object storage and a written, once-run restore. (Guide 1.1, 1.6.)

## What zcp must never do

Hold a Zerops key for any project but its own · read a sibling's variables from the container ·
push `main` · write `ZEROPS_TOKEN` into any CI secret · leave `zcli login` behind in an app container
· trust the project network as proof of anything.
