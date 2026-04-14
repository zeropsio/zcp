package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/workflow"
)

// TestCheckCLAUDEMdExists enforces the v9 rule: every codebase mount must
// ship a CLAUDE.md alongside README.md. README.md is the PUBLISHED recipe
// page content (fragments extracted to zerops.io/recipes). CLAUDE.md is
// the REPO-LOCAL dev-loop guide for anyone (human or Claude Code) who
// opens this codebase after cloning — SSH commands, dev server startup,
// migration commands, container traps like SSHFS uid + npx tsc + fuser.
//
// Scoped to all tiers (hello-world, minimal, showcase) because even
// minimal recipes ship a dev container — the repo-local operations guide
// is useful from the smallest recipe up.
func TestCheckCLAUDEMdExists(t *testing.T) {
	t.Parallel()

	target := workflow.RecipeTarget{Hostname: "api", Type: "nodejs@22"}

	tests := []struct {
		name         string
		claudeMdBody string
		skipWrite    bool
		wantPass     bool
		wantSubs     []string
	}{
		{
			name: "substantive CLAUDE.md passes",
			claudeMdBody: `# API — Dev Operations

## Dev loop

SSH in with zcli: ` + "`zcli vpn up`" + ` then ` + "`ssh zerops@apidev`" + `.

Start the dev server: ` + "`npm run start:dev`" + ` (NestJS watch mode with hot-reload).

## Migrations & seed

Run manually: ` + "`npx ts-node src/migrate.ts`" + ` then ` + "`npx ts-node src/seed.ts`" + `.
On deploy, ` + "`initCommands`" + ` runs both via ` + "`zsc execOnce ${appVersionId}`" + `.

## Container traps

- SSHFS ownership: files land owned by root, container runs as ` + "`zerops`" + ` (uid 2023). Fix: ` + "`sudo chown -R zerops:zerops /var/www/`" + `.
- ` + "`npx tsc`" + ` resolves to deprecated tsc@2.0.4 package. Use ` + "`node_modules/.bin/tsc`" + ` instead.
- Port 3000 occupied after background start: ` + "`fuser -k 3000/tcp`" + `.
`,
			wantPass: true,
		},
		{
			name:      "missing CLAUDE.md fails",
			skipWrite: true,
			wantPass:  false,
			wantSubs:  []string{"CLAUDE.md not found", "repo-local", "dev-loop"},
		},
		{
			name:         "stub CLAUDE.md (too short) fails",
			claudeMdBody: "# CLAUDE.md\nTODO\n",
			wantPass:     false,
			wantSubs:     []string{"bytes of real repo-ops content"},
		},
		{
			name: "CLAUDE.md with PLACEHOLDER fails",
			claudeMdBody: `# API — Dev Operations

## Dev loop

SSH into the dev container and run the dev server. PLACEHOLDER_COMMAND is where
you'd put the actual command. This file has enough content to clear the byte
floor but should fail the placeholder gate because the content is not yet
narrated — it still carries the template's stub marker instead of the real
commands the agent ran during deploy.
`,
			wantPass: false,
			wantSubs: []string{"TODO", "PLACEHOLDER"},
		},
		{
			name: "CLAUDE.md with TODO marker fails",
			claudeMdBody: `# API — Dev Operations

## Dev loop

TODO: add the actual SSH command and the dev server startup line. This file is
obviously not finished; the agent stopped before writing the real content. The
byte floor is cleared but the TODO marker is the honest signal that nothing has
been narrated yet — the check should reject it rather than admit a stub.
`,
			wantPass: false,
			wantSubs: []string{"TODO", "PLACEHOLDER"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			mountDir := filepath.Join(dir, "apidev")
			if err := os.MkdirAll(mountDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if !tt.skipWrite {
				if err := os.WriteFile(filepath.Join(mountDir, "CLAUDE.md"), []byte(tt.claudeMdBody), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			plan := &workflow.RecipePlan{Tier: workflow.RecipeTierMinimal}
			checks := checkCLAUDEMdExists(dir, target, plan)
			if len(checks) == 0 {
				t.Fatal("expected a check result")
			}
			c := checks[0]
			if c.Name != "api_claude_md_exists" {
				t.Errorf("unexpected check name %q", c.Name)
			}
			if tt.wantPass {
				if c.Status != "pass" {
					t.Errorf("expected pass, got %q: %s", c.Status, c.Detail)
				}
				return
			}
			if c.Status != "fail" {
				t.Errorf("expected fail, got %q: %s", c.Status, c.Detail)
			}
			for _, sub := range tt.wantSubs {
				if !strings.Contains(c.Detail, sub) {
					t.Errorf("fail detail missing %q:\n%s", sub, c.Detail)
				}
			}
		})
	}
}

// TestCheckCLAUDEMdExists_AllTiers verifies the check fires on every tier.
// An earlier scoping proposal limited CLAUDE.md to showcase tier; the user
// overruled it — even hello-world and minimal recipes ship a dev container
// that needs a local operations guide.
func TestCheckCLAUDEMdExists_AllTiers(t *testing.T) {
	t.Parallel()
	target := workflow.RecipeTarget{Hostname: "app", Type: "nodejs@22"}
	dir := t.TempDir()
	mountDir := filepath.Join(dir, "appdev")
	if err := os.MkdirAll(mountDir, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, tier := range []string{
		workflow.RecipeTierHelloWorld,
		workflow.RecipeTierMinimal,
		workflow.RecipeTierShowcase,
	} {
		plan := &workflow.RecipePlan{Tier: tier}
		checks := checkCLAUDEMdExists(dir, target, plan)
		if len(checks) == 0 {
			t.Errorf("tier %q: expected a check result, got none", tier)
			continue
		}
		if checks[0].Status != "fail" {
			t.Errorf("tier %q: expected fail (no CLAUDE.md written), got %q", tier, checks[0].Status)
		}
	}
}
