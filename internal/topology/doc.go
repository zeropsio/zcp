// Package topology is ZCP's foundational vocabulary for describing
// Zerops services. It owns the type axes (Mode, CloseDeployMode,
// GitPushState, BuildIntegration, RuntimeClass) and predicates
// (IsManagedService, IsPushSource, ServiceSupportsMode, IsRuntimeType,
// IsUtilityType, plus storage classifiers) that workflow/ and ops/
// both consume.
//
// Layering: foundational. Imports stdlib only. Pinned by depguard
// rules and internal/architecture_test.go. Spec: docs/spec-architecture.md.
//
// Deploy vocabulary: three orthogonal dimensions describe a service's
// deploy posture — CloseDeployMode (what fires at workflow close),
// GitPushState (whether git-push capability is set up), and
// BuildIntegration (which ZCP-managed CI integration responds to remote
// pushes). See plan
// `plans/archive/deploy-strategy-decomposition-2026-04-28.md` §3.1 for
// the orthogonality matrix and lifecycle scenarios.
package topology
