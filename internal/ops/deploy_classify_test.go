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

func TestClassifySSHError_NewPatterns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		errMsg         string
		wantMessage    string
		wantSuggestion string
	}{
		{
			name:           "no space left on device",
			errMsg:         "write /var/www/app.js: no space left on device",
			wantMessage:    "disk full",
			wantSuggestion: "Scale up",
		},
		{
			name:           "disk quota exceeded",
			errMsg:         "disk quota exceeded during git commit",
			wantMessage:    "disk full",
			wantSuggestion: "Scale up",
		},
		{
			name:           "permission denied",
			errMsg:         "open /var/www/.config: permission denied",
			wantMessage:    "permission denied",
			wantSuggestion: "buildCommands",
		},
		{
			name:           "module not found",
			errMsg:         "Error: module not found: github.com/example/pkg",
			wantMessage:    "module not found",
			wantSuggestion: "install command",
		},
		{
			name:           "cannot find module",
			errMsg:         "Error: cannot find module 'express'",
			wantMessage:    "module not found",
			wantSuggestion: "install command",
		},
		{
			name:           "exec format error",
			errMsg:         "exec /var/www/app: exec format error",
			wantMessage:    "exec format error",
			wantSuggestion: "linux/amd64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pe := classifySSHError(fmt.Errorf("%s", tt.errMsg), "builder", "app")
			if pe.Code != platform.ErrSSHDeployFailed {
				t.Errorf("code = %s, want %s", pe.Code, platform.ErrSSHDeployFailed)
			}
			if !contains(pe.Message, tt.wantMessage) {
				t.Errorf("message should mention %q, got: %s", tt.wantMessage, pe.Message)
			}
			if !contains(pe.Suggestion, tt.wantSuggestion) {
				t.Errorf("suggestion should mention %q, got: %s", tt.wantSuggestion, pe.Suggestion)
			}
		})
	}
}
