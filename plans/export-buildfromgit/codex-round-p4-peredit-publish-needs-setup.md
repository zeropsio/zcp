# Per-Edit Review: export-publish-needs-setup

## 1. Verdict
NEEDS-REVISION: the atom preserves the basic container chain but wrongly claims local-mode routing/support and is overconfident about the second export call.

## 2. Chain-contract correctness
The primary chain target matches: `export-publish-needs-setup` names `setup-git-push-container` as the chain target (`internal/content/atoms/export-publish-needs-setup.md:8`), and the setup atom's front matter has `id: setup-git-push-container` (`internal/content/atoms/setup-git-push-container.md:2`).

The action call shape is correct: the atom says `zerops_workflow action="git-push-setup" service="{targetHostname}" remoteUrl="{repoUrl}"` (`internal/content/atoms/export-publish-needs-setup.md:18-20`); `WorkflowInput` exposes `Action`, `Service`, and `RemoteURL` with JSON fields `action`, `service`, and `remoteUrl` (`internal/tools/workflow.go:31-43`), and the dispatcher routes `action="git-push-setup"` to `handleGitPushSetup` (`internal/tools/workflow.go:291-294`).

The export Phase C handler does not conditionalize the chain response on container vs local mode: it only checks `meta.GitPushState != topology.GitPushConfigured` and calls `gitPushSetupChainResponse(input.TargetService, bundle, ...)` (`internal/tools/workflow_export.go:162-164`). That response returns an action pointer, not a mode-specific atom ID (`internal/tools/workflow_export.go:273-283`). Therefore the atom's local-mode branch is inaccurate: it says `setup-git-push-local` is the chain target and that the dispatcher routes based on the chosen runtime's mode (`internal/content/atoms/export-publish-needs-setup.md:24-25`). The git-push setup walkthrough uses `workflow.SynthesizeStrategySetup(rt, ...)` (`internal/tools/workflow_git_push_setup.go:97-107`), and `SynthesizeStrategySetup` selects `Environment: DetectEnvironment(rt)` (`internal/workflow/synthesize.go:532-536`), not directly the chosen service's local/container mode.

## 3. Two-step resolve flow
The atom correctly gives a two-step flow: run `git-push-setup` (`internal/content/atoms/export-publish-needs-setup.md:16-22`), then re-call export (`internal/content/atoms/export-publish-needs-setup.md:27-36`). It explicitly carries `targetService`, `variant`, and `envClassifications` in the re-call example (`internal/content/atoms/export-publish-needs-setup.md:29-34`), which matters because the handler is stateless per request (`internal/tools/workflow.go:105-113`) and recomputes classification state from the supplied input (`internal/tools/workflow_export.go:152-159`).

Minor prose risk: line 36 says the re-run "lands at `status=\"publish-ready\"` ... this time" (`internal/content/atoms/export-publish-needs-setup.md:36`). That is too strong because the handler can still return scaffold-required if `zerops.yaml` is absent/empty (`internal/tools/workflow_export.go:111-117`) or classify-prompt if classifications are missing/stale (`internal/tools/workflow_export.go:158-159`).

## 4. Stale RemoteURL section accuracy
`readGitRemoteURL` reads the live origin from `/var/www` via `git remote get-url origin` (`internal/tools/workflow_export_probe.go:41-48`) and returns trimmed stdout (`internal/tools/workflow_export_probe.go:56`); its comment explicitly says cached `ServiceMeta.RemoteURL` is not consulted (`internal/tools/workflow_export_probe.go:41-45`). The atom's "export workflow always reads the live remote (not the cache)" is accurate (`internal/content/atoms/export-publish-needs-setup.md:40`).

Re-running `git-push-setup` with corrected `remoteUrl=` is consistent with confirm mode: the handler validates `input.RemoteURL` (`internal/tools/workflow_git_push_setup.go:121-127`) and overwrites `meta.RemoteURL = input.RemoteURL` while setting `GitPushState=configured` (`internal/tools/workflow_git_push_setup.go:129-130`). The atom's "overwrites the cache with the new value" is accurate (`internal/content/atoms/export-publish-needs-setup.md:40`).

The weak phrasing is "Phase 0 capture cached `meta.RemoteURL` to whatever was live at first config" (`internal/content/atoms/export-publish-needs-setup.md:40`). In the current code, the cache write shown here is `git-push-setup` confirm mode, not export Phase 0 (`internal/tools/workflow_git_push_setup.go:121-130`).

## 5. Compose-only escape evaluation
The escape valve is only safe for immediate review, not for later publish/import. The atom says to manually copy `bundle.importYaml` and `bundle.zeropsYaml` from the response if no-publish is desired (`internal/content/atoms/export-publish-needs-setup.md:42-44`). But the preview is a snapshot included only when the already-built bundle exists (`internal/tools/workflow_export.go:285-287`), and the same atom acknowledges the bundle can differ if project state shifts (`internal/content/atoms/export-publish-needs-setup.md:36`). Acting on stale copied YAML can omit new envs, managed services, or scaling changes; the section should require re-running export immediately before manual copying.

