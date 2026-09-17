# The full picture: Gitea, groups and Mates

2026-09-15; every decision closed 2026-09-16 · Where the design stands, in plain words. Detail — decisions D1–D18, paths P1–P11,
phases 0–7 — is in `zerops-auth-backbone-implementation-2026-09-15.md` (the guide). Measured facts are
in the fork's ledger (`z3/docs/internals/zerops/verified.md`, open questions in `questions.md`). The
diagrams are on the page *Mate Auth Backbone* (https://claude.ai/artifact/Eub6X6kiqyANmjV6amcpmd).

**Status (2026-09-17).** Every decision is made and nearly all of it is built and proven live from an
empty org: mate 0.11.5, zcp v9.176.0, gitea-mate v2.1. What is built, proven and open, slice by
slice, is the fork's `docs/internals/zerops/primer.md`; the decisions as landed are the spec's §10;
the build order is near the end.

---

## In one paragraph

Every Zerops account gets its own Gitea, made with its first project (D18 as landed). An app is a **group**:
a name and tags, not a Zerops project of its own. In Gitea each group has one **group repo** and one
repo per codebase. The group repo holds the recipe, the import for every environment. Each person
works through their own **Mate**, a Zerops project with zcp, the coding agent and a dev/stage pair
per codebase. From the Mate's first minute zcp does two jobs:

- it connects every dev pair to that codebase's repo;
- it keeps the recipe current through pull requests.

The group has any number of **stages**, each following a branch or a mix of branches. **Production**
gets code only through a release. A small **broker** next to Gitea holds the only key that can
deploy to stages and production. It deploys only what protected branches and tags allow. Zerops
roles are the one source of who may do what. Opening a Mate or signing in to Gitea proves who you are
with a throwaway token that can do nothing else; your real Zerops token never leaves the app.

## What an account holds

```
Zerops org (the owner)
├── Gitea project — owners and admins, started in the background at sign-up
│   ├── Gitea, its Postgres and volume
│   ├── runners — one per group, awake only for its jobs, no deploy keys
│   └── broker — copies Zerops rights into Gitea, signs people in to Gitea, gives Mates
│                their Gitea access, holds the only deploy key
└── Acme — a group: a registry entry and tags, no project of its own
    ├── Fen (Mate)      — zcp, the agent, a dev/stage pair per codebase (api, web), db
    ├── Nova (Mate)     — the same shape, built from the recipe
    ├── stage           — follows main (the default)
    ├── stage-client-x  — follows main + feature/invoices, to show a client
    └── production      — gets code only through a release

Gitea org "acme"
├── acme/group — the recipe (every environment's import), which branches feed each stage,
│                release tags; main takes merges only from people with production rights
├── acme/api   — code, zerops.yaml, workflows
└── acme/web   — code, zerops.yaml, workflows
```

The recipe has its own repo because an environment has many services from many repos. No single
code repo can hold the whole import.

---

## From sign-up to production with two Mates

1. **You register** on app.zerops.io. Zerops creates your org and hands you a ready project with zcp
   in it, from its pool. The Mate app receives your personal token, which never leaves the app.
2. **Gitea starts in the background:** the Gitea project, with the broker, in about three minutes
   (175 s measured). You don't wait for it. Later it may come from a pool of ready ones.
3. **You add a project**, "Acme", and answer *What are we building?* The app writes the group into
   the registry. The broker creates the Gitea org, its teams and the group repo.
4. **The first Mate is created in the group.** It is a new project, or the ready project from
   sign-up tagged in, which saves about two minutes. Its Zerops key is lowered to `BASIC_USER` on its
   own project and loses the right to create projects. The app fetches its Gitea bot's token from the
   broker, as you, and writes it into the Mate.
5. **You sign the coding agent in,** and you're recorded as its signer. Your words from *What are we
   building?* go to the agent by themselves.
6. **The Mate builds the app in its own project.** It creates the services. For each dev pair it gets
   a repo from the broker, connects git and pushes.
7. **The Mate proposes the recipe** to the group repo as a pull request as soon as the services
   exist. It keeps the recipe current with every later change.
8. **You merge the recipe.** This is where production's shape is decided. Only people with
   production rights can merge.
9. The Mate develops: code and `zerops.yaml` per codebase, deployed to its own dev/stage pairs.
10. **You add a stage.** The app creates its project from the recipe's stage tier, and the broker's
    key gains access to it. The broker deploys the head of `main`, either directly or when a
    workflow asks. You can add more stages at any time, each fed by other branches.
11. **You add production.** Its project is created empty, from the recipe's production tier.
12. **You release.** Mate shows what stage runs next to what production runs. You create a protected
    release tag on the group repo, as yourself. Gitea tells the broker, which checks in Zerops that
    you have production rights. It then deploys exactly the commits the tag lists, promoting what
    stage already built wherever it can.
13. **You add a second Mate,** for a colleague or for yourself. It is created from the recipe's
    AI-agent tier. If someone other than an owner creates it, an owner confirms before it gets
    access.
14. **Its user signs in their own agent.** The Mate clones each service repo, deploys its own pairs
    and runs migrations and seeds. From then on it works on a branch and lands changes through pull
    requests.

**You act eight times:** register, add the project, sign the agent in, merge the recipe, add stage,
add production, release, add the second Mate. Everything else happens by itself.

**Exists today:** registration, the Gitea import, raw project creation, agent sign-in, bootstrap and
develop. The rest is to build.

---

## Who can do what

Zerops roles are the only source. The broker copies them into Gitea every few minutes and on every
change it hears about. A Mate's door reads them live, and Zerops checks every deploy.

| In Zerops | In Gitea and Mate |
|---|---|
| org owner | everything; Gitea site admin |
| whoever created a Mate | write to the group's code repos; propose recipe changes; the only one who writes in that Mate |
| everyone else in the org (`READ_ONLY`) | read the group's repos; see every Mate in the list, open none |
| access to the production project | merge recipe changes; release |
| a Mate | write to the code repos it created, landing on `main` through pull requests; propose recipe changes |
| whoever signed an agent in | the only one who can run that agent |

- **Others see a Mate exists, and that's all (D11, D5).** Org members are `READ_ONLY` with *can
  create projects*, so everyone sees every Mate in the list. Whoever creates a Mate owns its project,
  so only they — and the org's owners and admins — open it and write there. Measured: a read-only member with *can create projects* sees every project, can't
  write where they didn't create, and owns what they create. Org owners and admins keep their org
  rights unless a project override lowers them.
- **Read-only people can't open a Mate (D5).** They see it in the list, with its owner's name, and
  the row says who opens it. A Mate's conversation carries whatever the agent printed — file contents,
  variable values — and that is between the Mate and its owner.
- **Only the person who signed an agent in runs it (D6).** The signer is recorded as a tag on the
  Mate's project, which the Mate's own key can't change, so its agent can't forge it. Everyone else
  sees the prompt box replaced by "Signed in by …", and the Mate's server refuses their prompts too.
  We keep no backward compatibility: a login with no recorded signer can't run until someone signs
  in through Mate. It is a guardrail, not a lock: everyone who can open a Mate can also write there
  and has a terminal, so they could copy the login; the gate keeps an owner or admin from running a
  colleague's agent by habit, and records who signed it in.
- **A Mate is handed over** — one created for a colleague, one left behind, a reassignment — by an
  owner or admin making that person its project owner. Until then a colleague only watches it.
- **A Mate is limited.** It cannot deploy group stages or production, create projects, or move itself
  between groups. It can release only if an owner turns on the group's switch, which is off by
  default (D8).
- **When someone is lowered or leaves, their tokens don't follow on their own** (measured). Tokens a
  person made keep their old rights after the person is lowered. Zerops also refuses to remove a
  member who still holds tokens. So *Remove member* in the app marks the leaver, and the app finishes
  the mark from any owner's session, in order, so a closed tab can't leave a half-removed person:
  1. mints a fresh key for each of the leaver's Mates as the owner, writes it in, restarts the Mate,
     checks it answers, and only then deletes the leaver's — the Mate is never without a key; the
     broker's key the same way, if the leaver made it;
  2. deletes the leaver's other tokens;
  3. then removes them and clears the mark.

  Measured: an owner can mint, regenerate and delete tokens for another person. Rotation works the
  same way everywhere: the new one first, the old one only after the new one is in use — so a crash
  halfway leaves two live keys for a few minutes, never a dead Mate. For people who are lowered,
  the broker reports tokens that now carry more than their creator may. The broker can't do these
  steps itself: an integration token can't delete or regenerate tokens.

## Where the keys are

| Key | Where | What it can do |
|---|---|---|
| your personal Zerops token | the Mate app, nowhere else | your whole account, which is why it never goes to a container |
| a throwaway sign-in token | made by the app for one Mate or one Gitea sign-in | proves who you are to that one receiver, nothing else; deleted within seconds |
| each Mate's Zerops key (`ZCP_API_KEY`) | the Mate's container | its own project, at `BASIC_USER` |
| each Mate's Gitea token | the Mate's container, put there by the app when the Mate is made | write to the code repos it created |
| the broker's Zerops key | the broker | read the org; deploy to the group's stages and production — **the only deploy key** |
| the broker's signing key | the broker | signs people in to Gitea |
| Gitea's admin | the Gitea project | nothing outside it |
| agent logins | each Mate | used only by whoever signed in |
| a deploy key in Gitea, a repo or a runner | — | **none, anywhere** |

**Why no deploy key goes into CI:**

- Gitea gives every job in a repo all of the org's and the repo's secrets. Only pull requests from
  forks are excluded.
- Gitea has no protected environments.
- Our runners share one container between jobs.

A key in CI would reach anyone who can push a workflow.

The runners sit in the Gitea project, one per group: our runners share a container between jobs, so
one pool for the whole account would let a job in one group read the next group's job token. A
group's runner sleeps when idle — the broker starts it when Gitea queues a job (Gitea says so within
seconds, runner or no runner, a stopped service starts in about six seconds, and a runner that wakes
takes the waiting job at once — all measured) and stops it after a quiet spell — and when the platform's sandbox primitive lands, each job gets its own, which ends the
shared-container caveat entirely. A container on Zerops reads only its own service's variables, so a
job cannot read Gitea's admin token or its database password from next door — and since a job *can*
open Gitea and the broker over the project's private network, neither treats that network as proof of
anything.

