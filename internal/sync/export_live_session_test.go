package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/workflow"
)

// TestExport_RefusesSessionlessWhenLiveSessionExists is the Cx-CLOSE-STEP-GATE-HARD
// RED→GREEN test. v36 F-8/F-11: sessionless `zcp sync recipe export`
// bypasses the close-step gate with only an advisory stderr note.
// The agent (or a confused user) can invoke it at any time, even
// mid-session, and the engine cannot refuse because it cannot tell
// whether the caller is "sessionless by design" or "forgot the
// --session flag". Result on v36: agent ran sessionless export at
// 16:04 UTC while close was in_progress; editorial-review never ran;
// deliverable shipped without staged writer content.
//
// Fix: before accepting sessionless export, walk the state registry
// and match any active session whose OutputDir equals the target
// recipeDir. If found, refuse with ErrExportBlocked naming the
// session ID and remediation commands. `--force-export` stays as
// explicit escape (prints stderr warning, same path as before).
func TestExport_RefusesSessionlessWhenLiveSessionExists(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	stateDir := filepath.Join(root, ".zcp", "state")

	// Build recipe output tree (OutputDir).
	recipeDir := minimalRecipeDir(t, root)

	// Register a live session whose state.Recipe.OutputDir == recipeDir.
	sessionID := "sess-live-xyz"
	writeSessionState(t, stateDir, sessionID, withOutputDir(recipeStateWithCloseStatus("in_progress"), recipeDir))
	if err := workflow.RegisterSession(stateDir, workflow.SessionEntry{
		SessionID: sessionID, PID: os.Getpid(), Workflow: "recipe",
	}); err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}

	// Sessionless export (no --session, no env var).
	_, err := ExportRecipe(ExportOpts{
		RecipeDir:       recipeDir,
		SessionStateDir: stateDir,
	})
	if err == nil {
		t.Fatal("expected ErrExportBlocked for sessionless export with live session")
	}
	msg := err.Error()
	if !strings.Contains(msg, "EXPORT_BLOCKED") {
		t.Errorf("error must carry EXPORT_BLOCKED code; got: %s", msg)
	}
	if !strings.Contains(msg, sessionID) {
		t.Errorf("error must name the live session ID %q; got: %s", sessionID, msg)
	}
	if !strings.Contains(msg, "--session") {
		t.Errorf("error must name --session remediation; got: %s", msg)
	}
}

// TestExport_AllowsSessionlessWhenNoLiveSessionMatches verifies the
// gate only fires when a live session's OutputDir matches the
// target. Ad-hoc exports of unrelated recipe dirs (no matching
// session) proceed — the gate doesn't block legitimate sessionless
// flows.
func TestExport_AllowsSessionlessWhenNoLiveSessionMatches(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	stateDir := filepath.Join(root, ".zcp", "state")
	recipeDir := minimalRecipeDir(t, root)

	// Register a live session for a DIFFERENT recipeDir.
	otherDir := filepath.Join(root, "other-showcase")
	writeFile(t, filepath.Join(otherDir, "README.md"), "# other")
	writeSessionState(t, stateDir, "sess-other", withOutputDir(recipeStateWithCloseStatus("in_progress"), otherDir))
	if err := workflow.RegisterSession(stateDir, workflow.SessionEntry{
		SessionID: "sess-other", PID: os.Getpid(), Workflow: "recipe",
	}); err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}

	result, err := ExportRecipe(ExportOpts{
		RecipeDir:       recipeDir,
		SessionStateDir: stateDir,
	})
	if err != nil {
		t.Fatalf("unrelated live session should not block export of %s: %v", recipeDir, err)
	}
	if result == nil || result.ArchivePath == "" {
		t.Errorf("expected archive produced; got result=%+v", result)
	}
	os.Remove(result.ArchivePath)
}

// TestExport_ForceExportBypassesLiveSessionGate verifies --force-export
// still lets the author escape when they know what they're doing.
func TestExport_ForceExportBypassesLiveSessionGate(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	stateDir := filepath.Join(root, ".zcp", "state")
	recipeDir := minimalRecipeDir(t, root)
	sessionID := "sess-bypass"
	writeSessionState(t, stateDir, sessionID, withOutputDir(recipeStateWithCloseStatus("in_progress"), recipeDir))
	if err := workflow.RegisterSession(stateDir, workflow.SessionEntry{
		SessionID: sessionID, PID: os.Getpid(), Workflow: "recipe",
	}); err != nil {
		t.Fatalf("RegisterSession: %v", err)
	}

	result, err := ExportRecipe(ExportOpts{
		RecipeDir:       recipeDir,
		SessionStateDir: stateDir,
		SkipCloseGate:   true,
	})
	if err != nil {
		t.Fatalf("--force-export should bypass live-session gate: %v", err)
	}
	if result == nil {
		t.Fatalf("expected archive produced")
	}
	os.Remove(result.ArchivePath)
}

func withOutputDir(rs *workflow.RecipeState, outputDir string) *workflow.RecipeState {
	rs.OutputDir = outputDir
	return rs
}
