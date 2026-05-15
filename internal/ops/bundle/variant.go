// Package bundle composes the zerops-project-import.yaml documents
// produced by the export + launch workflows. The package is layered
// per plans/workflow-family-architecture-2026-05-14.md §9.2:
//
//	Layer 1 — composition over typed Variant axis + per-service-type
//	          closed-table rules. No I/O. Pure functions.
//
// Inputs and outputs are owned here (canonical types). Legacy
// internal/ops/ entry points (BuildLaunchBundle, BuildBundle) expose
// the same names as Go type aliases so external callers compile
// unchanged.
package bundle

// Variant selects which composition rules apply. Closed enum;
// adding a value requires updating launch_variant.go / export_variant.go
// + the relevant ServiceTypeRules table entries.
//
// VariantLaunchExisting differs from VariantLaunchNew in one
// observable way today: the output omits the top-level `project:` block
// (the existing target project already has its own project metadata;
// PostProjectServiceStackImport rejects yaml carrying `project:`).
// Phase 6b wires the existing-project path; the variant is defined
// here so the Phase 1b refactor doesn't have to retroactively grow
// the enum.
type Variant int

const (
	// VariantExportDev packages the dev half of a standard pair for
	// re-import. Strips runtime envs (the target picks its own);
	// keeps managed-service config; adds buildFromGit pointing at the
	// source repo + current commit.
	VariantExportDev Variant = iota
	// VariantExportStage — same as VariantExportDev but for the stage
	// half. Identical composition shape; the runtime hostname + setup
	// name differ.
	VariantExportStage
	// VariantLaunchNew composes the import yaml that creates a fresh
	// production project. Promotes managed deps to HA (per
	// ServiceTypeRules), emits the full project block (name + tags +
	// envVariables), preserves buildFromGit on the runtime entry
	// (Phase 6b will flip this to startWithoutCode).
	VariantLaunchNew
	// VariantLaunchExisting composes services-only yaml for import
	// into an EXISTING target project. Same managed-service promotion
	// as VariantLaunchNew but omits the `project:` block — the API
	// (PostProjectServiceStackImport) rejects yaml with a project
	// block, and the target already carries its own metadata.
	VariantLaunchExisting
)

// String returns the variant's symbolic name for logging + audit.
func (v Variant) String() string {
	switch v {
	case VariantExportDev:
		return "export-dev"
	case VariantExportStage:
		return "export-stage"
	case VariantLaunchNew:
		return "launch-new"
	case VariantLaunchExisting:
		return "launch-existing"
	default:
		return "unknown"
	}
}

// IsLaunch reports whether the variant composes a launch bundle
// (vs an export bundle). Launch variants enable HA promotion + tag
// emission + the launch-specific source snapshot.
func (v Variant) IsLaunch() bool {
	return v == VariantLaunchNew || v == VariantLaunchExisting
}

// IsExport reports whether the variant composes an export bundle.
func (v Variant) IsExport() bool {
	return v == VariantExportDev || v == VariantExportStage
}
