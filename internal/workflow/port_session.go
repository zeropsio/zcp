package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"github.com/zeropsio/zcp/internal/topology"
)

const (
	portSessionDirName = "port"
	portSessionVersion = "1"
)

// PortSession is the per-PID sidecar for the OSS port workflow, persisted at
// .zcp/state/port/{pid}.json. It mirrors the WorkSession sidecar conventions
// (per-PID path, JSON, atomic write, dies with the process) but lives in its
// own directory so the port loop's iteration state is independent of the
// develop work session.
//
// It WRAPS the WorkSession (the port loop reuses work_session's deploy/verify
// record/load/save + CloseReasonIterationCap) and holds the recon PortPlan plus
// a placeholder for the iteration state later phases attach (fix-class history,
// stall counters, FitCeiling). Phase 0 populates Plan only.
type PortSession struct {
	Version string `json:"version"`
	PID     int    `json:"pid"`
	// StartTime mirrors WorkSession's (pid,startTime) identity guard so a
	// recycled PID can't inherit a dead predecessor's port session.
	StartTime string `json:"startTime,omitempty"`
	// WorkSession is the wrapped work session — the port loop records its
	// deploy/verify attempts here, reusing the existing work_session machinery.
	WorkSession *WorkSession `json:"workSession,omitempty"`
	// Plan is the recon classification (Stage A0). Set once at start.
	Plan PortPlan `json:"plan"`
	// Iteration is the loop turn counter — bumped once per recorded attempt.
	Iteration int `json:"iteration"`
	// Attempts is the per-iteration failure-class + signals + derived-fix
	// history of the deploy-debug loop. It lives on the PortSession (NOT on the
	// shared work_session DeployAttempt — low blast radius) precisely because
	// the fix-class table dispatches on SIGNAL IDs and DeployAttempt persists
	// only the coarse FailureClass category. Phase 2's two-counter stall
	// detection reads this history: classStallStreak keys on FailureClass,
	// phaseStallStreak keys on the failedPhase non-advancement implied by the
	// FixClass kind. Phase 1 only writes it; Phase 2 consumes it.
	Attempts []PortAttempt `json:"attempts,omitempty"`
}

// PortAttempt is one deploy-debug loop turn's observed failure + the fix-class
// the handler derived from it. The agent runs the deploy via the existing tools,
// observes the FailureClassification (class + signals), and passes both into the
// iterate handler, which records this entry. Phase 2 reads the Attempts history
// for stall detection — the persisted Signals are what make signal-level
// dispatch survive across turns (DeployAttempt drops them).
type PortAttempt struct {
	// Iteration is the loop turn this attempt belongs to (1-based).
	Iteration int `json:"iteration"`
	// RecordedAt is the RFC3339 timestamp the attempt was recorded.
	RecordedAt string `json:"recordedAt"`
	// Hostname is the deploy target the agent observed (optional — the agent
	// may report a project-level failure with no single hostname).
	Hostname string `json:"hostname,omitempty"`
	// Class is the observed FailureClass the agent read off the live
	// DeployFailureClassification. Empty when the agent reports success.
	Class topology.FailureClass `json:"class,omitempty"`
	// Signals are the observed signal IDs (e.g. "build:command-not-found").
	// Persisted here because the shared DeployAttempt does not carry them.
	Signals []string `json:"signals,omitempty"`
	// FixKind is the deterministic fix-class the handler derived. Empty when
	// the agent reported success (no fix needed).
	FixKind FixClassKind `json:"fixKind,omitempty"`
	// Escalate mirrors the derived PortFixClass.Escalate — an in-band
	// unfixable (build OOM). Phase 2 uses it as a hard escalation signal.
	Escalate bool `json:"escalate,omitempty"`
	// Succeeded is true when the agent reported the deploy reached its target
	// state (no failure observed this turn).
	Succeeded bool `json:"succeeded,omitempty"`
}

// RecordPortAttempt appends one loop-turn outcome to the session, bumping the
// iteration counter. Returns the newly recorded attempt. The caller persists
// the session via SavePortSession.
func (ps *PortSession) RecordPortAttempt(at PortAttempt) PortAttempt {
	ps.Iteration++
	at.Iteration = ps.Iteration
	ps.Attempts = append(ps.Attempts, at)
	return at
}

// NewPortSession constructs a fresh port session for the current PID, wrapping
// the supplied WorkSession and carrying the recon PortPlan. The PID + StartTime
// are taken from the wrapped WorkSession so the two sidecars share one identity.
func NewPortSession(ws *WorkSession, plan PortPlan) *PortSession {
	ps := &PortSession{
		Version:     portSessionVersion,
		Plan:        plan,
		WorkSession: ws,
	}
	if ws != nil {
		ps.PID = ws.PID
		ps.StartTime = ws.StartTime
	} else {
		ps.PID = os.Getpid()
		ps.StartTime = CurrentProcessStartTime()
	}
	return ps
}

// LoadPortSession reads the per-PID port session from disk. Returns (nil, nil)
// when no file exists — same not-found sentinel as LoadWorkSession.
func LoadPortSession(stateDir string, pid int) (*PortSession, error) {
	if stateDir == "" {
		return nil, nil //nolint:nilnil // not-found sentinel
	}
	path := portSessionPath(stateDir, pid)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil //nolint:nilnil
		}
		return nil, fmt.Errorf("read port session: %w", err)
	}
	var ps PortSession
	if err := json.Unmarshal(data, &ps); err != nil {
		return nil, fmt.Errorf("unmarshal port session: %w", err)
	}
	return &ps, nil
}

// SavePortSession atomically writes the port session to disk.
func SavePortSession(stateDir string, ps *PortSession) error {
	if stateDir == "" {
		return fmt.Errorf("save port session: empty state dir")
	}
	if ps == nil {
		return fmt.Errorf("save port session: nil session")
	}
	ps.Version = portSessionVersion
	dir := filepath.Join(stateDir, portSessionDirName)
	return atomicWriteJSON(dir, ".port-*.tmp", portSessionPath(stateDir, ps.PID), ps)
}

// DeletePortSession removes the per-PID port session file. Idempotent.
func DeletePortSession(stateDir string, pid int) error {
	if stateDir == "" {
		return nil
	}
	path := portSessionPath(stateDir, pid)
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("delete port session: %w", err)
	}
	return nil
}

func portSessionPath(stateDir string, pid int) string {
	return filepath.Join(stateDir, portSessionDirName, strconv.Itoa(pid)+".json")
}
