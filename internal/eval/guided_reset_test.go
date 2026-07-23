package eval

import (
	"testing"

	"github.com/zeropsio/zcp/internal/content"
)

func TestResetGuidedForScenario_RemovesMarker(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	if err := content.SetGuided(workDir, true); err != nil {
		t.Fatalf("SetGuided(on): %v", err)
	}
	if !content.GuidedEnabled(workDir) {
		t.Fatal("guided marker missing before reset")
	}

	if err := resetGuidedForScenario(workDir); err != nil {
		t.Fatalf("resetGuidedForScenario(marker present): %v", err)
	}
	if content.GuidedEnabled(workDir) {
		t.Error("guided marker remains after reset")
	}
	if err := resetGuidedForScenario(workDir); err != nil {
		t.Fatalf("resetGuidedForScenario(marker absent): %v", err)
	}
}
