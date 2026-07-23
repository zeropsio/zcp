# "Onboard me to Zerops" — agent-side onboarding mode (design, pre-implementation)

Provenance: product brief (Karel+Aleš welcome decision tree) → Codex fact inventory →
independent Codex design round → synthesized design → Codex adversarial review
(13 findings, all incorporated below). Status: design approved-for-build pending owner
sign-off on the two OPEN product decisions at the end.

## Product intent

The welcome screen's redesigned decision tree (GUI work, FE-side) offers:
1. onboarding with an agent (recommended) / 2. deploy a recipe yourself / 3. manual.
Path 1 = agent selection → bridge auth → agent opens with the kickoff phrase
**"onboard me to Zerops"**. The agent then greets the user in a few words and forks by
user state. The old two CTA prompts (`new`/`existing`) collapse into this single phrase;
the new-vs-existing decision moves INTO the conversation.

The mode is **content-only and user-only** (guided-mode precedent): no new MCP tool, no
workflow action, no Go state machine, no onboarding state files. ZCP ships a trigger block
+ one playbook document; existing tools do all the work.

## Mechanism

### Trigger block — `internal/content/templates/agents_onboarding.md`

- Composed by `BuildAgentsMD` iff `!rt.Authoring`, independent of guided, in BOTH local and
  container contexts.
- **Ordering (review finding 1):** rendered immediately after the environment preamble,
  BEFORE `agents_shared.md`'s "Route every user turn" — with an ordering assertion in the
  test, not just presence. The block states explicitly: onboarding outranks guided only
  until the user chooses a direction; then normal routing/guided own the work exactly once.
- **Narrow trigger (finding 12):** exact phrase (case/punct-insensitive) + clearly
  meta-onboarding variants only. Negative controls: "help me get started with PostgreSQL",
  "deploy this repo" must NOT trigger.

Draft block copy:

