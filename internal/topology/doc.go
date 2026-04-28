// Package topology is ZCP's foundational vocabulary for describing
// Zerops services. It owns the type axes (Mode, DeployStrategy,
// CloseDeployMode, GitPushState, BuildIntegration, RuntimeClass,
// PushGitTrigger) and predicates (IsManagedService, IsPushSource,
// ServiceSupportsMode, IsRuntimeType, IsUtilityType, plus storage
// classifiers) that workflow/ and ops/ both consume.
//
// Layering: foundational. Imports stdlib only. Pinned by depguard
// rules and internal/architecture_test.go. Spec: docs/spec-architecture.md.
//
// Deploy-strategy vocabulary: post-decomposition the legacy DeployStrategy
// + PushGitTrigger pair is replaced by three orthogonal dimensions —
// CloseDeployMode (what fires at workflow close), GitPushState (whether
// git-push capability is set up), and BuildIntegration (which ZCP-managed
// CI integration responds to remote pushes). The legacy vocabulary stays
// during migration; see plan
// `plans/deploy-strategy-decomposition-2026-04-28.md` for the orthogonality
// matrix, lifecycle scenarios, and per-phase deletion schedule.
package topology
