---
description: Procedural playbook for a new Mate's first turn — "Stand up development of the project." — what to tell the person as it goes, zerops_standup's two calls, dev pools, and the fallback when it reports a failure.
---

# Standing up a new Mate

Applies when the person's message is exactly "Stand up development of the project." — what a new Mate's own client sends once, for the person who added it, after they signed you in.

## 1. Keep the person told

You have already told the person what you are about to set up and roughly how long it takes, as AGENTS.md says. Call `zerops_standup` next, before the empty-mount adopt advice in AGENTS.md. Then give one short line per step as it goes, the fallback's steps included — what just finished and what comes next, not a line per minute of waiting — and once its first answer names the services, say how many builds are left.

## 2. The first call

It reads the project's recipe from HQ, adopts every dev/stage pair, checks the repository's `main` out onto your branch in each dev half, deploys the dev halves, and answers once they stand, with every service's next step and the stages queued. A part whose dev half is its dev pool (a Swarm pool named like its stage, `apipool` beside `apistage`) gets its pool's first image of `main` with the dev halves, and its stage is deployed from a lease of the pool: the pool's and the stage's next steps name the lease of the pool on `main`, the stage's deploy from it and the part's dev servers in it. Take the lease and start its dev servers; once running and verified, tell the person that dev copy's Preview is up with its address, then deploy the part's stage from that lease as its next step says.

Then do what it leaves you: start each remaining dev half's dev server with `zerops_dev_server` (no dev command is recorded anywhere zcp reads — take it from the repository), verify, and tell the person each dev copy's Preview is up, with its address.

## 3. The second call

Then call `zerops_standup` again in the same turn: it builds the stages, and once they stand, tell the person each part's stage is ready for its Review build. The dev copy's Preview is where they try the work. Nothing is committed, pushed or proposed by it.

## 4. A failure

If it reports a failure, carry on in the same turn with that failure in hand, using the tools you would have used without it: adopt with `zerops_workflow action="start" workflow="bootstrap" route="adopt"`, deploy with `zerops_deploy_batch` (each dev half, then its stage from it), start dev servers with `zerops_dev_server`. It is safe to call again after a fix — it skips what is done.
