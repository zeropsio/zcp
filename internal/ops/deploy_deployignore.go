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
// common traps. Errors are deploy-blocking; Warnings surface informationally.
//
// Run-11 P-3 — runs at deploy time so reflexive .deployignore authoring
// (run 10 worker bricked itself by listing dist) doesn't reach the
// runtime silently.
type DeployignoreLintResult struct {
	Warnings []string
	Errors   []string
}

// deployignoreHardRejectLines are deploy artifacts or bundled deps;
// listing either filters the runtime artifact and bricks the deploy.
// Match leading-./ and trailing-/ tolerantly.
var deployignoreHardRejectLines = []string{"dist", "node_modules"}

// deployignoreWarnLines are typically redundant — .git is auto-excluded
// by the Zerops builder; .idea/.vscode/log files belong in .gitignore.
var deployignoreWarnLines = []string{".git", ".idea", ".vscode", "*.log"}

// LintDeployignore reads <workingDir>/.deployignore (if present) and
// returns hard-reject + warning entries. Missing file is not an error.
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
		hardReject := false
		for _, bad := range deployignoreHardRejectLines {
			if canonical == bad {
				out.Errors = append(out.Errors, fmt.Sprintf(
					"%q in .deployignore filters the deploy artifact (or bundled deps) — remove this line; listing %s breaks runtime startup",
					line, bad,
				))
				hardReject = true
				break
			}
		}
		if hardReject {
			continue
		}
		for _, warn := range deployignoreWarnLines {
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
