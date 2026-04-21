// Package analyze builds the mechanical analysis layer for zcprecipator2
// recipe runs. It is the first defense against the v36 analysis-failure
// mode described in docs/zcprecipator2/runs/v36/CORRECTIONS.md: every
// claim in a verdict must survive grep or jq. Subjective judgment stays
// in prose, but is layered on top of evidence-enforced measurements here.
//
// The harness surfaces structural, session-metric, writer-compliance,
// and dispatch-integrity bars. Each bar is a deterministic measurement
// (filesystem walk, JSONL parse, or byte-diff) that produces a
// BarResult. BarResults compose into a MachineReport that the CLI
// writes as JSON and a checklist generator reads to drive the analyst
// worksheet.
//
// Tier-1 bars (this file + structural.go + session.go + writer_compliance.go):
// mechanical measurements that never require analyst judgment.
// Tier-2 (checklist.go): analyst-facing worksheet generated from the report.
// Tier-3 (tools/hooks/verify_verdict): git pre-commit refusing unbound verdicts.
package analyze

// SchemaVersion names the machine-report schema. Bump when breaking
// changes to the JSON shape land so downstream consumers (checklist
// generator, pre-commit hook) can refuse stale reports instead of
// silently mis-parsing.
const SchemaVersion = "1.0.0"

// Bar status values. Every BarResult carries one.
const (
	StatusPass = "pass"
	StatusFail = "fail"
	StatusSkip = "skip"
)

// BarResult is the common shape every mechanical bar emits. The zero
// value is a valid "skip" result — callers that can't measure set
// Status=skip with a Reason explaining why.
type BarResult struct {
	Description   string   `json:"description,omitempty"`
	Measurement   string   `json:"measurement,omitempty"`
	Threshold     int      `json:"threshold"`
	Observed      int      `json:"observed"`
	Status        string   `json:"status"`
	Reason        string   `json:"reason,omitempty"`
	EvidenceFiles []string `json:"evidence_files,omitempty"`
	EvidenceRaw   []string `json:"evidence_raw,omitempty"`
}

// PassOrFail maps a boolean condition to the Status constant.
func PassOrFail(ok bool) string {
	if ok {
		return StatusPass
	}
	return StatusFail
}

// StructuralIntegrity rolls up the deliverable-tree bars. Every field is
// a BarResult keyed in JSON by the B-N name so analyst citations match
// exactly: "[machine-report.structural_integrity.B-15_ghost_env_dirs]".
type StructuralIntegrity struct {
	GhostEnvDirs             BarResult `json:"B-15_ghost_env_dirs"`
	TarballPerCodebaseMd     BarResult `json:"B-16_tarball_per_codebase_md"`
	MarkerExactForm          BarResult `json:"B-17_marker_exact_form"`
	StandaloneDuplicateFiles BarResult `json:"B-18_standalone_duplicate_files"`
	AtomTemplateVarsBound    BarResult `json:"B-22_atom_template_vars_bound"`
}

// RetryCycle names a single failing check round plus its analyst-
// supplied attribution. The harness fills the cycle + failing_checks
// fields mechanically; the analyst fills Attribution during checklist
// completion. All populated before commit-hook runs.
type RetryCycle struct {
	Cycle         int      `json:"cycle"`
	Timestamp     string   `json:"timestamp"`
	Substep       string   `json:"substep,omitempty"`
	FailingChecks []string `json:"failing_checks"`
	Attribution   string   `json:"attribution,omitempty"`
}

// SessionMetrics rolls up the JSONL-derived bars.
type SessionMetrics struct {
	DeployReadmesRetryRounds  BarResult    `json:"B-20_deploy_readmes_retry_rounds"`
	SessionlessExportAttempts BarResult    `json:"B-21_sessionless_export_attempts"`
	WriterFirstPassFailures   BarResult    `json:"B-23_writer_first_pass_failures"`
	DispatchIntegrity         BarResult    `json:"B-24_dispatch_integrity"`
	SubAgentCount             int          `json:"sub_agent_count"`
	CloseStepCompleted        bool         `json:"close_step_completed"`
	EditorialReviewDispatched bool         `json:"editorial_review_dispatched"`
	CodeReviewDispatched      bool         `json:"code_review_dispatched"`
	CloseBrowserWalkAttempted bool         `json:"close_browser_walk_attempted"`
	RetryCycleAttributions    []RetryCycle `json:"retry_cycle_attributions"`
}

