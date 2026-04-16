package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// checkScaffoldHygiene verifies that a codebase's root on the published
// tree has `.gitignore` + `.env.example` present AND that no build-
// output / node_modules / OS-cruft artifacts leaked into the tree.
//
// Every codebase a recipe ships is a potential surface for hygiene
// regressions: v21's apidev scaffold shipped 208 MB of `node_modules`
// + 748 KB of `dist/` + `.DS_Store` files into the published recipe
// because the main agent's per-codebase scaffold brief dropped the
// conditional `.gitignore`/`.env.example` line during synthesis. With
// no `.gitignore` on the mount, the subsequent git-add captured every
// installed dependency.
//
// The check returns one `{hostname}_scaffold_hygiene` event per
// codebase. Passes cleanly when both files exist AND none of the leak
// patterns (`node_modules/`, `dist/`, `build/`, `.next/`, `target/`,
// `.DS_Store`) appear under the codebase root. When the codebase root
// doesn't exist (mount not set up / skipped), the check no-ops rather
// than fabricating a failure.
func checkScaffoldHygiene(codebaseDir, hostname string) []workflow.StepCheck {
	checkName := hostname + "_scaffold_hygiene"
	info, err := os.Stat(codebaseDir)
	if err != nil || !info.IsDir() {
		return nil
	}

	var problems []string

	if _, err := os.Stat(filepath.Join(codebaseDir, ".gitignore")); os.IsNotExist(err) {
		problems = append(problems, "`.gitignore` missing")
	}
	if _, err := os.Stat(filepath.Join(codebaseDir, ".env.example")); os.IsNotExist(err) {
		problems = append(problems, "`.env.example` missing")
	}

	// Root-level build-output leaks. We only check the codebase root
	// (not recursively) — `node_modules` legitimately exists inside the
	// dev container's own filesystem; what matters is that it didn't
	// leak into the published tree.
	for _, name := range []string{"node_modules", "dist", "build", ".next", "target"} {
		path := filepath.Join(codebaseDir, name)
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			problems = append(problems, fmt.Sprintf("`%s/` present in codebase root", name))
		}
	}

	// Recursive `.DS_Store` search — macOS scatters them anywhere.
	// Walk errors are tolerated (we're best-effort scanning a leaf tree);
	// a transient read error on one entry shouldn't abort the whole scan.
	_ = filepath.Walk(codebaseDir, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil //nolint:nilerr // best-effort recursive scan; skip entries we can't read
		}
		if info.IsDir() && info.Name() == "node_modules" {
			return filepath.SkipDir
		}
		if info.Name() == ".DS_Store" {
			rel, _ := filepath.Rel(codebaseDir, p)
			problems = append(problems, fmt.Sprintf("`.DS_Store` at `%s`", rel))
		}
		return nil
	})

	if len(problems) == 0 {
		return []workflow.StepCheck{{Name: checkName, Status: statusPass}}
	}
	return []workflow.StepCheck{{
		Name:   checkName,
		Status: statusFail,
		Detail: fmt.Sprintf(
			"%s has scaffold hygiene issues: %s. Every codebase ships with `.gitignore` (listing at minimum `node_modules/`, the framework's build output dir, `.env`, `.DS_Store`) and `.env.example` (listing every env var the codebase reads). Build-output directories and OS cruft must not leak into the published tree — they inflate the recipe repo and the v21 apidev run shipped 208 MB of `node_modules` because this check didn't exist.",
			hostname, strings.Join(problems, "; "),
		),
	}}
}
