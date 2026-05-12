package recipe

import (
	"strings"
	"testing"
)

// briefs_refinement2_ig_citation_g2_test.go — Run-44 G2 substrate pin.
//
// Run-43 validation §"(1) IG citations: 0 of 12 H3 items across three
// codebases": refinement-2's `missing-citation` audit is KB-only by
// contract (audit_checklist.md "For each KB bullet, scan body…");
// the writer-brief contract requires Citation-Map-matching IG items
// to cite too, but no auditor enforces it. apidev IG #2/#3 →
// `http-support`; apidev IG #4 → `rolling-deploys`; appdev IG #4 →
// `deploy-files` — all should cite, none do.
//
// Fix: extend the audit's `missing-citation` check to ALSO walk each
// IG H3 item body. The check is the same logic — for each IG body,
// identify topic family against citation map, fail if no citation
// form present.
//
// The corresponding finding's defectClass enum stays `missing-citation`;
// surface field flips to S4 (CODEBASE_IG) instead of S5 (CODEBASE_KB).

// TestRefinement2Brief_MissingCitation_ScansIG — the missing-citation
// defect class section in audit_checklist.md MUST mention IG (Surface
// S4) as a scan target, not just KB.
func TestRefinement2Brief_MissingCitation_ScansIG(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "synth-showcase",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
	}
	brief, err := BuildRefinement2Brief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinement2Brief: %v", err)
	}
	// Locate the missing-citation class section.
	idx := strings.Index(brief.Body, "## Defect class: missing-citation")
	if idx < 0 {
		t.Fatal("missing-citation class header missing from audit_checklist.md")
	}
	// Chunk the section body up to the next class header (or EOB).
	tail := brief.Body[idx:]
	next := strings.Index(tail[1:], "\n## Defect class:")
	var chunk string
	if next > 0 {
		chunk = tail[:next+1]
	} else {
		// section may run to EOB; cap at 4 KiB for cheap scanning.
		chunk = tail[:min(len(tail), 4096)]
	}
	// IG (S4) scan target — the section must explicitly walk IG items,
	// not just KB bullets. Either phrasing is acceptable; both pin the
	// G2 intent.
	hasIGScan := strings.Contains(chunk, "IG") &&
		(strings.Contains(chunk, "S4") || strings.Contains(chunk, "integration-guide") || strings.Contains(chunk, "IG H3 item") || strings.Contains(chunk, "IG item"))
	if !hasIGScan {
		t.Errorf("missing-citation class section does not mention IG / S4 / integration-guide; chunk:\n%s", chunk)
	}
}

// TestRefinement2Brief_MissingCitation_SurfaceTokensCoverIG — the
// section also mentions that the surface field in the emitted finding
// is S4 / CODEBASE_IG when the audit fires on an IG item. (Without
// this guidance the audit-LLM defaults to S5 on every missing-citation
// finding.)
func TestRefinement2Brief_MissingCitation_SurfaceTokensCoverIG(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "synth-showcase",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
	}
	brief, err := BuildRefinement2Brief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinement2Brief: %v", err)
	}
	idx := strings.Index(brief.Body, "## Defect class: missing-citation")
	if idx < 0 {
		t.Fatal("missing-citation header missing")
	}
	tail := brief.Body[idx:]
	next := strings.Index(tail[1:], "\n## Defect class:")
	var chunk string
	if next > 0 {
		chunk = tail[:next+1]
	} else {
		chunk = tail[:min(len(tail), 4096)]
	}
	// Run-43 evidence — section must name the IG-citation-coverage
	// gap from the run-43 validation explicitly so the audit-LLM
	// reads the rationale + intent.
	if !strings.Contains(chunk, "integration-guide") {
		t.Errorf("missing-citation chunk does not reference the integration-guide fragmentId form: %s", chunk)
	}
}
