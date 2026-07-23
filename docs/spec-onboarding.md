# Spec: Agent-side onboarding ("onboard me to Zerops")

The onboarding mode is a **content-only, user-only conversation router**: when a person
asks a ZCP-bound coding agent to be onboarded, the agent opens a short, state-aware
conversation — a warm greeting, a three-way fork (bring an app / start something new /
take a quick tour), and per-branch handoffs into the EXISTING workflows. ZCP ships exactly
two artifacts for it: an AGENTS.md trigger block and one playbook document. There is no
`zerops_onboard` tool, no workflow action, no state file, no flag, and no skill subtree —
the same anti-scope discipline as guided mode (spec-guided-mode.md G5).

Welcome-screen integration (the GUI sending the phrase as a kickoff prompt) is a separate,
later concern owned by spec-welcome-mode.md; this spec covers only the agent side.

---

## 1. Trigger contract (O1)

Owner: `internal/content/templates/agents_onboarding.md`, composed by `BuildAgentsMD`.

- The block renders **iff `!rt.Authoring`** — user contexts only, in BOTH the local and
  container preambles, independent of guided state. Authoring AGENTS.md never carries it.
- Position: **after the environment preamble, before `agents_shared.md`'s routing table**
  — spliced into the body concatenation, not appended. The block says "before the routing
  below"; position is load-bearing.
