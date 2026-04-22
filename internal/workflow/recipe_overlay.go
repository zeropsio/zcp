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

// OverlayRealREADMEs replaces per-codebase README scaffolds in the finalize
// output with the real READMEs written by the agent during the generate step,
// for every non-worker runtime target whose mount contains a valid README.
// This removes the TODO-scaffold footgun: the scaffold was always written to
// the deliverable folder, the agent wrote the real README(s) elsewhere (on
// the mounts), and the close-step sub-agent had to catch the mismatch.
//
// Best-effort: a mount that can't be read or whose README fails validation
// keeps its scaffold in place. Returns the number of READMEs overlaid.
func OverlayRealREADMEs(files map[string]string, plan *RecipePlan) int {
	if plan == nil || files == nil {
		return 0
	}
	base := recipeMountBase
	if recipeMountBaseOverride != "" {
		base = recipeMountBaseOverride
	}
	overlaid := 0
	for _, t := range plan.Targets {
		if !IsRuntimeType(t.Type) || t.IsWorker {
			continue
		}
		mountPath := filepath.Join(base, t.Hostname+"dev", "README.md")
		data, err := os.ReadFile(mountPath)
		if err != nil {
			continue
		}
		content := string(data)
		if !isValidAppREADME(content) {
			continue
		}
		files[t.Hostname+"dev/README.md"] = content
		overlaid++
	}
	return overlaid
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
	// Cx-SCAFFOLD-FRAGMENT-FRAMES: the v38 scaffold emits a single
	// `<!-- REPLACE THIS LINE ... -->` placeholder between each marker
	// pair. If the placeholder survives into what the writer returns,
	// the writer did not Edit the fragment — overlaying would publish
	// a REPLACE-THIS-LINE comment to zerops.io/recipes.
	if strings.Contains(content, "<!-- REPLACE THIS LINE") {
		return false
	}
	return true
}
