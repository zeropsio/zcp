# Spec: Agent-side onboarding ("onboard me to Zerops")

The onboarding mode is a **content-only, user-only conversation router**: when a person
asks a ZCP-bound coding agent to be onboarded, the agent opens a short, state-aware
conversation — an authored greeting with a one-sentence platform orientation, a three-way
fork (**Build something** / **Try a ready-made recipe** / **What are Zerops & ZCP?**), and
per-branch handoffs into the EXISTING workflows. ZCP ships exactly **three artifacts** for
it: an AGENTS.md trigger block and two playbook documents (`zerops://playbooks/onboarding`,
`zerops://playbooks/orientation`). There is no `zerops_onboard` tool, no
onboarding-specific workflow action, no state file, no flag, and no skill subtree — the
same anti-scope discipline as guided mode (spec-guided-mode.md G5). The recipe-conductor
behavior the branches ride on is owned by spec-workflows.md §8 RCO, including the
services-only provision YAML and the structured runtime URLs (RCO-6/RCO-7).

Welcome-screen integration (the GUI sending the phrase as a kickoff prompt) is a separate
concern owned by spec-welcome-mode.md; this spec covers only the agent side.

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

The opening is tool-call-free — the agent greets and offers the fork immediately after
the playbook fetch. State is read ON DEMAND — after the person picks a branch that needs
it, or when they ask what's already here — as a two-step, read-only check:

1. `zerops_workflow action="status"` — an active bootstrap/develop/launch session or a
   current intent/scope means MID-WORK: the agent leads with the work in progress.
2. Otherwise `zerops_discover` — classification comes from THIS call ONLY. The status
   Services list is ES-backed and excludes the control-plane (`SelfService`); it can lag
   and has shown spurious rows — it is never the classifier. Discover is the direct,
   lag-free read and shows the control-plane as `adoptionState:"zcp-self"`.
   - **FRESH**: every non-system row is `zcp-self` (or the list is empty), no live
     `activity`, no warnings.
   - **POPULATED**: anything else — `adoptable`, `adopted`, `managed-dep`, `resumable`,
     or `bootstrapping` rows, live activity, or warnings; the compact found-services
     summary and the **Continue this project** option appear at this point, not in the
     opening.

Ordering is demand-driven and pinned: greet with no state read → branch choice → status
→ discover → only then Continue / populated-project consent.

## 3. Conversation contract (O3)

Normative copy lives in the playbook (`zerops://playbooks/onboarding`); load-bearing
elements, pinned by tests:

- The opening is the SAME for every project state, and its menu block is rendered
  **VERBATIM** — the playbook's adaptation license covers transitions and explanations,
  never the greeting/menu block itself. The authored copy:

  > Welcome to Zerops! Zerops builds and runs apps and their supporting services,
  > connects them on a private project network, and can expose web services at a public
  > URL. I'm an agent that drives it through ZCP.
  >
  > What would you like to do?
  >
  > - **Build something** — describe an idea in one line, with a technology if you care
  >   ("create a weather dashboard in Bun" — or Node.js, Python, PHP, and many more;
  >   Zerops covers a wide range of stacks, so just ask); I set up the environment from
  >   a ready-made recipe and build it with you to a live URL.
  > - **Try a ready-made recipe** — a complete working app (Node, Python, PHP, Laravel,
  >   Go, Rust, …) running in minutes — and it becomes yours to develop further.
  > - **What are Zerops & ZCP?** — a short explanation of how it all works.
  >
  > Or just tell me what you want, in plain words — that works for everything here:
  > "scale the cpu to 4 cores", "show me the logs", "add a Postgres database".

  **Continue this project** joins the options only once state is known through an
  on-demand read or prior conversation knowledge.
- Once state is known, POPULATED gets one compact found-services line and MID-WORK
  leads with the in-progress work.
