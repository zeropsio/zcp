---
id: export-publish-needs-setup
priority: 5
phases: [export-active]
exportStatus: [git-push-setup-required]
environments: [container]
title: "Configure git-push capability before publishing the export bundle"
---
You hit `status="git-push-setup-required"`. Phase C cannot publish until `meta.GitPushState=configured` (and `meta.RemoteURL` is cached). Run the `git-push-setup` action below — it probe-proves the token, provisions GIT_TOKEN, and configures the remote URL the same way the develop workflow does.

## Why this fires

The live `git remote get-url origin` in the chosen source returned empty — no remote is configured, so there is nowhere to push. (A source WITHOUT probe-proven push capability but WITH a live remote does not land here — it lands on `compose-ready`, which hands the bundle over for you to commit yourself.) No bundle is composed on this path; the remote must exist first.

## Resolve in two steps

### 1. Run `git-push-setup`

```
zerops_workflow action="git-push-setup" service="{targetHostname}" remoteUrl="{repoUrl}"
```

If `GIT_TOKEN` is not yet set on the runtime container, the response is the walkthrough atom — run the steps it lists (set the token via `zerops_env action="set" project=true variables=["GIT_TOKEN={token}"]`, push once to confirm), then re-call with the same `remoteUrl` to stamp `GitPushState=configured`.

`git-push-setup` confirm mode validates URL format and writes `meta.GitPushState=configured` + `meta.RemoteURL`, but it does NOT verify that `GIT_TOKEN` actually authenticates against the remote. A subsequent push (during export Phase C, or any later `zerops_deploy strategy="git-push"`) can still surface `failureClassification.category=credential` if the token is rejected — re-run `git-push-setup` to rotate the token and try again.

<!-- axis-k-keep: signal-#3 -->
The walkthrough returned by `git-push-setup` is selected by the current ZCP runtime environment, not the chosen service's mode. If you are running zcp inside a Zerops container, you get the container walkthrough; if you are running locally, you get the local walkthrough. For a runtime that lives on the local machine (`mode=local-stage` / `mode=local-only`), invoke `git-push-setup` from a local zcp invocation so the local walkthrough fires.

### 2. Re-call export with the same inputs

```
zerops_workflow workflow="export" \
  targetService="{targetHostname}" \
  envClassifications=<your map: each project env mapped to its bucket>
```

The handler re-runs Phase A → Phase B with the same inputs, re-checks `meta.GitPushState`, and SHOULD land at `status="publish-ready"` if no other prereq changed. If state moved in the meantime (new envs added to the project, `zerops.yaml` removed, scaling change), the response may instead be `scaffold-required`, `classify-prompt`, or another chain. Read the new `status` and `nextSteps` and re-supply the same inputs (re-classify any new envs surfaced in the prompt) — never assume the second call publishes.

The bundle is composed AFTER the prereq resolves — re-call export once the remote is wired and review the fresh `bundle.importYaml` before writing.

## What if the remote URL has changed

`meta.RemoteURL` is cached when `git-push-setup` confirm mode runs (`zerops_workflow action="git-push-setup"` with `remoteUrl=<url>` writes the cache). If `git remote get-url origin` now returns a different URL than `meta.RemoteURL`, run `git-push-setup` again with the corrected `remoteUrl=` — that overwrites the cache with the new value. The export workflow always reads the live remote (not the cache), so after the cache is fixed both sources agree and the publish step unblocks. The export handler also refreshes `meta.RemoteURL` from the live remote on every pass (and surfaces a warning when they diverged) — so a manual `git-push-setup` re-run is reserved for intentional remote-URL changes, not ordinary cache drift.

## What if you cannot resolve the prereq

If the runtime is intentionally pull-only (no push capability) and you still want to export the bundle for review, the workflow does not yet support a "compose-only / no-publish" mode. The Phase A + Phase B body is in the current response (`bundle.importYaml`, `bundle.zeropsYaml`) — you may copy those bodies out manually for review, BUT the bundle is a snapshot of the project's state at the moment this response was generated. If you act on it later (e.g. paste into a new project's repo days later), the snapshot may have drifted from live state (new envs, scaling, schema changes). Always re-run export immediately before manual extraction; do not act on a stored copy.
