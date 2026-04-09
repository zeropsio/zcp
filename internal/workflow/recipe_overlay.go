package workflow

import (
	"os"
	"path/filepath"
	"strings"
)

// recipeMountBase is the base directory where SSHFS mounts live in the ZCP
// container (matches ops.mountBase). Not importable from ops to avoid a
// workflow → ops dependency cycle, so it is duplicated here as a const.
const recipeMountBase = "/var/www"

// recipeMountBaseOverride lets tests point mount-resolution at a temp dir.
// Production code leaves it empty and falls back to recipeMountBase.
var recipeMountBaseOverride string

// OverlayRealAppREADME replaces files["appdev/README.md"] with the real
// README written by the agent during the generate step, if that README
// exists on the SSHFS mount and passes basic validity checks. This removes
// the long-standing TODO-scaffold footgun: the scaffold was always written
// to the deliverable folder, the agent wrote the real README elsewhere
// (on the mount), and the close-step sub-agent had to catch the mismatch.
//
// Best-effort: if the real README cannot be read or fails validation, the
// scaffold is left in place. Returns true if the overlay was applied.
func OverlayRealAppREADME(files map[string]string, plan *RecipePlan) bool {
	if plan == nil || files == nil {
		return false
	}
	mountPath := mountREADMEPathForPlan(plan)
	if mountPath == "" {
		return false
	}
	data, err := os.ReadFile(mountPath)
	if err != nil {
		return false
	}
	content := string(data)
	if !isValidAppREADME(content) {
		return false
	}
	files["appdev/README.md"] = content
	return true
}

// mountREADMEPathForPlan constructs the expected app README path on the mount.
// Prefers the API target's README (documents the showcased framework in dual-runtime).
// Falls back to first non-worker runtime target. Returns "" if none found.
func mountREADMEPathForPlan(plan *RecipePlan) string {
	base := recipeMountBase
	if recipeMountBaseOverride != "" {
		base = recipeMountBaseOverride
	}
	// Prefer API service README (documents the showcased framework).
	for _, t := range plan.Targets {
		if IsRuntimeType(t.Type) && !t.IsWorker && t.Role == RecipeRoleAPI {
			return filepath.Join(base, t.Hostname+"dev", "README.md")
		}
	}
	// Fall back to first non-worker runtime.
	for _, t := range plan.Targets {
		if IsRuntimeType(t.Type) && !t.IsWorker {
			return filepath.Join(base, t.Hostname+"dev", "README.md")
		}
	}
	return ""
}

// isValidAppREADME returns true only if the README contains all three
// required extract-fragment markers the app README MUST have. This guards
// against overlaying a scaffold-in-progress or an unrelated README.
func isValidAppREADME(content string) bool {
	required := []string{
		"<!-- #ZEROPS_EXTRACT_START:intro# -->",
		"<!-- #ZEROPS_EXTRACT_END:intro# -->",
		"<!-- #ZEROPS_EXTRACT_START:integration-guide# -->",
		"<!-- #ZEROPS_EXTRACT_END:integration-guide# -->",
		"<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->",
		"<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->",
	}
	for _, marker := range required {
		if !strings.Contains(content, marker) {
			return false
		}
	}
	// Also reject content that still contains TODO scaffold text — if the
	// agent's file has TODO markers, it's not ready to overlay.
	if strings.Contains(content, "TODO: paste the full zerops.yaml content here") ||
		strings.Contains(content, "**TODO** \u2014 add framework-specific gotchas") {
		return false
	}
	return true
}
