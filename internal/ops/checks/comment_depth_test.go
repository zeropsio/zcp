package checks

import (
	"context"
	"strings"
	"testing"
)

func TestCheckCommentDepth_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		content    string
		wantStatus string
		wantDetail []string
	}{
		{
			name:       "under 3 blocks passes silently",
			content:    "# this is the only comment block that exists here\n\nservices:\n  api:\n    type: nodejs\n",
			wantStatus: "pass",
		},
		{
			name: "enough reasoning blocks pass",
			content: `# minContainers: 2 because rolling deploys drop in-flight requests otherwise
services:
  api:
# build-time caches enable predictable npm install during redeploy
  app:
# rotation must not drop secrets, so always configure trust proxy
  worker:
# this tier mirrors prod — we chose standard cache behavior`,
			wantStatus: "pass",
			wantDetail: []string{"explain WHY"},
		},
		{
			name: "all narration comments fail",
			content: `# npm install for dependencies
services:
  api:
# node_modules cache between builds
  app:
# standard build for prod
  worker:
# static file serving enabled`,
			wantStatus: "fail",
			wantDetail: []string{"explain WHY a decision"},
		},
		{
			name: "trailing EOF block counted",
			content: `services:
  api:
# because rolling deploys need drain before exit
# otherwise in-flight requests are lost
# prevents the split-brain scenario

# so that the rotation completes
# without dropping traffic
# always configure trust proxy`,
			wantStatus: "pass",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := CheckCommentDepth(context.Background(), tt.content, "env")
			shim := make([]stepCheckShim, 0, len(got))
			for _, c := range got {
				shim = append(shim, stepCheckShim{Name: c.Name, Status: c.Status, Detail: c.Detail})
			}
			check := findCheck(shim, "env_comment_depth")
			if check == nil {
				t.Fatalf("expected env_comment_depth, got %+v", shim)
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
