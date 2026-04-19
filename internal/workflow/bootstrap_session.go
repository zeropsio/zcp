package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// BootstrapSession records the lifecycle of one bootstrap run. It lives at
// state-dir/workflows/bootstrap/{pid}.json so concurrent PIDs don't clash and
// stale sessions are GC'd by the CleanStaleWorkSessions equivalent. Unlike
// WorkSession (which survives compaction within a single LLM task), bootstrap
// sessions end the moment the infrastructure is in place — they are
// short-lived by design.
type BootstrapSession struct {
	Version     string         `json:"version"`
	PID         int            `json:"pid"`
	Route       BootstrapRoute `json:"route"`
	RecipeMatch *RecipeMatch   `json:"recipeMatch,omitempty"`
	Intent      string         `json:"intent"`
	Steps       []StepProgress `json:"steps"`
	CreatedAt   time.Time      `json:"createdAt"`
	ClosedAt    *time.Time     `json:"closedAt,omitempty"`
}

// StepProgress records one sequence step within a BootstrapSession. A step
// completes when Finished is non-nil; it fails when Failures > 0 without a
// matching Finished.
type StepProgress struct {
	Name     string     `json:"name"`
	Started  time.Time  `json:"started"`
	Finished *time.Time `json:"finished,omitempty"`
	Failures int        `json:"failures"`
}

const (
	bootstrapSessionDirName = "bootstrap"
	bootstrapSessionVersion = "1"
)

// bootstrapSessionPath returns the on-disk location for a given PID's
// bootstrap session. Shared between read/write so the two never drift.
func bootstrapSessionPath(stateDir string, pid int) string {
	return filepath.Join(
		stateDir,
		"workflows",
		bootstrapSessionDirName,
		strconv.Itoa(pid)+".json",
	)
}

// NewBootstrapSession constructs a session with the chosen route + initial
// step list. Steps vary by route (§8.5):
//
//	Recipe:  import → wait-active → verify-deploy → verify → close
//	Classic: plan → import → wait-active → verify-deploy-per-runtime → verify → write-metas → close
//	Adopt:   discover → prompt-modes → write-metas → verify → close
//
// Each step starts un-started (Started is zero) and ends un-finished. The
// conductor marks Started/Finished as execution advances.
func NewBootstrapSession(route BootstrapRoute, intent string, match *RecipeMatch) *BootstrapSession {
	now := time.Now().UTC()
	return &BootstrapSession{
		Version:     bootstrapSessionVersion,
		PID:         os.Getpid(),
		Route:       route,
		RecipeMatch: match,
		Intent:      intent,
		Steps:       initialSteps(route),
		CreatedAt:   now,
	}
}

// initialSteps returns the unstarted step list for a given route. Kept as a
// pure function so callers can reference the canonical ordering in tests
// without reconstructing a session.
func initialSteps(route BootstrapRoute) []StepProgress {
	switch route {
	case BootstrapRouteRecipe:
		return namedSteps("import", "wait-active", "verify-deploy", "verify", "close")
	case BootstrapRouteClassic:
		return namedSteps(
			"plan", "import", "wait-active",
			"verify-deploy-per-runtime", "verify",
			"write-metas", "close",
		)
	case BootstrapRouteAdopt:
		return namedSteps("discover", "prompt-modes", "write-metas", "verify", "close")
	}
	return nil
}

func namedSteps(names ...string) []StepProgress {
	out := make([]StepProgress, len(names))
	for i, n := range names {
		out[i] = StepProgress{Name: n}
	}
	return out
}

// SaveBootstrapSession atomically writes the session to disk. The nested
// directory tree is created as needed. Caller is responsible for using the
// same PID across read/write cycles.
func SaveBootstrapSession(stateDir string, sess *BootstrapSession) error {
	if stateDir == "" {
		return fmt.Errorf("save bootstrap session: empty state dir")
	}
	if sess == nil {
		return fmt.Errorf("save bootstrap session: nil session")
	}
	sess.Version = bootstrapSessionVersion
	dir := filepath.Dir(bootstrapSessionPath(stateDir, sess.PID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create bootstrap session dir: %w", err)
	}
	return atomicWriteJSON(dir, ".bootstrap-*.tmp", bootstrapSessionPath(stateDir, sess.PID), sess)
}

// LoadBootstrapSession reads the per-PID session. Returns (nil, nil) when no
// file exists — not an error, just the absence of a bootstrap run.
func LoadBootstrapSession(stateDir string, pid int) (*BootstrapSession, error) {
	if stateDir == "" {
		return nil, nil //nolint:nilnil // absence sentinel
	}
	path := bootstrapSessionPath(stateDir, pid)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil //nolint:nilnil
		}
		return nil, fmt.Errorf("read bootstrap session: %w", err)
	}
	var sess BootstrapSession
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, fmt.Errorf("unmarshal bootstrap session: %w", err)
	}
	return &sess, nil
}

// DeleteBootstrapSession removes the per-PID session file. Idempotent.
func DeleteBootstrapSession(stateDir string, pid int) error {
	if stateDir == "" {
		return nil
	}
	path := bootstrapSessionPath(stateDir, pid)
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("delete bootstrap session: %w", err)
	}
	return nil
}

// MarkStepStarted flips the Started timestamp on the named step. No-op when
// the step doesn't exist or is already started — idempotency keeps the
// conductor free of "did I call this yet?" checks.
func (s *BootstrapSession) MarkStepStarted(name string, now time.Time) {
	for i := range s.Steps {
		if s.Steps[i].Name != name {
			continue
		}
		if s.Steps[i].Started.IsZero() {
			s.Steps[i].Started = now.UTC()
		}
		return
	}
}

// MarkStepFinished flips the Finished timestamp. No-op when the step isn't
// started yet — callers must Start before Finish. That ordering is the one
// invariant the session enforces.
func (s *BootstrapSession) MarkStepFinished(name string, now time.Time) {
	for i := range s.Steps {
		if s.Steps[i].Name != name {
			continue
		}
		if s.Steps[i].Started.IsZero() {
			return
		}
		t := now.UTC()
		s.Steps[i].Finished = &t
		return
	}
}

// RecordStepFailure bumps the Failures counter. Does NOT set Finished —
// failures aren't terminal by themselves; the conductor decides whether a
// failure count exhausts the retry budget and triggers close.
func (s *BootstrapSession) RecordStepFailure(name string) {
	for i := range s.Steps {
		if s.Steps[i].Name != name {
			continue
		}
		s.Steps[i].Failures++
		return
	}
}

// Close marks the session as closed at the given time. A closed session
// stays on disk for short-term forensic reads (e.g. status tool); the
// actual file deletion is a GC concern handled by higher layers.
func (s *BootstrapSession) Close(now time.Time) {
	if s.ClosedAt == nil {
		t := now.UTC()
		s.ClosedAt = &t
	}
}

// IsComplete returns true when every step has a non-nil Finished.
func (s *BootstrapSession) IsComplete() bool {
	for _, step := range s.Steps {
		if step.Finished == nil {
			return false
		}
	}
	return true
}
