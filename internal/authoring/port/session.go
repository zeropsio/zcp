package port

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/zeropsio/zcp/internal/topology"
)

const (
	portSessionDirName = "port"
	portSessionVersion = "1"
	// closeReasonIterationCap is the terminal close reason stamped when the
	// per-band iteration cap fires. Same string value as the core work
	// session's CloseReasonIterationCap so a human reading state files sees
	// one vocabulary — but it is port-owned: the port session never touches
	// the core work-session file (boundary contract C3).
	closeReasonIterationCap = "iteration-cap"
)

// processInstanceToken identifies THIS process instance for the
// (pid,startTime) staleness guard. The port session is strictly
// self-checked — written and read by the same zcp serve process keyed by
// its own PID — so the token only has to be stable within one process and
// distinct across process instances; it never has to match the kernel's
// start time for a foreign PID (the core registry's processStartTime
// concern). A package-init timestamp satisfies both properties without
// duplicating the per-OS /proc//sysctl readers into the authoring domain.
var processInstanceToken = time.Now().UTC().Format(time.RFC3339Nano)

// PortSession is the per-PID sidecar for the OSS port flow, persisted at
// .zcp/state/port/{pid}.json — the authoring-owned state namespace
// (docs/spec-authoring-boundary.md C3). It mirrors the core work-session
// sidecar conventions (per-PID path, JSON, atomic write, dies with the
// process) but is fully standalone: it records its own loop attempts and
// terminal close, and never reads or writes the core
// .zcp/state/{work,services,…} namespaces.
type PortSession struct {
	Version string `json:"version"`
	PID     int    `json:"pid"`
	// StartTime is the (pid,startTime) identity guard: the recorded
	// processInstanceToken of the process that created the session, so a
	// recycled PID can't inherit a dead predecessor's port session.
	StartTime string `json:"startTime,omitempty"`
	// CreatedAt is the RFC3339 session-creation timestamp — the wall-budget
	// anchor (EvaluatePortProgress measures the per-band budget from it).
	// Distinct from StartTime: StartTime is an opaque process-identity
	// token, CreatedAt is parseable time.
	CreatedAt string `json:"createdAt,omitempty"`
	// ProjectID, Environment, Intent describe what the session is for —
	// observability fields for a human reading the state file.
	ProjectID   string `json:"projectId,omitempty"`
	Environment string `json:"environment,omitempty"`
	Intent      string `json:"intent,omitempty"`
	// ClosedAt + CloseReason record the terminal close (iteration cap).
	// Idempotency always guards on CloseReason, never on a raw ClosedAt
	// read.
	ClosedAt    string `json:"closedAt,omitempty"`
	CloseReason string `json:"closeReason,omitempty"`
	// Plan is the recon classification (Stage A0). Set once at start.
	Plan PortPlan `json:"plan"`
	// Iteration is the loop turn counter — bumped once per recorded attempt.
	Iteration int `json:"iteration"`
	// RebudgetOrigin is the iteration at which the last T1 strategy escalation
	// fired. The iteration-cap terminator measures the band budget from this
	// origin (not from iteration 0) so a late strategy switch gets a fresh
	// sub-budget and is not starved by iterations spent on the abandoned
	// strategy. Zero = never re-budgeted (measure from the start). Spec §5.
	RebudgetOrigin int `json:"rebudgetOrigin,omitempty"`
	// Attempts is the per-iteration failure-class + signals + derived-fix
	// history of the deploy-debug loop. Failure signals live HERE, not on the
	// core work-session DeployAttempt (which persists only the coarse
	// FailureClass category): the fix-class table dispatches on SIGNAL IDs,
	// so they're threaded per-turn and stored port-side. Attempts is the
	// substrate for the two-counter stall detection: classStallStreak keys on
	// FailureClass, phaseStallStreak keys on fix-class phase non-advancement.
	Attempts []PortAttempt `json:"attempts,omitempty"`
	// FitCeiling is the latest MEASURED rubric roll-up (harden+score). It is
	// set by the harden handler and read at the iterate stop/bail point so the
	// graceful stop carries the measured ceiling. Nil until the first
	// harden+score.
	FitCeiling *FitCeiling `json:"fitCeiling,omitempty"`
}

// MeasuredCeiling returns the latest measured ceiling level + ok=false when no
// FitCeiling has been scored yet (or it scored infeasible). Used to compute the
// rubric tier-rise that feeds progressRose into the phase-stall seam.
func (ps *PortSession) MeasuredCeiling() (PortTierLevel, bool) {
	if ps.FitCeiling == nil || !ps.FitCeiling.Feasible {
		return PortTierNone, false
	}
	return ps.FitCeiling.MeasuredCeiling, true
}

// PortAttempt is one deploy-debug loop turn's observed failure + the fix-class
// the handler derived from it. The agent runs the deploy via the existing tools,
// observes the FailureClassification (class + signals), and passes both into the
// iterate handler, which records this entry. The persisted Signals are what make
// signal-level dispatch survive across turns.
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
	Signals []string `json:"signals,omitempty"`
	// FixKind is the deterministic fix-class the handler derived. Empty when
	// the agent reported success (no fix needed).
	FixKind FixClassKind `json:"fixKind,omitempty"`
	// Escalate mirrors the derived PortFixClass.Escalate — an in-band
	// unfixable (build OOM), a hard escalation signal.
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

// CloseOnIterationCap stamps the terminal iteration-cap close. Idempotent:
// the guard keys on CloseReason (the cap close is its only writer), never on
// a raw ClosedAt read.
func (ps *PortSession) CloseOnIterationCap(now time.Time) {
	if ps.CloseReason != "" {
		return
	}
	ps.ClosedAt = now.UTC().Format(time.RFC3339)
	ps.CloseReason = closeReasonIterationCap
}

// NewPortSession constructs a fresh port session for the current PID,
// carrying the recon PortPlan. now stamps CreatedAt (the wall-budget anchor);
// the (pid,startTime) identity comes from the current process.
func NewPortSession(projectID, environment, intent string, plan PortPlan, now time.Time) *PortSession {
	return &PortSession{
		Version:     portSessionVersion,
		PID:         os.Getpid(),
		StartTime:   processInstanceToken,
		CreatedAt:   now.UTC().Format(time.RFC3339),
		ProjectID:   projectID,
		Environment: environment,
		Intent:      intent,
		Plan:        plan,
	}
}

// LoadPortSession reads the per-PID port session from disk. Returns (nil, nil)
// when no file exists — the not-found sentinel.
//
// Recycled-PID guard: a port file keyed by our PID whose recorded StartTime
// differs from our processInstanceToken belongs to a dead predecessor that
// happened to share the PID number — it is NOT our session, so treat it as
// absent. An empty StartTime (legacy file) trusts the bare PID.
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
	if pid == os.Getpid() && ps.StartTime != "" && ps.StartTime != processInstanceToken {
		return nil, nil //nolint:nilnil // not-our-session sentinel (recycled PID)
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

// atomicWriteJSON writes v as indented JSON to target via a same-dir temp
// file + rename. Deliberately duplicated from the core workflow session
// machinery (a trivial utility — exporting it would widen the authoring
// boundary's import surface for no gain; same rationale as C5's
// today()/shortRand()).
func atomicWriteJSON(dir, tmpPattern, target string, v any) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	tmp, err := os.CreateTemp(dir, tmpPattern)
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
