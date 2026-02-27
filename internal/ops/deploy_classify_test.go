// Tests for: ops/deploy_classify.go — SSH error classification.
package ops

import (
	"fmt"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestClassifySSHError_NotInGitDirectory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		errMsg         string
		wantCode       string
		wantSuggestion string
	}{
		{
			name:           "fatal not in a git directory",
			errMsg:         "fatal: not in a git directory",
			wantCode:       platform.ErrSSHDeployFailed,
			wantSuggestion: "freshGit",
		},
		{
			name:           "not a git repository",
			errMsg:         "fatal: not a git repository (or any parent up to mount point /)",
			wantCode:       platform.ErrSSHDeployFailed,
			wantSuggestion: "freshGit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pe := classifySSHError(fmt.Errorf("%s", tt.errMsg), "builder", "app")
			if pe.Code != tt.wantCode {
				t.Errorf("code = %s, want %s", pe.Code, tt.wantCode)
			}
			if !contains(pe.Suggestion, tt.wantSuggestion) {
				t.Errorf("suggestion should mention %q, got: %s", tt.wantSuggestion, pe.Suggestion)
			}
		})
	}
}
