// Package topology is ZCP's foundational vocabulary for describing
// Zerops services. It owns the type axes (Mode, DeployStrategy,
// RuntimeClass, PushGitTrigger) and predicates (IsManagedService,
// ServiceSupportsMode, IsRuntimeType, IsUtilityType, plus storage
// classifiers) that workflow/ and ops/ both consume.
//
// Layering: foundational. Imports stdlib only. Pinned by depguard
// rules and internal/architecture_test.go. Spec: docs/spec-architecture.md.
package topology
