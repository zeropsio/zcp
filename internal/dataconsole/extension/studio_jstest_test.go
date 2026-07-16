package extension_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestStudioExtensionJS runs the Zerops Studio extension's JavaScript test
// suite (internal/dataconsole/extension/studiojs/*.test.js) with node. These are the
// "real-but-lean" JS logic tests the Studio PRD's test seam calls for — they
// EXECUTE the shipped extension logic (discoverToUIMap must-pin, the E1/E2
// directory-discovery contract, the branded shell render) rather than
// string-matching the template, so a dropped key or a duplicate handler type is
// caught by running the code.
//
// Skips cleanly when node isn't on PATH (e.g. a Go-only environment); on a dev
// machine and CI (where node is present) it runs. Each *.test.js exits non-zero
// on failure; a sibling slice adds its own *.test.js and it is picked up here.
func TestStudioExtensionJS(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found on PATH; skipping Studio extension JS tests")
	}

	const jsDir = "studiojs"
	tests, err := filepath.Glob(filepath.Join(jsDir, "*.test.js"))
	if err != nil {
		t.Fatalf("glob studio js tests: %v", err)
	}
	if len(tests) == 0 {
		t.Fatal("no studiojs/*.test.js found — the Studio JS test seam is missing")
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