What an account pays for at rest is Gitea, one Postgres and the broker: the recipe's HA database and
its three always-on runners go.

---

## How deploys and releases work

- **Stages.** Each stage names its source: one branch or several. When a source moves, Gitea notifies
  the broker, which deploys the new head.
- **Mixed stages.** A mix is kept as a branch `env/<name>` that only the broker writes. The broker
  re-merges it whenever a source moves. On a conflict the stage stays on its last good merge, and
  Mate says so.
- **Workflows orchestrate; the broker enforces.** A workflow can run tests, order steps and ask the
  broker to deploy. It asks with its job's Gitea token; the broker checks with Gitea, in one call,
  that the token belongs to a running job in the repo it claims (measured), and the token dies the
  moment the job ends. It picks the environment, never the commit, so at most it triggers a deploy
  that protected state already allows.
- **Release.** A release is a protected tag `v…` on the group repo that lists each service's commit.
  You create it in Mate, as yourself. Only people with production rights can create one; Gitea has
  no admin override on protected tags (measured). Gitea's signed notification names who pushed the
  tag. The broker re-checks that person in Zerops and records its verdict on the commit — a refused
  tag never deploys, not after a restart either. Then it deploys exactly those commits, promoting
  stage's builds where they exist (measured; only where stage and production build the same way).
