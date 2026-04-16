package tools

import (
	"strings"
	"testing"
)

// The bash_guard rejects pre-execution any shell command that would run
// a target-side executable (git, npm, nest, vite, tsc, pnpm, yarn, etc.)
// zcp-side against the SSHFS mount (`cd /var/www/{host} && <exec>`).
// v21 lost ~360 CPU-seconds to this pattern on 3 parallel git-add
// invocations from the main agent's bash; the postmortem's §3.2a
// upgrade catches it structurally before the shell runs.

func TestBashGuard_ZcpSideGitAdd_Rejected(t *testing.T) {
	t.Parallel()
	err := CheckBashCommand("cd /var/www/apidev && git init && git add -A")
	if err == nil {
		t.Fatal("expected rejection; got nil")
	}
	if !strings.Contains(err.Error(), "ZCP_EXECUTION_BOUNDARY_VIOLATION") {
		t.Fatalf("error must carry structured code: %s", err)
	}
}

func TestBashGuard_ZcpSideNpmInstall_Rejected(t *testing.T) {
	t.Parallel()
	err := CheckBashCommand("cd /var/www/workerdev && npm install")
	if err == nil {
		t.Fatal("expected rejection; got nil")
	}
}

func TestBashGuard_ZcpSideNest_Rejected(t *testing.T) {
	t.Parallel()
	err := CheckBashCommand("cd /var/www/apidev && npx nest build")
	if err == nil {
		t.Fatal("expected rejection for npx nest")
	}
}

func TestBashGuard_ZcpSideVite_Rejected(t *testing.T) {
	t.Parallel()
	err := CheckBashCommand("cd /var/www/appdev && vite build")
	if err == nil {
		t.Fatal("expected rejection for vite")
	}
}

func TestBashGuard_ZcpSideTsc_Rejected(t *testing.T) {
	t.Parallel()
	err := CheckBashCommand("cd /var/www/apidev && tsc --noEmit")
	if err == nil {
		t.Fatal("expected rejection for tsc")
	}
}

func TestBashGuard_ZcpSidePnpm_Rejected(t *testing.T) {
	t.Parallel()
	err := CheckBashCommand("cd /var/www/x && pnpm install")
	if err == nil {
		t.Fatal("expected rejection for pnpm")
	}
}

func TestBashGuard_ZcpSideRailsAndComposer_Rejected(t *testing.T) {
	t.Parallel()
	for _, cmd := range []string{
		"cd /var/www/apidev && rails db:migrate",
		"cd /var/www/phpdev && composer install",
		"cd /var/www/rbdev && bundle install",
		"cd /var/www/godev && go build ./...",
		"cd /var/www/rsdev && cargo build",
		"cd /var/www/pydev && python manage.py migrate",
		"cd /var/www/phpdev && php artisan migrate",
	} {
		if err := CheckBashCommand(cmd); err == nil {
			t.Errorf("expected rejection for %q", cmd)
		}
	}
}

func TestBashGuard_SshGitAdd_Allowed(t *testing.T) {
	t.Parallel()
	if err := CheckBashCommand(`ssh apidev "cd /var/www && git add -A"`); err != nil {
		t.Fatalf("ssh-wrapped form must be allowed: %v", err)
	}
}

func TestBashGuard_SshNpmInstall_Allowed(t *testing.T) {
	t.Parallel()
	if err := CheckBashCommand(`ssh apidev "cd /var/www && npm install"`); err != nil {
		t.Fatalf("ssh-wrapped npm must be allowed: %v", err)
	}
}

func TestBashGuard_ZcpSideCatHarmless_Allowed(t *testing.T) {
	t.Parallel()
	// Reading files over the mount is fine; only executables trigger.
	cases := []string{
		"cat /var/www/apidev/package.json",
		"ls /var/www/appdev/src",
		"grep foo /var/www/apidev/src/main.ts",
		"head /var/www/workerdev/README.md",
	}
	for _, cmd := range cases {
		if err := CheckBashCommand(cmd); err != nil {
			t.Errorf("%q should pass (file read through mount is safe): %v", cmd, err)
		}
	}
}

func TestBashGuard_ZcpSideFindHarmless_Allowed(t *testing.T) {
	t.Parallel()
	// find/ls as standalone commands are fine; the check targets `cd /var/www/X && <exec>` patterns.
	if err := CheckBashCommand("find /var/www/apidev -name .gitignore"); err != nil {
		t.Errorf("find as standalone should be allowed: %v", err)
	}
}

func TestBashGuard_NestedSshEscape_Allowed(t *testing.T) {
	t.Parallel()
	// cd inside an ssh-quoted command is fine — that's the correct shape.
	cmd := `ssh apidev "cd /var/www/subdir && npm test"`
	if err := CheckBashCommand(cmd); err != nil {
		t.Errorf("cd inside ssh quote should be allowed: %v", err)
	}
}

func TestBashGuard_CdToRepoRoot_Allowed(t *testing.T) {
	t.Parallel()
	// `cd /var/www/apidev` without executable follow-up is just navigation.
	if err := CheckBashCommand("cd /var/www/apidev"); err != nil {
		t.Errorf("bare cd should be allowed: %v", err)
	}
}

func TestBashGuard_SemicolonChain_Rejected(t *testing.T) {
	t.Parallel()
	// `;` separator is also a violation — not only `&&`.
	err := CheckBashCommand("cd /var/www/apidev ; git commit -m 'wip'")
	if err == nil {
		t.Fatal("expected rejection for ; chain")
	}
}

func TestBashGuard_ErrorMessageIncludesSuggestion(t *testing.T) {
	t.Parallel()
	err := CheckBashCommand("cd /var/www/apidev && git add -A")
	if err == nil {
		t.Fatal("expected rejection")
	}
	msg := err.Error()
	for _, want := range []string{"ZCP_EXECUTION_BOUNDARY_VIOLATION", "ssh", "where-commands-run"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message must include %q: %s", want, msg)
		}
	}
}

func TestBashGuard_EmptyOrWhitespace_Allowed(t *testing.T) {
	t.Parallel()
	for _, cmd := range []string{"", "   ", "\n"} {
		if err := CheckBashCommand(cmd); err != nil {
			t.Errorf("empty/whitespace command should pass: %v", err)
		}
	}
}
