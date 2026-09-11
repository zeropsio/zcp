package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFarmConsole_RefusesWithoutConsoleToken pins docs/spec-eval-farm.md
// §8.1 FM-49: the console never starts without ZCP_FARM_CONSOLE_TOKEN —
// refused before any farm-bucket config is even read, so a missing sink
// env var never masks the real problem.
func TestFarmConsole_RefusesWithoutConsoleToken(t *testing.T) {
	t.Setenv("ZCP_FARM_CONSOLE_TOKEN", "")
	t.Setenv("ZCP_FARM_S3_URL", "")
	t.Setenv("ZCP_FARM_S3_BUCKET", "")
	t.Setenv("ZCP_FARM_S3_KEY", "")
	t.Setenv("ZCP_FARM_S3_SECRET", "")

	if got := runFarmConsole(nil); got != 1 {
		t.Errorf("runFarmConsole with no ZCP_FARM_CONSOLE_TOKEN: got exit %d, want 1", got)
	}
}

// TestConsole_KillSwitchEnvDisablesWorker pins §8.1/§8.5: ZCP_FARM_OBSERVER
// =off is the console-wide kill switch, read once via
// observerKillSwitchEnabled.
//
// non-parallel: t.Setenv
func TestConsole_KillSwitchEnvDisablesWorker(t *testing.T) {
	t.Setenv("ZCP_FARM_OBSERVER", "off")
	if !observerKillSwitchEnabled() {
		t.Error("observerKillSwitchEnabled() = false with ZCP_FARM_OBSERVER=off, want true")
	}

	t.Setenv("ZCP_FARM_OBSERVER", "")
	if observerKillSwitchEnabled() {
		t.Error("observerKillSwitchEnabled() = true with ZCP_FARM_OBSERVER unset, want false")
	}
}

// TestConsole_RelativeClaudeFlagResolvedAtStartup pins §7.4 FM-44: the
// --claude path is made absolute (exec.LookPath, then filepath.Abs) before
// the child starts, because the child's working directory is a fresh empty
// temp dir — so a relative --claude value must resolve to an absolute
// path, not be passed through as-is.
//
// non-parallel: os.Chdir
func TestConsole_RelativeClaudeFlagResolvedAtStartup(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "fakeclaude")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	t.Chdir(dir)

	got := resolveClaudePath("./fakeclaude")
	if got == "" {
		t.Fatal(`resolveClaudePath("./fakeclaude") = "", want a resolved absolute path`)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("resolveClaudePath(\"./fakeclaude\") = %q, want an absolute path", got)
	}
}
