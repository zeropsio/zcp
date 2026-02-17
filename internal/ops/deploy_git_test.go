// Tests for: ops/deploy.go — Git/command helper tests.
package ops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
		setup      string
		workDir    string
		includeGit bool
		freshGit   bool
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
			name:      "with setup command",
			authInfo:  testAuthInfo(),
			serviceID: "svc-456",
			setup:     "npm install",
			workDir:   "/opt/app",
			wantParts: []string{
				"test -d .git",
				"git init -q",
				"git config user.email 'test@example.com'",
				"git config user.name 'Test User'",
				"npm install",
				"zcli push --serviceId svc-456",
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
				"test -d .git",
				"fra1",
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
			name:      "freshGit removes existing .git and reinitializes",
			authInfo:  testAuthInfo(),
			serviceID: "svc-123",
			workDir:   "/var/www",
			freshGit:  true,
			wantParts: []string{
				"rm -rf .git",
				"git init -q",
				"git config user.email 'test@example.com'",
				"git config user.name 'Test User'",
				"git add -A",
				"git commit -q -m 'deploy'",
				"zcli push --serviceId svc-123",
			},
			wantAbsent: []string{
				"test -d .git",
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
			cmd := buildSSHCommand(tt.authInfo, tt.serviceID, tt.setup, tt.workDir, tt.includeGit, id, tt.freshGit)

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

func TestBuildZcliArgs_IncludeGit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		serviceID  string
		workingDir string
		includeGit bool
		wantArgs   []string
	}{
		{
			name:       "without includeGit",
			serviceID:  "svc-1",
			workingDir: "/tmp/app",
			wantArgs:   []string{"push", "--serviceId", "svc-1", "--workingDir", "/tmp/app"},
		},
		{
			name:       "with includeGit",
			serviceID:  "svc-1",
			workingDir: "/tmp/app",
			includeGit: true,
			wantArgs:   []string{"push", "--serviceId", "svc-1", "--workingDir", "/tmp/app", "-g"},
		},
		{
			name:       "includeGit without workingDir",
			serviceID:  "svc-1",
			includeGit: true,
			wantArgs:   []string{"push", "--serviceId", "svc-1", "-g"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			args := buildZcliArgs(tt.serviceID, tt.workingDir, tt.includeGit)

			if len(args) != len(tt.wantArgs) {
				t.Fatalf("args count = %d, want %d\ngot: %v\nwant: %v", len(args), len(tt.wantArgs), args, tt.wantArgs)
			}
			for i, arg := range args {
				if arg != tt.wantArgs[i] {
					t.Errorf("args[%d] = %q, want %q", i, arg, tt.wantArgs[i])
				}
			}
		})
	}
}

