# Substep: snapshot-dev (showcase)

This substep completes when every dev target returns `ACTIVE` from a fresh `zerops_deploy setup=dev`, with the feature sub-agent's edits baked into the deployed container image. It runs only for showcase-tier recipes, immediately after the feature sub-agent returns.

## Why re-deploy here

The feature sub-agent wrote code to the SSHFS mount. The currently running dev containers still hold the pre-feature image. The feature-sweep-dev and browser-walk-dev substeps that follow exercise the deployed state, not the mount — a snapshot-dev re-deploy is what transfers the sub-agent's work into the live runtime.

## Dispatch shape

For clusters of two or more targets, use the batch call so the N builds run in parallel server-side:

```
zerops_deploy_batch targets=[
  {"targetService": "apidev", "setup": "dev"},
  {"targetService": "appdev", "setup": "dev"},
  {"targetService": "workerdev", "setup": "dev"}
]
```

For a single-target showcase (rare), call `zerops_deploy targetService="appdev" setup="dev"` directly.

## After snapshot-dev returns

A fresh deploy creates fresh containers. Every background process on every target is gone — the primary server (if it was a dev-server-managed process), the asset dev server, any worker consumer. Re-run the `start-processes` substep's shapes against every target before entering `feature-sweep-dev`. A feature-sweep against a container with no asset dev server running will 500 on every HTML route that references a build-pipeline output.

Verify-dev ran once already before the feature sub-agent. After snapshot-dev it runs again implicitly — the feature-sweep-dev substep is the next gate, and it rejects any target not returning 2xx on its declared API surfaces. Treat feature-sweep-dev as the post-snapshot verification.

## Attestation shape

One line per target: the target name and the deploy status (`ACTIVE`). A failed target at snapshot-dev fails the substep and the fix loop returns to the feature sub-agent's scope, not to the platform-deploy scope.
