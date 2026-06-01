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
	// VariantLaunchNew composes the import yaml that creates a fresh
	// production project. Promotes managed deps to HA (per
	// ServiceTypeRules), emits the full project block (name + tags +
	// envVariables), preserves buildFromGit on the runtime entry.
	//
	// It is the ZERO VALUE (iota 0): BuildLaunch treats an unset
	// LaunchBundleInputs.Variant as launch-new. (Export composition uses
	// its own narrowing — there is no export Variant value; the dead
	// VariantExportDev/Stage half was removed with the export `variant`
	// dimension.)
	VariantLaunchNew Variant = iota
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
	case VariantLaunchNew:
		return "launch-new"
	case VariantLaunchExisting:
		return "launch-existing"
	default:
		return "unknown"
	}
}

// IsLaunch reports whether the variant composes a launch bundle. Always
// true today (the export half was removed); retained as the explicit
// predicate the launch composer keys on.
func (v Variant) IsLaunch() bool {
	return v == VariantLaunchNew || v == VariantLaunchExisting
}
