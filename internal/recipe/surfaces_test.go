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
