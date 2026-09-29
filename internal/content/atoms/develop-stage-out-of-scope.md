---
id: develop-stage-out-of-scope
priority: 1
phases: [develop-active, develop-closed-auto]
stageScope: [out-of-scope]
multiService: aggregate
title: "Stage left out of this session — the work ends at dev"
references-atoms: [develop-auto-close-semantics]
---

### The stage stays as it is this session

This develop session leaves the stage half of these pairs out of scope:

{services-list:- `{hostname}` — its stage `{stage-hostname}` stays as it is}

The work runs on the dev half alone: deploy it, start its dev server and
verify it. The session closes once the dev half is deployed and verified.

Leave the stage untouched: no deploy, cross-deploy or promotion onto it, and
no commit or push for it. In a Mate wired to its group's Gitea, a deploy onto
a stage half is a delivery — a commit, a push and a pull request — so it
happens only when the person asks for it.
<!-- axis-k-keep: signal-#1 — the stand-up's whole point is that nothing reaches the stage or the repository unasked -->

A Mate's stand-up is such a session: when a Mate first deploys services it
adopted, zcp leaves their stage out on its own, and the stand-up ends at a
running dev.

When the dev half is verified, end by telling the person what runs on the dev
half, and that promoting it to the stage is the next step they can ask for.
