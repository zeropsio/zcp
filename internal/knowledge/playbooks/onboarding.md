---
description: Conversation playbook for onboarding a new user to Zerops — state-aware opening, three-way fork, branch handoffs into the standard workflows.
---

# Onboard me to Zerops — conversation playbook

You were asked to onboard this person to Zerops. Open a short, warm conversation —
never a lecture, never a tool dump. This playbook tells you how to read the project
state, what to offer, and where each choice hands off. Nothing mutates until the
person picks a direction and — for the two recipe branches — works through the
staged consent in §3.

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
> - **Build something** — describe an idea in one line, with a technology if you care ("a weather dashboard in Bun"); I set up the environment from a ready-made recipe and build it with you to a live URL.
> - **Try a ready-made recipe** — a complete working app (Node, Python, PHP, Laravel, Go, Rust, …) running in minutes — and it becomes yours to develop further.
> - **What are Zerops & ZCP?** — a short explanation before we change anything.
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
recipe scaffold; they share the slug mapping, the staged consent sequence, the three
concepts, and the after-import step below, then diverge in their tails.

### Language → recipe slug mapping

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

### Staged consent (both recipe branches)

Nothing mutates from the bare menu choice — picking **Build something** or **Try a
ready-made recipe** is not consent to provision. Walk this exact order:

1. **Footprint consent** — before any route commit, name what will be created: a dev
   service, a stage service, and (when the recipe has one) a database. Say plainly
   that the stage service is the one that will serve the public URL. Offer dev-only
   narrowing (`recipeNarrow="dev-only"` on the confirm step) only if the person
   explicitly asks for it. Get a yes.
2. **Commit + derive/confirm** — `zerops_workflow action="start" workflow="bootstrap"
   route="recipe" recipeSlug="<slug-from-the-mapping>"`, then follow the
   derive/confirm step the returned guide describes.
3. **Renewed consent on EXISTS** — if confirm flips a managed dependency to EXISTS,
   say plainly that the recipe's boot migration may create tables in, or write into,
   that existing database, and get a fresh yes before provisioning. In a POPULATED
   project (existing non-system services already here), do not propose mixing the
   scaffold in at all until the person deliberately confirms after this disclosure —
   offer **Continue this project** first.
4. **Narrate, then import** — explain the three concepts below, THEN run the
   blocking `zerops_import`. There is no narration while it runs.

### Three concepts (narrate before the import)

1. A project is a private network; each service in it runs in its own containers
   (your app) or is a managed service (your database).
2. Services reach each other by hostname on that private network — the app finds
   its database as `db`, no addresses or credentials in code.
3. Source becomes a running app through build → deploy → run; the subdomain URL is
   the public door to the stage service.

### After import (both recipe branches)

Poll to ACTIVE. The handoff URL is the STAGE service's subdomain URL, taken from
what the bootstrap response reports — never hand-compose one. The dev service idles
by design (`zsc noop`) and answers 502; never present the dev URL as the app. Verify
the URL responds before you present it.

### Build something

Parse the idea, and the technology if the person named one, from their one-liner;
resolve a slug from the mapping above (default `nodejs-hello-world` with no stated
preference). Run the staged consent, then the shared after-import step. Once the URL
is verified, continue into the normal develop loop toward the person's idea — the
routing rules in AGENTS.md (and the guided skill, when present) own the rest.

### Try a ready-made recipe

Resolve a slug from the mapping the same way. Run the staged consent, then the
shared after-import step. Once the URL is verified, offer ownership: wire delivery
to the person's own Git repository (`zerops_workflow action="git-push-setup"`;
GIT_TOKEN is a user-held secret you never fabricate) or export the project setup
(`zerops_export`); link `https://app.zerops.io/recipes/<slug>` for the
human-readable guide. Never promise recipe-specific takeover content beyond what
the corpus actually has.

### What are Zerops & ZCP?

Fetch `zerops_knowledge uri="zerops://playbooks/orientation"` once. Explain it at
the person's altitude, in their words, at their pace. Mutate nothing. Close by
re-offering the two active options — **Build something** and **Try a ready-made
recipe** — plus the plain-words escape.

### Freeform bring lane

Behind the escape line, not a menu slot: the person may name source in this
workspace or a Git repository the workspace can access. Laptop-only code with no
repository gets the truthful bridge: "push it to a Git repository and I'll take it
from there." A running-deployment-only request gets an honest refusal — source is
required, and you can offer **Build something** instead. Data transfer and DNS
cutover are explicitly deferred, never moved automatically. Credentials are
user-owned — never fabricate repo access.

## 4. Failure ending

If provisioning or the build fails, name exactly which services and processes
succeeded and which failed. Do not claim a URL. Do not proceed to the ownership
step. Offer to clean up only the services THIS attempt created, and only after an
explicit yes (`zerops_delete`, one service at a time) — never offer to delete or
rewrite a service that already existed before this attempt. Surface the recovery
the tool reported.

## 5. Boundaries

- The opening itself makes no tool calls; picking a menu option is not consent to
  provision — provisioning starts only after the staged consent sequence in §3.
- Once the person chooses, hand off — this playbook never replaces the develop/
  bootstrap guidance those workflows return.
- Plain words drive everything after a completed build or provision: scaling, logs,
  env, domains — no special mode, just describe what you want.
- If the person re-asks for onboarding later, re-read state and re-open — state may
  have changed; there is no onboarding flag anywhere.
