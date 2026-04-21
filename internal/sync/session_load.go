package sync

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/zeropsio/zcp/internal/workflow"
)

// resolveSessionID returns the session ID to use for export gating and
// the source label for error messages. Precedence: explicit --session
// flag value → $ZCP_SESSION_ID env var → empty string (ad-hoc CLI).
//
// The source label lets the gate error name WHERE the ID came from so
// the author can correct the specific input.
func resolveSessionID(optExplicit string) (id, source string) {
	if optExplicit != "" {
		return optExplicit, "--session"
	}
	if env := os.Getenv("ZCP_SESSION_ID"); env != "" {
		return env, "$ZCP_SESSION_ID"
	}
	return "", ""
}

// loadRecipeSession reads the per-session state file and returns its
// RecipeState. The state dir follows the standard sessions/{id}.json
// layout. When sessionStateDir is empty, the default {projectRoot}/.zcp/state
// path is used (derived from CWD's .zcp/state directory, the same
// convention the workflow package applies).
func loadRecipeSession(sessionStateDir, sessionID string) (*workflow.RecipeState, error) {
	dir := sessionStateDir
	if dir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("resolve cwd: %w", err)
		}
		dir = filepath.Join(cwd, ".zcp", "state")
	}
	state, err := workflow.LoadSessionByID(dir, sessionID)
	if err != nil {
		return nil, err
	}
	if state == nil || state.Recipe == nil {
		return nil, fmt.Errorf("session %q has no recipe state", sessionID)
	}
	return state.Recipe, nil
}

// recipeStepStatus returns the status of the named recipe step in the
// state, or empty string if the step is not present. Used by the export
// gate to check close-step status.
func recipeStepStatus(state *workflow.RecipeState, stepName string) string {
	if state == nil {
		return ""
	}
	for _, s := range state.Steps {
		if s.Name == stepName {
			return s.Status
		}
	}
	return ""
}

// findLiveSessionForRecipe walks the registry and returns the first
// active recipe session whose state.Recipe.OutputDir matches
// opts.RecipeDir (absolute-path comparison). Used by the Cx-CLOSE-STEP-
// GATE-HARD branch that turns sessionless export into a hard refusal
// when an obviously-bound session exists.
//
// Errors from registry / session reads are swallowed — the gate's
// fallback is "skip, no session context" so a transient registry
// read failure shouldn't destabilize legitimate ad-hoc exports.
func findLiveSessionForRecipe(opts ExportOpts) (string, bool, error) {
	if opts.RecipeDir == "" {
		return "", false, nil
	}
	absTarget, err := filepath.Abs(opts.RecipeDir)
	if err != nil {
		return "", false, err
	}
	dir := opts.SessionStateDir
	if dir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", false, err
		}
		dir = filepath.Join(cwd, ".zcp", "state")
	}
	sessions, err := workflow.ListSessions(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	for _, s := range sessions {
		if s.Workflow != "recipe" {
			continue
		}
		state, err := workflow.LoadSessionByID(dir, s.SessionID)
		if err != nil || state == nil || state.Recipe == nil {
			continue
		}
		if state.Recipe.OutputDir == "" {
			continue
		}
		absOutput, err := filepath.Abs(state.Recipe.OutputDir)
		if err != nil {
			continue
		}
		if absOutput == absTarget {
			return s.SessionID, true, nil
		}
	}
	return "", false, nil
}
