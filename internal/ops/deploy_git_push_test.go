package ops

import "testing"

func TestBuildGitPushCommand_Basic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		workDir   string
		remoteURL string
		branch    string
		id        GitIdentity
		wantParts []string // substrings that must appear in the command
		skipParts []string // substrings that must NOT appear
	}{
		{
			name:      "full params",
			workDir:   "/var/www",
			remoteURL: "https://github.com/user/repo",
			branch:    "main",
			id:        GitIdentity{Name: "Test User", Email: "test@example.com"},
			wantParts: []string{
				"trap 'rm -f ~/.netrc' EXIT",
				"machine github.com login oauth2 password $GIT_TOKEN",
				"chmod 600 ~/.netrc",
				"cd /var/www",
				"git config user.email",
				"git remote add origin 'https://github.com/user/repo'",
				"git remote set-url origin 'https://github.com/user/repo'",
				"git push -u origin main",
			},
			// Pre-flight gates ensure .git + HEAD exist before this command runs.
			// The old init+auto-commit fallbacks are gone (they masked "agent
			// forgot to commit" bugs). See plan phase A.2.
			skipParts: []string{
				"git init",
				"git rev-parse HEAD",
				"initial commit",
				"git add -A",
			},
		},
		{
			name:      "custom branch",
			workDir:   "/var/www",
			remoteURL: "https://github.com/user/repo",
			branch:    "develop",
			id:        GitIdentity{Name: "Test", Email: "t@t.com"},
			wantParts: []string{
				"git push -u origin develop",
			},
			skipParts: []string{
				"git init",
			},
		},
		{
			name:      "default branch when empty",
			workDir:   "/var/www",
			remoteURL: "https://github.com/user/repo",
			branch:    "",
			id:        GitIdentity{Name: "Test", Email: "t@t.com"},
			wantParts: []string{
				"git push -u origin main",
			},
		},
		{
			name:      "no remoteURL — skip remote setup",
			workDir:   "/var/www",
			remoteURL: "",
			branch:    "main",
			id:        GitIdentity{Name: "Test", Email: "t@t.com"},
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
			id:        GitIdentity{Name: "Test", Email: "t@t.com"},
			wantParts: []string{
				"machine gitlab.com login oauth2 password $GIT_TOKEN",
			},
		},
		{
			name:      "custom host in netrc",
			workDir:   "/var/www",
			remoteURL: "https://git.mycompany.com/team/repo",
			branch:    "main",
			id:        GitIdentity{Name: "Test", Email: "t@t.com"},
			wantParts: []string{
				"machine git.mycompany.com login oauth2 password $GIT_TOKEN",
			},
		},
		{
			name:      "remoteURL shell-quoted",
			workDir:   "/var/www",
			remoteURL: "https://github.com/user/repo with spaces",
			branch:    "main",
			id:        GitIdentity{Name: "Test", Email: "t@t.com"},
			wantParts: []string{
				"'https://github.com/user/repo with spaces'",
			},
		},
		{
			name:      "identity shell-quoted",
			workDir:   "/var/www",
			remoteURL: "https://github.com/user/repo",
			branch:    "main",
			id:        GitIdentity{Name: "O'Brien", Email: "test@example.com"},
			wantParts: []string{
				"'O'\\''Brien'",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd := BuildGitPushCommand(tt.workDir, tt.remoteURL, tt.branch, tt.id)
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
