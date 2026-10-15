//go:build linux

package ops

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// browserMainPID is the warm browser's daemon, or "0" when none runs.
func browserMainPID(t *testing.T) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "systemctl", "show", browserUnit, "--property=MainPID", "--value").Output()
	if err != nil {
		t.Fatalf("systemctl show: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// Run on a Linux systemd container with agent-browser, explicitly selected
// by the operator. Non-parallel: the warm browser is one per container.
func TestWarmBrowser_RealChrome_ReusedUntilACallEndsUnclean(t *testing.T) {
	if os.Getenv("ZCP_BROWSER_REAL_TEST") != "1" {
		t.Skip("requires a systemd container with agent-browser")
	}
	ctx := context.Background()
	page := "data:text/html,<h1>warm</h1>"
	walk := func(in BrowserBatchInput) *BrowserBatchResult {
		t.Helper()
		in.URL = page
		r, err := BrowserBatch(ctx, in)
		if err != nil {
			t.Fatalf("BrowserBatch(%+v): %v", in, err)
		}
		return r
	}

	if r := walk(BrowserBatchInput{Isolation: BrowserIsolationNewBrowser}); r.BrowserRecycled || r.Message != "" {
		t.Fatalf("first call: recycled=%v message=%q", r.BrowserRecycled, r.Message)
	}
	first := browserMainPID(t)
	if first == "0" {
		t.Fatal("no warm browser after a clean call")
	}
	walk(BrowserBatchInput{Commands: [][]string{{"get", "text", "body"}}})
	walk(BrowserBatchInput{Isolation: BrowserIsolationFreshContext})
	if got := browserMainPID(t); got != first {
		t.Errorf("tab and fresh-context calls must reuse the browser: pid %s, then %s", first, got)
	}
	walk(BrowserBatchInput{Isolation: BrowserIsolationNewBrowser})
	if got := browserMainPID(t); got == first || got == "0" {
		t.Errorf("new-browser must start a fresh browser: pid %s, then %s", first, got)
	}

	cut := walk(BrowserBatchInput{TimeoutSeconds: 2, Commands: [][]string{{"wait", "10000"}}})
	if !cut.BrowserRecycled || len(cut.Steps) < 2 {
		t.Errorf("a cut call must keep its finished steps and recycle: recycled=%v steps=%d", cut.BrowserRecycled, len(cut.Steps))
	}
	if got := browserMainPID(t); got != "0" {
		t.Errorf("the browser must be gone after a cut call, main pid %s", got)
	}
	if _, err := os.Stat(browserTmpDir); !os.IsNotExist(err) {
		t.Errorf("the browser profile must be removed with the browser: %v", err)
	}
}
