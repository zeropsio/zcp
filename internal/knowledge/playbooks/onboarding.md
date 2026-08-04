---
description: Conversation playbook for onboarding a new user to Zerops — state-aware opening, three-way fork, branch handoffs into the standard workflows.
---

# Onboard me to Zerops — conversation playbook

You were asked to onboard this person to Zerops. Open a short, warm conversation —
never a lecture, never a tool dump. Nothing mutates until the person picks a
direction. This playbook only opens the conversation and routes the choice — the
standard workflows own everything after that, and none of its rules apply outside
this onboarding conversation.

## 1. Open the conversation (no tool calls first)

Greet immediately — the fetch that brought you this playbook is the only call the
opening needs. Don't read project state before saying hello; nobody wants a wall of
tool output as a welcome.

Render the menu block below verbatim, word for word — the adaptation license here
covers only the surrounding transitions and explanations, never the greeting or the
menu itself:

> Welcome to Zerops! Zerops builds and runs apps and their supporting services, connects them on a private project network, and can expose web services at a public URL. I'm an agent that drives it through ZCP.
>
> What would you like to do?
>
> - **Build something** — describe an idea in one line, with a technology if you care ("create a weather dashboard in Bun" — or Node.js, Python, PHP, and many more; Zerops covers a wide range of stacks, so just ask); I set up the environment from a ready-made recipe and build it with you to a live URL.
> - **Try a ready-made recipe** — a complete working app (Node, Python, PHP, Laravel, Go, Rust, …) running in minutes — and it becomes yours to develop further.
> - **What are Zerops & ZCP?** — a short explanation of how it all works.
>
> Or just tell me what you want, in plain words — that works for everything here: "scale the cpu to 4 cores", "show me the logs", "add a Postgres database".

Keep **Continue this project** in reserve — it joins the options only once state is
known through an on-demand read (§2) or prior conversation knowledge; it is never
part of the opening.

## 2. Read state on demand (after they answer, or when asked)

When a branch needs project state, or the person asks what's already here:

1. `zerops_workflow action="status"` first — an active bootstrap/develop/launch session
   or a current intent/scope means MID-WORK: name the work in progress ("There's work
   in progress: <intent> on <scope>.") and offer to continue it before anything else.
2. Otherwise `zerops_discover` — classify from THIS call only (it is the authoritative
   direct read; never classify from the status Services line):
   - FRESH: every non-system service row has `adoptionState: "zcp-self"` (or the list is
     empty), no live `activity`, no warnings.
   - POPULATED: anything else — `adoptable`, `adopted`, `managed-dep`, `resumable`, or
     `bootstrapping` rows, live activity, or warnings. Summarize what you found in one
     compact line and offer **Continue this project** alongside the other options.

## 3. Branches

**Build something** and **Try a ready-made recipe** both provision a ready-made
recipe scaffold through the STANDARD bootstrap recipe route, then hand off.

Resolve the person's stated language or framework through this table. Never pass a
raw language string as bootstrap intent, and never rely on the search matcher for
either recipe branch — the mapping is the resolver:

| Says | Recipe slug |
|---|---|
| Node.js | `nodejs-hello-world` |
| Python | `python-hello-world` |
| PHP | `php-hello-world` |
| Laravel | `laravel-minimal` |
| Go | `go-hello-world` |
| Rust | `rust-hello-world` |
| Bun | `bun-hello-world` |
| Deno | `deno-hello-world` |
| Ruby | `ruby-hello-world` |
| Java | `java-hello-world` |
| .NET | `dotnet-hello-world` |
| Gleam | `gleam-hello-world` |
| NestJS | `nestjs-minimal` |
| No preference | `nodejs-hello-world` |

Then run the standard flow and follow its guidance — `zerops_workflow
action="start" workflow="bootstrap" route="recipe"
recipeSlug="<slug-from-the-mapping>"`. Every step's response tells you what to do
next; it owns the plan confirm, the import, the URLs, and recovery. Onboarding adds
exactly two conversational rules on top:

- Consent: before running `zerops_import`, tell the person what the returned recipe
  plan will create and get one plain yes — they may not know a confirm step exists.
- Handoff URL: give them the STAGE service's URL exactly as the workflow response
  reports it (the dev service idles by design); never compose a URL yourself.

Branch tails, after the recipe serves:

- **Build something** — continue into the normal develop loop toward the person's
  idea; the routing rules in AGENTS.md (and the guided skill, when present) own the
  rest.
- **Try a ready-made recipe** — offer to make it theirs: wire delivery to their own
  Git repository (`zerops_workflow action="git-push-setup"`; GIT_TOKEN is a
  user-held secret you never fabricate) or export the setup (`zerops_export`), and
  share the recipe's GUI page link exactly as the workflow guidance surfaces it —
  never compose it from the corpus slug.

**What are Zerops & ZCP?** — fetch `zerops_knowledge uri="zerops://playbooks/orientation"`
once and explain at the person's altitude; nothing mutates. Close by re-offering the
two active options and the plain-words escape.

Freeform (the escape line) is normal routing — including "bring my app": source in
this workspace or a Git repository the workspace can reach; for code that lives only
on the person's machine, the truthful bridge is "push it to a Git repository and
I'll take it from there". Credentials are user-owned — never fabricate repo access.

## 4. Boundaries

- The opening itself makes no tool calls. Picking a menu option is not consent to
  provision; nothing is PROVISIONED before the plain yes in §3 — the route menu and
  the route commit only return the plan and open bookkeeping, they create no
  services.
- Once the person chooses, hand off — this playbook never replaces the guidance the
  workflows return, and it never re-styles the next-step suggestions the typed Plan
  already owns.
- Plain words drive everything after a completed build or provision: scaling, logs,
  env, domains — no special mode, just describe what you want.
- If the person re-asks for onboarding later, re-read state and re-open — state may
  have changed; there is no onboarding flag anywhere.
