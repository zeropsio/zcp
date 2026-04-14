package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// minClaudeMdBytes is the minimum byte count for a substantive CLAUDE.md.
// A shorter file is almost always a stub — "# TODO" or a single line of
// placeholder. The floor is low enough that a genuinely small hello-world
// dev-loop guide still clears it (~5 lines of SSH + start-server
// commands), tight enough that trailing-newline stubs fail.
const minClaudeMdBytes = 300

// checkCLAUDEMdExists verifies each codebase mount ships a CLAUDE.md
// alongside README.md. The two files have distinct audiences and neither
// substitutes for the other:
//
//  1. README.md is PUBLISHED recipe-page content. Its extract fragments
//     land at zerops.io/recipes; readers are integrators bringing their
//     own codebase and want to know "what must I change in my app to run
//     it on Zerops". Fragment-scoped, strictly formatted, validated for
//     authenticity and dedup.
//
//  2. CLAUDE.md is REPO-LOCAL operational knowledge for anyone (human or
//     Claude Code) who clones the codebase and needs to work in it:
//     how to SSH into the dev container, how to start the dev server,
//     how to run migrations/seed, container idioms the agent hit during
//     debugging (SSHFS uid, npx-tsc-resolves-wrong-package, fuser -k to
//     free a port). Not extracted, not published, no fragment rules.
//
// The check fires at deploy-step completion and is scoped to every tier
// (hello-world, minimal, showcase) because every recipe ships a dev
// container that benefits from a repo-local operations guide — even the
// smallest recipe has a "how to start the dev server" answer that is
// worth writing down.
//
// Failure modes reported in priority order: file missing → contents too
// short → contains TODO/PLACEHOLDER marker. Each produces a targeted
// error message the agent can iterate against.
func checkCLAUDEMdExists(projectRoot string, target workflow.RecipeTarget, plan *workflow.RecipePlan) []workflow.StepCheck {
	_ = plan // accepted for signature parity; the check runs on every tier
	hostname := target.Hostname
	checkName := hostname + "_claude_md_exists"

	mountDir := projectRoot
	for _, candidate := range []string{hostname + "dev", hostname} {
		mountPath := filepath.Join(projectRoot, candidate)
		if info, err := os.Stat(mountPath); err == nil && info.IsDir() {
			mountDir = mountPath
			break
		}
	}
	path := filepath.Join(mountDir, "CLAUDE.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		return []workflow.StepCheck{{
			Name:   checkName,
			Status: statusFail,
			Detail: fmt.Sprintf(
				"CLAUDE.md not found at %s — every codebase must ship a repo-local dev-loop operations guide alongside README.md. README.md is PUBLISHED recipe content extracted to zerops.io/recipes (for integrators porting their own code). CLAUDE.md is REPO-LOCAL (for anyone, human or Claude Code, who clones this codebase to work in it). They have different audiences and neither substitutes for the other. Write CLAUDE.md during the deploy `readmes` sub-step alongside README.md. Contents: (1) how to SSH into the dev container, (2) exact command to start the dev server, (3) how to run migrations/seed manually, (4) container traps you hit (SSHFS uid fix, npx tsc wrong-package trap, fuser -k for stuck ports), (5) how to run tests. Use plain markdown — no fragment markers, no extraction rules. Fetch the template: zerops_guidance topic=\"claude-md\".",
				path,
			),
		}}
	}
	content := strings.TrimSpace(string(raw))
	if len(content) < minClaudeMdBytes {
		return []workflow.StepCheck{{
			Name:   checkName,
			Status: statusFail,
			Detail: fmt.Sprintf(
				"%s is %d bytes — needs >= %d bytes of real repo-ops content. A stub like \"# CLAUDE.md\\nTODO\" is not enough. Narrate the dev loop you actually used during this build: the exact SSH command, the exact dev server startup line, the migration/seed commands, and any container traps you hit. Short is fine — tight narration beats padded boilerplate — but the file must actually carry the commands a returning developer needs to re-enter the workflow.",
				path, len(content), minClaudeMdBytes,
			),
		}}
	}
	if strings.Contains(content, "TODO") || strings.Contains(content, "PLACEHOLDER") {
		return []workflow.StepCheck{{
			Name:   checkName,
			Status: statusFail,
			Detail: fmt.Sprintf(
				"%s contains a TODO/PLACEHOLDER marker — finish narrating the section before completing the deploy step. CLAUDE.md is the repo-local operations guide; shipping a stub with unresolved markers defeats its purpose.",
				path,
			),
		}}
	}
	return []workflow.StepCheck{{
		Name: checkName, Status: statusPass,
	}}
}
