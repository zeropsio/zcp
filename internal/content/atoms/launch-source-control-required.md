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
| `git-push-unconfigured-<hostname>` | `meta.GitPushState != configured` — no probe-proven remote is wired for this service yet. Production cannot build from "whatever happens to be in the bootstrapped git config"; that's how recipe templates accidentally end up as the production source. | `zerops_workflow action="git-push-setup" service="<hostname>" remoteUrl="<url>" gitToken="<PAT>"` (container) or `... remoteUrl="<url>"` (local). The handler probes the remote BEFORE writing project state. Then re-call launch. |
| `remote-mismatch-<hostname>` | Live `git remote get-url origin` differs from the recorded `meta.RemoteURL`. Could be a manual rewrite, a recipe-template leftover, or drift since last setup. | Re-run `zerops_workflow action="git-push-setup" service="<hostname>" remoteUrl="<corrected-URL>" gitToken="<PAT>"` — the handler probes the new URL and syncs origin on success. Then re-call launch. |
| `dev-tree-dirty-<hostname>` | `git status --porcelain` on the dev push source is non-empty — uncommitted / staged / untracked changes. Those changes will NOT make it to production (Zerops clones the remote's HEAD; git push only pushes commits). The deploy tool refuses to push a dirty tree; the commit step is yours. | Commit the working tree first, then push: `ssh <hostname> "cd /var/www && git add -A && git commit -m '<msg>'"` (container) or `git -C <workingDir> add -A && git -C <workingDir> commit -m '<msg>'` (local). Then `zerops_deploy targetService="<hostname>" strategy="git-push"`. Then re-call launch. |
| `head-not-pushed-<hostname>` | Local HEAD on the push source does not match the remote HEAD (or remote HEAD unreachable). Local commits are ahead of the configured remote; production would build stale code. | `zerops_deploy targetService="<hostname>" strategy="git-push"` pushes the existing commits. If HEAD is reachable on the remote but the SHAs differ, you have unpushed local commits — `git log --oneline origin/HEAD..HEAD` shows them. Then re-call launch. |
| `build-integration-recommended-<hostname>` (warn) | `meta.BuildIntegration=none` — stage has no auto-build pipeline. Recommended to set up before promoting so the source pair behaves like production will after launch. Optional — does not block. | Ask the user: configure now (recommended) or skip? On configure: `zerops_workflow action="build-integration" service="<hostname>" integration="actions"` (or `webhook` for GitLab / policy-constrained repos). On skip: re-call launch with `skipBuildIntegration=["<hostname>"]` to acknowledge the choice; subsequent calls will not re-surface the warn. |
| `service-not-bootstrapped` | No `ServiceMeta` exists for the chosen `targetService`. Bootstrap never ran (or the meta got deleted). | `zerops_workflow action="start" workflow="bootstrap" route="adopt"` to adopt the existing services, then re-call launch. |

**Multi-runtime promotion.** When `Promotables` lists more than one runtime, each runtime's blockers appear with its hostname suffix. Resolve them in the order the gate emits — one chained call per step, then re-call launch. The handler is stateless; passing the same accumulated inputs each turn is sufficient.

**Trust boundary.** All chained actions above run with the standing project-scoped `ZCP_API_KEY`. The one-shot launch-window token (`launchKey`) is requested only at `ready-to-launch`, after every source-control blocker is cleared.

**Prefer the orchestrated flow.** `git-push-setup` probes auth before writing project state; `zerops_deploy strategy="git-push"` pushes already-committed code via the project-level `GIT_TOKEN`. Running `git push` directly from outside this flow bypasses the gate's source-of-truth checks (meta.RemoteURL vs live origin) — the next launch re-call may still surface `remote-mismatch` until `git-push-setup` re-syncs the meta.

After every blocker clears, re-call:

```
zerops_workflow action="start" workflow="launch-production"
  productionProjectName="<from inputs>"
  targetService="<from inputs>"
  envClassifications=<from inputs>
  [skipBuildIntegration=[...]]  // only when user explicitly opted out
```

The next response advances to `classify-prompt` (envs classification) and onward through the canonical state machine.
