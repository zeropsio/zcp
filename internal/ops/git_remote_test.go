package ops

import "testing"

// TestParseGitRemoteOwnerRepo pins the three shapes ServiceMeta.RemoteURL
// may carry. Used by handleBuildIntegration to splice into prefilled
// `gh secret set -R owner/repo` snippets for the Actions integration
// handoff — a parse miss falls back to placeholder text in the response,
// so this needs to cover the realistic forms users paste.
func TestParseGitRemoteOwnerRepo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		remote    string
		wantOwner string
		wantRepo  string
		wantOK    bool
	}{
		{name: "https with .git suffix", remote: "https://github.com/owner/repo.git", wantOwner: "owner", wantRepo: "repo", wantOK: true},
		{name: "https without .git suffix", remote: "https://github.com/owner/repo", wantOwner: "owner", wantRepo: "repo", wantOK: true},
		{name: "https trailing slash", remote: "https://github.com/owner/repo/", wantOwner: "owner", wantRepo: "repo", wantOK: true},
		{name: "scp-form ssh", remote: "git@github.com:owner/repo.git", wantOwner: "owner", wantRepo: "repo", wantOK: true},
		{name: "ssh URI", remote: "ssh://git@github.com/owner/repo.git", wantOwner: "owner", wantRepo: "repo", wantOK: true},
		{name: "gitlab https", remote: "https://gitlab.com/group/sub/repo.git", wantOwner: "sub", wantRepo: "repo", wantOK: true},
		{name: "empty", remote: "", wantOK: false},
		{name: "whitespace only", remote: "   ", wantOK: false},
		{name: "no path", remote: "https://github.com/", wantOK: false},
		{name: "single path segment", remote: "https://github.com/justone", wantOK: false},
		{name: "scp form no path", remote: "git@github.com:", wantOK: false},
		{name: "garbage", remote: "not-a-url", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotOwner, gotRepo, gotOK := ParseGitRemoteOwnerRepo(tt.remote)
			if gotOK != tt.wantOK {
				t.Errorf("ParseGitRemoteOwnerRepo(%q) ok = %v, want %v", tt.remote, gotOK, tt.wantOK)
			}
			if gotOwner != tt.wantOwner {
				t.Errorf("ParseGitRemoteOwnerRepo(%q) owner = %q, want %q", tt.remote, gotOwner, tt.wantOwner)
			}
			if gotRepo != tt.wantRepo {
				t.Errorf("ParseGitRemoteOwnerRepo(%q) repo = %q, want %q", tt.remote, gotRepo, tt.wantRepo)
			}
		})
	}
}
