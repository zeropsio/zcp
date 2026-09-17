# Live run on mate.zerops.io — screen-by-screen feedback (2026-09-16)

The owner walks the released backbone (mate 0.11.0, zcp 9.176.0, gitea-mate v1) from an
emptied `Mate` org. One entry per screen, in the order seen. Findings, not decisions —
decisions go to spec-mate.md once taken.

The project's standing state — what is built, proven and open, slice by slice — is the fork's
`docs/internals/zerops/primer.md`; the open items of these entries are listed there in the owner's
order.

## 1. Empty projects page (signed in, no projects, no Gitea)

What it shows: hero card "Start with a Mate" with a *New project* button; sidebar with
"+ New project" and a second "No Zerops projects yet / New project"; below, a muted
*Tools* section with "+ Add Gitea".

- **The primary action is a dead end.** Every *New project* leads to the wizard, and the
  wizard refuses without a Gitea ("Your account's Gitea is still being set up."). The
  one step the account actually needs is the smallest, greyest link on the page.
  Hierarchy is inverted: the required step reads as optional, the optional-looking
  button is required.
- **The refusal's copy lies on this account.** "still being set up" describes a Gitea
  that is provisioning; here nothing was started. Two states, one sentence.
- **Three *New project* affordances on one empty screen** (sidebar header, sidebar
  empty state, hero). The sidebar empty state duplicates the hero; one is enough.
- **Hero copy**: "A project holds as many as the people on it want." — "as many" has
  no noun; reads as a typo.
- **The person should never have to know the word Gitea to start.** Candidate fix:
  *New project* on an account without Gitea stands Gitea up as the first step of the
  creation (a `create-tool` step ahead of `create-project`), and the Tools section
  only ever shows what exists. Alternative: the hero's button becomes "Set up" and
  does both. Either way the muted link goes.
- **"+ Add Gitea" is styled like a disabled label**: muted grey text, no button shape,
  while the hero button is a filled blue pill. On a page with two possible actions the
  contrast between them is the page's whole message, and it says the wrong thing.
- **Two visual languages on one page**: the hero is a raised white card with a big
  radius and marketing spacing; Tools below it is a bare heading and a line of text.
  Either the Tools section is a card too, or the hero loses its card and becomes the
  page's first section. As it is, Tools looks like a footnote.
- **Sidebar empty state is centered** ("No Zerops projects yet" + outlined button)
  while every other sidebar element is left-aligned; it also repeats the hero. Drop it
  or left-align it as one quiet line.
- **Tools heading is muted grey** at the same weight as its description; nothing marks
  it as a section. Compare with "Projects" which is the only real heading on the page.
- Fine as is: the face at rest, the page header with its refresh, the quiet sidebar
  footer icons, the header's org switcher.

Second look, after the fix (still an empty state that fails):

- **The page frames emptiness as a failed list**: a "Projects" title with a refresh
  icon over nothing. First run should own the page, not sit as a card under a list
  header.
- **The card is stranded top-left** in the upper fifth of a wide grey field, aligned to
  a title, the rest blank. Neither centered as an invitation nor flowing as a page.
- **The sidebar is pure chrome** on an empty account: a nav entry, a centered second
  empty state, a footer of icons, and a fifth of the width. Gone or collapsed until
  there is something to list.
- **Heading and button disagree**: "Start with a Mate" over "New project" — two nouns
  for one act, neither met yet.
- **One verb, three styles**: filled pill in the hero, small outlined button in the
  sidebar, plain "+" entry in the sidebar header.
- **A paragraph where a line belongs**: four clauses explaining a Mate to someone who
  has not seen one; the added Git-hosting sentence made it longer and clunkier.
- **The face is a placeholder, not an invitation**: small, idle, slate, beside text.
  If the Mate is the point, the Mate is the picture.
- **Nothing sets expectations**: the click starts minutes of provisioning and a
  running project; the page says nothing about what happens or how long.

**Done (fork 7ea884702, iterated on localhost:5734):** New project stands Gitea up as
the first half of the creation ("Setting up Git hosting" on the button), the wizard's
refusal is gone, an account that has not started sees no Tools section at all, the hero
sentence has its noun. Still open from this screen: the hero-vs-Tools card mismatch on
accounts that have started, the sidebar's duplicate empty state.