// FragmentCompliance captures the per-fragment check result inside a
// per-codebase README.
type FragmentCompliance struct {
	MarkersPresent       bool   `json:"markers_present"`
	MarkersExactForm     bool   `json:"markers_exact_form"`
	LineCount            int    `json:"line_count,omitempty"`
	InRange              bool   `json:"in_range,omitempty"`
	H3Count              int    `json:"h3_count,omitempty"`
	EveryH3HasFencedCode bool   `json:"every_h3_has_fenced_code_block,omitempty"`
	GotchasH3Present     bool   `json:"gotchas_h3_present,omitempty"`
	GotchaBulletCount    int    `json:"gotcha_bullet_count,omitempty"`
	BulletsInRange       bool   `json:"bullets_in_range,omitempty"`
	Status               string `json:"status"`
	Reason               string `json:"reason,omitempty"`
}

// CLAUDECompliance captures the per-codebase CLAUDE.md bar.
type CLAUDECompliance struct {
	FileExists          bool     `json:"file_exists"`
	SizeBytes           int      `json:"size_bytes"`
	SizeGE1200          bool     `json:"size_ge_1200_bytes"`
	BaseSectionsPresent []string `json:"base_sections_present"`
	CustomSectionCount  int      `json:"custom_section_count"`
	CustomSectionsGE2   bool     `json:"custom_sections_ge_2"`
	Status              string   `json:"status"`
}

// ReadmeCompliance is a per-codebase README result. The three fragment
// bars (intro, integration-guide, knowledge-base) are measured
// independently so an analyst can cite "apidev README knowledge-base
// fragment failed" without ambiguity.
type ReadmeCompliance struct {
	FileExists               bool               `json:"file_exists"`
	SizeBytes                int                `json:"size_bytes"`
	IntroFragment            FragmentCompliance `json:"intro_fragment"`
	IntegrationGuideFragment FragmentCompliance `json:"integration_guide_fragment"`
	KnowledgeBaseFragment    FragmentCompliance `json:"knowledge_base_fragment"`
	Status                   string             `json:"status"`
}

// DispatchResult captures per-role dispatch prompt measurements. The
// byte-diff-against-Go-source check (B-24 backing) lives here so an
// analyst can cite divergence per role.
type DispatchResult struct {
	PromptSizeBytes        int      `json:"dispatch_prompt_size_bytes"`
	AtomsStitchedInOrder   bool     `json:"atoms_stitched_in_envelope_order"`
	DiffStatus             string   `json:"dispatch_vs_source_diff_status"`
	Divergences            []string `json:"divergences,omitempty"`
	TemplateVarsResolved   bool     `json:"template_vars_resolved"`
	UnresolvedTemplateVars []string `json:"unresolved_template_vars,omitempty"`
}

// MachineReport is the root JSON shape. Field order below matches the
// JSON output; encoding/json respects struct-field order so the output
// is deterministic across runs.
type MachineReport struct {
	Run              string `json:"run"`
	GeneratedAt      string `json:"generated_at"`
	GeneratorVersion string `json:"generator_version"`
	Tier             string `json:"tier"`
	Slug             string `json:"slug"`
	DeliverableDir   string `json:"deliverable_dir"`
	SessionsLogsDir  string `json:"sessions_logs_dir"`

	StructuralIntegrity StructuralIntegrity         `json:"structural_integrity"`
	SessionMetrics      SessionMetrics              `json:"session_metrics"`
	WriterReadmes       map[string]ReadmeCompliance `json:"writer_readmes"`
	WriterClaudeMd      map[string]CLAUDECompliance `json:"writer_claude_md"`
	DispatchIntegrity   map[string]DispatchResult   `json:"dispatch_integrity"`

	SchemaVersion string `json:"schema_version"`
}

// ReportInput aggregates the inputs recipe-run needs. Collected at the
// CLI boundary and threaded through bar implementations so tests can
// pass a populated struct without touching flag parsing.
type ReportInput struct {
	DeliverableDir  string
	SessionsLogsDir string
	Tier            string
	Slug            string
	Run             string
	// AppCodebases names the per-codebase subdirectories the harness
	// inspects for writer-authored markdown. When empty the harness
	// auto-discovers any first-level subdirectory that matches
	// "{name}dev" or contains a README.md at the top level (heuristic
	// is narrow; the CLI flag lets callers pin exact names).
	AppCodebases []string
}