- **Your Gitea CI files keep working.** Workflows still run tests, checks and the order of steps;
  only a deploy step changes. Instead of `zcli push` with a Zerops token, it asks the broker (a sketch
  is in the guide, 5.4). Zerops builds from the commit and its `zerops.yaml`, as `zcli push` does
  today. Each environment deploys on every push, or only when a workflow asks, so tests can gate a
  stage.
- **Rollback:** a new release listing an earlier release's commits — one click on it in Mate.
- **Migrations and other one-off tasks** run inside Zerops (`initCommands` with `zsc execOnce`), never
  on a runner that holds production credentials.

**Rejected:**

- **A release repo per app,** holding the production token and a runner. It only fences a key the
  broker makes unnecessary. With any number of stages it becomes a repo per environment. It records
  deploys as a token rather than a person, and it puts a runner into every production project.
- **A runner per stage.** It serves no purpose; one per group is enough.
- **A project of their own for the runners.** The point would have been to keep job code away from
  Gitea's own secrets, and the platform already does that: a container reads only its own service's
  variables, so Gitea's admin token and database password are unreadable from a runner beside it. A
  second project would cost every account another project for nothing. What the pool's place does
  require is that nothing in that project trusts the private network: Gitea's header sign-in stays
  off and the broker authenticates every caller.
