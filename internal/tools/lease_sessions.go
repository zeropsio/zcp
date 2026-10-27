package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A lease is bound in Mate to the conversation whose acquire took it; a
// second conversation acquiring the same branch on the same pool gets it back
// `reused` and is bound to nothing (docs/spec-mate.md §6.3). A release of the
// branch on the first conversation's archive would take the lease from under
// the second, so every acquire inside a Mate records which zcp session — one
// per MCP server process (docs/spec-work-session.md) — took or reused each
// lease it answers, and `zcp lease release` refuses a lease another live
// session reused (docs/spec-workflows.md §8 DP-35). The record is zcp's own,
// beside the lease's, never the lease's: <stateDir>/lease-sessions/<alias>/
// <pid>.json, one file per session, so sessions never write one file at once.

// leaseSessionsDir is where zcp records the sessions of each lease, under the
// state dir.
const leaseSessionsDir = "lease-sessions"

// leaseSession is one zcp session's acquire of a lease.
type leaseSession struct {
	// AcquiredAt is the lease's own acquire time, as zcp's record keeps it:
	// a session of an earlier lease under the same alias names another.
	AcquiredAt string `json:"acquiredAt"`
	PID        int    `json:"pid"`
	// Start is the process's start time (workflow.CurrentProcessStartTime),
	// so a recycled PID never reads as the session.
	Start string `json:"start,omitempty"`
	// At is when the session's acquire answered.
	At string `json:"at"`
	// Reused: the session's acquire returned the lease another acquire took.
	Reused bool `json:"reused"`
}

// recordLeaseSessions records this zcp session against each lease an acquire
// answered, best-effort: a session already recorded as the lease's acquirer
// stays one, and records of an earlier lease under an alias go.
func recordLeaseSessions(stateDir string, views []ops.LeaseView) {
	if stateDir == "" {
		return
	}
	pid := os.Getpid()
	now := time.Now().UTC().Format(time.RFC3339)
	for _, v := range views {
		if v.Alias == "" || v.AcquiredAt == "" {
			continue
		}
		dir := filepath.Join(stateDir, leaseSessionsDir, v.Alias)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			continue
		}
		pruneLeaseSessions(dir, v.AcquiredAt)
		s := leaseSession{AcquiredAt: v.AcquiredAt, PID: pid, Start: workflow.CurrentProcessStartTime(), At: now, Reused: v.Reused}
		path := filepath.Join(dir, strconv.Itoa(pid)+".json")
		if kept, ok := readLeaseSession(path); ok && kept.AcquiredAt == v.AcquiredAt && !kept.Reused {
			s.Reused, s.At = false, kept.At
		}
		raw, err := json.Marshal(s)
		if err != nil {
			continue
		}
		tmp := path + ".tmp"
		if os.WriteFile(tmp, raw, 0o600) == nil {
			_ = os.Rename(tmp, path)
		}
	}
}

// pruneLeaseSessions removes the records in dir of another lease than the
// one acquired at acquiredAt.
func pruneLeaseSessions(dir, acquiredAt string) {
	paths, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	for _, path := range paths {
		if s, ok := readLeaseSession(path); ok && s.AcquiredAt != acquiredAt {
			_ = os.Remove(path)
		}
	}
}

// readLeaseSession reads one session record; false when it cannot be read.
func readLeaseSession(path string) (leaseSession, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return leaseSession{}, false
	}
	var s leaseSession
	if json.Unmarshal(raw, &s) != nil || s.PID <= 0 {
		return leaseSession{}, false
	}
	return s, true
}

// readLeaseSessions is every session record of alias, by PID; none when none
// is kept or the directory cannot be read.
func readLeaseSessions(stateDir, alias string) []leaseSession {
	paths, err := filepath.Glob(filepath.Join(stateDir, leaseSessionsDir, alias, "*.json"))
	if err != nil {
		return nil
	}
	var out []leaseSession
	for _, path := range paths {
		if s, ok := readLeaseSession(path); ok {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(a, b leaseSession) int { return a.PID - b.PID })
	return out
}

// forgetLeaseSessions removes, best-effort, the session records of each
// released lease.
func forgetLeaseSessions(stateDir string, aliases []string) {
	if stateDir == "" {
		return
	}
	for _, alias := range aliases {
		_ = os.RemoveAll(filepath.Join(stateDir, leaseSessionsDir, alias))
	}
}

// releasedAliases is the aliases of the views a release answered gone —
// released or lost — never one it left held.
func releasedAliases(views []ops.LeaseView) []string {
	var out []string
	for _, v := range views {
		if v.ReleaseOutcome() != "" {
			out = append(out, v.Alias)
		}
	}
	return out
}

// leaseSessionsInUse is how many live zcp sessions other than its acquirer's
// reused lease since it was acquired: sessions of this lease (its acquire
// time), returned it reused, whose process alive says still runs, the
// acquirer's own process excepted (a conversation's own repeat acquire). 0 for
// a lease no other live session reused.
func leaseSessionsInUse(stateDir string, lease ops.LeaseTargetInfo, alive func(pid int, start string) bool) int {
	sessions := readLeaseSessions(stateDir, lease.Alias)
	acquirer := 0
	for _, s := range sessions {
		if s.AcquiredAt == lease.AcquiredAt && !s.Reused {
			acquirer = s.PID
		}
	}
	n := 0
	for _, s := range sessions {
		if s.AcquiredAt == lease.AcquiredAt && s.Reused && s.PID != acquirer && alive(s.PID, s.Start) {
			n++
		}
	}
	return n
}