- **Consent before provisioning** — nothing mutates from the bare phrase, and the
  branch pick is NOT provisioning consent. The playbook adds exactly ONE consent rule
  on top of the standard recipe flow: before running `zerops_import`, the agent tells
  the person what the returned recipe plan will create — sourced from the workflow's
  own confirm guidance, never from playbook-authored copy — and gets one plain yes.
  Everything else (plan confirm, EXISTS/collision handling, dev-only narrowing, URLs,
  failure recovery) is owned by the guidance the workflow responses return; the
  playbook never restates it. This is the MINIMAL-INSTRUCTION principle: the playbook
  routes into the standard machinery and adds only what a first-time conversation
  needs — every rule duplicated from the machinery is a chance to contradict it.
  Structurally: no `action="start"`, `zerops_import`, or `zerops_deploy` directive may
  appear in the playbook before its Branches section.
- The playbook never re-styles next-step suggestions that the typed Plan already owns
  (`planIdle` primary/alternatives); it funnels into them.

## 4. Branch playbooks (O4)

Both recipe branches resolve the scaffold through the **authored language→slug mapping**
in the playbook (post-remap corpus slugs; the matcher is never the resolver here; a raw
language is never passed as bootstrap intent): Node.js→`nodejs-hello-world`,
Python→`python-hello-world`, PHP→`php-hello-world`, Laravel→`laravel-minimal`,
Go→`go-hello-world`, Rust→`rust-hello-world`, Bun→`bun-hello-world`,
Deno→`deno-hello-world`, Ruby→`ruby-hello-world`, Java→`java-hello-world`,
.NET→`dotnet-hello-world`, Gleam→`gleam-hello-world`, NestJS→`nestjs-minimal`; no
preference→`nodejs-hello-world`. Every mapped slug must resolve in the embedded corpus
with a non-empty import YAML (guard test).

After the route commit the playbook DEFERS: every step's response carries the guidance
that owns plan confirm, import, URLs, and recovery. The playbook's only per-branch
additions:

- **Build something** — resolve the slug from the mapping, run the standard recipe
  route with the one-plain-yes rule (§3); the handoff URL is the STAGE service's URL
  exactly as the workflow response reports it (structured runtime URLs, RCO-7; the dev
  service idles by design) — never hand-composed; then the normal develop loop
  (standard routing / guided) owns building the idea.
- **Try a ready-made recipe** — same route and consent; after the URL handoff, offer
  ownership: wire delivery to the person's own Git repository (`git-push-setup`;
  GIT_TOKEN is a user-held secret the agent never fabricates) or export the setup;
  share the GUI page link exactly as the workflow guidance surfaces it — NEVER
  composed from the corpus slug (corpus slugs can differ from GUI slugs via sync
  remap).
- **What are Zerops & ZCP?** — fetch `zerops_knowledge uri="zerops://playbooks/orientation"`
  once; explain at the person's altitude; mutate nothing; close by re-offering the two
  active options and the plain-words escape.
- **Freeform bring lane** (behind the escape line, not a menu slot) — source in this
  workspace or a Git repository the workspace can reach; laptop-only code gets the
  truthful bridge "push it to a Git repository and I'll take it from there";
  credentials are user-owned — the agent never fabricates repo access.

## 5. Content home (O5)

- Both playbooks live in the **committed, repo-owned knowledge family**
  `internal/knowledge/playbooks/` (embed + `knowledgeDirs`), URIs
  `zerops://playbooks/onboarding` and `zerops://playbooks/orientation`. The family is
  NOT Strapi-synced: sync pull/push never touches it; the docs embed even in an unsynced
  checkout (tests in `internal/tools`, outside the corpus guard).
- The family is **direct-fetch-only**: excluded from `Store.Search` at the single
  all-docs scoring loop, so playbook copy never competes with recipes/guides in `query=`
  hits. `Store.Get` (and thus `uri=` fetch) stays family-blind. A playbook is reachable
  by explicit URI only — any surface that depends on one must name its URI.
- All three artifacts sit under the agent-facing content lints: bare-`zerops://` is
  forbidden (tool-call form only) and no hardcoded service versions.
