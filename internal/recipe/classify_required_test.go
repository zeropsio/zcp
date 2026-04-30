package recipe

import (
	"strings"
	"testing"
)

// TestRecordFragment_RequiresClassification_OnKBAndIG pins the run-19
// prep escalation. Run-15 §F.3 made the Classification field optional
// (back-compat for callers that don't classify). Run-18 surfaced the
// failure mode: codebase-content-app submitted 5 KB bullets with no
// Classification field, the F.3 refusal never fired, and four bullets
// classifying as framework-quirk / library-metadata / self-inflicted
// shipped to porter-facing KB despite spec §337-347 forbidding any
// surface for those classes.
//
// Surfaces that admit MULTIPLE compatible classes (KB takes
// platform-invariant + intersection; IG takes platform-invariant +
// scaffold-decision-config + scaffold-decision-code) MUST require
// classification at record-time so the engine can refuse the
// incompatible classes deterministically. Surfaces that admit a single
// class (zerops-yaml-comments only takes scaffold-decision; CLAUDE.md
// only takes operational; intro is engine-emitted) keep optional.
func TestRecordFragment_RequiresClassification_OnKBAndIG(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		fragmentID string
		wantError  bool
	}{
		{"KB without classification", "codebase/api/knowledge-base", true},
		{"IG legacy without classification", "codebase/api/integration-guide", true},
		{"IG slotted without classification", "codebase/api/integration-guide/2", true},
		{"intro without classification (no requirement)", "codebase/api/intro", false},
		{"zerops-yaml-comments without classification (single-class surface)", "codebase/api/zerops-yaml-comments/run.start", false},
		{"claude-md without classification (single-class surface)", "codebase/api/claude-md", false},
		{"env intro without classification (no requirement)", "env/0/intro", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sess := newTestSessionWithPlan(t)
			res := handleRecordFragment(sess, RecipeInput{
				Action:     "record-fragment",
				FragmentID: tc.fragmentID,
				Fragment:   "- **Stub topic** — body content here for the placeholder fragment.",
				// Classification deliberately empty.
			}, RecipeResult{Action: "record-fragment"})

			if tc.wantError {
				if res.Error == "" {
					t.Errorf("expected refusal for %s without classification, got OK", tc.fragmentID)
				}
				if res.Error != "" && !strings.Contains(res.Error, "classification") {
					t.Errorf("refusal message must reference classification; got %q", res.Error)
				}
			} else if res.Error != "" && strings.Contains(res.Error, "classification") && strings.Contains(res.Error, "required") {
				// Other validators (slot-shape, etc.) may still fire on
				// the stub body — but the refusal MUST NOT be about a
				// missing classification field.
				t.Errorf("did not expect classification-required refusal for %s; got %q", tc.fragmentID, res.Error)
			}
		})
	}
}

// TestSurfaceFromFragmentID_SlottedIG pins that the slotted IG shape
// (`codebase/<h>/integration-guide/<n>`) resolves to SurfaceCodebaseIG.
// Run-18 codebase-content sub-agents authored every IG slot with the
// `/<n>` suffix; the prior SurfaceFromFragmentID returned (_, false)
// for slotted IDs so F.3 classification refusal silently bypassed.
func TestSurfaceFromFragmentID_SlottedIG(t *testing.T) {
	t.Parallel()

	cases := []struct {
		fragmentID string
		want       Surface
		wantOK     bool
	}{
		{"codebase/api/integration-guide", SurfaceCodebaseIG, true},
		{"codebase/api/integration-guide/2", SurfaceCodebaseIG, true},
		{"codebase/api/integration-guide/5", SurfaceCodebaseIG, true},
		{"codebase/worker/knowledge-base", SurfaceCodebaseKB, true},
		{"codebase/api/zerops-yaml-comments/run.start", SurfaceCodebaseZeropsComments, true},
		{"codebase/api/zerops-yaml-comments/build.deployFiles", SurfaceCodebaseZeropsComments, true},
		{"codebase/api/claude-md", SurfaceCodebaseCLAUDE, true},
		{"codebase/api/claude-md/notes", SurfaceCodebaseCLAUDE, true},
	}

	for _, tc := range cases {
		t.Run(tc.fragmentID, func(t *testing.T) {
			t.Parallel()
			got, ok := SurfaceFromFragmentID(tc.fragmentID)
			if ok != tc.wantOK {
				t.Errorf("SurfaceFromFragmentID(%q) ok=%v, want %v", tc.fragmentID, ok, tc.wantOK)
			}
			if got != tc.want {
				t.Errorf("SurfaceFromFragmentID(%q) = %q, want %q", tc.fragmentID, got, tc.want)
			}
		})
	}
}

// newTestSessionWithPlan returns a Session shape sufficient for
// handleRecordFragment unit tests (Plan with one Codebase + one
// managed service so SurfaceFromFragmentID resolves and the slot-shape
// validators don't crash on a nil plan).
func newTestSessionWithPlan(_ *testing.T) *Session {
	return &Session{
		Plan: &Plan{
			Slug:      "test-recipe",
			Framework: "test",
			Codebases: []Codebase{
				{Hostname: "api", Role: RoleAPI},
				{Hostname: "worker", Role: RoleWorker, IsWorker: true},
			},
			Services: []Service{
				{Hostname: "db", Type: "postgresql@18", Kind: ServiceKindManaged},
			},
			Fragments: map[string]string{},
		},
	}
}
