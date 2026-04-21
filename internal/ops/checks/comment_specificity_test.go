package checks

import (
	"context"
	"strings"
	"testing"
)

func TestCheckCommentSpecificity_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		yaml       string
		isShowcase bool
		wantStatus string
		wantDetail []string
	}{
		{
			name:       "minimal tier returns nil",
			yaml:       "# zerops because\n# execOnce for advisory lock\n",
			isShowcase: false,
			wantStatus: "",
		},
		{
			name:       "no comments returns nil",
			yaml:       "zerops:\n  - setup: dev\n",
			isShowcase: true,
			wantStatus: "",
		},
		{
			name:       "all boilerplate fails",
			yaml:       "# npm ci for reproducible builds\n# cache node_modules between builds\n# fast dev iteration\n# standard prod build\n",
			isShowcase: true,
			wantStatus: "fail",
			wantDetail: []string{"specificity too low"},
		},
		{
			name:       "3 specific + 1 boilerplate passes",
			yaml:       "# npm ci ensures reproducible builds\n# execOnce prevents duplicate seed\n# trust proxy required for L7 balancer\n# standard cache behavior\n",
			isShowcase: true,
			wantStatus: "pass",
			wantDetail: []string{"3 of 4"},
		},
		{
			name: "specific-count above threshold but ratio dips below fails",
			yaml: `# npm ci because we need reproducible builds
# execOnce prevents duplicates
# trust proxy required for L7
# cache npm
# cache node_modules
# small prod
# typical
# default config
# standard build
# nothing special
# boilerplate line
# more boilerplate
# yet another generic comment
# and another
# and one more`,
			isShowcase: true,
			wantStatus: "fail",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := CheckCommentSpecificity(context.Background(), tt.yaml, tt.isShowcase)
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
			check := findCheck(shim, "comment_specificity")
			if check == nil {
				t.Fatalf("expected comment_specificity, got %+v", shim)
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
