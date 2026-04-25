package ops

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// DeployignoreLintResult is the outcome of scanning a .deployignore for
// common traps. All findings flow through Warnings — none block deploy.
// Per the 2026-04-25 architectural reframe (system.md §4), hard-rejecting
// specific filenames was the catalog reification this list was about
// to grow into. The TEACH-side .deployignore paragraph in
// internal/knowledge/themes/core.md (P-1) does the teaching; this
// linter surfaces a runtime warning when those rules are visibly
// broken, then lets the deploy proceed.
type DeployignoreLintResult struct {
	Warnings []string
}

// deployignoreArtifactLines are deploy artifacts or bundled deps;
// listing either filters the runtime artifact and breaks startup.
// Surfaced as warnings — see DeployignoreLintResult.
var deployignoreArtifactLines = []string{"dist", "node_modules"}

// deployignoreRedundantLines are typically redundant — .git is auto-
// excluded by the Zerops builder; .idea/.vscode/log files belong in
// .gitignore.
var deployignoreRedundantLines = []string{".git", ".idea", ".vscode", "*.log"}

// LintDeployignore reads <workingDir>/.deployignore (if present) and
// returns warnings for entries that almost-always indicate a mistake.
// Missing file is not an error. Deploy never blocks on these.
func LintDeployignore(workingDir string) (DeployignoreLintResult, error) {
	path := filepath.Join(workingDir, ".deployignore")
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return DeployignoreLintResult{}, nil
		}
		return DeployignoreLintResult{}, fmt.Errorf("read .deployignore: %w", err)
	}
	defer f.Close()

	var out DeployignoreLintResult
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		raw := sc.Text()
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		canonical := strings.TrimSuffix(strings.TrimPrefix(line, "./"), "/")
		artifact := false
		for _, bad := range deployignoreArtifactLines {
			if canonical == bad {
				out.Warnings = append(out.Warnings, fmt.Sprintf(
					"%q in .deployignore filters the deploy artifact (or bundled deps) — remove this line; listing %s breaks runtime startup",
					line, bad,
				))
				artifact = true
				break
			}
		}
		if artifact {
			continue
		}
		for _, warn := range deployignoreRedundantLines {
			if matchesLine(canonical, warn) {
				out.Warnings = append(out.Warnings, fmt.Sprintf(
					"%q in .deployignore is typically redundant (%s belongs in .gitignore; the Zerops builder already excludes .git/) — confirm rationale or remove",
					line, warn,
				))
				break
			}
		}
	}
	if err := sc.Err(); err != nil {
		return DeployignoreLintResult{}, fmt.Errorf("scan .deployignore: %w", err)
	}
	return out, nil
}

// matchesLine compares a canonical .deployignore line against a watch
// pattern. Plain string compare for now; supports the trailing `*.log`
// glob shape via suffix match.
func matchesLine(canonical, pattern string) bool {
	if canonical == pattern {
		return true
	}
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // ".log"
		return strings.HasSuffix(canonical, suffix)
	}
	return false
}
