// Tests for: ops/deploy.go — Git/command helper tests.
package ops

import (
	"testing"

	"github.com/zeropsio/zcp/internal/auth"
)

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && contains(s, sub))
}

func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestBuildSSHCommand_GitGuard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		authInfo   auth.Info
		serviceID  string
		workDir    string
		includeGit bool
		wantParts  []string
		wantAbsent []string
	}{
		{
			name:      "basic command contains git guard with user identity",
			authInfo:  testAuthInfo(),
			serviceID: "svc-123",
			workDir:   "/var/www",
			wantParts: []string{
				"test -d .git",
				"git init -q",
				"git config user.email 'test@example.com'",
				"git config user.name 'Test User'",
				"git add -A",
				"git commit -q -m 'deploy'",
				"(test -d .git || (git init -q",
				"zcli push --serviceId svc-123",
			},
			wantAbsent: []string{
				"rm -rf .git",
			},
		},
		{
			name: "with different region",
			authInfo: auth.Info{
				Token:   "my-token",
				APIHost: "api.app-fra1.zerops.io",
				Region:  "fra1",
			},
			serviceID: "svc-789",
			workDir:   "/var/www",
			wantParts: []string{
				"zcli login my-token",
				"test -d .git",
				"zcli push --serviceId svc-789",
			},
		},
		{
			name:       "with includeGit flag",
			authInfo:   testAuthInfo(),
			serviceID:  "svc-123",
			workDir:    "/var/www",
			includeGit: true,
			wantParts: []string{
				"zcli push --serviceId svc-123 -g",
			},
		},
		{
			name: "custom email and name in command",
			authInfo: auth.Info{
				Token:    "my-token",
				APIHost:  "api.app-prg1.zerops.io",
				Region:   "prg1",
				Email:    "deploy@company.io",
				FullName: "Deploy Bot",
			},
			serviceID: "svc-100",
			workDir:   "/var/www",
			wantParts: []string{
				"git config user.email 'deploy@company.io'",
				"git config user.name 'Deploy Bot'",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			id := GitIdentity{Name: tt.authInfo.FullName, Email: tt.authInfo.Email}
			cmd := buildSSHCommand(tt.authInfo, tt.serviceID, tt.workDir, tt.includeGit, id)

			for _, part := range tt.wantParts {
				if !contains(cmd, part) {
					t.Errorf("command missing %q\ngot: %s", part, cmd)
				}
			}
			for _, absent := range tt.wantAbsent {
				if contains(cmd, absent) {
					t.Errorf("command should NOT contain %q\ngot: %s", absent, cmd)
				}
			}
		})
	}
}
