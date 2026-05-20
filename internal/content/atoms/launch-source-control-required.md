---
id: launch-source-control-required
priority: 4
phases: [launch-production-active]
title: "Source-control prerequisites — resolve before launch advances"
references-fields: []
---

### Source-control prerequisites — resolve before launch advances

Launch refuses to advance past scope-prompt while any promoted runtime fails the source-control gate. The production project clones from `buildFromGit:`; that URL must point at a repo you own AND match the live origin in `/var/www`, NOT the recipe template the service was bootstrapped from.

**Resolve blockers top-down — one re-call between each step.** The gate re-runs on every re-call and surfaces only the still-failing blockers.

| Blocker ID | What it means | Recovery |
|---|---|---|
| `git-push-unconfigured-<hostname>` | `meta.GitPushState != configured` — no user-owned remote is wired for this service yet. Production cannot build from "whatever happens to be in `.git/config`"; that's how recipe templates accidentally end up as the production source. | `zerops_workflow action="git-push-setup" service="<hostname>"` — collect the user's repo URL + fine-grained PAT + CI integration choice, then re-call launch. |
| `remote-mismatch-<hostname>` | Live `git remote get-url origin` on the push hostname differs from the recorded `meta.RemoteURL`. Could be a manual rewrite of `.git/config`, a recipe-template leftover, or drift between `git-push-setup` and the current container. | Re-run `zerops_workflow action="git-push-setup" service="<hostname>" remoteUrl="<recorded-or-corrected-URL>"` — confirms the remote matches the meta and rewrites `.git/config` to align. |
| `build-integration-recommended-<hostname>` (warn) | `meta.BuildIntegration=none` — stage has no auto-build pipeline. Recommended to set up before promoting so the source pair behaves like production will after launch. Optional — does not block. | Ask the user: configure now (recommended) or skip? On configure: `zerops_workflow action="build-integration" service="<hostname>" integration="actions"` (or `webhook` for GitLab / policy-constrained repos). On skip: re-call launch with `skipBuildIntegration=["<hostname>"]` to acknowledge the choice; subsequent calls will not re-surface the warn. |
| `service-not-bootstrapped` | No `ServiceMeta` exists for the chosen `targetService`. Bootstrap never ran (or the meta got deleted). | `zerops_workflow action="start" workflow="bootstrap" route="adopt"` to adopt the existing services, then re-call launch. |

**Multi-runtime promotion.** When `Promotables` lists more than one runtime, each runtime's blockers appear with its hostname suffix. Resolve them in the order the gate emits — one chained call per step, then re-call launch. The handler is stateless; passing the same accumulated inputs each turn is sufficient.

**Trust boundary.** All chained actions above run with the standing project-scoped `ZCP_API_KEY`. The one-shot launch-window token (`launchKey`) is requested only at `ready-to-launch`, after every source-control blocker is cleared.

**Do not** manually run `git push` from outside ZCP to "satisfy" the gate — `zerops_deploy strategy="git-push"` (chained after `git-push-setup`) is the right call; it commits the live working tree on the dev container, pushes to the configured remote, and stamps the dev meta with the deploy timestamp the gate verifies.

After every blocker clears, re-call:

```
zerops_workflow action="start" workflow="launch-production"
  productionProjectName="<from inputs>"
  targetService="<from inputs>"
  envClassifications=<from inputs>
  [skipBuildIntegration=[...]]  // only when user explicitly opted out
```

The next response advances to `classify-prompt` (envs classification) and onward through the canonical state machine.