- Trigger semantics (normative copy lives in the template): the **exact phrase "onboard
  me to Zerops"** (any capitalization/punctuation) or a **clear meta-onboarding request**
  ("get me started with Zerops", "I'm new here — what now?") enters the onboarding
  conversation. A request to get started with a **specific technology or task** ("help me
  get started with PostgreSQL", "deploy this repo") is normal routing, never intercepted.
- On trigger the agent: fetches `zerops_knowledge uri="zerops://playbooks/onboarding"`
  once and follows it; mutates nothing until the person picks a direction; hands off to
  normal routing (and guided, when present) after the choice; and reports tool/auth
  unavailability plainly, surfacing the reported recovery — never simulating onboarding.
- The block must not violate the composition-wide content constraints: no
  ``- `bootstrap` ``/`- `develop` `` bullet tokens (first-occurrence ordering pins), no
  environment-specific strings, no authoring tokens, no guided marker, tools-only
  `zerops://` form.

## 2. State resolution (O2)

The playbook opens with a two-step, read-only state check:

1. `zerops_workflow action="status"` — an active bootstrap/develop/launch session or a
   current intent/scope means MID-WORK: the agent leads with the work in progress.
2. Otherwise `zerops_discover` — classification comes from THIS call ONLY. The status
   Services list is ES-backed and excludes the control-plane (`SelfService`); it can lag
   and has shown spurious rows — it is never the classifier. Discover is the direct,
   lag-free read and shows the control-plane as `adoptionState:"zcp-self"`.
   - **FRESH**: every non-system row is `zcp-self` (or the list is empty), no live
     `activity`, no warnings.
   - **POPULATED**: anything else — `adoptable`, `adopted`, `managed-dep`, `resumable`,
     or `bootstrapping` rows, live activity, or warnings.

## 3. Conversation contract (O3)

Normative copy lives in the playbook (`zerops://playbooks/onboarding`); load-bearing
elements, pinned by tests:

- FRESH opening: three options with these exact labels — **Bring an app**, **Start
  something new**, **Take a quick tour** — plus the freeform escape line "Or tell me the
  outcome you want."
- POPULATED opening: one compact found-services line, then the same options with
  **Continue this project** prepended. MID-WORK leads with the in-progress work.
- **Consent before provisioning**: nothing mutates from the bare phrase. The only
  pre-consent bootstrap call is the read-only route menu (opens no session —
  spec-workflows.md §2); committing `route="recipe"` requires an explicit yes
  (RCO-5 alignment). Structurally: no `action="start"`, `zerops_import`, or
  `zerops_deploy` directive may appear in the playbook before its Branches section.
- The playbook never re-styles next-step suggestions that the typed Plan already owns
  (`planIdle` primary/alternatives); it funnels into them.

## 4. Branch playbooks (O4)

- **Bring an app** — read-only workspace scan first; if the source location is ambiguous,
  exactly ONE question (this workspace / a Git repository / only the running deployment).
  Lanes: workspace → standard bootstrap route menu → develop → deploy; Git repo → clone
  into the workspace, then the workspace lane (git-push delivery configuration comes
  AFTER the first successful deploy); running-deployment-only → honest refusal (source
  required) + offer Start; data/DNS → explicitly deferred, never moved automatically.
  Credentials are user-owned — the agent never fabricates repo access.
- **Start something new** — demo: route menu read-only → explicit yes → `route="recipe"`
  → bootstrap steps to a running URL; idea: a normal build request owned by standard
  routing/guided from there.
- **Take a quick tour** — fetch `zerops_knowledge uri="zerops://themes/model"` once;
  explain exactly three concepts (project ⊃ services ⊃ containers; private network +
  hostname addressing; build → deploy → run), connected to the discover result; never
  recite pricing, YAML fields, limits, or the full reference; finish by offering to show
  the pieces live or set up a small demo.

## 5. Content home (O5)

- The playbook lives in a **committed, repo-owned knowledge family**
  `internal/knowledge/playbooks/` (embed + `knowledgeDirs`), URI
  `zerops://playbooks/onboarding`. It is NOT Strapi-synced: sync pull/push never touches
  the family; the doc embeds even in an unsynced checkout (tests for it live in
  `internal/tools`, outside the corpus guard).
- The family is **direct-fetch-only**: excluded from `Store.Search` at the single
  all-docs scoring loop, so playbook copy never competes with recipes/guides in `query=`
  hits. `Store.Get` (and thus `uri=` fetch) stays family-blind.
- Both new surfaces (the template + the playbook family) sit under the agent-facing
  content lints: bare-`zerops://` is forbidden (tool-call form only) and no hardcoded
  service versions.

## 6. Guided + authoring interplay (O6)

- **Guided ON**: the onboarding fork still runs first — the bare phrase carries no
  product intent for guided inference. Once the person chooses to build or bring an app,
  guided's always-on contract owns the work, exactly once. The tour stays knowledge-only.
- **Authoring**: mutually exclusive, same as guided — the block never renders under
  `ZCP_AUTHORING`; onboarding is an end-user surface.

## 7. Invariants (pinned)

| # | Invariant | Pinned by |
|---|---|---|
| O1 | Trigger block renders iff `!Authoring`, both envs, guided-independent, ordered before the routing table; trigger copy carries exact phrase + variant + negative rule + fetch directive | `TestBuildAgentsMD_OnboardingGate_UserOnly`, `TestBuildAgentsMD_OnboardingFirst_BeforeRouting`, `TestBuildAgentsMD_OnboardingTriggerCopy`, `TestRefreshAgentContext_OnboardingPreserved` |
| O2 | Playbook resolves state status-first, classifies from discover only; fresh rule = all non-system rows `zcp-self`, no activity/warnings | `TestPlaybookOnboarding_ContentPins_CoreContract` (ordering + wording pins) |
| O3 | Fork labels + Continue + escape line present; no mutating directive before the Branches section | `TestPlaybookOnboarding_ContentPins_CoreContract` |
| O4 | Tour fetches `themes/model` only (no `scope="infrastructure"`); BRING asks one source question | `TestPlaybookOnboarding_ContentPins_CoreContract` |
| O5 | `playbooks/` fetchable by URI in an unsynced checkout; excluded from search; under both content lints | `TestKnowledgeTool_PlaybookURI_FetchesEmbedded`, `TestSearch_ExcludesPlaybooks_NoHits`, `TestNoBareZeropsURIInAgentContent` (extended dirs), `TestTemplatesContent_NoHardcodedVersions` |
| O6 | No new tool/action/state (content-only anti-scope) | absence — no new tool in `annotations_test.go` |
| O7 | Behavioral coverage: trigger positive/variant/negative, populated, guided-on scenarios exist and parse | `TestOnboardingScenarios_ExistAndParse` + scenario manifest lint |
