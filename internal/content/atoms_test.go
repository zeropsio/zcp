package content

import (
	"strings"
	"testing"
)

// TestReadAllAtoms_ReturnsSortedMarkdown confirms the embedded atoms are
// discoverable, non-empty, and returned in a deterministic order.
func TestReadAllAtoms_ReturnsSortedMarkdown(t *testing.T) {
	t.Parallel()

	atoms, err := ReadAllAtoms()
	if err != nil {
		t.Fatalf("ReadAllAtoms: %v", err)
	}
	if len(atoms) == 0 {
		t.Fatal("expected at least one embedded atom")
	}
	for i := 1; i < len(atoms); i++ {
		if atoms[i-1].Name >= atoms[i].Name {
			t.Errorf("atoms not sorted: %s >= %s", atoms[i-1].Name, atoms[i].Name)
		}
	}
	for _, a := range atoms {
		if !strings.HasPrefix(a.Content, "---\n") {
			t.Errorf("atom %s missing frontmatter opening", a.Name)
		}
		if !strings.HasSuffix(a.Name, ".md") {
			t.Errorf("atom %s not .md", a.Name)
		}
	}
}
