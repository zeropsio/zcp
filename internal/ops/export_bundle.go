// Package ops — export_bundle.go retains the legacy entry-point names
// (ops.BuildBundle, ops.ExportBundle, ops.BundleInputs,
// ops.ProjectEnvVar, ops.ManagedServiceEntry) for external callers
// while the actual composer logic lives in internal/ops/bundle/
// (Phase 1b refactor per
// plans/workflow-family-architecture-2026-05-14.md §9.2 + §11).
//
// Go type aliases preserve compile-time compatibility: external code
// using `ops.BundleInputs{...}` etc. constructs the underlying
// bundle/ type through the alias.
package ops

import (
	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/topology"
)

// ExportBundle — alias of bundle.ExportBundle. See bundle/outputs.go.
type ExportBundle = bundle.ExportBundle

// BundleInputs — alias of bundle.BundleInputs. See bundle/inputs.go.
type BundleInputs = bundle.BundleInputs

// ProjectEnvVar — alias of bundle.ProjectEnvVar. See bundle/inputs.go.
// Note: distinct from platform.ProjectEnvVar (Phase 1a SDK wrapper).
// The bundle's ProjectEnvVar is the composer's input contract
// (Key/Value only); platform.ProjectEnvVar is the SDK shape carrying
// Type/Sensitive/Editable. Phase 2 will reconcile these via the
// envclass layer.
type ProjectEnvVar = bundle.ProjectEnvVar

// ManagedServiceEntry — alias of bundle.ManagedServiceEntry. See
// bundle/inputs.go.
type ManagedServiceEntry = bundle.ManagedServiceEntry

// BuildBundle is the legacy entry point for export bundle composition.
// Delegates to bundle.BuildExport which owns the composition pipeline
// (verify setup, classify envs, compose import yaml, schema-validate
// both yamls).
func BuildBundle(
	inputs BundleInputs,
	classifications map[string]topology.SecretClassification,
) (*ExportBundle, error) {
	return bundle.BuildExport(inputs, classifications)
}
