# Slice 3 — Origin + GitHost adapters (BREAKING migration, part 1 of 2)

Base: `feat/git-foundation` after slices 1, 2a. Needs owner ack (vocabulary is user-facing).

## Replace
`GitPushState` + `git-push-setup` + `deploy strategy=git-push` + `DeliveryState` +
`RecommendDelivery` → `Origin` + `zerops_workflow action=origin`.
`BuildIntegration` + `build-integration` + close-mode delivery derivation are slice 4.

## Vocabulary (topology/, stdlib only)
- `OriginKind none|managed|external`; `Origin{Kind, URL (canonical), Host GitHostKind}`.
- `GitHostKind github|gitlab|gitea|generic` (extend `git_pat.go` classifier; gitea =
  `/api/v1/version` answers, generic = ls-remote only).
- `GitHostCapabilities{CreateRepo, Token, PlatformPull, Webhook}` per kind — a pure
  table: github/gitlab all true; gitea CreateRepo/Token/Webhook true, PlatformPull
  false; generic all false.
- Meta: `ServiceMeta.Origin` replaces `GitPushState`+`RemoteURL`; migration on load:
  `configured` + RemoteURL → `external`, else `none` (fold in `service_meta.go` load,
  same place as the legacy git-push close-mode fold). `broken` → `external` + a live
  probe result in the envelope (`origin.reachable=false`), never a stored state.

## Tool surface
- `action=origin` inputs: `service`, `kind` (`none|external`; `managed` refused until
  slice 7 with a clear message), `url`, `token` (user-owned; never fabricated —
  `appendCredentialContract` on missing). Behaviour = today's `git-push-setup` body
  (identity, remote set-url, credential store in container, ls-remote probe, stamp)
  minus the "delivery configured" semantics; the push itself moves to close (slice 4).
- `zerops_deploy`: delete `strategy` and `deploy_git_push.go`. A deploy is always a
  push from a commit (slice 1) or today's tree push.
- Envelope: `origin: {kind, host, url(redacted), reachable}` per dev service.

## Delete (follow the chain: every reader in the scout map)
`topology/delivery.go`, `delivery_state.go`, `GitPushState` consts + tests;
`tools/workflow_git_push_setup*.go` (+ container/local/identity/reconstruct/service-token
tests → rewritten as `workflow_origin*_test.go`), `deploy_git_push.go`,
`deploy_local_git.go` git-push parts, `adopt_gitpush_reconcile.go` → `adopt_origin_reconcile.go`
(discover the remote from the appVersion / live `git remote` and stamp `external`),
`launch_source_control_gate.go` checks 1–3 (keep the live-vs-meta URL match as
`origin` reachability), `deploy_repo_delivery.go`/`resolve_build_target.go` delivery
resolution, `ops/bundle/inputs.go` invariant text, `TestResolve_ConfiguredDrivesGitPushDelivery`,
S13 in `scenarios_test.go`, `delivery_ladder_test.go`.
Atoms: `setup-git-push-{container,local}`, `develop-git-push-{delivery,broken}` →
`origin-connect-{container,local}`, `origin-unreachable`; frontmatter axis
`gitPushStates` → `originKinds` (`atom.go` parser + `synthesize.go` predicate +
`atoms_lint`). Re-bless goldens.
Eval: delete `git-push-setup-then-actions.md` + `delivery-git-push-actions-setup.md` +
`preseed/close-mode-git-push-setup.sh`; write G6 `origin-github-track-main.md` as
`pending: scenario` until slice 4 gives `track` (G6 needs env source). Rename the
`gitPushState`/`buildIntegration` fields the preseed scripts plant
(`discover-resumable-orphan-meta.sh`, `subdomain-user-disabled-stays-off.sh`,
`discover-adopted-pair-meta.sh`) to `origin: {kind: none}`. Update
`eval_scenario_drift_test.go` guards. Gate set: remove C1; spec §9.3 row C1 → G6 pending.
Spec: `spec-workflows.md §4.3` rewrite (origin, not delivery), §11.33 git lifecycle;
CLAUDE.md invariant line "Deploy delivery is DERIVED from GitPushState" → "Origin is
where the repo is shared; delivery is per environment (§4.3/§4.10)".
Mate: `../z3 ZeropsPolicy.ts` untouched here (slice 8).

## Tests
Origin migration table (old meta JSON → new), classifier table, capability table,
`action=origin` handler table (none/external/managed-refused/missing token/unreachable),
adopt reconcile table, envelope block, `TestNoGitPushStateReferences` grep guard.
