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
// placeholder. Raised in the v16 content pass: 300 bytes produced ~39-line
// CLAUDE.md files that hit the minimum and stopped, filling with
// boilerplate "ssh in and start the dev server" narration. The bar now
// demands enough volume for at least two sections of genuine repo-ops
// content beyond the template header.
const minClaudeMdBytes = 1200

// minClaudeMdCustomSections is the number of H2/H3 headings a CLAUDE.md
// must carry BEYOND the template boilerplate (Dev Loop / Migrations /
// Container Traps / Testing). These extra sections force the agent to
// document codebase-specific operational knowledge it would otherwise
// omit: how to add a new managed service, how to reset the dev database,
// how to tail logs, how to drive a test job by hand. Without the floor,
// the agent settles for the four template headings and calls it done.
const minClaudeMdCustomSections = 2

// claudeMdTemplateHeadings are the boilerplate section headings that
// appear in the CLAUDE.md template shipped with readme-fragments. These
// are REQUIRED (verified by checkCLAUDEMdExists) but they don't count
// toward the minCustomSections floor — the floor exists to force
// content beyond the template.
var claudeMdTemplateHeadings = map[string]bool{
	"dev loop":              true,
	"migrations & seed":     true,
	"migrations and seed":   true,
	"container traps":       true,
	"testing":               true,
	"testing / smoke check": true,
	"smoke check":           true,
}

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
				"CLAUDE.md not found at %s — every codebase must ship a repo-local dev-loop operations guide alongside README.md. README.md is PUBLISHED recipe content extracted to zerops.io/recipes (for integrators porting their own code). CLAUDE.md is REPO-LOCAL (for anyone, human or Claude Code, who clones this codebase to work in it). They have different audiences and neither substitutes for the other. Write CLAUDE.md during the deploy `readmes` sub-step alongside README.md. Contents: (1) how to SSH into the dev container, (2) exact command to start the dev server, (3) how to run migrations/seed manually, (4) container traps you hit (SSHFS uid fix, npx tsc wrong-package trap, fuser -k for stuck ports), (5) how to run tests. Fetch the template via zerops_guidance topic=\"readme-fragments\" — the CLAUDE.md template is inside that block alongside the README fragments.",
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
				"%s is %d bytes — needs >= %d bytes of substantive repo-ops content. Version 16's CLAUDE.md files cleared the old 300-byte floor with boilerplate (\"ssh into the container, start dev server, run migrations\") and stopped there. The bar is higher now: beyond the template headings (Dev Loop / Migrations / Container Traps / Testing), add codebase-specific operational sections the agent actually needs: how to add a new managed service, how to reset the dev database without a full redeploy, how to tail logs in each service, how to drive a test job or smoke a single endpoint by hand, how to recover from a burned `zsc execOnce` key. Be specific to THIS codebase's commands and ports.",
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

	// Custom-section floor — enforce operational depth beyond template.
	// Parse H2/H3 headings, drop the template boilerplate, count what's
	// left. Fewer than minClaudeMdCustomSections means the agent wrote
	// the template and stopped.
	customSections := countClaudeMdCustomSections(content)
	if customSections < minClaudeMdCustomSections {
		return []workflow.StepCheck{{
			Name:   checkName,
			Status: statusFail,
			Detail: fmt.Sprintf(
				"%s has %d custom sections beyond the template boilerplate (Dev Loop / Migrations / Container Traps / Testing); needs at least %d. Add codebase-specific operational sections. Suggested: \"Adding a managed service\" (how to wire a new dependency without redeploying from scratch), \"Log Tailing\" (the exact `tail -f` paths for each process in this codebase), \"Resetting dev state\" (how to re-seed or drop tables without a redeploy cycle), \"Driving a test request\" (a real curl/grpcurl/psql command that exercises the feature path end-to-end on the dev container). Short sections are fine — a named section with 4 lines of specific commands beats a 20-line generic narration.",
				path, customSections, minClaudeMdCustomSections,
			),
		}}
	}
	return []workflow.StepCheck{{
		Name: checkName, Status: statusPass,
	}}
}

// countClaudeMdCustomSections counts H2/H3 headings in a CLAUDE.md that
// are NOT part of the boilerplate template (Dev Loop, Migrations,
// Container Traps, Testing). Headings are matched case-insensitively and
// trimmed of markdown decoration.
func countClaudeMdCustomSections(content string) int {
	custom := 0
	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "## ") && !strings.HasPrefix(trimmed, "### ") {
			continue
		}
		heading := strings.TrimLeft(trimmed, "# ")
		// Strip trailing markdown emphasis and punctuation.
		heading = strings.Trim(heading, " *_`.")
		lower := strings.ToLower(heading)
		if claudeMdTemplateHeadings[lower] {
			continue
		}
		// Also skip variants with a leading number prefix like "## 1. Dev Loop".
		for boilerplate := range claudeMdTemplateHeadings {
			if strings.HasSuffix(lower, boilerplate) {
				lower = "" // mark as template
				break
			}
		}
		if lower == "" {
			continue
		}
		custom++
	}
	return custom
}
