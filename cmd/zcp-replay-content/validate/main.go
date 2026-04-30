// validate runs checkSlotShapeWithPlan over every fragment in a replay
// output directory. Maps the file-path naming convention back to the
// canonical fragment id for the dispatcher.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/recipe"
)

func main() {
	fragsDir := flag.String("frags", "/tmp/replay/fragments-new", "directory of generated fragments (per-codebase subdirs)")
	planDir := flag.String("plan", "docs/zcprecipator3/runs/17/environments", "dir containing plan.json")
	flag.Parse()

	plan, err := recipe.ReadPlan(*planDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read plan:", err)
		os.Exit(1)
	}

	totalFiles := 0
	totalFires := 0
	for _, cb := range plan.Codebases {
		cbDir := filepath.Join(*fragsDir, cb.Hostname)
		entries, err := os.ReadDir(cbDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip %s (%v)\n", cbDir, err)
			continue
		}
		fmt.Printf("\n=== %s ===\n", cb.Hostname)
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			fragmentID := pathNameToFragmentID(e.Name(), cb.Hostname)
			body, err := os.ReadFile(filepath.Join(cbDir, e.Name()))
			if err != nil {
				fmt.Fprintf(os.Stderr, "read %s: %v\n", e.Name(), err)
				continue
			}
			totalFiles++
			refusals := recipe.CheckSlotShapeForReplay(fragmentID, string(body), plan)
			if len(refusals) == 0 {
				fmt.Printf("  ✓ %s (%s)\n", e.Name(), fragmentID)
				continue
			}
			totalFires++
			fmt.Printf("  ✗ %s (%s) — %d refusal(s):\n", e.Name(), fragmentID, len(refusals))
			for _, r := range refusals {
				fmt.Printf("      • %s\n", truncate(r, 200))
			}
		}
	}
	fmt.Printf("\n=== summary ===\n%d fragments scanned, %d fired refusals\n", totalFiles, totalFires)
}

// pathNameToFragmentID maps the file naming convention (used by the
// replay agent — `__` separator instead of `/`) back to the canonical
// fragment id. e.g. `codebase__api__integration-guide__2.md` →
// `codebase/api/integration-guide/2`.
func pathNameToFragmentID(filename, _ string) string {
	id := strings.TrimSuffix(filename, ".md")
	return strings.ReplaceAll(id, "__", "/")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
