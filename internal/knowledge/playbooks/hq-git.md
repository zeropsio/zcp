---
description: Procedural playbook for a Mate's repositories in HQ — how git reaches HQ, bringing main up to date, a change built on another, a repository kept on GitHub, and an existing repository coming in as main.
---

# HQ git — a Mate's repositories in HQ

AGENTS.md carries the rules for every turn: every git command through `zcp git <host>` in the dev service, one change per branch, never rebase, never push `main`, never force-push. This playbook holds the rest.

## 1. The repository and how git reaches it

This Mate's code lives in its HQ: each dev/stage pair has a repository there, in the application HQ holds this Mate in, named after its dev hostname, and ZCP wires it; a repository of several parts stays one repository — never a dev service per part (`zerops_knowledge uri="zerops://playbooks/monorepo"`). The pair's origin is `https://<HQ>/git/<organisation>/<application>/<repository>.git`, its checkout on `main`. Core deploys the application's environments from the repository's commits with the keys HQ keeps (§10.8); deploying this Mate's own pair stays a ZCP deploy.

Git signs in to HQ as the user `mate` with this Mate's own credential: nobody mints a token for it and there is no settings page to go looking for. `zcp git <host> <git args>` runs git in the dev service's checkout, and zcp answers git's credential requests there — for HQ with this Mate's credential, for a fetch from a repository on GitHub with the read token HQ mints for that call — never kept anywhere, never in `.git/config`. Git on the mounted repository `/var/www/<host>` and git in the dev service on one `.git` break it. After a `zcp git` command that changes files the mounted folder can show the old state for up to 20 s.

## 2. Bringing `main` up to date

Bring `main` up to date with `zcp git <host> checkout main && zcp git <host> fetch origin && zcp git <host> merge --ff-only origin/main`. A `main` that will not fast-forward holds commits that never landed: keep them with `zcp git <host> branch <name>`, then `zcp git <host> reset --keep origin/main` — unless `origin/main` holds only HQ's initial commit, when that `main` is the repository's first history: keep it as it is and cut your branch from it; merged, its change becomes `main`.

Keep a branch current by merging `main` into it: `zcp git <host> fetch origin && zcp git <host> merge origin/main`.

## 3. Changes

A follow-up continues its open draft on its own branch; status lists each repository's open changes by number and branch. Start an unrelated draft from current `main`, never another draft. Before the next edit of an open draft, fetch and merge `main` into it if `main` moved; verify the draft and push.

Never open a change only to record another's merge or to update a plan, roadmap or log: put it in the change doing the work, or in the next one that has work in it. A file every change appends to — a work log, a diary kept in one file — makes each open change conflict with `main` once another merges: give each change an entry of its own, such as a file per change, or per day and change. An HQ that keeps one open change per repository answers its open one instead, and open-change then says the work goes on that change's branch.

The person reviews a draft by its description: write it with `zerops_workflow action="describe-change"`. Describing it does not build the stage. Add to your app starts the build; if it fails, fix and verify the same draft and push, and zcp builds again automatically. Deploys within this project follow the ZCP workflow's guidance.

## 4. A repository kept on GitHub

When the person wants this code on GitHub — a GitHub copy, GitHub pull requests from its repository, "connect to GitHub" — and its repository has no remote (status says `origin.mode=none` on its dev service), ask with `zerops_mirror action="request" repo="<the dev hostname>" why="<one line>"`: Mate shows them a card where they pick the GitHub account and a repository there, or have a new private one made in the same press (no GitHub repository needs to exist first), and HQ mirrors it from then on.

A repository whose code the person keeps on GitHub — status says `origin.mode=github` on its dev service — is worked on like any other: its checkout's origin is its repository in HQ too, never GitHub, and no second remote names GitHub — a checkout an older ZCP pointed at GitHub is moved back by ZCP, at the Mate's start or develop's start, and until it is, status's `checkoutOrigin` names the origin the checkout has now and says the move is pending. A branch you pushed to GitHub yourself before that move is not carried on there by a push to origin — HQ pushes on only to branches it put on GitHub: finish that work on a new branch pushed to origin, open a new change for it with open-change, and tell the person the earlier pull request is superseded. Push your branch to origin, then open its change with open-change.

HQ alone pushes to GitHub: it mirrors such a repository itself — `main` to GitHub's default branch, each open change's branch, each change's pull request — and a person merges the change in Mate, so a pull request of your own (`gh pr create`) is never where the work ends, and a push of your own to GitHub is refused: `zcp git` answers GitHub only for fetches. Never sync GitHub by hand either: never push its default branch, nor copy a merged change onto it from a clone of your own (a note of yours that says to is out of date once HQ mirrors the repository); a merged change reaches GitHub as its pull request, squash-merged there, and GitHub's default branch moves only by that merge. A pass says once when HQ starts mirroring a repository, and once when GitHub's default branch and HQ's `main` diverge — HQ then moves neither — with the one change that brings them back in step.

## 5. A repository that already exists

A repository that already exists comes into HQ as `main` itself: HQ makes every repository's `main` with one initial commit, and an existing history never goes on top of it. Code kept on GitHub: connect its origin first — `zerops_origin action="request" url="<its GitHub address>" repo="<the dev hostname>" why="<one line>"` — and once the person connects it, HQ takes the repository's `main` from GitHub, so `main` starts as GitHub's `main`; take it into the checkout as the answer says, and never clone it in and merge it. Code from anywhere else — a bundle, another host, a GitHub repository HQ cannot connect — goes in as a branch of its own, exactly as it is: fetch it in the dev service — from an address, `zcp git <host> fetch <address> <its main branch>:<branch>`; a bundle copied in first (`scp <bundle> <host>:/tmp/<name>.bundle`), `zcp git <host> fetch /tmp/<name>.bundle <its main branch>:<branch>` — then `zcp git <host> push origin <branch>`, and open its change from that branch with open-change; merged, HQ makes that history `main`, and the checkout's `main`, which holds only that initial commit, moves onto it (`zcp git <host> fetch origin && zcp git <host> checkout -B main origin/main`). Either way, never merge or rebase it onto `main`'s initial commit, and never merge HQ's initial commit into it. The Mate's own setup — the dev setups in `zerops.yaml`, `.zcp/dev-servers.json` — goes in afterwards as an ordinary change on a branch cut from that `main`; on a repository kept on GitHub that change is a pull request, merged by squash.