> ## Zerops onboarding
> When the user asks to be onboarded to Zerops — the exact phrase "onboard me to Zerops"
> (any capitalization/punctuation), or a clear meta-onboarding request ("get me started
> with Zerops", "I'm new here — what now?") — run the onboarding conversation before the
> routing below. A request to get started with a SPECIFIC technology or task ("help me get
> started with PostgreSQL", "deploy this repo") is normal routing, not onboarding.
> 1. Fetch `zerops_knowledge uri="zerops://playbooks/onboarding"` once and follow it.
> 2. It opens with a state check and a fork; don't provision, import, or mutate anything
>    until the user picks a direction — read-only inspection is fine.
> 3. Once the user chooses to build or bring an app, normal routing (and the guided skill,
>    when present) owns the work — onboarding only opens the conversation.
> 4. If Zerops tools are unavailable or auth fails, say so plainly and surface the reported
>    recovery — never simulate onboarding.

### Content home — new `playbooks` knowledge family (review "option d")

- `internal/knowledge/playbooks/onboarding.md`, repo-owned + committed (NOT Strapi-synced:
  push_guides.go sweeps every guides/*.md upstream — shared ownership rejected; themes are
  semantically platform references — rejected).
- Add `playbooks/*.md` to the `go:embed` directive + `"playbooks"` to `knowledgeDirs`
  (documents.go). URI derives mechanically: `zerops://playbooks/onboarding`.
- **Direct-fetch-only (finding 5):** exclude `zerops://playbooks/` from `Store.Search` so
  onboarding copy never competes with recipes/guides in `query=` hits. Pin with
  `TestSearch_ExcludesPlaybooks`.
- No catalog/briefing/corpus-guard/sections side effects (all verified keyed elsewhere).
- The embedded-fetch test lives in `internal/tools` (corpus guard exits `internal/knowledge`
  package tests in unsynced checkouts — finding 8).

## Conversation (playbook content)

### State resolution (findings 3+4)

1. `zerops_workflow action="status"` first — if it reports active bootstrap/develop/launch
   or an intent/scope, the project is POPULATED-ACTIVE: lead with the work in progress.
2. Otherwise `zerops_discover` (direct REST, authoritative — status's ES-backed list can
   lag and its IdleEmpty ignores managed deps). FRESH iff: no warnings, no live activity,
   and every non-system service has `adoptionState="zcp-self"` (empty list is also fresh).
   Any `managed-dep`/`adopted`/`adoptable`/`resumable`/`bootstrapping` service → populated.
3. Reality pins needed: envelope fixtures for `zcp@1`-only and `zcp@1`+managed-dep; one
   live smoke on a fresh welcome-flow project.

### Opening copy — fresh

> Welcome to Zerops. What would you like to do?
> - **Bring an app** — use source in this workspace or a Git repository, including an app
>   that currently runs elsewhere.
> - **Start something new** — try a ready-made demo or build an idea together.
> - **Take a quick tour** — understand the platform before we change anything.
>
> Or tell me the outcome you want.

### Opening copy — populated

If work is active, lead with: "There's work in progress: <intent> on <scope>." Then:
"I found <compact live-service summary> in this project. What would you like to do?" with
**Continue this project** prepended to the three options above + the freeform escape line.

### BRING branch

Read-only workspace scan first. If the source is ambiguous, ask exactly one question:

> Where should I get the app's source?
> - This workspace
> - A Git repository you can give me access to
> - I only have the running deployment, not the source

Lanes: workspace → bootstrap (classic/adopt) → develop → deploy; Git repo → clone/inspect →
workspace lane (`git-push-setup` optional AFTER first deploy); deployment-only → honest
refusal ("ZCP needs the source; it cannot reconstruct an app from a running deployment")
+ offer START as replacement path. Data/DNS: "Let's get the application running first.
Database transfer and DNS cutover are separate follow-ups; I won't move either
automatically." Import files (finding 13): `zerops_import buildFromGit` only for a
user/repo-owned import definition inspected inside the active workflow with a source URL
already present — never generated as an onboarding shortcut; private/unreachable repos ask
the user, never fabricate credentials.

### START branch

Demo → bootstrap without route (read-only route menu, opens no session) → user confirms the
surfaced recipe → `route="recipe"` commit → import → discover-poll → provision/close →
subdomain URL. Idea → guided ON: guided lifecycle owns the build after the fork; guided
OFF: bootstrap + develop. **Provisioning never auto-runs from the bare phrase** (consent
stance — see OPEN decisions).

### LEARN branch

Fetch `zerops_knowledge uri="zerops://themes/model"` ONLY (never `scope="infrastructure"` —
it prepends the live stack catalog and includes the YAML reference; wrong altitude).
Explain exactly three concepts (project⊃services⊃containers; private network + hostname
addressing; build→deploy→run), connect them to the discovery result, never dump YAML/
pricing/limits. Finish: "Want to see those pieces in this project, or set up a small demo
together?"

## Welcome CTA slice (separate, FE-adjacent, ext-version-gated)

- Replace both `CTA_PROMPTS` + both tiles with one `onboard` path whose clipboard payload
  is exactly `onboard me to Zerops`. Wording everywhere: the CTA **launches the agent and
  copies the phrase** (clipboard-first; `sendText` stays forbidden).
- **Authoring gate (finding 2, release blocker):** reject `start-onboarding` host-side under
  `ZCP_AUTHORING=1`, hide the tile, test zero-launch + zero-clipboard — otherwise authoring
  users get a phrase their AGENTS.md doesn't understand.
- Update the path enum in the message allowlist + all three suites with two-prompt
  assumptions: `cta_flow.test.js`, `ui_structure.test.js`, `message_allowlist.test.js`.
- Bump BOTH version owners (Go const + `vscode-bootstrap-package.json`) — same-version
  install is a content no-op.
- The webview's static "tour" rail stays (GUI-side LEARN complement).

## Spec + test surface

- New `docs/spec-onboarding.md` owns: trigger semantics + exact copy, block ordering,
  state adaptation (fresh test), branch playbooks, guided/authoring interplay, edges.
- `docs/spec-welcome-mode.md` §7 → single canonical prompt + pointer to spec-onboarding.
- Tests: `TestBuildAgentsMD_OnboardingGate` (present local+container, absent under
  authoring, present with guided on AND off, ordered before shared); refresh-preserves-block;
  `TestSearch_ExcludesPlaybooks`; embedded-fetch in `internal/tools`; URI lint dirs extended
  to `internal/content/templates/` + `internal/knowledge/playbooks/` (today's lint scans
  neither); no-hardcoded-version lint over both new surfaces; welcomejs suites above;
  envelope fixtures zcp-only / zcp+managed.
- Flow-eval scenarios (manifest-conformant): exact phrase + variants + negative controls;
  zcp-only container / local empty / managed-only leftovers / zcp+app / live activity /
  resumable bootstrap; guided on/off; authoring CTA rejection; no-MCP + auth-error honesty;
  private Git / monorepo roots / deployment-only / data-DNS deferral / trusted import file;
  CTA exact clipboard text + failure path.

## V1 exclusions

No new MCP tool or workflow action; no onboarding state files/flags/phase enum; no skill
subtree; no auto-provisioning from the bare phrase; no repo-credential brokerage; no
BYO-GitHub import state machine; no Heroku/VPS data migration or DNS cutover; no monorepo
decomposition beyond naming candidate roots + asking once; no hardcoded recipe slugs or
platform facts in content.

## OPEN product decisions (owner)

1. **Consent vs auto-run.** Aleš floated auto-running the recommended prompts. Design says:
   the bare phrase auto-runs only reads (playbook fetch, status, discover) + the read-only
   route menu after a demo choice; provisioning always gets one explicit confirmation.
   Override possible but recommended against (surprise resource creation on a first touch).
2. **CTA collapse.** Two tiles ("Build something new" / "Integrate my existing app") →
   one onboarding tile + the single phrase. Costs one extra conversational turn for users
   who already know which of the two they wanted; buys the state-aware fork + CONTINUE.

## Side-findings (repo hygiene, independent of this feature)

- `docs/spec-welcome-mode.md` §6 W-SKILLS is stale: curated embedded skills were replaced by
  community skill packs; `TestWelcomeSkillsMaterialized` (cited in W7) no longer exists.
- `docs/spec-scenarios.md` recipe-bootstrap rows are stale vs production: discover advances
  with `action=complete step=discover` (not `iterate`) and closes with
  `action=complete step=close` (not `action=close`).
