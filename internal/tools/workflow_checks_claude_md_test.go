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
			// Substantive CLAUDE.md — clears the v17 byte floor (1200)
			// AND carries >= 2 custom sections beyond the template.
			name: "substantive CLAUDE.md passes",
			claudeMdBody: `# API — Dev Operations

## Dev Loop

SSH in with zcli: ` + "`zcli vpn up`" + ` then ` + "`ssh zerops@apidev`" + `.
Start the dev server: ` + "`npm run start:dev`" + ` (NestJS watch mode with hot-reload).
Source lives at ` + "`/var/www/`" + ` on the container, mounted from zcp at ` + "`/var/www/apidev/`" + ` via SSHFS.

## Migrations & Seed

Run manually: ` + "`npx ts-node src/migrate.ts`" + ` then ` + "`npx ts-node src/seed.ts`" + `.
On deploy, ` + "`initCommands`" + ` runs both via ` + "`zsc execOnce ${appVersionId}`" + ` so only one container per version executes them.
If seed fails mid-insert the execOnce key is burned; touch any source file and redeploy to rotate ` + "`appVersionId`" + `.

## Container Traps

- SSHFS ownership: files land owned by root but the container runs as ` + "`zerops`" + ` (uid 2023). Fix: ` + "`sudo chown -R zerops:zerops /var/www/`" + `.
- ` + "`npx tsc`" + ` resolves to deprecated tsc@2.0.4. Use ` + "`node_modules/.bin/tsc`" + ` or ` + "`npx --yes typescript -- --noEmit`" + `.
- Port 3000 occupied after a background dev-server crash: ` + "`lsof -ti:3000 | xargs kill -9`" + ` before restarting.

## Testing

Smoke the dev server from inside the container:
    npm run start:dev &
    sleep 5
    curl -s http://localhost:3000/api/health

From zcp: ` + "`curl -s http://apidev:3000/api/health`" + `.

## Resetting dev state

Drop and re-seed without a redeploy: ` + "`psql $DB_HOST -U $DB_USER -c 'DROP SCHEMA public CASCADE; CREATE SCHEMA public;'`" + ` then rerun the migrate + seed commands from "Migrations & Seed" above. This avoids the ` + "`appVersionId`" + ` rotation step and keeps your current SSH session workflow intact.

## Log Tailing

Main app log: ` + "`tail -f /tmp/nest.log`" + ` (from inside the container after ` + "`npm run start:dev > /tmp/nest.log 2>&1 &`" + `).
To see live request logs from outside the container: ` + "`zerops_logs hostname=apidev`" + ` from the zcp MCP surface.
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
			wantSubs:     []string{"substantive repo-ops content"},
		},
		{
			// Must contain enough bytes to pass the byte floor but
			// trip the PLACEHOLDER word detector. The content below
			// exceeds the 1200-byte floor and carries 2+ custom
			// sections, so the ONLY reason it fails is the marker.
			name: "CLAUDE.md with PLACEHOLDER fails",
			claudeMdBody: `# API — Dev Operations

## Dev Loop

SSH into the dev container and run the dev server. PLACEHOLDER_COMMAND is where
you'd put the actual command. This file has enough content to clear the byte
floor but should fail the placeholder gate because the content is not yet
narrated — it still carries the template's stub marker instead of the real
commands the agent ran during deploy.

## Migrations & Seed

Similar story here — the real migration and seed commands belong in this
section but instead we have PLACEHOLDER_MIGRATION as a reminder to fill in
the actual ts-node / zsc execOnce wiring. The check should reject the file
whether the body is 100 bytes or 10,000 bytes as long as PLACEHOLDER appears.

## Container Traps

Placeholder entries for SSHFS ownership and port cleanup would go here once
the agent has actually hit them during the deploy step. Until then the file
is still a stub and the TODO/PLACEHOLDER gate keeps it out.

## Testing

Smoke test command placeholder and curl fixture.

## Resetting dev state

Placeholder instructions for dropping the schema and re-seeding without a
full redeploy cycle.

## Log Tailing

Placeholder instructions for tailing the dev server log file.
`,
			wantPass: false,
			wantSubs: []string{"TODO", "PLACEHOLDER"},
		},
		{
			name: "CLAUDE.md with TODO marker fails",
			claudeMdBody: `# API — Dev Operations

## Dev Loop

TODO: add the actual SSH command and the dev server startup line. This file is
obviously not finished; the agent stopped before writing the real content. The
byte floor is cleared but the TODO marker is the honest signal that nothing has
been narrated yet — the check should reject it rather than admit a stub.

## Migrations & Seed

TODO: describe migration/seed commands here with the exact ts-node or compiled
node invocations. Also note the zsc execOnce burn-recovery procedure when a
seed crashes mid-run. Include the recovery steps for a stuck appVersionId key
so the next agent doesn't have to rediscover the rotation trick.

## Container Traps

TODO: list the container traps the agent hit during deploy. SSHFS uid fix,
the npx tsc wrong-package trap, fuser -k for stuck ports, any framework
debugger settings that don't survive a container restart. Include the exact
error messages so text search in the repo surfaces them later.

## Testing

TODO: smoke test shell script with curl health check and all integration
endpoints. Add a second curl sequence that exercises the job dispatch + worker
round-trip path for cross-codebase contract verification.

## Resetting dev state

TODO: instructions for dropping the schema and re-seeding without a full
redeploy cycle. This avoids burning a fresh appVersionId each time and is
the single biggest dev-loop time saver during feature iteration.

## Log Tailing

TODO: how to tail the dev server log from the container and from zcp. Include
both the local tail -f path and the zerops_logs MCP call so the agent knows
which surface to reach for in which situation.
`,
			wantPass: false,
			wantSubs: []string{"TODO", "PLACEHOLDER"},
		},
		{
			// New v17 case: byte floor cleared but fewer than 2 custom
			// sections beyond the boilerplate template. This is the v16
			// regression shape — 39-line CLAUDE.md with only the four
			// template sections filled in.
			name: "CLAUDE.md with only template sections fails depth",
			claudeMdBody: `# API — Dev Operations

## Dev Loop

SSH in via zcli and run the dev server. Source lives at /var/www/ on the container. The dev server runs on port 3000. The container starts idle until you start the process. This section carries enough prose to clear the byte floor without adding any custom sections beyond the template. We describe the dev loop in detail: zcli vpn up, ssh zerops@apidev, cd /var/www, npm run start:dev, wait for "Nest application successfully started", curl http://localhost:3000/api/health to verify.

## Migrations & Seed

Migrations via npx ts-node src/migrate.ts. Seeder via npx ts-node src/seed.ts. On deploy, both are wrapped by zsc execOnce so only one container per version runs them. If the seed crashes mid-run, the execOnce key is burned and subsequent deploys skip it; touch a source file to rotate the key. This section is deliberately verbose to clear the byte floor so the depth-section check is the only thing that fires.

## Container Traps

SSHFS ownership fix is sudo chown -R zerops:zerops. npx tsc wrong-package is fixed by node_modules/.bin/tsc. Port 3000 stuck is fixed by lsof -ti:3000 | xargs kill -9. Additional padding here to clear the byte floor without adding a new section heading.

## Testing

curl http://localhost:3000/api/health from inside the container after starting the dev server. Also curl the /api/status endpoint to verify all five managed-service integrations are green. Log-tail pattern: tail -f /tmp/nest.log from inside the SSH session.
`,
			wantPass: false,
			wantSubs: []string{"custom sections beyond the template"},
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
