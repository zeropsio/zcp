// Package ops — launch_bundle.go retains the legacy entry-point names
// (ops.BuildLaunchBundle, ops.LaunchBundle, ops.LaunchBundleInputs,
// ops.SourceSnapshot) for external callers while the actual composer
// logic lives in internal/ops/bundle/ (Phase 1b refactor per
// plans/workflow-family-architecture-2026-05-14.md §9.2 + §11).
//
// Go type aliases preserve compile-time compatibility: external code
// using `ops.LaunchBundleInputs{...}` constructs a bundle.LaunchBundleInputs
// value through the alias; no conversion shim required.
package ops

import (
	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/topology"
)

// LaunchBundle — alias of bundle.LaunchBundle. See bundle/outputs.go
// for the canonical definition + field docs.
type LaunchBundle = bundle.LaunchBundle

// SourceSnapshot — alias of bundle.SourceSnapshot. See bundle/outputs.go.
type SourceSnapshot = bundle.SourceSnapshot

// LaunchBundleInputs — alias of bundle.LaunchBundleInputs. See
// bundle/inputs.go.
type LaunchBundleInputs = bundle.LaunchBundleInputs

// LaunchRuntimeInput — alias of bundle.LaunchRuntimeInput. See
// bundle/inputs.go for the canonical definition + field docs.
type LaunchRuntimeInput = bundle.LaunchRuntimeInput

// ManagedDepReference — alias of bundle.ManagedDepReference. See
// bundle/classify.go for the canonical definition + field docs.
type ManagedDepReference = bundle.ManagedDepReference

// BuildLaunchBundle is the legacy entry point for launch bundle
// composition. Delegates to bundle.BuildLaunch which owns the
// composition pipeline (verify setup, classify envs, compose
// services/project blocks, marshal yaml, schema-validate, compute
// SourceSnapshot).
func BuildLaunchBundle(
	inputs LaunchBundleInputs,
	classifications map[string]topology.SecretClassification,
) (*LaunchBundle, error) {
	return bundle.BuildLaunch(inputs, classifications)
}
