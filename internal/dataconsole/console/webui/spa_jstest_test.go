package webui

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestDataConsoleSPAPureJS runs the Data Console SPA pure-logic JavaScript
// tests with node. The tests live outside dist/ so webui/embed.go does not
// embed or serve them; each test require()s the shipped no-build module it
// covers from ../dist/.
func TestDataConsoleSPAPureJS(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Data Console SPA JS tests in short mode")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		fatal, msg := jsGateAction(jsGateRequired(), "node")
		if fatal {
			t.Fatal(msg)
		}
		t.Skip(msg)
	}

	const jsDir = "spa"
	tests, err := filepath.Glob(filepath.Join(jsDir, "*.test.js"))
	if err != nil {
		t.Fatalf("glob spa js tests: %v", err)
	}
	if len(tests) == 0 {
		t.Fatal("no spa/*.test.js found - the Data Console SPA JS test seam is missing")
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
