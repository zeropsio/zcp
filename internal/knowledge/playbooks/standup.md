---
description: Procedural playbook for a new Mate's first turn — "Stand up development of the project." — what to tell the person as it goes, zerops_standup's two calls, dev pools, and the fallback when it reports a failure.
---

# Standing up a new Mate

Applies when the person's message is exactly "Stand up development of the project." — what a new Mate's own client sends once, for the person who added it, after they signed you in.

## 1. Keep the person told

You have already told the person what you are about to set up and roughly how long it takes, as AGENTS.md says. Call `zerops_standup` next, before the empty-mount adopt advice in AGENTS.md. Then give one short line per step as it goes, the fallback's steps included — what just finished and what comes next, not a line per minute of waiting — and once its first answer names the services, say how many builds are left.

## 2. The first call

It reads the project's recipe from HQ, adopts every dev/stage pair, checks the repository's `main` out onto your branch in each dev half, deploys the dev halves, and answers with each service's state and next step. For a part whose dev half is its dev pool (a Swarm pool named like its stage, `apipool` beside `apistage`), take the lease and start and verify its dev servers. The paired stage serves added work; the workflow builds it when a draft is added and reconciles outside merges. Describe work as a draft the person opens in Mate, without an address in chat.

Then do what it leaves you: start each remaining dev half's dev server with `zerops_dev_server` (no dev command is recorded anywhere zcp reads — take it from the repository), and verify. Describe work as a draft the person opens in Mate, without an address in chat.

## 3. The second call

Then call `zerops_standup` again in the same turn: it builds the stages, and once they stand, tell the person the setup is ready. Work is a draft they can open in Mate; adding it starts its build. Nothing is committed, pushed or proposed by it.

## 4. A failure

If it reports a failure, carry on in the same turn with that failure in hand, using the tools you would have used without it: adopt with `zerops_workflow action="start" workflow="bootstrap" route="adopt"`, deploy with `zerops_deploy_batch` (each dev half, then its stage from it), start dev servers with `zerops_dev_server`. It is safe to call again after a fix — it skips what is done.
