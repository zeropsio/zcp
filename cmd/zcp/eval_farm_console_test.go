package main

import "testing"

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
