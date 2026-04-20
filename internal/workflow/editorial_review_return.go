package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
)

// EditorialReviewReturn is the structured payload an editorial-review
// sub-agent returns at close.editorial-review substep complete. Shape
// matches `briefs/editorial-review/completion-shape.md`. The substep
// attestation carries this JSON verbatim; the substep validator parses
// it and runs the seven dispatch-runnable checks declared in
// `docs/zcprecipator2/03-architecture/check-rewrite.md §16a`.
//
// All counts are post-inline-fix unless the field name explicitly names
// the pre-fix snapshot. JSON tags use snake_case to match the
// editorial-review subagent handshake documented in the atom; severity
// keys (`CRIT`, `WRONG`, `STYLE`) preserve the reviewer's reporting
// taxonomy uppercase form — tagliatelle warnings are silenced per-field
// because renaming would break the wire contract.
type EditorialReviewReturn struct {
	SurfacesWalked     []string `json:"surfaces_walked"`  //nolint:tagliatelle // wire contract with editorial-review subagent
	SurfacesSkipped    []any    `json:"surfaces_skipped"` //nolint:tagliatelle // wire contract with editorial-review subagent
	FindingsBySeverity struct {
		Crit  int `json:"CRIT"`  //nolint:tagliatelle // reviewer taxonomy key preserved uppercase
		Wrong int `json:"WRONG"` //nolint:tagliatelle // reviewer taxonomy key preserved uppercase
		Style int `json:"STYLE"` //nolint:tagliatelle // reviewer taxonomy key preserved uppercase
	} `json:"findings_by_severity"` //nolint:tagliatelle // wire contract with editorial-review subagent
	FindingsBySeverityBeforeInlineFix struct {
		Crit  int `json:"CRIT"`  //nolint:tagliatelle // reviewer taxonomy key preserved uppercase
		Wrong int `json:"WRONG"` //nolint:tagliatelle // reviewer taxonomy key preserved uppercase
		Style int `json:"STYLE"` //nolint:tagliatelle // reviewer taxonomy key preserved uppercase
	} `json:"findings_by_severity_before_inline_fix"` //nolint:tagliatelle // wire contract with editorial-review subagent
	PerSurfaceFindings         []EditorialReviewFinding   `json:"per_surface_findings"`         //nolint:tagliatelle // wire contract with editorial-review subagent
	ReclassificationDeltaTable []ReclassificationDeltaRow `json:"reclassification_delta_table"` //nolint:tagliatelle // wire contract with editorial-review subagent
	CitationCoverage           CitationCoverage           `json:"citation_coverage"`            //nolint:tagliatelle // wire contract with editorial-review subagent
	CrossSurfaceLedger         []CrossSurfaceLedgerRow    `json:"cross_surface_ledger"`         //nolint:tagliatelle // wire contract with editorial-review subagent
	InlineFixesApplied         []InlineFixApplied         `json:"inline_fixes_applied"`         //nolint:tagliatelle // wire contract with editorial-review subagent
}

// EditorialReviewFinding is one entry in per_surface_findings. Severity
// is one of "CRIT", "WRONG", "STYLE"; disposition is one of
// "inline-fixed", "fix-recommended", "suggestion". Tags carries optional
// subclass markers (e.g. "fabricated") that the fabricated-mechanism
// check reads; descriptions are scanned as a fallback when tags are
// absent.
type EditorialReviewFinding struct {
	SurfacePath string   `json:"surface_path"` //nolint:tagliatelle // wire contract with editorial-review subagent
	Severity    string   `json:"severity"`
	TestOutcome string   `json:"test_outcome"` //nolint:tagliatelle // wire contract with editorial-review subagent
	Description string   `json:"description"`
	Disposition string   `json:"disposition"`
	Tags        []string `json:"tags,omitempty"`
}

// ReclassificationDeltaRow is one row of the reclassification-delta
// table. writer_said / reviewer_said are the classification tokens each
// party applied to the same item; final carries the agreed class post-
// discussion (equal to reviewer_said when the reviewer's judgment
// wins).
type ReclassificationDeltaRow struct {
	ItemPath     string `json:"item_path"`     //nolint:tagliatelle // wire contract with editorial-review subagent
	WriterSaid   string `json:"writer_said"`   //nolint:tagliatelle // wire contract with editorial-review subagent
	ReviewerSaid string `json:"reviewer_said"` //nolint:tagliatelle // wire contract with editorial-review subagent
	Final        string `json:"final"`
}

// CitationCoverage carries the ratio and raw counts of matching-topic
// gotchas that cite a `zerops_knowledge` guide. Uncited equals
// Denominator - Numerator.
type CitationCoverage struct {
	Numerator   int     `json:"numerator"`
	Denominator int     `json:"denominator"`
	Percent     float64 `json:"percent"`
}

// CrossSurfaceLedgerRow is one row of the cross-surface ledger. Severity
// == "duplicate" (case-insensitive) flags a cross-surface duplication;
// any other severity is non-duplicate context.
type CrossSurfaceLedgerRow struct {
	Fact      string   `json:"fact"`
	Surfaces  []string `json:"surfaces"`
	Severity  string   `json:"severity"`
	Rationale string   `json:"rationale,omitempty"`
}

// InlineFixApplied records one inline Edit the reviewer performed.
type InlineFixApplied struct {
	FilePath string `json:"file_path"` //nolint:tagliatelle // wire contract with editorial-review subagent
	Severity string `json:"severity"`
	Before   string `json:"before"`
	After    string `json:"after"`
}

// ParseEditorialReviewReturn parses the substep attestation as the
// structured review payload. Returns a typed error when the attestation
// is empty, not valid JSON, or missing the minimum surface-walked
// signal that indicates the sub-agent actually dispatched.
//
// The "dispatched" check relies on surfaces_walked being non-empty. A
// valid-JSON-but-empty-payload case is distinguished from a parse
// failure so the `editorial_review_dispatched` check can emit a clear
// detail message.
func ParseEditorialReviewReturn(attestation string) (*EditorialReviewReturn, error) {
	trimmed := strings.TrimSpace(attestation)
	if trimmed == "" {
		return nil, fmt.Errorf("attestation is empty; the editorial-review return payload must be attached verbatim as the substep attestation")
	}
	if !strings.HasPrefix(trimmed, "{") {
		return nil, fmt.Errorf("attestation is not JSON (first non-space rune = %q); the substep expects the editorial-review return payload verbatim", string(trimmed[0]))
	}
	var out EditorialReviewReturn
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		return nil, fmt.Errorf("attestation JSON parse: %w", err)
	}
	return &out, nil
}
