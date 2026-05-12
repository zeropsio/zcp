package recipe

import (
	"strings"
	"testing"
)

// briefs_refinement2_self_inflicted_g3_test.go — Run-44 G3 substrate pin.
//
// Run-43 validation §"(3) Two borderline self-inflicted bullets":
// refinement-2's Check #1 for self-inflicted-as-gotcha cross-references
// against IG #1's shipped envVariables. Run-43 missed apidev KB #3
// X-Cache cross-origin (the trap's deviation point is CORS code config
// — `exposedHeaders` — NOT a yaml env var). Codex flagged a broad
// fenced-code scan would over-fire on legitimate intersections.
//
// Fix: extend Check #1 with a SECOND test step — a named-artifact
// pattern list drawn from synthesis_workflow.md §"DISCARD —
// self-inflicted" (the spec's source-of-truth example list). The
// patterns are specific named artifacts (`${storage_apiHost}`/
// `${storage_apiUrl}` confusion, `exposedHeaders` / CORS custom
// response header invisibility), NOT a generic fenced-code scanner.
// Add as a sub-bullet under Check #1 with explicit "scan for these
// named artifacts in IG code blocks" language.

// TestRefinement2Brief_SelfInflicted_NamedArtifactPatterns — the
// self-inflicted-as-gotcha class section in audit_checklist.md MUST
// include a named-artifact pattern list under Check #1, covering at
// minimum:
//   - storage env-var deviation (apiHost vs apiUrl) — the 301-redirect trap
//   - CORS exposedHeaders — the X-Cache cross-origin invisibility trap
func TestRefinement2Brief_SelfInflicted_NamedArtifactPatterns(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "synth-showcase",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
	}
	brief, err := BuildRefinement2Brief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinement2Brief: %v", err)
	}
	// Locate the self-inflicted-as-gotcha class section.
	idx := strings.Index(brief.Body, "## Defect class: self-inflicted-as-gotcha")
	if idx < 0 {
		t.Fatal("self-inflicted-as-gotcha class header missing")
	}
	tail := brief.Body[idx:]
	next := strings.Index(tail[1:], "\n## Defect class:")
	var chunk string
	if next > 0 {
		chunk = tail[:next+1]
	} else {
		chunk = tail[:min(len(tail), 6000)]
	}

	// G3 — named-artifact pattern list MUST be present.
	for _, want := range []string{
		// Section marker — explicit name for the new sub-list under Check #1.
		"named-artifact",
		// Pattern 1 — storage env-var deviation. The shipped IG ships
		// ${storage_apiUrl}; the trap fires when porter composes
		// http://${storage_apiHost}.
		"${storage_apiHost}",
		"${storage_apiUrl}",
		// Pattern 2 — CORS exposedHeaders. The shipped IG ships
		// exposedHeaders: ['X-Cache', ...]; the trap fires when porter
		// authoring expects custom response headers to be readable
		// cross-origin without the allowlist.
		"exposedHeaders",
		// Plumbing — the section must explicitly point at IG code blocks
		// (so the audit doesn't generalize to a fenced-code scan of any
		// surface).
		"IG #1",
	} {
		if !strings.Contains(chunk, want) {
			t.Errorf("self-inflicted-as-gotcha class missing named-artifact anchor %q", want)
		}
	}
}
