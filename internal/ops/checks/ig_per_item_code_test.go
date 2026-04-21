package checks

import (
	"context"
	"strings"
	"testing"
)

// igMultiSection wraps a body with the EXTRACT markers and prefixes a
// single-line H1 so splitByH3 doesn't trip on the fragment-start marker.
func igMultiSection(body string) string {
	return "# Intro\n\n" +
		"<!-- #ZEROPS_EXTRACT_START:integration-guide# -->\n" +
		body +
		"\n<!-- #ZEROPS_EXTRACT_END:integration-guide# -->\n"
}

func TestCheckIGPerItemCode_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		content    string
		isShowcase bool
		wantStatus string
		wantDetail []string
	}{
		{
			name:       "minimal tier returns nil",
			content:    igMultiSection("### 1. A\n### 2. B\n"),
			isShowcase: false,
			wantStatus: "",
		},
		{
			name:       "no fragment returns nil",
			content:    "no markers here",
			isShowcase: true,
			wantStatus: "",
		},
		{
			name:       "single H3 passes (minimal shape)",
			content:    igMultiSection("### 1. zerops.yaml\n```yaml\nzerops: []\n```"),
			isShowcase: true,
			wantStatus: "pass",
		},
		{
			name: "multi-H3 all with fenced code passes",
			content: igMultiSection(`### 1. zerops.yaml
` + "```yaml\nzerops: []\n```" + `

### 2. Trust proxy
` + "```ts\napp.set('trust proxy', 1);\n```"),
			isShowcase: true,
			wantStatus: "pass",
			wantDetail: []string{"2 IG items"},
		},
		{
			name: "second H3 prose-only fails",
			content: igMultiSection(`### 1. zerops.yaml
` + "```yaml\nzerops: []\n```" + `

### 2. Host binding
Prose describing binding with no code.`),
			isShowcase: true,
			wantStatus: "fail",
			wantDetail: []string{"2. Host binding"},
		},
		{
			name: "three H3s with two missing reports both",
			content: igMultiSection(`### 1. Prose only
just text

### 2. Prose only too
more text

### 3. Has code
` + "```bash\nnpm install\n```"),
			isShowcase: true,
			wantStatus: "fail",
			wantDetail: []string{"1. Prose only", "2. Prose only too"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := CheckIGPerItemCode(context.Background(), tt.content, tt.isShowcase)
			shim := make([]stepCheckShim, 0, len(got))
			for _, c := range got {
				shim = append(shim, stepCheckShim{Name: c.Name, Status: c.Status, Detail: c.Detail})
			}
			if tt.wantStatus == "" {
				if len(shim) != 0 {
					t.Errorf("expected nil, got %+v", shim)
				}
				return
			}
			check := findCheck(shim, "integration_guide_per_item_code")
			if check == nil {
				t.Fatalf("expected integration_guide_per_item_code, got %+v", shim)
			}
			if check.Status != tt.wantStatus {
				t.Errorf("status: got %q, want %q (detail: %s)", check.Status, tt.wantStatus, check.Detail)
			}
			for _, w := range tt.wantDetail {
				if !strings.Contains(check.Detail, w) {
					t.Errorf("detail missing %q; full: %s", w, check.Detail)
				}
			}
		})
	}
}
