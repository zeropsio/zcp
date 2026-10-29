---
id: develop-git-push-delivery
priority: 2
phases: [develop-active]
gitPushStates: [configured]
closeDeployModes: [auto, unset]
modes: [standard, simple, local-stage, local-only]
stageScope: [in-scope, none]
deployStates: [deployed]
multiService: aggregate
title: "Delivery = commit + git push — the repo is the source of truth"
references-atoms: [develop-git-push-broken]
pointer-atoms: [develop-git-push-delivery-results]
---
Git push is configured for this service (`gitPush=configured`), so the repo is the source of truth and delivery happens by pushing to it. Development work ends with a push, not a redeploy — code that reached the remote persists across container replacement, which is what made redeploy-for-persistence necessary in the first place. Where an integration consumes the pushes (`buildIntegration=webhook|actions`), direct self/cross `zerops_deploy` calls on this pair answer `push-delivery-required` with the recommended push call instead of deploying; on `buildIntegration=none` nothing rebuilds from the repo, so those calls deploy and flag that the service now runs code the remote does not carry.

**Push source vs build target.** For a standard pair, the push originates from the DEV half (push source) and the build lands on the STAGE half (build target). For simple / single-runtime modes, push source equals build target. <!-- axis-k-keep: signal #1 names topology roles (push source vs build target) load-bearing for the iteration cycle -->
<!-- axis-l-keep -->
<!-- axis-m-keep -->
<!-- axis-n-keep -->

## Ship the work

```
{services-list:zerops_deploy targetService="{hostname}" setup="<source-setup>" strategy="git-push"}
```

`targetService` is the PUSH SOURCE hostname (dev half of a standard pair, or the service itself for simple modes). Leave `branch` unset: it defaults to `main`. On this Mate's HQ the call does not run: code goes there by plain git on a branch of your own, and `main` moves only when a person merges a change. The call refuses an empty tree or uncommitted changes — commit first (`zcp git <host> add -A && zcp git <host> commit -m '<msg>'` in a container, which runs git in the service's checkout; `git -C <workingDir> add -A && git -C <workingDir> commit -m '<msg>'` on a dev machine). It pushes HEAD to the configured remote and then follows the integration build on the build target until it settles.

**Setup parameter:** `setup=<name>` MUST match a `setup:` block name in the project's `zerops.yaml` (the push pre-flight validates the SOURCE setup; the remote build consumes its own setup name — for standard pairs typically `prod`). Read the project's `zerops.yaml` first and pass the matching name explicitly; an `INVALID_ZEROPS_YML` error response carries `attemptedSetup` + `availableSetups` for a one-round-trip recovery.

If the push fails with a credential cause, the token was rotated or revoked upstream — ask the user for a fresh token and re-run `zerops_workflow action="git-push-setup" service="{hostname}" remoteUrl="..." gitToken="<fresh PAT>"`. Never invent or reuse a token the user didn't supply.

A push rejected because the remote carries commits yours doesn't returns `GIT_PUSH_NON_FAST_FORWARD` with a `next` block naming exactly three options (rebase / merge / replace-remote) and their exact commands — that decision belongs to the user; never run `git push --force` or merge on their behalf without asking first. <!-- axis-k-keep: GF-11 — the whole point of this guardrail is that zcp never auto-resolves a non-fast-forward rejection -->
