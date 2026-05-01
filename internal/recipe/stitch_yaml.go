package recipe

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// stitch_yaml.go — codebase zerops.yaml stitcher.
//
// Authoring contract (run-21-prep): the codebase-content sub-agent
// records ONE fragment per codebase named `codebase/<host>/zerops-yaml`
// whose body is the entire commented zerops.yaml. The stitcher writes
// the body verbatim to `<SourceRoot>/zerops.yaml`, replacing the bare
// scaffold version.
//
// This replaces the legacy per-block fragment shape
// (`codebase/<h>/zerops-yaml-comments/<setup>.<path>.<leaf>`) where the
// agent emitted N small fragments and the engine merged them via
// per-block injection. The audit on sim 21-input-1 found that shape
// produces uneven comment coverage because the agent loses sight of the
// document as a whole — see docs/zcprecipator3/runs/20/ANALYSIS.md and
// run-21-prep notes.
//
// Stitcher behavior:
//   - Fragment present → write body verbatim to disk. The agent owns
//     yaml structure + comment placement + voice.
//   - Fragment absent → leave on-disk bare yaml untouched. Pre-codebase-
//     content callers (refinement-before-codebase-content, sim emit
//     before any agent dispatch) hit this path.
//   - Refuse-to-wipe — if the fragment body is empty bytes AND the
//     on-disk yaml is non-empty, refuse rather than blow away scaffold
//     output. Carry-over from run-23 fix-2 (run-20 prod hit a 0-byte
//     zerops.yaml on appdev/workerdev that ZCP couldn't reproduce
//     locally; refusing-to-wipe closes that vector by construction).

// fragmentIDCodebaseZeropsYAML returns the canonical whole-yaml fragment
// id for a given codebase hostname.
func fragmentIDCodebaseZeropsYAML(hostname string) string {
	return "codebase/" + hostname + "/zerops-yaml"
}

// WriteCodebaseYAMLWithComments writes the codebase-content sub-agent's
// authored zerops.yaml (`codebase/<hostname>/zerops-yaml` fragment) to
// `<SourceRoot>/zerops.yaml`. Idempotent — calling twice with the same
// plan produces byte-identical output. When no fragment is recorded yet
// the on-disk bare yaml is left untouched.
func WriteCodebaseYAMLWithComments(plan *Plan, hostname string) error {
	if plan == nil {
		return fmt.Errorf("WriteCodebaseYAMLWithComments: nil plan")
	}
	var srcRoot string
	for _, cb := range plan.Codebases {
		if cb.Hostname == hostname {
			srcRoot = cb.SourceRoot
			break
		}
	}
	if srcRoot == "" {
		return fmt.Errorf("WriteCodebaseYAMLWithComments: no SourceRoot for codebase %q", hostname)
	}
	yamlPath := filepath.Join(srcRoot, "zerops.yaml")

	body, ok := plan.Fragments[fragmentIDCodebaseZeropsYAML(hostname)]
	if !ok {
		// No whole-yaml fragment recorded yet — early phase or pre-
		// codebase-content. Leave bare scaffold yaml on disk untouched.
		return nil
	}

	// Refuse-to-wipe guard: empty fragment body would blow away the bare
	// scaffold yaml. Surface as an error so the next codebase-content
	// pass investigates rather than silently corrupting.
	if strings.TrimSpace(body) == "" {
		raw, err := os.ReadFile(yamlPath)
		if err == nil && len(raw) > 0 {
			return fmt.Errorf("refuse-to-wipe: empty %s fragment body would erase non-empty on-disk %s — leaving file untouched",
				fragmentIDCodebaseZeropsYAML(hostname), yamlPath)
		}
	}

	// Preserve original file mode if possible (default 0o600 if stat fails).
	mode := os.FileMode(0o600)
	if info, err := os.Stat(yamlPath); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(yamlPath, []byte(body), mode); err != nil {
		return fmt.Errorf("write %s: %w", yamlPath, err)
	}
	return nil
}

// stripYAMLComments removes lines whose first non-whitespace character
// is `#`, with two carve-outs:
//
//  1. The `#zeropsPreprocessor=on` shebang at line 0 (no leading
//     whitespace) — that's the import-yaml preprocessor activation
//     pragma, not a causal comment.
//  2. Inline trailing comments on data lines (e.g. `port: 3000 # note`)
//     stay because the line carries data — only `^\s*#` lines are
//     stripped.
//
// Used by WriteCodebaseYAMLWithComments to make stitch idempotent: the
// on-disk yaml is normalized to its bare form before fragments are
// re-injected, so re-running stitch never duplicates prior content.
func stripYAMLComments(yamlBody string) string {
	lines := strings.Split(yamlBody, "\n")
	out := make([]string, 0, len(lines))
	for i, ln := range lines {
		trimmed := strings.TrimLeft(ln, " \t")
		switch {
		case i == 0 && strings.HasPrefix(trimmed, "#zeropsPreprocessor="):
			// Preserve the file-start preprocessor shebang.
			out = append(out, ln)
		case strings.HasPrefix(trimmed, "#"):
			// Pure comment line — drop.
			continue
		default:
			out = append(out, ln)
		}
	}
	return strings.Join(out, "\n")
}
