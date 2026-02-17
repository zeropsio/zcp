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
		wantParts  []string
	}{
		{
			name:      "basic command contains git guard",
			authInfo:  testAuthInfo(),
			serviceID: "svc-123",
			workDir:   "/var/www",
			wantParts: []string{
				"test -d .git",
				"git init -q",
				"git add -A",
				"git commit -q -m 'deploy'",
				"(test -d .git || (git init -q",
				"zcli push --serviceId svc-123",
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
				"zcli push --serviceId svc-123 -G",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := buildSSHCommand(tt.authInfo, tt.serviceID, tt.setup, tt.workDir, tt.includeGit)

			for _, part := range tt.wantParts {
				if !contains(cmd, part) {
					t.Errorf("command missing %q\ngot: %s", part, cmd)
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
			wantArgs:   []string{"push", "--serviceId", "svc-1", "--workingDir", "/tmp/app", "-G"},
		},
		{
			name:       "includeGit without workingDir",
			serviceID:  "svc-1",
			includeGit: true,
			wantArgs:   []string{"push", "--serviceId", "svc-1", "-G"},
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

	tests := []struct {
		name       string
		setupDir   func(t *testing.T, dir string)
		wantGitDir bool
		wantCommit bool
		wantNoOp   bool // existing repo should not be modified
	}{
		{
			name: "dir without .git creates repo with commit",
			setupDir: func(t *testing.T, dir string) {
				t.Helper()
				// Create a file so git add -A has something to commit.
				if err := os.WriteFile(filepath.Join(dir, "index.ts"), []byte("console.log('hi')"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantGitDir: true,
			wantCommit: true,
		},
		{
			name: "dir with existing .git is no-op",
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
			setupDir: func(t *testing.T, _ string) {
				t.Helper()
				// No files — prepareGitRepo should use --allow-empty.
			},
			wantGitDir: true,
			wantCommit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			tt.setupDir(t, dir)

			// Capture HEAD before if we expect no-op.
			var headBefore string
			if tt.wantNoOp {
				headBefore = gitHead(t, dir)
			}

			err := prepareGitRepo(context.Background(), dir)
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
		})
	}
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