- The orientation document teaches: the three concepts, what a `*.zerops.app` subdomain
  URL is, the dev/stage pattern (stage serves), what ZCP is, and the consent boundary —
  destructive/irreversible acts and user-held credentials require an explicit yes; it
  must NOT claim every deployment needs approval (a corrective `zerops_deploy` is
  routine and ungated — CLAUDE.md trap).

## 6. Guided + authoring interplay (O6)

- **Guided ON**: the onboarding fork still runs first — the bare phrase carries no
  product intent for guided inference. Once the person chooses **Build something** (or a
  freeform build/bring), guided's always-on contract owns the work, exactly once.
  Choosing **Try a ready-made recipe** is a provisioning act (the standard bootstrap
  recipe flow), not a build/change request — it runs the same way with guided on or off;
  any subsequent change to the provisioned recipe is a build request and enters guided
  as usual. The orientation branch stays knowledge-only.
- **Authoring**: mutually exclusive, same as guided — the block never renders under
  `ZCP_AUTHORING`; onboarding is an end-user surface.
- **Anti-scope**: no NEW onboarding-specific tool, action, or persisted state. The
  recipe-conductor amendments the branches rely on (RCO-6/RCO-7) amend the existing
  bootstrap workflow contract in spec-workflows.md §8 — they are not onboarding surface.

## 7. Invariants (pinned)

| # | Invariant | Pinned by |
|---|---|---|
| O1 | Trigger block renders iff `!Authoring`, both envs, guided-independent, ordered before the routing table; trigger copy carries exact phrase + variant + negative rule + fetch directive | `TestBuildAgentsMD_OnboardingGate_UserOnly`, `TestBuildAgentsMD_OnboardingFirst_BeforeRouting`, `TestBuildAgentsMD_OnboardingTriggerCopy`, `TestRefreshAgentContext_OnboardingPreserved` |
| O2 | Playbook opens tool-call-free; on-demand state resolution is status-first, classified from discover only; fresh rule = all non-system rows `zcp-self`, no activity/warnings; demand-driven ordering greet→choice→status→discover→consent | `TestPlaybookOnboarding_ContentPins_CoreContract` (ordering + wording pins) |
| O3 | Menu block verbatim (full bullet lines + escape line pinned, not labels alone; Build-something example is the imperative "create a weather dashboard in Bun"); **Continue this project** post-state only; ONE consent rule — show the returned recipe plan, get one plain yes before `zerops_import` (everything else deferred to workflow guidance, minimal-instruction principle); no mutating directive before the Branches section | `TestPlaybookOnboarding_ContentPins_CoreContract` |
| O4 | Mapping slugs resolve in the embedded corpus with non-empty import YAML; stage URL handed over exactly as the workflow reports it, never composed; ownership offer (git-push-setup/export + surfaced GUI link, never composed from the corpus slug); orientation branch fetches `zerops://playbooks/orientation` only; playbook defers to workflow guidance after the route commit | `TestPlaybookOnboarding_ContentPins_CoreContract`, `TestPlaybookMapping_SlugsResolveInCorpus`, `TestPlaybookOrientation_ContentPins_CoreContract` |
| O5 | Both playbooks fetchable by URI in an unsynced checkout; excluded from search; under both content lints | `TestKnowledgeTool_PlaybookURI_FetchesEmbedded` (+ orientation case), `TestSearch_ExcludesPlaybooks_NoHits`, `TestNoBareZeropsURIInAgentContent`, `TestTemplatesContent_NoHardcodedVersions` |
| O6 | No new onboarding-specific tool/action/state (content-only anti-scope; RCO amendments live in spec-workflows §8) | absence — no new tool in `annotations_test.go` |
| O7 | Behavioral coverage: trigger positive/variant/negative, populated (chooses a branch, withholds consent → no commit/import), guided-on scenarios exist and parse; retired v2 labels guarded by the content drift test | `TestOnboardingScenarios_ExistAndParse` + `eval_scenario_drift_test.go` retired-token guard |
