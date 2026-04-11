package knowledge

import (
	"testing"
)

// TestDocument_H3Section exercises the H3-granular extractor added for the
// recipe delivery refactor. It must handle: exact H3 matches, the first
// subsection case (ensuring parsing doesn't lose content before the first
// sibling H3), and missing-key defaults.
func TestDocument_H3Section(t *testing.T) {
	t.Parallel()
	doc := &Document{Content: `## import.yaml Schema

### Service fields

- hostname
- type

### Project block

Fields for project-level configuration.

### verticalAutoscaling

minRam, maxRam, minCPU, maxCPU.

## Another Heading

ignored.`}
	tests := []struct {
		name string
		h2   string
		h3   string
		want string
	}{
		{"exact match — last h3", "import.yaml Schema", "verticalAutoscaling", "minRam, maxRam, minCPU, maxCPU."},
		{"first subsection", "import.yaml Schema", "Service fields", "- hostname\n- type"},
		{"middle subsection", "import.yaml Schema", "Project block", "Fields for project-level configuration."},
		{"missing h2 returns empty", "missing", "x", ""},
		{"missing h3 returns empty", "import.yaml Schema", "not-present", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := doc.H3Section(tt.h2, tt.h3)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDocument_H3Section_StopsAtSiblingH2 ensures the walk terminates at the
// next H2 even when the target H3 spans multiple paragraphs.
func TestDocument_H3Section_StopsAtSiblingH2(t *testing.T) {
	t.Parallel()
	doc := &Document{Content: `## First H2

### Target

Line one.

Line two.

## Second H2

### Also Target

should not be returned.`}
	got := doc.H3Section("First H2", "Target")
	want := "Line one.\n\nLine two."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