## 2. New project form (name, brief, location, Continue)

- **The brief is premature.** "What are we building?" asks for a task before the person
  has seen a Mate, an environment or a conversation; what they type vanishes into the
  form and reappears minutes later as a message they did not watch being sent (D17's
  handoff). Recommendation: drop the field; the conversation opens during provisioning
  and the composer queues a first message until the Mate answers — same benefit, right
  place. Reverses D17; the owner decides.
- **Title said twice**: breadcrumb "Projects / New project" and an H1 "New project".
- **The subtitle talks about stage and production** to someone making their first Mate.
- **"Continue … in Mate"**: absurd when the org is called Mate; the org belongs in the
  header (present on the projects page, absent here).
- **"Continue" means a step two** (agents). A first run should not need one: default the
  agents, create.
- **Location**: an internal code in the label ("EU Central (prg1)") and two sentences of
  helper for a preselected default.
- **Grey on grey**: card, inputs and page share one tint; nothing reads as a field until
  focused.
- **Full-width inputs**: a name field a thousand pixels wide; the form wants ~560px.
- **Sidebar** as on screen 1: chrome plus a duplicate empty state.

## 3. Provisioning wait ("Preparing your project")

- **Four ways of saying one thing**: "Preparing your project", a "PREPARING" pill,
  "Waiting for the Zerops Mate container to start", and a lowercase orphan "project is
  being created" — a raw platform status leaking through.
- **Status words, no life**: no spinner, no elapsed time, no steps. The only live element
  is the Mate's face in the sidebar — the element the design says carries state — and
  the page ignores it.
- **Stale header**: still "New project" with the form's subtitle, though the form is gone
  and the project already exists in the sidebar.
- **"Zerops Mate container"** is our vocabulary.
- **"Back to projects" reads as cancel**: the only button on a wait page, outlined, and
  nothing says leaving stops nothing.
- **Card style flipped again**: grey form card → white wait card.
- **Dead time wasted**: up to five minutes with nothing to do, the Mate row one click away.

Recommendation (follows the design rules — one environment = one conversation, the face
carries the state, syncing = header spinner): on create, land in the new Mate's
conversation. Face "coming up", header spinner, one quiet line with the expectation, the
composer takes the first message and holds it until the Mate answers. Kills this page and
the form's brief field in one move; reverses D17 — owner decides.

## 4. Agent selection step