- **Deploy secrets on runners,** via a Gitea patch that scopes secrets to protected branches. Our
  runners share a container between jobs, so a secret there reaches whatever ran before.
- **A lone release button.** It can't orchestrate a real deploy; workflows can, through the broker.

## The broker

- **What it is:** one small service in the Gitea project, built from zcp, about 2–3k lines of Go.
- **Stateless:** no database, cache or queue. Everything it needs already lives somewhere
  authoritative, so a restart is always safe:
  - rights in Zerops;
  - groups in the registry tags;
  - environments and releases in the group repo;
  - deploy state in Zerops and in Gitea's commit statuses.
- **One Zerops key:** it reads the org and has `BASIC_USER` on the stages and production. Adding an
  environment adds a grant to that key, so no secret ever travels. Measured: such a key writes only
  where granted, and a new grant works at once without a new key.
- **Five endpoints, plus Gitea's sign-in:**
  - a Mate's Gitea credential — the app asks for it as you when the Mate is made and writes it into
    the Mate, so the Mate never sends its Zerops key anywhere — and a new repo, which the Mate asks
    for as its bot;
  - deploy, and deploy status, for workflows;
  - Gitea's signed notifications;
  - *Sign in with Zerops* for Gitea, checked with a throwaway token like a Mate's.

  It works on a timer and on Gitea's notifications; nothing else can start a pass.
- **It catches up.** Every pass compares what each environment should run with what it runs and
  deploys the difference, so a push it missed while down is not lost; a merged recipe change reaches
  every environment built from it.
- **A bad read writes nothing.** A member list or registry that fails or comes back short ends the
  pass without disabling anyone, deleting a token or narrowing a Mate.
- **Never runs repo code.** It moves Gitea archives to Zerops, and Zerops builds. Measured: a key of
  that shape deployed a commit archive by itself (`ACTIVE` in 59 s). Gitea has to serve archives
  without the repo-name folder it adds by default (`PREFIX_ARCHIVE_FILES = false` in its config);
  with the folder, the build fails.
- **If it is down,** Gitea keeps serving every session and token, and Mates keep theirs. New Gitea
  sign-ins, deploys and permission changes wait, then catch up.
- **If it is taken,** whoever holds it holds Gitea's admin, the key to every stage and production,
  and the key that signs anyone in to Gitea — the whole account's code and its production. That is
  why it stays small, runs no repository code and reads nothing but its own variables.

## Gitea inside Mate

The app signs in to Gitea as you, over Gitea's own OAuth with PKCE. Gitea enforces the rights copied
from Zerops, and the broker isn't in the path.

For each group Mate shows:

- every environment, with the branch and commit it runs and its last deploy;
- open pull requests;
- workflow runs, with logs;
- releases.

You can act on them from Mate: review and merge, rerun or cancel a run, release.

**Every Mate has a Git tab** in its right panel, next to diff, browser and data. Per code repo it
shows the branch the Mate is on — the working copy's fact, read from the container, not from Gitea —
how far ahead of or behind `main` it is and what is unpushed, then what Gitea knows about that branch:
the pull request from it, its checks, the last run, and which stage picks it up on merge. Below, the
group's stages and production with the commit each runs, releases, and the open recipe pull requests.
Open, review, merge, rerun and release from there, as yourself. Each line comes from whoever can
prove it — the branch from the container, what you may do from your own Gitea token, what's deployed
from the commit named on the app version — never inferred from another, so it can't say "configured"
about a broken setup.

