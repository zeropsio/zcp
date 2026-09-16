package tools

import (
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestGitPush_DefaultBranchFromMeta pins GF-7 (docs/spec-workflows.md
// §12.6): the container git-push default branch reads meta.TrackedRef —
// falling back to "main" only when meta carries none — instead of the old
// hardcoded "main" literal. An explicit input branch always overrides.
func TestGitPush_DefaultBranchFromMeta(t *testing.T) {
	t.Parallel()

	t.Run("recorded tracked ref wins over the hardcoded default", func(t *testing.T) {
		t.Parallel()
		stateDir := t.TempDir()
		if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
			Hostname:         "appdev",
			Mode:             topology.PlanModeSimple,
			TrackedRef:       "release",
			BootstrapSession: "t",
			BootstrappedAt:   "2026-09-15",
		}); err != nil {
			t.Fatalf("WriteServiceMeta: %v", err)
		}
		if got := resolveTrackedBranch(stateDir, "appdev", ""); got != "release" {
			t.Errorf("resolveTrackedBranch = %q, want %q (recorded trackedRef)", got, "release")
		}
	})

	t.Run("no recorded trackedRef falls back to main", func(t *testing.T) {
		t.Parallel()
		stateDir := t.TempDir()
		if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
			Hostname:         "appdev",
			Mode:             topology.PlanModeSimple,
			BootstrapSession: "t",
			BootstrappedAt:   "2026-09-15",
		}); err != nil {
			t.Fatalf("WriteServiceMeta: %v", err)
		}
		if got := resolveTrackedBranch(stateDir, "appdev", ""); got != "main" {
			t.Errorf("resolveTrackedBranch = %q, want %q (fallback)", got, "main")
		}
	})

	t.Run("no meta at all falls back to main", func(t *testing.T) {
		t.Parallel()
		if got := resolveTrackedBranch(t.TempDir(), "unknown-host", ""); got != "main" {
			t.Errorf("resolveTrackedBranch = %q, want %q (fallback, no meta)", got, "main")
		}
	})

	t.Run("explicit input branch overrides the recorded trackedRef", func(t *testing.T) {
		t.Parallel()
		stateDir := t.TempDir()
		if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
			Hostname:         "appdev",
			Mode:             topology.PlanModeSimple,
			TrackedRef:       "release",
			BootstrapSession: "t",
			BootstrappedAt:   "2026-09-15",
		}); err != nil {
			t.Fatalf("WriteServiceMeta: %v", err)
		}
		if got := resolveTrackedBranch(stateDir, "appdev", "hotfix"); got != "hotfix" {
			t.Errorf("resolveTrackedBranch = %q, want %q (explicit override)", got, "hotfix")
		}
	})
}

// TestTrackedRefOrDefault_NilMeta_ReturnsMain pins the nil-safety of the
// single "main" fallback owner shared by every GF-7 reader.
func TestTrackedRefOrDefault_NilMeta_ReturnsMain(t *testing.T) {
	t.Parallel()
	if got := trackedRefOrDefault(nil); got != "main" {
		t.Errorf("trackedRefOrDefault(nil) = %q, want %q", got, "main")
	}
}
