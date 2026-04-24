package ops

import "testing"

func TestBuildGitPushCommand_Basic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		workDir   string
		remoteURL string
		branch    string
		wantParts []string // substrings that must appear in the command
		skipParts []string // substrings that must NOT appear
	}{
		{
			name:      "full params",
			workDir:   "/var/www",
			remoteURL: "https://github.com/user/repo",
			branch:    "main",
			wantParts: []string{
				"trap 'rm -f ~/.netrc' EXIT",
				"machine github.com login oauth2 password $GIT_TOKEN",
				"chmod 600 ~/.netrc",
				"cd /var/www",
				"git remote add origin 'https://github.com/user/repo'",
				"git remote set-url origin 'https://github.com/user/repo'",
				"git push -u origin main",
			},
			// Pre-flight gates ensure .git + HEAD exist before this command
			// runs. The old init+auto-commit fallbacks are gone (they masked
			// "agent forgot to commit" bugs). Identity config is gone too
			// (redundant with InitServiceGit at bootstrap — GLC-3).
			skipParts: []string{
				"git init",
				"git rev-parse HEAD",
				"initial commit",
				"git add -A",
				"git config user.email",
				"git config user.name",
			},
		},
		{
			name:      "custom branch",
			workDir:   "/var/www",
			remoteURL: "https://github.com/user/repo",
			branch:    "develop",
			wantParts: []string{
				"git push -u origin develop",
			},
			skipParts: []string{
				"git init",
				"git config user",
			},
		},
		{
			name:      "default branch when empty",
			workDir:   "/var/www",
			remoteURL: "https://github.com/user/repo",
			branch:    "",
			wantParts: []string{
				"git push -u origin main",
			},
		},
		{
			name:      "no remoteURL — skip remote setup",
			workDir:   "/var/www",
			remoteURL: "",
			branch:    "main",
			wantParts: []string{
				"git push -u origin main",
			},
			skipParts: []string{
				"git remote add",
				"git remote set-url",
			},
		},
		{
			name:      "gitlab host in netrc",
			workDir:   "/var/www",
			remoteURL: "https://gitlab.com/user/repo.git",
			branch:    "main",
			wantParts: []string{
				"machine gitlab.com login oauth2 password $GIT_TOKEN",
			},
		},
		{
			name:      "custom host in netrc",
			workDir:   "/var/www",
			remoteURL: "https://git.mycompany.com/team/repo",
			branch:    "main",
			wantParts: []string{
				"machine git.mycompany.com login oauth2 password $GIT_TOKEN",
			},
		},
		{
			name:      "remoteURL shell-quoted",
			workDir:   "/var/www",
			remoteURL: "https://github.com/user/repo with spaces",
			branch:    "main",
			wantParts: []string{
				"'https://github.com/user/repo with spaces'",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd := BuildGitPushCommand(tt.workDir, tt.remoteURL, tt.branch)
			for _, want := range tt.wantParts {
				if !containsSubstring(cmd, want) {
					t.Errorf("command missing %q\ngot: %s", want, cmd)
				}
			}
			for _, skip := range tt.skipParts {
				if containsSubstring(cmd, skip) {
					t.Errorf("command should NOT contain %q\ngot: %s", skip, cmd)
				}
			}
		})
	}
}

// TestBuildGitPushCommand_NoInlineIdentity locks the Fix 6 cleanup: no
// caller-controlled GitIdentity (removed from signature) and no inline
// `git config user.*` statements in the emitted shell. A regression that
// re-adds identity would either put it outside the pre-flight guard (bad —
// nothing to guard against, wasted SSH work per deploy) or duplicate
// InitServiceGit's bootstrap-time write (worse — two sources of truth
// for the same value, GLC-3 violated).
func TestBuildGitPushCommand_NoInlineIdentity(t *testing.T) {
	t.Parallel()

	cmd := BuildGitPushCommand("/var/www", "https://github.com/x/y", "main")
	for _, forbidden := range []string{
		"git config user.email",
		"git config user.name",
		"git config --local user.",
		"git -c user.email",
	} {
		if containsSubstring(cmd, forbidden) {
			t.Errorf("BuildGitPushCommand must not emit identity config (%q): %s", forbidden, cmd)
		}
	}
}

func TestParseGitHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
		want string
	}{
		{"github https", "https://github.com/user/repo", "github.com"},
		{"github https with .git", "https://github.com/user/repo.git", "github.com"},
		{"gitlab https", "https://gitlab.com/user/repo", "gitlab.com"},
		{"custom domain", "https://git.mycompany.com/team/repo", "git.mycompany.com"},
		{"with port", "https://git.example.com:8443/repo", "git.example.com"},
		{"http scheme", "http://github.com/user/repo", "github.com"},
		{"empty string", "", "github.com"},
		{"no scheme fallback", "github.com/user/repo", "github.com"},
		{"with trailing slash", "https://github.com/user/repo/", "github.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseGitHost(tt.url)
			if got != tt.want {
				t.Errorf("parseGitHost(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}