This needs CORS switched on in Gitea, listing every origin the app runs from. Measured: with that,
the browser finishes Gitea's OAuth exchange itself, with PKCE and no secret, and the broker is not in
the path.

---

## How a Mate and Gitea learn who you are (D1)

**Why anything is sent.** A Mate's server is reachable by anyone who knows its address, so before it
lets anyone in, it asks who is connecting. Zerops gives outside apps no sign-in, so the only proof it
can check is a Zerops token. Until now the app sent your real one, at every connect and every 15
minutes. That token works in all your orgs and never expires, and it passes through a container that
its owner, its agent and whatever code the agent runs can change.

**What the app sends instead:**

1. When you open a Mate, the app makes a Zerops token with no rights, named for that one Mate.
2. The Mate asks Zerops who made it — a token names its creator — and gets you.
3. The Mate checks your role on its project with its own key.
4. The app deletes the token seconds later.
5. From then on the Mate re-checks your role itself, so nothing is re-sent.

The container only ever sees a worthless token. Gitea sign-in works the same way, with each org's
broker as the *Sign in with Zerops* provider, so there is no central service to run.

**Measured** with a no-access member (`NO_ACCESS`, the lowest role) and with a read-only one:

- **A member can mint one.** Both minted a grant-less token, from a personal token or a plain sign-in,
  with no extra confirmation step.
- **The token names them.** It worked on its first call. Its self-read showed its name, when it was
  created, and the member as its creator.
- **A Mate's key can check them.** A key shaped like a Mate's found the member in the org's member
  list and saw their role on its project.
- **A stolen throwaway is nearly worthless.** It cannot mint another token (the platform refuses:
  `notAllowedForIntegrationTokenWithoutDelegation`), raise its own rights, delete itself, or read a
  project. All it can see is the org's member list and the names of its creator's other tokens,
  never their values.
- **Speed:** about 0.65 s per call. A deleted token stops working about 0.6 s later.

The details are in the ledger, under *A member's throwaway token as their identity* and *A member's
rights and the tokens they made*.

**Rejected:**

- **A relay:** a central service that takes your token once and issues two-minute passes. It is one
  more service to run, and it holds everyone's tokens for a moment. `infra/relay` stays the
  mobile-notification relay, outside this plan.
- **A long-lived door key per person and Mate:** it would be stored on every device, pile up in the
  org's token list, and block removing that person until deleted.
- **Zerops signing short-lived passes itself** would be ideal, but it is a platform feature that
  doesn't exist.

## What zcp learns

1. **Git per dev pair, as early as possible.** Right after bootstrap creates a pair, zcp reads its
   Gitea credential from its env (the app put it there when the Mate was made), asks the broker for a
   repo as its bot, runs `git-push-setup` and pushes. `git-push-setup` already works with Gitea.
2. **The import file in the group repo.** As soon as the services exist, zcp proposes the recipe by
   pull request and keeps it current with every service change. The recipe is every tier's whole
   import, in the published recipe layout.
   - Stage and production tiers come from the production transform: HA, at least two containers.
   - Secrets are generated or left as `REPLACE_ME`.
3. **Gitea as a forge,** with workflows that deploy through the broker, never with a Zerops key.
4. **Joining from the recipe.** A new Mate clones each service repo and syncs to `main`. It then
   deploys its pairs and runs migrations and seeds.

## Adopting an existing app (D14)

Adoption is the same flow as a new app, with existing source code in place of a description.

1. *Add project* → *I have code*: a repo URL per codebase. The source code is the only source of
   truth — never what a project happens to be running, which is build output.
   - A private GitHub repo is migrated once, with your GitHub token.
   - Code that lives only on a disk is pushed by you to a fresh service repo the app creates.
   - A running Zerops project adds at most a shape hint: its configuration with secrets stripped.
2. The Mate turns it into an app in its own project, writing a `zerops.yaml` where one is missing.
   Then come the service repos and the import file, exactly as for a new app.
3. Stages and production come from the recipe.

**Nothing is adopted in place.** The old project keeps running until you move its traffic.

**Nothing is copied from production.** Keys arrive as `REPLACE_ME`, and the dev database starts
empty until migrations and seeds fill it.