`ZCP_AGENTS` is presentation policy only (bootstrap extension: "which agents this container
offers, in which order — NOT authorization"); the image carries all five, the key absent
means offer everything, and the sign-in page can show them all. The step pre-narrows a menu
the person sees in full one screen later, and cannot even express "none" (empty omits the
key = all). Later Mates in a group inherit the group's authorized agents from what people
signed into — that path stays. Recommendation: drop the step; omit the key.

**Pending the owner's yes, one cut**: no brief, no agents step, name + location → Create,
land in the Mate's conversation while it comes up (screens 2, 3, 4).

## 5. Projects page after a reload, first Mate still starting

- **"Wait for it" is styled as an action and is not one**: blue link-weight text beside
  a status sentence; reads as a punchline. The face already says "starting".
- **Three add affordances for a project two minutes old**: a dashed "+ Add Mate" tile as
  big as the Mate, "+ Add stage" and "+ Add production" below, before the first Mate
  has booted. The dashed placeholder tile is the cliché; hide the adds until the first
  Mate is up, and make them a row's quiet verb, not a tile.
- **Gitea's row is a service list** ("broker, db, volume, web"): hostnames, not "ready /
  setting up" and, later, where it is. It is still building at this moment and the row
  says nothing.
- **Two row treatments on one page**: Mate = white card, Gitea = bare line.
- **"New project" twice again**: sidebar and a filled header pill — the loudest element
  on a page whose loud thing should be the Mate.
- **Sidebar footer became "← Back"** on the root page; the footer icons from the empty
  state are gone, so the sidebar changes shape between two visits to one route.
- **A colored sliver along the sidebar's left edge** (purple→green, a few px): a bleed.
- **Group heading is bare bold text**: no count, no menu, no line.
- Question: did the card flash another state before settling on "starting" after the
  reload? One frame cannot tell; if yes, the identity-cache rule (a reload paints
  nothing it takes back) is broken here.

## 6. First Mate comes up: "Wait for it" → silence → "Starting…" → "Connect"

Why no auto-connect: `autoConnectServedZeropsEnvironment` fires only for the candidate
whose container origin equals the page's origin — the Mate serving the app itself. On
mate.zerops.io and localhost nothing matches, so every Mate waits for a click. The wizard's
wait would have followed the creation into the conversation; the reload killed it and the
projects page does not resume it, though the creation handoff is still in storage.

The silent middle: container ACTIVE → row leaves "provisioning" → action `pending` renders
no word until the health probe answers → "Starting…" → "Connect". Three states in words,
one mute; "Wait for it" was never a thing to click (it starts the same wait the page could
start itself).

- **Resume the creation on reload**: handoff in storage + Mate not connected → start the
  wait, land in the conversation when it answers.
- **Clicking a Mate opens it**: connect is a step on the way, not a verb to learn. Card
  and sidebar row open the conversation and connect if they must.
- **The face carries the boot**: asleep (container starting), waking (server
  initializing), awake (answers). No "Wait for it", no "Starting…", no mute gap.
- **"Server 0.11.0" off the card**: menu or settings.

## 7. Connect fails: "Could not connect to this container. The environment could not authorize the connection."

**A bug, not a design note.** The 0.11.0 server's door read the org member list under
`items`; the platform answers `clientUserList` (ledger 2026-09-16, *The first Mate on an
emptied org*). Every connect answered 500. Fixed in fork `7e57be0e3`, released as 0.11.1;
the test Mate's container restarted to converge.

Design notes on the failure screen itself:
- **"Project ready" + a green READY pill + "Zerops Mate is ready in this project" above a
  red box** saying it could not connect. The card contradicts itself; the state is the
  failure, and the heading should say so.
- **The message names nothing the person can act on**: "could not authorize" reads as
  "you are not allowed", when the server fell over. A 500 and a refusal need different
  sentences; and a trace id somewhere for the report.
- **"Try again" as a grey pill inside a pink box, "Back to projects" as an outlined
  button below**: two styles, two places, one escape.
- Header and sidebar as on screen 5.

## 8. Delivering 0.11.1 to a running Mate

The release took a minute; the restart did not converge — boot log: "mate 0.11.0 already
installed, no network reached". zcp's manifest cache (`$Prefix/manifest.json`) is keyed by
time only, one hour from the fetch, and a plain restart keeps the filesystem. Without a
shell in the container (`zcp mate update` refreshes) the earliest pickup is the next
restart after expiry. Open item for zcp: a boot should refresh the manifest when the cache
is older than the running process (or always on `init mate`), so a fix reaches a Mate on
the restart the app already does.

## Built from the notes above (fork main, 2026-09-16 evening)

- `271a530d8` wizard: one form (name, location), "Create project", no brief, no agents
  step, no wait page; returns to the projects page. (screens 2, 3, 4)
- `e090a363b` sidebar: no duplicate empty state; footer keeps its icons on /zerops with
  Zerops active. (screens 1, 5) The left-edge sliver was not found in our code; the
  owner runs `elementFromPoint(3, y)` on the projects page to name the painter.
- `68634f145` projects page: first run owns the page (no title, refresh or header pill;
  large face, "Start a project", one button); header pill gone everywhere; the face
  carries the boot ("Coming up. A few minutes." / "Almost there." / nothing), no
  "Wait for it" / "Starting…" / "Connect"; clicking a Mate opens it; a creation
  resumes after a reload; the "Preparing your project" takeover deleted, wait/timeout/
  failure on the Mate's card; add verbs wait for the first Mate, "Add Mate" a quiet verb;
  Gitea a card with a link or "Setting up.". (screens 1, 3, 5, 6, 7)

Not built: typing a first message while the Mate is still coming up (needs the
conversation route to open without a server connection). The group heading (bare bold
text) and the "one verb, three styles" note are covered by removing two of the three.

## 1b. Empty state after the fix — owner's verdict: still a poor standard

Removing the noise did not add design. Open, to be designed rather than patched:
- **the real Mate logo** (the mark), not a generic idle face, as the picture;
- **the left column closed or hidden** on an empty account; the page is the whole viewport;
- **a proper full onboarding / empty state**: composed like a first screen someone chose —
  type, spacing, one motion moment, name + button as the only inputs, the few-minutes
  expectation said once; editorial treatment, not utilitarian. A real design pass with
  screenshots at 1786 and 1280, both themes, before it ships.
- The left-edge sliver (navy → green) is visible on the empty state too; likely the desktop
  past the window edge in the capture — confirm with `elementFromPoint(3, y)`.

## 2b. New project form after the fix — owner's verdict: design very poor

Fewer fields, no better form. Open, to be designed with the empty state (1b) as one flow:
- a floating white card on a grey field with grey inputs inside it; the intro line sits
  outside the card as an orphan; the org switcher is missing from this page's header;
- the location select shows an internal code and a bare chevron; the button is a small
  pill disabled to pale blue with no explanation of why;
- the form is a generic settings form. It should read as the second beat of onboarding:
  one question, the name, large; location as a quiet secondary control; the card frame
  gone or full-bleed; type and spacing from the same pass as 1b.

## 5b. Projects page while the first Mate boots, after the fix — owner's verdict: still poor

The general design and vibe, not one element: a bold group label, a white card with a
small face and a sentence, an empty half-page to its right, a muted "Tools" heading and a
second card, and nothing else in a wide grey field. Belongs to the same onboarding pass as
1b and 2b: the first minutes of an account are one composed screen, not a list with one row.

Two defects on top:
- **Layout shift / flash on the transition from New project**: the page paints the empty
  state (no candidates yet) and then the list once the inventory read lands — the rule "a
  reload paints nothing it takes back" broken. Fix: a pending creation handoff counts as a
  project — render its group and a coming-up card from the handoff's name before the
  inventory answers, and never the first-run screen while one exists. Done in part
  (fork 7abefe78f): no first-run screen while a creation is pending; the placeholder card
  from the handoff is still open.
- **Gitea reads "Not available." while it is being set up**: `deriveGiteaState` calls a
  missing `web` service "unavailable"; right after the import the service list has not
  caught up. Fixed: a missing web on a live project is "provisioning".

## 9. Second run: the Mate project never left NEW ("Coming up" forever)

Platform side: Gitea `goAPyoSmR2u4U65zehEjig` created and built fine at 20:21:16Z; the Mate
project `txRlx5AcRbexBQEkAUIDLg` one second later got `project.create` FAILED
(`internalServerError`, process `j2cJQm8VSTSyMQEZvm4e9g`, 20:21:18.151Z) and `stack.build`
FAILED (`l5mjyAAIRfiGsiHlLX1t9A`, pipelineFailed 20:21:19Z); the project stayed NEW, the zcp
service READY_TO_DEPLOY, no log. Org `Mate`, owner ales+mate@zerops.io. Zerops-side error;
the previous run at 19:2x with the same timing succeeded.

Client side, two gaps:
- **The create-project step never looks at the process.** `POST /client/{id}/project` answers
  200 with the project and the creation went on as if it had succeeded; the wizard navigated
  away. Fix: after the POST, poll `project.create` for that project (process search by
  projectId + actionName) to a terminal state, bounded; FAILED fails the step with the
  platform's message and nothing else runs.
- **A project stuck in NEW reads as "Coming up" forever.** Fix: a NEW/CREATING project whose
  `project.create` process is FAILED (or that is older than a bound with no process running)
  is a failed creation on the card — "Could not be created." with a "Remove" verb that
  deletes it — never a boot that never ends.
Org wiped again for a third run.

Built (fork `938de7167`, merged `2ebc914c7`, released as 0.11.2): the create-project step
polls `project.create` to a verdict (2 s, 60 s cap) and fails with the platform's message;
a NEW/CREATING project whose process FAILED/CANCELED is "Could not be created." with a
"Remove" verb (delete + forget the handoff). Caveat: the wizard's own path creates project
and container in one call (`createProjectWithZeropsMate`) and does not run that step, so
for the flow the owner uses it is the page-side verdict that catches a failed creation,
on the card, after the wizard has returned to the projects page.

## 10. A Mate whose server is restarting (release rollout) reads as "not connected"

The owner's tab showed Milo as not connected during the container restart that installed
0.11.4, with no hint he was coming back, then "all of a sudden" online when the socket
reconnected on its own. The reconnect is right; the gap is unexplained.
- The face should carry it: away, header spinner, and any sentence says the Mate is
  restarting, never that it is gone.
- The app cannot tell a restart from an outage today. A restart the app did not start is
  exactly what a release rollout looks like, so this state will be common; the platform's
  service status (RESTARTING / the process list) is readable and could name it.

## 11. The Mate's Gitea credential arrives only when someone opens the projects page

After the third run's conversation opened, Milo had none of `GITEA_URL`, `MATE_BROKER_URL`,
`GITEA_TOKEN`, though Gitea and the broker were up: the credential reconcile runs on the
projects page, and the owner had been on the Mate's page since it came up. When it does
run it writes the three variables and restarts the container, minutes after the person
started talking to the Mate. Two gaps: the reconcile should run wherever the app is (the
conversation included), and the first credential should land before the Mate is first
opened, so its arrival is never a restart mid-conversation.

## 12. D20 built (2026-09-17 morning): the broker delivers a Mate's Gitea access

gitea-mate `3c64092`/`8bffe09` (v2): the rights loop gathers every registered Mate's zcp service
and variables, plans `DeliverMateAccess` when the token is absent, names another Gitea, is not
the bot's newest generation, or the bot has none live; writes create/update, never restarts;
`POST /mate/credential` deleted. Fork `fe4552b46`/`648df9a8a` (0.11.5): registering a Mate
widens the broker's token with its project (shared with stages via `grantBrokerProject`); the
app's credential fetch, receipts and the creation step are deleted; the Git tab's `provisioned`
evidence went with the receipts (the remote probe is the remaining proof).
Open: a reconcile that re-grants a registered Mate the broker cannot reach (a grant that failed
after the registry write has no UI retry today; the loop reports it every pass). Research and
measurements: ledger *Broker-shaped and Mate-shaped tokens against a seconds-old project*.

## 13. D20 driven end to end (2026-09-17, ~07:34Z)

Empty org → Gitea imported (gitea-mate v2) → group and Mate registered → Mate made with the
client's YAML → broker granted the Mate project → nothing. The broker booted before Gitea
published its admin token and held the reference unresolved (20 min of 401s; fix in flight:
the broker resolves the admin credentials from the platform). After one broker restart: one
pass, 13 actions, the Mate's three variables 20 s later; bot restricted in the `read` team, group
repo and hook made, the delivered token authenticates as the bot and `POST /mate/repository`
created `imperial-titan/hello`. Minor: the bot's full name is the project's name, not the Mate's.

Boot-order fix landed as gitea-mate v2.1 (`47752ff`: the broker resolves its admin pair from
`web`'s user-data through the Zerops API when unresolved or refused). Final run from an emptied
org: delivered 328 s after registration, 226 s after the Mate was up, nothing restarted. The org
is left populated for the owner (Gitea, Imperial Titan - dev, Mate Nova) on mate 0.11.5.

## 14. Owner's run on mate.zerops.io (2026-09-17, ~10:40Z): backend ready; a Gitea serves one origin

Read-only check before the first prompt, from the API and Gitea's admin API (a temporary
integration token, deleted after). Gitea `web`/`broker`/`db`/`volume` ACTIVE; registry
`imperial-titan` + Mate `Imperial Titan - dev` (Zane); the broker healthy, OIDC discovery and
`GET /gitea/oauth-client` answering; Gitea org `imperial-titan` private with `Owners`/`read`/
`write`/`release`, `imperial-titan/group` private with `main` protected to `release` and `env/*` to
the admin, one org hook to the broker; the bot `mate-{projectId}` restricted, active, in `read`;
the delivered `GITEA_TOKEN` authenticates as the bot; `mate:signer:claude-code` written. Nothing
restarted, nobody asked — D20 through the released client, first time by the owner.

Finding: the app's OAuth2 client carries one redirect, `https://mate.zerops.io/gitea/callback`, and
Gitea's `[cors]` the same list — the origins the creating client sent. A Gitea made from the hosted
app serves the hosted app; the localhost dev pair cannot sign this org's person in to Gitea (PKCE
redirect mismatch) or call its API from the browser. Open: how a second origin joins an existing
Gitea (`MATE_APP_ORIGINS` on the broker and `GITEA_CORS_ALLOW_DOMAIN` on `web`, both restart) —
today only by hand.

