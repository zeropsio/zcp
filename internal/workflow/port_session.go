package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
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
	// Iteration is the loop turn counter (Phase 1+). Placeholder in Phase 0.
	Iteration int `json:"iteration"`
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
