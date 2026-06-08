package topology

import "testing"

func TestCanonicalRepoURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		in, want string
	}{
		// THE bug: a trailing ".git" makes Zerops buildFromGit clone-preflight
		// reject the import (terminal FAILED in ~0.3s, no build logs). The
		// canonical form drops it.
		{"https with .git", "https://github.com/owner/repo.git", "https://github.com/owner/repo"},
		{"https no suffix", "https://github.com/owner/repo", "https://github.com/owner/repo"},
		{"https trailing slash", "https://github.com/owner/repo/", "https://github.com/owner/repo"},
		{"https .git/ combo", "https://github.com/owner/repo.git/", "https://github.com/owner/repo"},
		{"surrounding whitespace", "  https://github.com/owner/repo.git\n", "https://github.com/owner/repo"},
		{"scp ssh form with .git", "git@github.com:owner/repo.git", "git@github.com:owner/repo"},
		{"gitlab subgroup .git", "https://gitlab.com/grp/sub/repo.git", "https://gitlab.com/grp/sub/repo"},
		{"empty", "", ""},
		// Host/scheme/case/auth are repo IDENTITY-irrelevant suffix only —
		// never rewritten. A repo literally named "...git" (no dot) keeps it.
		{"no dot-git not stripped", "https://github.com/owner/mygit", "https://github.com/owner/mygit"},
		{"case preserved", "https://GitHub.com/Owner/Repo.git", "https://GitHub.com/Owner/Repo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := CanonicalRepoURL(tt.in); got != tt.want {
				t.Errorf("CanonicalRepoURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestCanonicalRepoURL_Idempotent pins that canonicalizing twice equals
// canonicalizing once — required so emit sites and the gate/cache compare
// sites can apply it freely without double-strip surprises.
func TestRedactRepoURLCredentials(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in, want string
	}{
		{"pat user+token", "https://octocat:ghp_abcd1234@github.com/owner/repo.git", "https://***@github.com/owner/repo.git"},
		{"token as user only", "https://ghp_abcd1234@github.com/owner/repo", "https://***@github.com/owner/repo"},
		{"http embedded", "http://user:tok@github.com/owner/repo", "http://***@github.com/owner/repo"},
		{"clean https unchanged", "https://github.com/owner/repo.git", "https://github.com/owner/repo.git"},
		{"scp-form unchanged (host login, not secret)", "git@github.com:owner/repo.git", "git@github.com:owner/repo.git"},
		{"ssh scheme unchanged", "ssh://git@github.com/owner/repo", "ssh://git@github.com/owner/repo"},
		{"empty unchanged", "", ""},
		{"garbage unchanged", "not a url", "not a url"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := RedactRepoURLCredentials(tt.in); got != tt.want {
				t.Errorf("RedactRepoURLCredentials(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestCanonicalRepoURL_Idempotent(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"https://github.com/owner/repo.git",
		"https://github.com/owner/repo.git/",
		"git@github.com:owner/repo.git",
		"https://github.com/owner/repo",
		"",
	} {
		once := CanonicalRepoURL(in)
		if twice := CanonicalRepoURL(once); twice != once {
			t.Errorf("not idempotent for %q: once=%q twice=%q", in, once, twice)
		}
	}
}
