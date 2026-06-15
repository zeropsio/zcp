package recipe

import "testing"

func TestStartsWithH3Heading_Table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
		want bool
	}{
		{name: "gotchas_h3_bullet_passes", body: "### Gotchas\n\n- bullet", want: true},
		{name: "maintenance_h3_paragraph_passes", body: "### Maintenance Mode\n\nparagraph", want: true},
		{name: "h2_blocks", body: "## Tips and Others\n\n- bullet", want: false},
		{name: "flat_bullets_block", body: "- **Topic** — bullet", want: false},
		{name: "leading_blank_h3_passes", body: "\n\n### Gotchas\n", want: true},
		{name: "empty_skips_helper_false", body: "", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := startsWithH3Heading(tc.body); got != tc.want {
				t.Fatalf("startsWithH3Heading=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestGateRequireH3InKnowledgeBaseFragment_Table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		fragments map[string]string
		want      int
	}{
		{
			name:      "h3_fragment_passes",
			fragments: map[string]string{"codebase/api/knowledge-base": "### Gotchas\n\n- bullet"},
			want:      0,
		},
		{
			name:      "h2_fragment_blocks",
			fragments: map[string]string{"codebase/api/knowledge-base": "## Tips and Others\n\n- bullet"},
			want:      1,
		},
		{
			name:      "flat_fragment_blocks",
			fragments: map[string]string{"codebase/api/knowledge-base": "- **Topic** — bullet"},
			want:      1,
		},
		{
			name:      "missing_fragment_skips",
			fragments: map[string]string{},
			want:      0,
		},
		{
			name:      "empty_fragment_skips",
			fragments: map[string]string{"codebase/api/knowledge-base": " \n\t"},
			want:      0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan := &Plan{
				Codebases: []Codebase{{Hostname: "api", SourceRoot: "api"}},
				Fragments: tc.fragments,
			}
			vs := gateRequireH3InKnowledgeBaseFragment(GateContext{Plan: plan})
			if len(vs) != tc.want {
				t.Fatalf("len(violations)=%d, want %d: %+v", len(vs), tc.want, vs)
			}
		})
	}
}

func TestCodebaseContentGates_KBH3GateRegistered(t *testing.T) {
	t.Parallel()
	if !gateRegistered(CodebaseContentGates(), "kb-h3-required") {
		t.Fatalf("CodebaseContentGates missing kb-h3-required")
	}
}