func TestPrepareGitRepo(t *testing.T) {
	t.Parallel()

	defaultID := GitIdentity{Name: "Test User", Email: "test@example.com"}

	tests := []struct {
		name          string
		id            GitIdentity
		freshGit      bool
		setupDir      func(t *testing.T, dir string)
		wantGitDir    bool
		wantCommit    bool
		wantNoOp      bool // existing repo should not be modified
		wantEmail     string
		wantName      string
		wantNewCommit bool // HEAD should differ from before
	}{
		{
			name: "dir without .git creates repo with commit",
			id:   defaultID,
			setupDir: func(t *testing.T, dir string) {
				t.Helper()
				// Create a file so git add -A has something to commit.
				if err := os.WriteFile(filepath.Join(dir, "index.ts"), []byte("console.log('hi')"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantGitDir: true,
			wantCommit: true,
			wantEmail:  "test@example.com",
			wantName:   "Test User",
		},
		{
			name: "dir with existing .git is no-op when freshGit false",
			id:   defaultID,
			setupDir: func(t *testing.T, dir string) {
				t.Helper()
				// Initialize a real git repo with a commit.
				runGit(t, dir, "init", "-q")
				runGit(t, dir, "config", "user.email", "test@test.com")
				runGit(t, dir, "config", "user.name", "test")
				if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644); err != nil {
					t.Fatal(err)
				}
				runGit(t, dir, "add", "-A")
				runGit(t, dir, "commit", "-q", "-m", "initial")
			},
			wantGitDir: true,
			wantCommit: true,
			wantNoOp:   true,
		},
		{
			name: "empty dir creates repo with allow-empty commit",
			id:   defaultID,
			setupDir: func(t *testing.T, _ string) {
				t.Helper()
				// No files — prepareGitRepo should use --allow-empty.
			},
			wantGitDir: true,
			wantCommit: true,
		},
		{
			name:     "freshGit reinitializes existing repo",
			id:       defaultID,
			freshGit: true,
			setupDir: func(t *testing.T, dir string) {
				t.Helper()
				runGit(t, dir, "init", "-q")
				runGit(t, dir, "config", "user.email", "old@test.com")
				runGit(t, dir, "config", "user.name", "old")
				if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644); err != nil {
					t.Fatal(err)
				}
				runGit(t, dir, "add", "-A")
				runGit(t, dir, "commit", "-q", "-m", "original")
			},
			wantGitDir:    true,
			wantCommit:    true,
			wantNewCommit: true,
			wantEmail:     "test@example.com",
			wantName:      "Test User",
		},
		{
			name: "custom identity appears in git config",
			id:   GitIdentity{Name: "Deploy Bot", Email: "deploy@company.io"},
			setupDir: func(t *testing.T, dir string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte("// app"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantGitDir: true,
			wantCommit: true,
			wantEmail:  "deploy@company.io",
			wantName:   "Deploy Bot",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			tt.setupDir(t, dir)

			// Capture HEAD before if we expect no-op or want to check new commit.
			var headBefore string
			if tt.wantNoOp || tt.wantNewCommit {
				headBefore = gitHead(t, dir)
			}

			err := prepareGitRepo(context.Background(), dir, tt.id, tt.freshGit)
			if err != nil {
				t.Fatalf("prepareGitRepo() error: %v", err)
			}

			// Check .git exists.
			if tt.wantGitDir {
				info, err := os.Stat(filepath.Join(dir, ".git"))
				if err != nil {
					t.Fatalf(".git directory should exist: %v", err)
				}
				if !info.IsDir() {
					t.Fatal(".git should be a directory")
				}
			}

			// Check commit exists.
			if tt.wantCommit {
				head := gitHead(t, dir)
				if head == "" {
					t.Fatal("expected at least one commit, got none")
				}
			}

			// No-op: HEAD should be unchanged.
			if tt.wantNoOp {
				headAfter := gitHead(t, dir)
				if headBefore != headAfter {
					t.Errorf("HEAD changed: before=%s, after=%s", headBefore, headAfter)
				}
			}

			// freshGit: HEAD should differ from before.
			if tt.wantNewCommit {
				headAfter := gitHead(t, dir)
				if headBefore == headAfter {
					t.Errorf("HEAD should have changed with freshGit, but stayed at %s", headBefore)
				}
			}

			// Check git config identity.
			if tt.wantEmail != "" {
				email := gitConfig(t, dir, "user.email")
				if email != tt.wantEmail {
					t.Errorf("git user.email = %q, want %q", email, tt.wantEmail)
				}
			}
			if tt.wantName != "" {
				name := gitConfig(t, dir, "user.name")
				if name != tt.wantName {
					t.Errorf("git user.name = %q, want %q", name, tt.wantName)
				}
			}
		})
	}
}

// gitConfig reads a git config value from the repo in dir.
func gitConfig(t *testing.T, dir, key string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", "config", key)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out[:len(out)-1]) // trim newline
}

// runGit executes a git command in the given directory.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

// gitHead returns the HEAD commit hash, or empty string if no commits.
func gitHead(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out[:len(out)-1]) // trim newline
}
