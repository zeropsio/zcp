package sync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/workflow"
)

// writeSessionState is a test helper that writes a WorkflowState JSON
// under sessions/{id}.json inside stateDir so LoadSessionByID can read it.
func writeSessionState(t *testing.T, stateDir, sessionID string, recipe *workflow.RecipeState) {
	t.Helper()
	state := &workflow.WorkflowState{
		Version:   "1",
		SessionID: sessionID,
		PID:       os.Getpid(),
		Workflow:  "recipe",
		Recipe:    recipe,
	}
	dir := filepath.Join(stateDir, "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(dir, sessionID+".json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// recipeStateWithCloseStatus returns a minimal RecipeState where the
// close step carries the given status.
func recipeStateWithCloseStatus(status string) *workflow.RecipeState {
	steps := []workflow.RecipeStep{
		{Name: "research", Status: "complete"},
		{Name: "provision", Status: "complete"},
		{Name: "generate", Status: "complete"},
		{Name: "deploy", Status: "complete"},
		{Name: "finalize", Status: "complete"},
		{Name: "close", Status: status},
	}
	return &workflow.RecipeState{Active: true, Tier: "showcase", Steps: steps}
}

// minimalRecipeDir sets up a minimal recipe dir with one env folder so
// ExportRecipe can proceed past the directory existence check.
func minimalRecipeDir(t *testing.T, root string) string {
	t.Helper()
	recipeDir := filepath.Join(root, "test-showcase")
	writeFile(t, filepath.Join(recipeDir, "README.md"), "# root")
	writeFile(t, filepath.Join(recipeDir, "environments", "0 \u2014 AI Agent", "import.yaml"), "project:\n")
	return recipeDir
}

// TestExportRecipe_RefusesWhenCloseInProgress — v8.97 Fix 1.
// A declared session with close step = in_progress must refuse export
// with ErrExportBlocked and a message naming the in_progress status.
func TestExportRecipe_RefusesWhenCloseInProgress(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	stateDir := filepath.Join(root, ".zcp", "state")
	writeSessionState(t, stateDir, "sess-inprog", recipeStateWithCloseStatus("in_progress"))

	_, err := ExportRecipe(ExportOpts{
		RecipeDir:       minimalRecipeDir(t, root),
		SessionID:       "sess-inprog",
		SessionStateDir: stateDir,
	})
	if err == nil {
		t.Fatal("expected ErrExportBlocked, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "EXPORT_BLOCKED") {
		t.Errorf("error message missing EXPORT_BLOCKED code; got: %s", msg)
	}
	if !strings.Contains(msg, "in_progress") {
		t.Errorf("error message must name close step status \"in_progress\"; got: %s", msg)
	}
}

// TestExportRecipe_AllowsWhenCloseComplete — v8.97 Fix 1.
// A declared session with close=complete proceeds to produce an archive.
func TestExportRecipe_AllowsWhenCloseComplete(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	stateDir := filepath.Join(root, ".zcp", "state")
	writeSessionState(t, stateDir, "sess-done", recipeStateWithCloseStatus("complete"))

	result, err := ExportRecipe(ExportOpts{
		RecipeDir:       minimalRecipeDir(t, root),
		SessionID:       "sess-done",
		SessionStateDir: stateDir,
	})
	if err != nil {
		t.Fatalf("expected export success, got: %v", err)
	}
	if result == nil || result.ArchivePath == "" {
		t.Fatalf("expected archive path, got %+v", result)
	}
}

// TestExportRecipe_ForceExportBypassWarning — v8.97 Fix 1.
// SkipCloseGate bypasses the gate AND prints a stderr warning when a
// session is declared but close is not complete.
func TestExportRecipe_ForceExportBypassWarning(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	stateDir := filepath.Join(root, ".zcp", "state")
	writeSessionState(t, stateDir, "sess-force", recipeStateWithCloseStatus("in_progress"))

	// Capture stderr.
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w

	_, exportErr := ExportRecipe(ExportOpts{
		RecipeDir:       minimalRecipeDir(t, root),
		SessionID:       "sess-force",
		SessionStateDir: stateDir,
		SkipCloseGate:   true,
	})
	w.Close()
	os.Stderr = origStderr

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	stderrText := string(buf[:n])

	if exportErr != nil {
		t.Fatalf("expected force-export success, got: %v", exportErr)
	}
	if !strings.Contains(stderrText, "--force-export bypasses the close-step gate") {
		t.Errorf("expected stderr to contain bypass warning; got: %s", stderrText)
	}
}

// TestExportRecipe_DeclaredSessionWithMissingStateReturnsBlocked — v8.97 Fix 1.
// A --session ID that points at a non-existent state file returns
// EXPORT_BLOCKED with a message that names the session ID AND the source
// label (--session) so the author can correct the specific input.
func TestExportRecipe_DeclaredSessionWithMissingStateReturnsBlocked(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	stateDir := filepath.Join(root, ".zcp", "state")
	if err := os.MkdirAll(filepath.Join(stateDir, "sessions"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	_, err := ExportRecipe(ExportOpts{
		RecipeDir:       minimalRecipeDir(t, root),
		SessionID:       "nonexistent",
		SessionStateDir: stateDir,
	})
	if err == nil {
		t.Fatal("expected ErrExportBlocked for missing session state")
	}
	msg := err.Error()
	if !strings.Contains(msg, "EXPORT_BLOCKED") {
		t.Errorf("error missing EXPORT_BLOCKED code; got: %s", msg)
	}
	if !strings.Contains(msg, "nonexistent") {
		t.Errorf("error must name session ID; got: %s", msg)
	}
	if !strings.Contains(msg, "--session") {
		t.Errorf("error must name source label --session; got: %s", msg)
	}
}

// TestExportRecipe_NoSessionContextSkipsGate — v8.97 Fix 1.
// Ad-hoc CLI export (both --session AND $ZCP_SESSION_ID unset) skips the
// gate AND prints a stderr note for transparency. Export proceeds.
func TestExportRecipe_NoSessionContextSkipsGate(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	// Ensure env var is unset.
	t.Setenv("ZCP_SESSION_ID", "")

	// Capture stderr.
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w

	result, exportErr := ExportRecipe(ExportOpts{
		RecipeDir: minimalRecipeDir(t, root),
	})
	w.Close()
	os.Stderr = origStderr

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	stderrText := string(buf[:n])

	if exportErr != nil {
		t.Fatalf("expected export success for ad-hoc CLI mode, got: %v", exportErr)
	}
	if result == nil || result.ArchivePath == "" {
		t.Fatalf("expected archive path, got %+v", result)
	}
	if !strings.Contains(stderrText, "no session context") {
		t.Errorf("expected stderr to contain \"no session context\" note; got: %s", stderrText)
	}
}

// TestResolveSessionID_PrecedenceExplicitOverEnv — v8.97 Fix 1.
// When both the --session flag and $ZCP_SESSION_ID are set, the flag
// wins and the source label reflects "--session".
func TestResolveSessionID_PrecedenceExplicitOverEnv(t *testing.T) {
	t.Setenv("ZCP_SESSION_ID", "env-sess")

	id, source := resolveSessionID("flag-sess")
	if id != "flag-sess" {
		t.Errorf("expected flag to win; got id=%q", id)
	}
	if source != "--session" {
		t.Errorf("expected source label \"--session\"; got %q", source)
	}

	// Fall back to env when flag empty.
	id, source = resolveSessionID("")
	if id != "env-sess" {
		t.Errorf("expected env fallback; got id=%q", id)
	}
	if source != "$ZCP_SESSION_ID" {
		t.Errorf("expected source label \"$ZCP_SESSION_ID\"; got %q", source)
	}

	// Empty when neither is set.
	t.Setenv("ZCP_SESSION_ID", "")
	id, source = resolveSessionID("")
	if id != "" {
		t.Errorf("expected empty id when neither set; got %q", id)
	}
	if source != "" {
		t.Errorf("expected empty source when neither set; got %q", source)
	}
}
