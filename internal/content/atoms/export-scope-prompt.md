---
id: export-scope-prompt
priority: 2
phases: [export-active]
exportStatus: [scope-prompt]
environments: [container]
title: "Pick the runtime service to export"
---
You are at `status="scope-prompt"`. The export workflow needs to know which runtime service to package — `targetService` was not supplied on this call, so the response carries the project's `runtimes` list instead of a bundle.

## Pick a hostname from `runtimes`

The `runtimes` array in the response lists every non-managed (non-infrastructure) hostname in the project. Pick the runtime that owns the source repo + zerops.yaml you want to package; managed services (`db`, `redis`, `valkey`, `mongo`, …) come along automatically as bundle dependencies — they are NOT export targets and do NOT appear in `runtimes`.

For a project with a single runtime, you can skip this prompt on the next call by supplying `targetService` directly. For a multi-runtime project (e.g. `app` + `worker`), the choice of `targetService` decides which repo's `zerops.yaml` and `/var/www` tree the bundle captures.

## Re-call with `targetService`

```
zerops_workflow workflow="export" targetService="<hostname-from-runtimes>"
```

The chosen hostname alone determines which half of a pair is packaged (`appdev` → dev half, `appstage` → stage half) — there is no separate dev/stage choice. The next response is one of `scaffold-required` / `git-push-setup-required` / `classify-prompt` / `validation-failed` / `publish-ready` depending on which preconditions hold for that runtime.