## 6. Axis hygiene
Axis L: no standalone `container` / `local` token appears in the title (`internal/content/atoms/export-publish-needs-setup.md:6`) or headings (`internal/content/atoms/export-publish-needs-setup.md:10,14,16,27,38,42`). The heading `Run setup-git-push-container` contains the atom ID, not a standalone qualifier (`internal/content/atoms/export-publish-needs-setup.md:16`).

Axis K: the marker on line 24 applies to the next non-blank line per marker-window convention (`internal/content/atoms/export-publish-needs-setup.md:24-25`; `internal/content/atoms_lint_axes.go:158-168`). It covers the `local-only` token on line 25 (`internal/content/atoms/export-publish-needs-setup.md:25`), but the covered prose is semantically wrong per Section 2.

Axis M: no bare "the container", "the platform", "the tool", or "the agent" appears in the atom body; those are the linted terminology-drift patterns (`internal/content/atoms_lint_axes.go:179-195`). "runtime container" is canonical-prefixed (`internal/content/atoms/export-publish-needs-setup.md:22`; `internal/content/atoms_lint_axes.go:197-201`).

Axis N: not applicable because the atom is environment-scoped to `[container]` (`internal/content/atoms/export-publish-needs-setup.md:5`), and Axis N runs only on universal atoms (`internal/content/atoms_lint_axes.go:266-277`).

## 7. Front-matter integrity
Front matter matches the requested contract: `priority: 5`, `phases: [export-active]`, and `environments: [container]` (`internal/content/atoms/export-publish-needs-setup.md:3-5`). I found no `gitPushStates` axis in the atom front matter (`internal/content/atoms/export-publish-needs-setup.md:1-7`). That aligns with the handler comment that export chain routing uses inline `nextSteps`, not atom-axis routing, because `SynthesizeImmediatePhase` has no service context (`internal/tools/workflow_export.go:34-38`; `internal/workflow/synthesize.go:521-525`).

## 8. Phase 6 forward-compat
Yes, the stale RemoteURL section will need a rewrite after Phase 6. The plan says Phase 6 adds `refreshRemoteURL` that SSH-reads live origin and updates `ServiceMeta.RemoteURL` on every export pass (`plans/export-buildfromgit-2026-04-28.md:623-637`), and surfaces mismatch warnings while using the live value (`plans/export-buildfromgit-2026-04-28.md:638-643`). After that, the atom's instruction to manually run `git-push-setup` just to fix cache drift (`internal/content/atoms/export-publish-needs-setup.md:40`) should be narrowed to intentional remote changes or setup confirmation failures, not ordinary cache refresh.

## 9. Failure-mode coverage gaps
Local service / wrong setup atom: not adequately documented. The atom claims local modes route to `setup-git-push-local` (`internal/content/atoms/export-publish-needs-setup.md:24-25`), but export's chain response does not branch (`internal/tools/workflow_export.go:162-164,273-283`) and setup synthesis keys environment off `runtime.Info` (`internal/tools/workflow_git_push_setup.go:97-107`; `internal/workflow/synthesize.go:532-536`). This can send a local-mode service through container-flavored prose without a recovery note.

Invalid `GIT_TOKEN`: partially covered indirectly. The setup atom says credential failures indicate a missing or rejected `GIT_TOKEN` (`internal/content/atoms/setup-git-push-container.md:34`), while this atom only says to push once to confirm and then re-call setup (`internal/content/atoms/export-publish-needs-setup.md:22`). Because confirm mode only validates URL format and writes meta (`internal/tools/workflow_git_push_setup.go:121-130`), the export atom should explicitly say a later push/deploy can still reject an invalid token.

Different prereq on re-call: not covered. The atom promises publish-ready on the next export call (`internal/content/atoms/export-publish-needs-setup.md:36`), but the handler may hit scaffold-required (`internal/tools/workflow_export.go:111-117`), setup-name/YAML errors (`internal/tools/workflow_export_probe.go:70-100`), or classify-prompt (`internal/tools/workflow_export.go:158-159`) if state changed.

## 10. Recommended amendments
Remove or rewrite the local-mode paragraph on lines 24-25. Accurate replacement: the export chain points at `action="git-push-setup"`; the walkthrough atom returned by that action is selected by the current ZCP runtime environment, while local-mode export support is not proven by the Phase C chain response.

Change line 36 from "lands at `status=\"publish-ready\"` ... this time" to "should land at publish-ready if no other prereq changed; otherwise follow the new status/nextSteps and re-supply the same export inputs."

Revise line 40 to avoid "Phase 0 capture" and say confirm mode writes/overwrites `meta.RemoteURL`; add a Phase 6 TODO that ordinary cache drift will be refreshed by export once `refreshRemoteURL` lands.

Strengthen line 44: manual copy is review-only unless the agent re-runs export immediately first and verifies the copied `bundle.importYaml` / `bundle.zeropsYaml` are from the latest response.