**Closed 2026-09-17 (D22, mate 0.11.7 + gitea-mate v3.1).** The owner, on being told a Gitea made
from mate.zerops.io could not be driven from localhost: "didn't you just make it so you don't have
to login to gitea and you can use mate's logged in user's token to auth to gitea api?" — and about
Gitea's own pages: "when someone uses the gitea url they should still be able to login with zerops".
Both hold: under D21 every browser call carries a bearer and no cookie, so the two allowlists proved
nothing and only pinned the Gitea to its creating origin. Gitea's `[cors]` and `POST /person/token`
now answer `*`, the import sends no origin list (`__CORS__`, `MATE_APP_ORIGINS`,
`GITEA_CORS_ALLOW_DOMAIN` and `appOrigins` are gone); Gitea's own-page OIDC sign-in is untouched.

Second finding, same check: Zane's token is lowered (`NO_ACCESS` + `BASIC_USER`, the group-reach
reconcile's write) but its creation delegation is still there and the project runs
`envIsolation: none` with `ZCP_API_KEY` as a plain project variable — 0.4 and 0.10 exist only as
`createEnvironment` steps, which the one-call _New project_ of 0.11.2 skips, and nothing on the
projects page applies them. Not blocking the run; a restart is safe (the key is re-injected from
the project). Fix: run both right after the create, before the container's first boot, so nothing
restarts.