**GitHub and GitLab are where code comes from, not where Mate works (D19).** A repo is migrated
once, with a push mirror back if you want one; from then on Gitea is the forge and the broker the
only way to a group's stages and production. Mate doesn't drive GitHub's environments or GitLab's
protected variables — "Zerops roles are the one source" only holds where the forge's permissions come
from Zerops, and that's Gitea.

*Set up Mate* goes away.

---

## Decisions

All decided on 2026-09-15 and 2026-09-16.

| Id | Decision |
|---|---|
| D1 | Opening a Mate or signing in to Gitea proves who you are with a throwaway token; no relay |
| D2 | Closed: no relay for identity |
| D3 | Group membership is kept as tags on the Gitea project, which only owners and admins can write |
| D4 | The broker is its own small service in the Gitea project, built from zcp |
| D5 | Read-only people see a Mate in the list and cannot open it (revised 2026-09-16) |
| D6 | Only the person who signed an agent in runs it, recorded as a tag on the Mate's project; no backward compatibility |
| D8 | Mates release only if an owner turns on the group's switch; off by default |
| D9 | Until the new sign-in lands, the Mate's sign-in refuses integration tokens (after checking the eval farm) |
| D10 | Org admins get Gitea teams by role; Gitea site admin is the org owner only |
| D11 | Others read a Mate, never write: members are `READ_ONLY` with *can create projects*, creators own their Mates |
| D12 | Every Mate keeps its own dev/stage pairs |
| D13 | The recipe lives in the group repo, never in a code repo |
| D14 | Adoption is the new-app flow with existing source; nothing in place |
| D15 | The broker holds the only deploy key; workflows orchestrate by calling it |
| D16 | Any number of stages, each fed by a branch or a mix; the default follows `main` |
| D17 | The first brief is sent by itself only when it is your own words from *What are we building?* |
| D18 | Gitea starts at sign-up, in the background |
| D19 | Gitea is the only forge Mate drives; GitHub and GitLab are sources to migrate from |

D7, stage deploys with a stage token in CI, is replaced by D15 and D16.

## Not measured yet

- **Q-11:** can the owner lower the key of the Mate the pool hands out at sign-up? Needs a live
  sign-up.
- **Q-16:** what does removing a member with `force` delete? Does it take their Mates' keys, and what
  does a running Mate do then?
Measured and closed on 2026-09-16: the browser's OAuth exchange (Q-19); how the broker verifies a
workflow job's token (Q-25); that switching a project's isolation reaches running containers in
seconds, though a running process keeps what it captured until restarted (Q-23); that zcp runs with
its key as a service variable and the platform doesn't put the project one back (Q-24); the tag
budget — 65 534 bytes per project, about 2 500 registry entries (Q-26); and that the broker's key
can stop and start a runner service in about six seconds each way, while Gitea reports a queued job
within seconds and a waking runner takes it at once (Q-27).

## Wrong on 2026-09-15

- Opening a Mate sends your personal token, which covers your whole account and never expires, to a
  container any `BASIC_USER` colleague can change.
- A Mate's key is project `ADMIN`. It can still create one project, because the permission used to
  create the Mate is kept.
- zcp's self-deploy logs in with the Mate's key inside the app's dev container, where the app's
  dependencies run. The platform hands that key to every container in the Mate's project anyway — it
  is a variable of the project, not of the Mate.
- Every Mate project runs with service isolation switched off (the platform sets it that way for a
  zcp container — measured on all four live Mates), so each app container and build in it can read
  zcp's variables as `zcp_…`: its Gitea token, the VS Code password, the agent's login. The Mate's
  Zerops key is written there as a plain, non-sensitive project variable. Three stage and production
  projects made the old way carry such a key and the same setting.
- zcp's GitHub integration copies the Mate's key into repo secrets.
- Integration tokens open Mates as full operators.
- A Mate can tag itself into another group, and the app then widens its access on its own.
- The Mate the pool hands out at sign-up skips the creation hardening.
- Any admitted member with a terminal can copy the agent's login file; the terminal runs as the
  agent's user.
- *Set up Mate* on a live project would have the agent deploy straight into production.
- The recipe store is a mock, and a clone of a Mate carries no code.
- Removing someone takes about 17 minutes to reach an open Mate, and never reaches an open Gitea
  session.
