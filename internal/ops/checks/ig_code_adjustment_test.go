package checks

import (
	"context"
	"strings"
	"testing"
)

// igContent wraps body with the EXTRACT fragment markers the predicate
// expects so each test case can focus on the fragment body itself.
func igContent(body string) string {
	return "# Intro\n\n" +
		"<!-- #ZEROPS_EXTRACT_START:integration-guide# -->\n" +
		body +
		"\n<!-- #ZEROPS_EXTRACT_END:integration-guide# -->\n"
}

func TestCheckIGCodeAdjustment_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		content    string
		isShowcase bool
		wantStatus string
		wantDetail []string
	}{
		{
			name:       "minimal tier returns nil (out of scope)",
			content:    igContent("```yaml\nzerops:\n  - setup: dev\n```"),
			isShowcase: false,
			wantStatus: "",
		},
		{
			name:       "missing fragment returns nil (upstream fragment_* reports)",
			content:    "# No fragment here\nsome prose",
			isShowcase: true,
			wantStatus: "",
		},
		{
			name:       "no code blocks returns nil (upstream reports 'no yaml block')",
			content:    igContent("## Prose only\n\nNo fences at all."),
			isShowcase: true,
			wantStatus: "",
		},
		{
			name:       "yaml-only fails",
			content:    igContent("### 1. zerops.yaml\n```yaml\nzerops:\n  - setup: dev\n```"),
			isShowcase: true,
			wantStatus: "fail",
			wantDetail: []string{"zerops.yaml only"},
		},
		{
			name: "yaml + typescript passes",
			content: igContent(`### 1. zerops.yaml
` + "```yaml\nzerops:\n  - setup: dev\n```" + `

### 2. Trust proxy
` + "```typescript\napp.set('trust proxy', 1);\n```"),
			isShowcase: true,
			wantStatus: "pass",
			wantDetail: []string{"typescript"},
		},
		{
			name: "multiple non-yaml blocks listed",
			content: igContent(`` + "```yaml\nzerops: []\n```" + `
` + "```php\n<?php CORS::allow();\n```" + `
` + "```bash\nnpm run build\n```"),
			isShowcase: true,
			wantStatus: "pass",
			wantDetail: []string{"php", "bash"},
		},
		{
			name: "untagged fence ignored",
			content: igContent(`` + "```\nuntagged\n```" + `
` + "```yaml\nzerops: []\n```"),
			isShowcase: true,
			wantStatus: "fail",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := CheckIGCodeAdjustment(context.Background(), tt.content, tt.isShowcase)
			shim := make([]stepCheckShim, 0, len(got))
			for _, c := range got {
				shim = append(shim, stepCheckShim{Name: c.Name, Status: c.Status, Detail: c.Detail})
			}
			if tt.wantStatus == "" {
				if len(shim) != 0 {
					t.Errorf("expected no check emitted, got %+v", shim)
				}
				return
			}
			check := findCheck(shim, "integration_guide_code_adjustment")
			if check == nil {
				t.Fatalf("expected integration_guide_code_adjustment check, got %+v", shim)
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

func TestUniqueStrings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   []string
		want []string
	}{
		{in: nil, want: []string{}},
		{in: []string{}, want: []string{}},
		{in: []string{"a", "b", "a"}, want: []string{"a", "b"}},
		{in: []string{"a", "a", "a"}, want: []string{"a"}},
	}
	for _, tt := range tests {
		got := uniqueStrings(tt.in)
		if len(got) != len(tt.want) {
			t.Errorf("uniqueStrings(%v)=%v, want %v", tt.in, got, tt.want)
			continue
		}
		for i, v := range got {
			if v != tt.want[i] {
				t.Errorf("uniqueStrings(%v)[%d]=%q, want %q", tt.in, i, v, tt.want[i])
			}
		}
	}
}
