package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/runtime"
)

// Plan §P7 F2 — walkthrough steps[] field gives the agent an
// enumerable, machine-readable view of the call sequence. Container
// mode (3 steps: collect → probe-confirm-with-token → wire CI); local
// mode (2 steps: collect → combined probe + CI). The same prose lives
// in gitPushWalkthroughNextStep but agents that render checklists
// without re-parsing natural language consume this slice instead.
func TestGitPushWalkthroughSteps_ContainerVsLocal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		rt           runtime.Info
		service      string
		wantCount    int
		wantStep1    string
		wantStep2Has string
	}{
		{
			name:         "container — 3 steps with PAT",
			rt:           runtime.Info{InContainer: true},
			service:      "appdev",
			wantCount:    3,
			wantStep1:    "Collect inputs from user",
			wantStep2Has: "gitToken=<PAT>",
		},
		{
			name:         "local — 2 steps no PAT",
			rt:           runtime.Info{InContainer: false},
			service:      "appdev",
			wantCount:    2,
			wantStep1:    "Collect inputs from user",
			wantStep2Has: "remoteUrl=<url>",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			steps := gitPushWalkthroughSteps(tt.rt, tt.service)
			if len(steps) != tt.wantCount {
				t.Fatalf("step count: got %d, want %d", len(steps), tt.wantCount)
			}
			// N is always 1-indexed and monotonic.
			for i, s := range steps {
				if s.N != i+1 {
					t.Errorf("steps[%d].N: got %d, want %d (1-indexed monotonic)", i, s.N, i+1)
				}
				if s.Title == "" {
					t.Errorf("steps[%d].Title empty", i)
				}
				if s.Call == "" {
					t.Errorf("steps[%d].Call empty", i)
				}
			}
			if steps[0].Title != tt.wantStep1 {
				t.Errorf("steps[0].Title: got %q, want %q", steps[0].Title, tt.wantStep1)
			}
			if !strings.Contains(steps[1].Call, tt.wantStep2Has) {
				t.Errorf("steps[1].Call: got %q, want substring %q",
					steps[1].Call, tt.wantStep2Has)
			}
		})
	}
}

// TestValidateRemoteURL_RejectsEmbeddedCredential pins B10b: a remote URL with
// an embedded credential (https://user:token@host/...) is refused, so the PAT
// never lands in meta.RemoteURL or the container's .git/config. Auth is via the
// gitToken PAT, not the URL. Clean https + scp-form stay accepted.
func TestValidateRemoteURL_RejectsEmbeddedCredential(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		remote    string
		wantError bool
	}{
		{"credential https rejected", "https://octocat:ghp_abcd@github.com/owner/repo.git", true},
		{"token-as-user rejected", "https://ghp_abcd@github.com/owner/repo", true},
		{"clean https accepted", "https://github.com/owner/repo.git", false},
		{"scp-form accepted (host login, not secret)", "git@github.com:owner/repo.git", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateRemoteURL(tt.remote)
			if (err != nil) != tt.wantError {
				t.Errorf("validateRemoteURL(%q): err=%v, wantError=%v", tt.remote, err, tt.wantError)
			}
			// The error itself must not echo the secret.
			if err != nil && strings.Contains(err.Error(), "ghp_abcd") {
				t.Errorf("validateRemoteURL error leaked the token: %v", err)
			}
		})
	}
}
