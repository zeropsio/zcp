package tools

import (
	"os"
	"path/filepath"
	"strings"
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

// TestGitPush_ACodeVolumePairPushesTheVolumesCommits: a variant-B pair's
// repository lives on its code volume (ServiceMeta.WorkingDirFor), so a
// git-push from its dev half without workingDir pushes the commits made
// there — never /var/www's deploy copy, which holds none of them.
func TestGitPush_ACodeVolumePairPushesTheVolumesCommits(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	volume := lab.fillCodeVolume()
	if err := os.WriteFile(filepath.Join(volume, "footer.js"), []byte("the footer\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lab.git("-C", volume, "add", "-A")
	lab.git("-C", volume, "-c", "user.name=mate", "-c", "user.email=mate@example.invalid", "commit", "-qm", "Add a footer")
	lab.hq.landOnMain("appdev", map[string]string{"README.md": "another Mate's work\n"})
	deployCopyHead := lab.git("rev-parse", "HEAD")

	text := getTextContent(t, callTool(t, lab.gitPushTool(), "zerops_deploy", map[string]any{"targetService": "appdev", "strategy": "git-push"}))
	if !strings.Contains(text, `"status":"PUSHED"`) {
		t.Fatalf("the volume's commit must be pushed:\n%s", text)
	}
	if head, want := lab.remoteHead("mate/p-mate/1"), lab.git("-C", volume, "rev-parse", "HEAD"); head != want {
		t.Errorf("change #1's branch is at %q, want the volume's HEAD %q", head, want)
	}
	if !lab.descends(labApp, "main", "mate/p-mate/1") {
		t.Errorf("change #1's branch must take main in on the volume before it is pushed")
	}
	if got := lab.git("rev-parse", "HEAD"); got != deployCopyHead {
		t.Errorf("/var/www's checkout moved to %s: the push worked there, not on the volume", got)
	}
}
