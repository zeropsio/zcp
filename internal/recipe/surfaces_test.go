package recipe

import (
	"strings"
	"testing"
)

func TestSurfaceRegistry_Complete(t *testing.T) {
	t.Parallel()

	expect := []Surface{
		SurfaceRootREADME,
		SurfaceEnvREADME,
		SurfaceEnvImportComments,
		SurfaceCodebaseIG,
		SurfaceCodebaseKB,
		SurfaceCodebaseCLAUDE,
		SurfaceCodebaseZeropsComments,
	}

	got := Surfaces()
	if len(got) != len(expect) {
		t.Fatalf("Surfaces() count = %d, want %d", len(got), len(expect))
	}
	for i, s := range expect {
		if got[i] != s {
			t.Errorf("Surfaces()[%d] = %q, want %q", i, got[i], s)
		}
	}
}

func TestSurfaceRegistry_ContractFields(t *testing.T) {
	t.Parallel()

	for _, s := range Surfaces() {
		c, ok := ContractFor(s)
		if !ok {
			t.Errorf("surface %q: no contract", s)
			continue
		}
		if c.Name != s {
			t.Errorf("surface %q: Name = %q, want %q", s, c.Name, s)
		}
		if c.Author == "" {
			t.Errorf("surface %q: empty Author", s)
		}
		if c.FormatSpec == "" {
			t.Errorf("surface %q: empty FormatSpec", s)
		}
		if !strings.HasPrefix(c.FormatSpec, "docs/spec-content-surfaces.md#") {
			t.Errorf("surface %q: FormatSpec %q does not anchor into spec-content-surfaces.md", s, c.FormatSpec)
		}
		if len(c.Owns) == 0 {
			t.Errorf("surface %q: empty Owns", s)
		}
		if c.FactHint == "" {
			t.Errorf("surface %q: empty FactHint", s)
		}
	}
}

func TestSurfaceRegistry_UniqueFactHint(t *testing.T) {
	t.Parallel()
	// Every surface that writes facts has a unique FactHint tag so records
	// route to exactly one surface. Cross-surface references are explicit
	// via AdjacentSurfaces, not via shared hints.
	seen := make(map[string]Surface)
	for _, s := range Surfaces() {
		c, _ := ContractFor(s)
		if prev, collides := seen[c.FactHint]; collides {
			t.Errorf("FactHint %q appears on both %q and %q", c.FactHint, prev, s)
		}
		seen[c.FactHint] = s
	}
}

// TestSurfaceFromFragmentID covers every fragment-id shape from
// handlers.RecipeInput.FragmentID's schema. Run-15 F.2 — the engine
// returns the resolved surface's contract on every record-fragment
// response; if SurfaceFromFragmentID misroutes a fragment id, the
// agent gets the wrong contract at authoring decision time.
func TestSurfaceFromFragmentID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		id   string
		want Surface
	}{
		{"root/intro", SurfaceRootREADME},
		{"env/0/intro", SurfaceEnvREADME},
		{"env/3/intro", SurfaceEnvREADME},
		{"env/5/import-comments/project", SurfaceEnvImportComments},
		{"env/2/import-comments/api", SurfaceEnvImportComments},
		{"env/2/import-comments/db", SurfaceEnvImportComments},
		{"codebase/api/intro", SurfaceCodebaseIG},
		{"codebase/api/integration-guide", SurfaceCodebaseIG},
		{"codebase/api/knowledge-base", SurfaceCodebaseKB},
		{"codebase/api/claude-md/service-facts", SurfaceCodebaseCLAUDE},
		{"codebase/api/claude-md/notes", SurfaceCodebaseCLAUDE},
	}
	for _, tc := range cases {
		got, ok := SurfaceFromFragmentID(tc.id)
		if !ok {
			t.Errorf("%q: not recognized; want %q", tc.id, tc.want)
			continue
		}
		if got != tc.want {
			t.Errorf("%q: got %q, want %q", tc.id, got, tc.want)
		}
	}
	// Negative — unknown ids must NOT silently route to a surface.
	for _, bad := range []string{"", "unknown", "env//intro", "codebase/api", "codebase/api/unknown-section"} {
		if _, ok := SurfaceFromFragmentID(bad); ok {
			t.Errorf("%q: unexpectedly routed to a surface", bad)
		}
	}
}

// TestSurfaceContract_HasCaps pins the run-15 F.2 invariant: every
// writer-authored surface must populate at least one structural cap
// (LineCap, ItemCap, or IntroExtractCharCap). Caps are returned on
// record-fragment responses so the agent measures its output against
// the spec at authoring decision time.
func TestSurfaceContract_HasCaps(t *testing.T) {
	t.Parallel()
	// Surface 7 (codebase zerops.yaml comments) has no numeric cap by
	// design — the spec leaves comment granularity to the author. Every
	// other writer-authored surface must declare at least one cap.
	exempt := map[Surface]bool{
		SurfaceCodebaseZeropsComments: true,
	}
	for _, s := range Surfaces() {
		c, _ := ContractFor(s)
		if c.Author != AuthorWriter {
			continue
		}
		if exempt[s] {
			continue
		}
		hasCap := c.LineCap > 0 || c.ItemCap > 0 || c.IntroExtractCharCap > 0
		if !hasCap {
			t.Errorf("surface %q (writer-authored): no structural cap populated; spec demands at least one", s)
		}
	}
}

// TestSurfaceContract_ReaderAndTestPopulated verifies the reader-
// description and self-review test live on every contract — the
// authoring agent reads them at record-time per the F.2 contract.
func TestSurfaceContract_ReaderAndTestPopulated(t *testing.T) {
	t.Parallel()
	for _, s := range Surfaces() {
		c, _ := ContractFor(s)
		if c.Reader == "" {
			t.Errorf("surface %q: empty Reader", s)
		}
		if c.Test == "" {
			t.Errorf("surface %q: empty Test", s)
		}
	}
}
