---
description: Procedural playbook for a Mate's application — observing stage and production, the secrets they need, a release waiting for approval, asking the Chief or the person's other Mates, domains and addresses, and a runtime that serves only the person.
---

# A Mate's application — stage, production and beyond

AGENTS.md carries the rules for every turn: stage and production are read with `zerops_observe` and changed only by a proposal the person approves, and code reaches them only through a merged change a person releases. This playbook holds the rest. The `zerops_propose_*` tools' own descriptions say how each proposal works.

## 1. Stage and production

HQ holds this Mate in an application, whose stage and production are other Zerops projects; this container's token reaches none of them, and the application's other Mates are not readable from here. Observe stage and production through HQ with `zerops_observe`: `action="environments"` lists the ones you may see, `action="status" projectId="<id>"` their services, status and active versions, and `action="logs" projectId="<id>" serviceId="<id>"` a service's recent logs, by the IDs it returns.

The `zerops_*` tools that set or manage act on this Mate's own project only, and a domain or a port in stage or production is the person's to set in Mate. Look there before you provision anything of your own — what you are about to build may already run. Code reaches stage and production only through the repository: a person merges a change and releases it, never from here, and another Mate's work reaches you through `main`.

Inside this project the ZCP workflow owns deploys, including promoting a dev service to its paired stage service with `zerops_deploy sourceService="<dev>" targetService="<stage>"` — that stage service previews this Mate's work, and it is not the application's stage. To the person it is this Mate's preview, never "stage"; once the application's production runs a release, its address is the real app's: Mate tells you that address in a note on your next turn.

## 2. A secret stage or production needs

Ask for one with `zerops_propose_env secret=true`. Every value the app reads — a secret above all — is referenced in its service's zerops.yaml `run.envVariables` (or `build.envVariables`), never read straight from an auto-injected vault value: the release review sees only what zerops.yaml references, so an unreferenced read ships a production that crashes. Before you propose a release whose zerops.yml reads a secret that environment lacks, ask for it first; when a crash or a log there names a missing value, ask for it.

## 3. A release

When HQ says production's effects wait for approval (`production_effect_approval_required`, or a preparation problem named `approval`), that approval is the release itself, and the release creates what production lacks first: there is no separate control to approve in Mate, so propose the release or the creation and never send the person to approve anything on a page.

## 4. Beyond your application

When you need what neither this project nor `zerops_observe` shows — another project's logs, another application's production error, the organization's state — ask the person's Chief, their Mate for the whole organization, when they have one: `zerops_list_mates` names it among their Mates, and you message it with `zerops_message_mate`: the ask, written for the Chief, and the lines that matter as the attachment. Its answer comes back to this conversation as a new message after your turn, so do not wait or poll for it.

## 5. Your person's other Mates

Your person's Mates in their other projects can be listed with `zerops_list_mates` and asked with `zerops_message_mate`; the answer comes back to this conversation as a new message after your turn, so do not wait or poll for it.

## 6. Addresses

The person puts a project on their own domain, turns a service's zerops.app address off and opens a port straight to the internet in Mate's Addresses. Adding a domain there shows the DNS records to add, with copy buttons, and the domain's state until it is live. Send them there; this container cannot do it for stage and production. The DNS facts still hold: on the free shared IPv4 a domain needs both an A record to that IPv4 and an AAAA record to the project's IPv6; behind Cloudflare, SSL/TLS mode is Full (strict) — Flexible loops redirects.

## 7. What serves only the person

A runtime that looks like it serves the person rather than the app — their cloud agent, their own tooling — leaves the group's Stage and Small Production only at their word: ask them, and record the answer with `zerops_workflow action="group-recipe" mateOnly={"<hostname>": true}` (false carries it in every tier). Never guess.
