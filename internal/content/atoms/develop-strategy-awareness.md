---
id: develop-strategy-awareness
priority: 5
phases: [develop-active]
closeDeployModes: [auto, manual]
multiService: aggregate
title: "Deploy config — recorded dimensions + how delivery derives"
references-fields: [workflow.ServiceSnapshot.CloseDeployMode, workflow.ServiceSnapshot.GitPushState, workflow.ServiceSnapshot.BuildIntegration]
---

### Deploy config — recorded dimensions + how delivery derives

Each runtime service records three deploy-config dimensions — the
rendered Services block shows them as
`closeMode=auto|manual|unset gitPush=unconfigured|configured|broken buildIntegration=none|webhook|actions`:

- `closeMode` — who owns "done". `auto` lets the work session close
  itself once every in-scope service has a successful deploy + passing
  verify; `manual` yields close decisions to you / external
  orchestration. `unset` is the bootstrap-written placeholder that
  develop converts on first use.
- `gitPush` — capability state for push delivery. `configured`
  means the last `git-push-setup` probe **proved end-to-end auth**: the
  supplied token authenticates against the remote URL, the push source carries
  `GIT_TOKEN` (sensitive), and the working tree's git config has its
  `origin` synced. `broken` means a previously-configured token stopped
  working (e.g. PAT rotation) — re-run setup before pushing.
- `buildIntegration` — the ZCP-managed CI shape that was picked. `actions`
  (GitHub Actions workflow + secrets), `webhook` (Zerops dashboard OAuth),
  or `none`. Requires `gitPush=configured`. The flag records the choice
  and the handoff shape (workflow YAML body / dashboard URL); workflow
  commit, secrets landing, and OAuth completion happen outside ZCP's
  reach and are not verified by this flag. Treat as "this is the
  integration shape we wired", not "the build trigger is confirmed live".

**The delivery mechanism is DERIVED, not chosen separately:** when
`gitPush=configured` (and closeMode is not `manual`), delivery is
commit + git push — direct `zerops_deploy` self/cross calls on that pair
answer `push-delivery-required` with the recommended push call. Otherwise
delivery is the direct `zerops_deploy` path. Want push delivery? Configure
the capability; there is no separate mode switch.

Change any dimension without closing the session:

- `close-mode` is **per-pair** under the hood (one record per dev/stage pair) but accepts a multi-entry map: one call sets close-mode for any subset of services. Passing both halves of a pair with the SAME value is accepted (canonical write once). Passing both halves with DIFFERENT values is rejected with an explicit conflict diagnostic — pick one value for the pair.
- `git-push-setup` and `build-integration` are **per-pair**: capability is stamped on the dev half's record and shared by both halves. `git-push-setup` rejects stage-half input with `INVALID_PARAMETER` (it mutates push-side state — would write to the wrong target). `build-integration` is permissive: pair-keyed lookup resolves either half to the dev record and the response carries `pushSource`/`buildTarget`/`topologyNote` so the redirect is visible. Either way: prefer passing the dev half directly.

```
zerops_workflow action="close-mode" closeMode={"{hostname}":"auto"}
zerops_workflow action="git-push-setup" service="{hostname}" remoteUrl="..."
zerops_workflow action="build-integration" service="{hostname}" integration="actions"
```

Substitute `{hostname}` with the dev-half hostname (or single-runtime hostname). For a multi-service project, repeat each call once per dev-half service — never per stage-half.

Mixed config across services in one project is fine — each service's dimensions are independent in the envelope.
