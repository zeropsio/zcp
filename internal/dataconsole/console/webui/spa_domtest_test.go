package webui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestDataConsoleSPADOM runs the Data Console SPA's jsdom-backed DOM tests
// (domtest/*.dom.test.js) with node. Unlike TestDataConsoleSPAPureJS (the
// side-effect-free dc-*.test.js suite, which only needs node), these tests
// additionally need the jsdom devDependency installed via `npm ci` in this
// directory (see package.json + package-lock.json) — so on top of the
// existing node-on-PATH skip, this test also skips cleanly when
// node_modules/jsdom is absent (a node-less or npm-ci-less CI, or a dev
// checkout that hasn't run `npm ci` yet) rather than failing hard.
//
// One-command local run: `npm ci && node domtest/xss.dom.test.js` (or any
// other domtest/*.dom.test.js file) from this directory.
func TestDataConsoleSPADOM(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Data Console SPA DOM tests in short mode")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found on PATH; skipping Data Console SPA DOM tests")
	}
	if _, err := os.Stat("node_modules/jsdom"); err != nil {
		t.Skip("node_modules/jsdom not installed; run `npm ci` in internal/dataconsole/console/webui to enable the DOM test suite")
	}

	const jsDir = "domtest"
	tests, err := filepath.Glob(filepath.Join(jsDir, "*.dom.test.js"))
	if err != nil {
		t.Fatalf("glob domtest js tests: %v", err)
	}
	if len(tests) == 0 {
		t.Fatal("no domtest/*.dom.test.js found - the Data Console SPA DOM test seam is missing")
	}

	for _, tf := range tests {
		name := filepath.Base(tf)
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, node, name)
			cmd.Dir = jsDir
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("node %s failed: %v\n%s", name, err, out)
			}
		})
	}
}
