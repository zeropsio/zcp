package workflow

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

func TestWriteStrategyNote_Empty(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	writeStrategyNote(&sb, "")
	note := sb.String()
	if !strings.Contains(note, "Not set") {
		t.Errorf("empty strategy should say 'Not set', got: %s", note)
	}
	for _, s := range []string{"push-dev", "push-git", "manual"} {
		if !strings.Contains(note, s) {
			t.Errorf("should list %q as option, got: %s", s, note)
		}
	}
}

func TestWriteStrategyNote_Set(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		strategy topology.DeployStrategy
		wantAlts []string
	}{
		{"push-dev", topology.StrategyPushDev, []string{"push-git", "manual"}},
		{"push-git", topology.StrategyPushGit, []string{"push-dev", "manual"}},
		{"manual", topology.StrategyManual, []string{"push-dev", "push-git"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var sb strings.Builder
			writeStrategyNote(&sb, tt.strategy)
			note := sb.String()
			if !strings.Contains(note, "Currently: "+string(tt.strategy)) {
				t.Errorf("should say 'Currently: %s', got: %s", tt.strategy, note)
			}
			for _, alt := range tt.wantAlts {
				if !strings.Contains(note, alt) {
					t.Errorf("should list %q as alternative, got: %s", alt, note)
				}
			}
		})
	}
}
