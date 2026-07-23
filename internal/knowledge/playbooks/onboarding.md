---
description: Conversation playbook for onboarding a new user to Zerops — state-aware opening, three-way fork, branch handoffs into the standard workflows.
---

# Onboard me to Zerops — conversation playbook

You were asked to onboard this person to Zerops. Open a short, warm conversation —
never a lecture, never a tool dump. This playbook tells you how to read the project
state, what to offer, and where each choice hands off. Nothing mutates until the
person picks a direction.

## 1. Read the state (before you greet)

1. `zerops_workflow action="status"` — if it reports an active bootstrap/develop/launch
   session or a current intent/scope, the project is MID-WORK: greet, name the work in
   progress ("There's work in progress: <intent> on <scope>."), and offer to continue it
   before anything else.
2. Otherwise `zerops_discover` — classify from THIS call only (it is the authoritative
   direct read; never classify from the status Services line):
   - FRESH: every non-system service row has `adoptionState: "zcp-self"` (or the list is
     empty), no live `activity`, no warnings.
   - POPULATED: anything else — `adoptable`, `adopted`, `managed-dep`, `resumable`, or
     `bootstrapping` rows, live activity, or warnings.

## 2. Open the conversation

FRESH project:

> Welcome to Zerops. What would you like to do?
>
> - **Bring an app** — use source in this workspace or a Git repository, including an
>   app that currently runs elsewhere.
> - **Start something new** — try a ready-made demo or build an idea together.
> - **Take a quick tour** — understand the platform before we change anything.
>
> Or tell me the outcome you want.

POPULATED project: lead with one compact line about what you found ("I found <compact
service summary> in this project."), then the same three options with **Continue this
project** prepended. MID-WORK: lead with the in-progress work instead.

Keep the bold option labels verbatim (plus Continue this project when present) and
the freeform escape line; adapt only the surrounding phrasing to the medium.

## 3. Branches

### Bring an app

Read-only first: scan the workspace (manifests, lockfiles, zerops.yaml, git remotes).
If the source location is still ambiguous, ask exactly one question:

> Where should I get the app's source?
> - This workspace
> - A Git repository you can give me access to
> - I only have the running deployment, not the source

Lanes:
- This workspace: enter the standard flow — `zerops_workflow action="start"
  workflow="bootstrap"` returns the route menu (adopt/classic per the menu); then
  develop, then deploy. The routing rules in AGENTS.md own the rest.
- A Git repository: get the source into the workspace (clone), then the workspace lane.
  Never fabricate credentials — for a private repo, ask the person how they want to
  grant access. Git-push delivery configuration comes AFTER the first successful
  deploy, not before.
- Only a running deployment: say plainly that you need the source — "I can't
  reconstruct an app from a running deployment. If the source is unavailable, I can
  help you build a replacement." — and offer Start something new.
- Data or DNS involved: "Let's get the application running first. Database transfer
  and DNS cutover are separate follow-ups; I won't move either automatically."

### Start something new

Ask one thing: a ready-made demo, or an idea to build (one sentence).
- Demo: `zerops_workflow action="start" workflow="bootstrap" intent="..."` — the route
  menu surfaces matching recipes read-only, no session opens. Present the surfaced
  recipe and get an explicit yes BEFORE committing with `zerops_workflow action="start"
  workflow="bootstrap" route="recipe" recipeSlug="<value from routeOptions>"` —
  provisioning creates real services. Then follow the bootstrap steps through to a
  running URL.
- Idea: a normal build request — the routing rules in AGENTS.md (and the guided skill,
  when present) own it from here.

### Take a quick tour

Fetch `zerops_knowledge uri="zerops://themes/model"` once. Explain, at the person's
altitude, exactly three ideas: a project contains services and services run in
containers; services share a private network and reach each other by hostname; source
is built, then deployed, then run by a service. Connect them to what status/discover
showed. Do not recite pricing, YAML fields, limits, or the full reference. Finish with:
"Want to see those pieces in this project, or set up a small demo together?"

## 4. Boundaries

- Nothing mutates before an explicit choice; the route-menu call is the only
  pre-consent bootstrap call (it opens no session).
- Once the person chooses, hand off — this playbook never replaces the develop/
  bootstrap guidance that those workflows return.
- If the person re-asks for onboarding later, re-read state and re-open — state may
  have changed; there is no onboarding flag anywhere.
