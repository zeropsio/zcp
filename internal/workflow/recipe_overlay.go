package workflow

import (
	"encoding/json"
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

// OverlayManifest stages the writer-authored ZCP_CONTENT_MANIFEST.json
// into the deliverable files map at its canonical deliverable-root
// path. Source path is `<mountBase>/zcprecipator/<slug>/ZCP_CONTENT_
// MANIFEST.json` — the recipe output root the writer writes to, per
// the Cx-WRITER-SCOPE-REDUCTION canonical-output-tree atom.
//
// Guard: the manifest body must parse as JSON. A malformed manifest
// is skipped silently (returns false) so the deliverable never ships
// a broken manifest. Missing file is also silent (returns false);
// the caller logs its own "manifest not authored" note.
func OverlayManifest(files map[string]string, plan *RecipePlan) bool {
	if plan == nil || files == nil {
		return false
	}
	if strings.TrimSpace(plan.Slug) == "" {
		return false
	}
	base := recipeMountBase
	if recipeMountBaseOverride != "" {
		base = recipeMountBaseOverride
	}
	src := filepath.Join(base, "zcprecipator", plan.Slug, "ZCP_CONTENT_MANIFEST.json")
	data, err := os.ReadFile(src)
	if err != nil {
		return false
	}
	if !json.Valid(data) {
		return false
	}
	files["ZCP_CONTENT_MANIFEST.json"] = string(data)
	return true
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
