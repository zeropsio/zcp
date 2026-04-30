package recipe

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// stitch_yaml.go — run-19 prep. Closes the run-18 §1.3 bare-runtime-yaml
// gap: codebase-content sub-agents recorded
// `codebase/<h>/zerops-yaml-comments/<block>` fragments correctly, but
// the existing stitch path (assemble.go::injectZeropsYamlComments) only
// embedded those fragments into IG #1 of the README. The on-disk
// `<SourceRoot>/zerops.yaml` stayed bare for apidev + workerdev because
// no engine step wrote the commented version back.
//
// WriteCodebaseYAMLWithComments closes that loop: read bare yaml from
// disk, strip prior comments (idempotence — re-running stitch after a
// fragment edit must not duplicate prior content), inject fragments via
// injectZeropsYamlComments, write back. Stitch path runs this once per
// codebase right after writeCodebaseSurfaces emits README + CLAUDE.md.

// WriteCodebaseYAMLWithComments stamps fragment-recorded yaml comments
// into the on-disk `<SourceRoot>/zerops.yaml` for one codebase. Reads
// the current on-disk yaml, strips comments above directive groups
// (preserves the `#zeropsPreprocessor=on` shebang if present), applies
// every matching `codebase/<hostname>/zerops-yaml-comments/<block>`
// fragment, and writes the result back. Idempotent — calling twice with
// the same plan produces byte-identical output.
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
	raw, err := os.ReadFile(yamlPath)
	if err != nil {
		// No yaml means scaffold didn't run for this codebase yet —
		// silent skip rather than error so early-phase callers
		// (refinement before scaffold) don't fail.
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", yamlPath, err)
	}
	stripped := stripYAMLComments(string(raw))
	commented := injectZeropsYamlComments(stripped, plan.Fragments, hostname)
	// Preserve original file mode if possible (default 0o600 if stat fails).
	mode := os.FileMode(0o600)
	if info, err := os.Stat(yamlPath); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(yamlPath, []byte(commented), mode); err != nil {
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
