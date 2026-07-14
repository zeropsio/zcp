//go:build captureinspector_browser

package web

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBrowserSmoke(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("node"); err != nil {
		t.Fatalf("capture inspector browser test requires node: %v", err)
	}
	server, _, _ := newTestServer(t)
	script := filepath.Join("..", "..", "browsertest", "smoke.cjs")
	command := exec.CommandContext(t.Context(), "node", script, server.LaunchURL()) //nolint:gosec // fixed test harness and local one-time URL
	command.Env = append(os.Environ(), "ZCP_BROWSER_OUTPUT="+t.TempDir())
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("capture inspector browser smoke failed: %v\n%s", err, output)
	}
	t.Logf("browser smoke:\n%s", output)
}