- The Gitea demo keeps a production token in repo secrets, where every job in the repo can read it.

Phase 0 fixes most of these without new infrastructure. The personal token stops reaching containers
in Phase 3.

**2026-09-17:** every item above is fixed and live (mate 0.11.0–0.11.5, zcp v9.176.0, gitea-mate
v1–v2.1) but three: the terminal-copy limit stands by design (D6 is a guardrail, not a lock),
*Set up Mate* remains for a project with no container, and the old demo's production token in repo
secrets stays until the demo account is migrated.

---

## Build order

- **Phase 0 — harden the Mates.** Lower each Mate's key and delete its creation permission. Narrow
  group access. Refuse integration tokens at the door. Enforce who runs an agent. Stop zcp leaking
  its key; isolate each Mate's project and move the key onto zcp.
- **Phase 1 — Gitea at sign-up and the broker.** Harden the Gitea recipe: private, sign-in only, SSH
  off, CORS for Mate. Start Gitea at sign-up. Build the registry, the broker, the rights mirror,
  Mate access and the runner pool beside them.
- **Phase 2 — zcp's two jobs.** Git per dev pair and the import file; Gitea as a forge; joining from
  the recipe.
- **Phase 3 — people's identity.** Throwaway tokens at a Mate's sign-in, the Mate re-checking roles
  itself, and the broker signing people in to Gitea. Read-only people see Mates listed and can't open
  them. The app stops sending tokens to containers.
- **Phase 4 — the app.** *Add project* creates a virtual group. Then the first Mate and the brief,
  *Add Mate* from the recipe, and Gitea inside Mate.
- **Phase 5 — environments and release through the broker.** Stages declared in the group repo,
  *Add stage* and *Add production*, broker deploys, workflows that orchestrate, release, rollback.
- **Phase 6 — adopting an existing app.**
- **Phase 7 — remove the old paths.** The raw-token door, the mock recipe store and lossy clones,
  *Set up Mate*, and zcp's delegated launch for group Mates.

Phases 1–2 don't need Phase 3; Phase 4 needs both.

## Where it lives

- **This summary:** `zcp/plans/zerops-auth-backbone-summary-2026-09-15.md`.
- **Where it stands (2026-09-17):** `z3/docs/internals/zerops/primer.md` — every slice's state, what
  is proven, what is open.
- **The broker and Gitea on Zerops:** `zeropsio/gitea-mate` (`~/www/gitea-mate`); the contracts the three
  codebases share are its `docs/`.
- **The guide:** `zcp/plans/zerops-auth-backbone-implementation-2026-09-15.md`.
- **zcp's part, extracted:** `zcp/plans/zerops-auth-backbone-zcp-requirements-2026-09-16.md` — git per
  dev pair, the import file, the forge kind, joining, the key, the broker, the role function, the
  recipe.
- **Research and findings:** `zcp/plans/research/zerops-auth-backbone-2026-09-15.md`. Its design parts
  are superseded; its facts stand.
- **Measured facts:** `z3/docs/internals/zerops/verified.md`, the five 2026-09-15 sections — the auth
  surface, Gitea as a mirror, throwaway tokens, a member's rights and tokens, the broker's key and a
  Gitea-layout archive — and three of 2026-09-16: *What a container sees of its project-mates* (env
  isolation, secrets redacted to read-only, every live Mate on `none`), *A job's token, the queued-job
  webhook and the browser's exchange* (the lab Gitea) and *Isolation flipped live, the key moved, the
  tag budget, a service asleep*.
- **Open questions:** `z3/docs/internals/zerops/questions.md`: Q-11 and Q-16.
- **The spec:** `zcp/docs/spec-mate.md`. §10 records the backbone as landed (2026-09-17); §4.7 the
  *New project* form as it is; §6.6 D20.
- **The page:** *Mate Auth Backbone*, https://claude.ai/artifact/Eub6X6kiqyANmjV6amcpmd.
- **The old Gitea demo:** `zcp/plans/z3-gitea-demo-runbook-2026-09-06.md`. It covers demo mechanics
  only.
