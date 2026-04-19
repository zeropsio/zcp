package tools

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/workflow"
)

// scaffoldArtifactGlobs enumerates the path patterns the check scans on each
// codebase mount. The list tracks v29's defect and the asymmetric shapes a
// scaffold subagent might leave behind: `scripts/preship.sh`,
// `verify/readme_check.sh`, `preflight/` helpers, and `_scaffold*` /
// `scaffold-*` audit files that only make sense during recipe authoring.
//
// Additions must stay anchored to a leaf-directory or filename prefix. A
// broad pattern like `*.sh` would false-positive every legitimate shell
// script the recipe commits on purpose.
var scaffoldArtifactGlobs = []string{
	"scripts/*.sh",
	"scripts/*.py",
	"verify/*",
	"assert/*",
	"preflight/*",
	"_scaffold*",
	"scaffold-*",
}

// checkScaffoldArtifactLeak walks the codebase mount at ymlDir and fails when
// any scaffold-phase artifact (pre-ship assertion script, verify helper,
// audit checker) remains committed without being referenced by the
// codebase's own zerops.yaml. v29 shipped apidev/scripts/preship.sh in the
// published tree because no rule forbade committed self-test files; this
// check turns the rule into a hard gate at generate-complete.
//
// Reference detection runs in two passes against each match: (1) structured
// scan of command-bearing fields on the parsed ZeropsYmlDoc (run.start,
// run.prepareCommands, build.prepareCommands, build.buildCommands), and (2)
// raw-YAML substring fallback to catch fields not modeled on the local
// zeropsYmlRun struct (notably `initCommands`). A false-negative (real
// script flagged as leak) is worse than a false-positive-free match — err
// on the "reference exists, skip" side. Patterns match relative paths, so a
// needle is the path itself plus a `./` prefix variant.
func checkScaffoldArtifactLeak(ymlDir string, doc *ops.ZeropsYmlDoc, rawYAML, hostname string) []workflow.StepCheck {
	var leaks []string
	for _, pattern := range scaffoldArtifactGlobs {
		matches, _ := filepath.Glob(filepath.Join(ymlDir, pattern))
		for _, m := range matches {
			rel, err := filepath.Rel(ymlDir, m)
			if err != nil {
				continue
			}
			if isReferencedByZeropsYml(rel, doc, rawYAML) {
				continue
			}
			leaks = append(leaks, filepath.ToSlash(rel))
		}
	}
	if len(leaks) == 0 {
		return []workflow.StepCheck{{
			Name:   hostname + "_scaffold_artifact_leak",
			Status: statusPass,
		}}
	}
	return []workflow.StepCheck{{
		Name:   hostname + "_scaffold_artifact_leak",
		Status: statusFail,
		Detail: fmt.Sprintf(
			"scaffold-phase artifacts present in %s but not referenced by zerops.yaml: %s. Scaffold subagents must run pre-ship verification via inline `bash -c` or `ssh <host> \"set -e; ...\"` chains, not committed files under the codebase tree. Remove these and amend the scaffold commit (inline git identity so amend succeeds even on an unconfigured container): ssh %s \"cd /var/www && rm %s && git add -A && git -c user.email=scaffold@zcp.local -c user.name=scaffold commit --amend --no-edit\".",
			hostname, strings.Join(leaks, ", "),
			hostname, strings.Join(leaks, " "),
		),
	}}
}

// isReferencedByZeropsYml returns true when any command-bearing field in any
// zerops.yaml entry mentions the script path. See the function doc on
// checkScaffoldArtifactLeak for the two-pass scan rationale.
func isReferencedByZeropsYml(relPath string, doc *ops.ZeropsYmlDoc, rawYAML string) bool {
	slashPath := filepath.ToSlash(relPath)
	needles := []string{slashPath, "./" + slashPath}

	if doc != nil {
		for _, entry := range doc.Zerops {
			candidates := make([]string, 0, 4)
			candidates = append(candidates, entry.Run.Start)
			candidates = append(candidates, anyCommandsToStrings(entry.Run.PrepareCommands)...)
			candidates = append(candidates, anyCommandsToStrings(entry.Build.PrepareCommands)...)
			candidates = append(candidates, anyCommandsToStrings(entry.Build.BuildCommands)...)
			for _, cmd := range candidates {
				for _, n := range needles {
					if strings.Contains(cmd, n) {
						return true
					}
				}
			}
		}
	}

	for _, n := range needles {
		if strings.Contains(rawYAML, n) {
			return true
		}
	}
	return false
}

// anyCommandsToStrings normalizes a YAML `any` field that may be a string or
// []string into a flat []string. Both shapes are legal per the zerops
// schema: `buildCommands: - cmd1` (list) and `buildCommands: "cmd1"`
// (scalar).
func anyCommandsToStrings(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return x
	}
	return nil
}