## 15. Zane opened, before the first prompt (2026-09-17): the Git tab reads as "not set up"

Screenshot: "REPOSITORIES — This Mate has no code repository yet." and "THE PROJECT — Sign in to
Gitea to see this project's environments, its releases and its pull requests. [Sign in to Gitea]";
the composer empty with its generic placeholder; the hero "What should Zane do on Imperial Titan?".
The owner's reaction: "you said this was set up?" Both lines are true — a repository is made with
the first dev pair, and the Gitea side acts as the person, who has not signed in yet — but the tab
says nothing about what IS ready (Zane's Git hosting, delivered by the broker), so a ready Mate and
a broken one read the same. Open: the empty state should say the true state ("Git hosting is ready;
a repository appears with the first service") and frame the Gitea sign-in as the one-time step it
is; the composer should have carried the setting-up job (`useZeropsCreationJob` composes it once
the agent is signed in) and did not — cause not yet known. Not blocking the run.

Owner, on the button: "why am I not automatically logged into that Gitea? … it should be able to
use the same login token as I, as a user, have." The chain behind the pill today is four screens:
the pill, Gitea's login page (*Sign in with Zerops*), the app's own consent page (a click that
mints the throwaway), Gitea's *Authorize application* page for the public client, then the PKCE
exchange. Two of the four are ours and need no click; two are Gitea's. Proposed decision (D21,
for the owner): a person's Gitea credential for the app is minted by the broker on a throwaway,
exactly like the door — the rights loop creates every active member in Gitea bound to the OIDC
source (so a later sign-in to Gitea's own UI still matches), and `POST /person/token` by throwaway
answers a scoped token as the person, which the broker's loop retires after a bound. No Gitea
screen, no button; the broker is in the token path, as it already is for OIDC. Cost: Gitea tokens
carry no expiry, so retirement is the broker's job. Build after this run (a release restarts Zane).

Built (2026-09-17 afternoon): D21 — gitea-mate v3 `93969e9` (`POST /person/token`: the person by
throwaway, the account made bound to the OIDC source, one pass, a token minted as site admin; app
tokens retired after 12 h; the OAuth2 client, its route and its registration deleted), mate 0.11.6
`e6b1662f2` (the Git surface acquires the session by itself, the pill and the PKCE code gone, the
consent page completes on mount). The lab measurements are in the ledger. Zane's broker runs v2.1:
the new route reaches an org only with a broker built from `main` — a fresh Gitea (wipe and
create), or the broker service redeployed.

## 16. Owner's run through localhost on 0.11.7 (2026-09-17, ~11:18Z): a stale card, a stuck sign-in

The owner, from the emptied org, on localhost (D22 live on the org's broker: its preflight answered
every origin). What they saw, in their words:

- "seems to be still stuck at almost there" — the Mate's card, minutes after its `zcp` reported
  `initComplete`; "on refresh it redirected me to the convo". The wait reads the container from the
  pushed inventory; a missed push leaves the card waiting. Open (primer §7, item 2).
- The Git tab: "this is just stuck on this" ("Signing you in to Gitea…"); "why am I not signed in to
  gitea once at page load when gitea exist anyway?" — it was signing in, every twenty seconds, and
  being refused: `admin-init.sh` had left the OIDC source "for a later boot" (the broker's secret
  unresolved nine seconds before the broker listened), no later boot came, and Gitea answered every
  account creation `422 login source does not exist [id: 1]`. The broker turned that into a `502`,
  the platform's edge replaced the `502` with its own HTML page, and the app read a bodiless `502`
  as "still setting up".
- On being asked for the runtime log: "why the fuck would you do anything from outside? you have
  email and pass to ales+mate and you have agent-browser and all other tools" — the process note:
  a stuck screen is diagnosed from inside, with the test owner's login, the logs and a replay of
  the app's own call, before anything is asked of the owner.

Unblocked by a restart of `web` (the source added in three seconds; `POST /person/token` → 200 as
the owner, site admin, source 1). Fixed for good: gitea-mate v3.2 (`start.sh` serves only with the
source; a Gitea refusal is `424 gitea_refused` in Gitea's words) and mate 0.11.8 (the refusal shown
in place of the sign-in line). Ledger: _The owner's run through localhost on 0.11.7_.

- The Git tab, once signed in: "btw appstage wont even have repo, it's the stage pair, it gets
  compiled code deployed to it, its not mounted either" — the tab listed every runtime service and
  said "appstage · no repository yet". Fixed in 0.11.10: one block per codebase, the dev half of
  each pair, by the service map's own folding rule.
- While Juno worked, the owner's design sketch: a Gitea button in the sidebar's bottom bar opening
  an overview of every repository they can reach and its open PRs; and each group in the sidebar
  drawn as a timeline — Mates, their open PRs as an accordion, then stage, then production. Noted
  for the design pass (primer §7); the data is one person-token away since D21.

## 17. Juno done with the app base (2026-09-17, ~12:05Z): no production, the recipe not merged

"so Juno is done with the app base, but it never asked me to setup production, and I feel like it
didnt auto merge the import yaml, can you check everything?" Read from inside: the pair built (zcp's
first deploy goes direct to the pair's own stage); the app's code never pushed to `appdev` (zcp
pushes on a git-push deploy, and the agent used the direct one); the recipe open as PR #1 on the
group repo, `main` bare, no `environments.yaml`; so nothing could offer a stage or production.
The tool gate refused a merge without review; "but they all should be able to merge on the import
yaml repo imo" → **D23** (gitea-mate v3.3): `main` on the group repo keeps no merge whitelist and the
broker merges a Mate's recipe pull request on arrival. Scope note from the owner: "our point is not
to make zcp's prompting/flows flawless, there will be a separate person working on that for
hardening.. but we at least need a full PoC working" — the push-after-deploy in zcp is left for
them; for this run the owner pushes from the Git tab.

Also seen: the "Authorize coding agents" card lists Claude Code and Codex only — "we removed the
new project step where you select which agents should show up, but then they don't show up here".
That list is `ZeropsAgentId` in the contracts, the two agents the Mate server can drive; the
image's other CLIs belong to zcp's own code-server page (`ZCP_AGENTS`), which the removed step
narrowed. Showing more here means porting their providers. And the Mate server titled the thread
with Codex, which has no credentials in this Mate, and failed (its log); the provider choice for
titles is to follow the signed-in agent.

## 18. Waiting for the Mate on 0.11.10 (2026-09-17, ~14:50Z): the page blanked on every re-read

"every now and then when waiting for the mate to come up it does this full refresh, that's crazy
bad" — two screenshots: "Reading your projects…" over an empty left menu, then the roster again with
Dara at "Almost there." and Gitea "Setting up.". Cause: the 0.11.9 clock (every twenty seconds while
a creation is on its way) re-took the inventory leases, and a released lease drops what it read —
the same blank on the header's reload, the access renewal and after every start or restart. Fix
(0.11.11): the runtime re-reads an organization on a fresh receiver and releases nothing
(`runtime.refresh`); the page and the menu paint from the list already read (`readOnce`), and the
header's glyph alone spins.

## 19. Dara's first prompt (2026-09-17, 13:17Z): one simple-mode service, no pair, "no repo"

"check the convo, it didnt do aything" / "no repo, no pairs" / "what confusing as well is that it
didnt even create dev/stage pairs". Read from inside: the prompt "create a todo app" ran through
zcp's classic route (no recipe matched) and the agent's plan chose `bootstrapMode: simple` — one
`todoapp` service self-deployed over its own working copy, plus `tododb`; the app is live, the
recipe PR merged by the broker in nine seconds, the service repo made empty by the broker on the
deploy. Nothing was pushed, and the Git tab's row says "no repository yet" because a simple-mode
service keeps no checkout. zcp's own gate names the rule: a simple-mode service cannot be a push
source; production needs a repo-backed pair (`bootstrapMode: standard`, an explicit stage
hostname). For the hardening person: the plan's mode is the agent's choice on the classic route,
and "create a todo app" got the mode that cannot enter the release chain; a Mate whose group has a
Gitea should default to standard, or the bootstrap should ask. For the run: the owner answers the
agent's stack question with "standard mode, a dev/stage pair".

## 20. After the first prompt (2026-09-17, ~13:30Z): the tab not live, and expanding into a pair

"well I can see the repo now, after refresh, its not updated live? also can I tell it to turn this
into a dev/stage pair and the continue on with the test". The Git tab's row had read its status once
at subscription, and the turn's end refreshed only the workspace root, never a repository — fixed in
mate 0.11.12 (the turn's end refreshes every mounted checkout; reaches a Mate at its next update, so
the running Dara still needs the reload). The expansion is zcp's own flow — the adopt route with
`isExisting`, `bootstrapMode: standard` and an explicit `stageHostname`; `bootstrap_outputs.go`
merges the service meta on "dev/simple → standard" — and the dev half keeps its hostname, so the
pair reads `todoapp` / `todoappstage`; the app folds that as a pair since 0.11.12. The owner tells
the agent in words: "expand todoapp into a standard dev/stage pair, todoapp as the dev half and
todoappstage as its stage, then continue".

## 21. "What do we need to change to have a proper PoC run?" (2026-09-17, ~15:55Z)

"so this is fucked up form the beigning I feel, what do we need to change to have a proper PoC run?"
/ "I can start from scratch, but it needs to be a success in full". The backbone measured fine on
the fresh org; the breaks were the agent's shape (simple mode), the agent's delivery (direct deploys,
no pull request) and the page's silence about the next step. Landed the same afternoon: zcp
`8ae175f6` — a Gitea-wired Mate plans standard pairs only on the classic route, a direct deploy of a
wired pair names the git-push deploy that opens the pull request, the develop session's auto-close
note asks the agent to hand the person the pull request's link and the stage/production step; mate
0.11.13 — the projects page signs in to Gitea by itself and lists the tiers the recipe offers as
rows with their verbs ("Stage · not set up yet · Add stage"). Release and rollback (5.5, 5.6) have
never run live; the whole chain is rehearsed in the audit browser as the test owner before the
owner's run.

## 22. The rehearsal of the later legs (2026-09-17, 14:20Z–): what zcp leaves behind after an expansion

Rehearsed in the audit browser as the test owner on Dara's org. The app's own breaks (the Git
tab's pull-request and merge verbs unwired, the tier parser blind to four-space items, the project
block rewritten at the wrong indentation) are fixed in the fork. Two things for the hardening
person: (1) after `todoapp` was expanded into a pair, `zerops.yaml` got `prod` and `dev` setups but
the group recipe on `main` still named `zeropsSetup: todoapp` — the broker refused the stage's
first deploy with "has no setup "todoapp", which the tier names"; the recipe was never re-proposed
(spec 2.2 "kept current"), and the person had to ask the agent for it; a topology or setup change
should re-propose. (2) The service repo's workflow zcp writes runs on every push to `main` and fails
when no environment follows `main` yet — a red check the Git tab shows as failing; the workflow
could skip cleanly when the broker answers "no environment".
