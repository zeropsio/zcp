package mate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	updatePostponed = "postponed"
	updateSwitching = "switching"
	updateUpdated   = "updated"
	updateFailed    = "failed"
)

// UpdateState is minimal crash recovery state. last-good is the authority for
// rollback; this record identifies an uncommitted switch and a failed release.
type UpdateState struct {
	Protocol        int    `json:"protocol"`
	Phase           string `json:"phase"`
	Candidate       string `json:"candidate,omitempty"`
	Previous        string `json:"previous,omitempty"`
	RunningVersion  string `json:"runningVersion,omitempty"`
	FailedVersion   string `json:"failedVersion,omitempty"`
	WorkerPID       int    `json:"workerPid,omitempty"`
	WorkerStart     string `json:"workerStart,omitempty"`
	OldBoot         string `json:"oldBoot,omitempty"`
	PreventRollback bool   `json:"preventRollback,omitempty"`
}

func UpdateStatePath() string { return filepath.Join(Prefix(), "update.json") }
func LastGoodLink() string    { return filepath.Join(Prefix(), "last-good") }

func ReadUpdateState() (UpdateState, error) {
	raw, err := os.ReadFile(UpdateStatePath())
	if err != nil {
		return UpdateState{}, fmt.Errorf("read update state: %w", err)
	}
	var s UpdateState
	if err := json.Unmarshal(raw, &s); err != nil {
		return UpdateState{}, fmt.Errorf("parse update state: %w", err)
	}
	return s, nil
}

func WriteUpdateState(s UpdateState) error {
	s.Protocol = 1
	body, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("encode update state: %w", err)
	}
	if err := os.MkdirAll(Prefix(), 0o755); err != nil {
		return fmt.Errorf("create update directory: %w", err)
	}
	f, err := os.CreateTemp(Prefix(), "update-*")
	if err != nil {
		return fmt.Errorf("create update state: %w", err)
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.Write(body); err != nil {
		_ = f.Close()
		return fmt.Errorf("write update state: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync update state: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close update state: %w", err)
	}
	if err := os.Rename(f.Name(), UpdateStatePath()); err != nil {
		return fmt.Errorf("activate update state: %w", err)
	}
	return syncPrefix()
}

func AutomaticUpdateAllowed(installed, failed string, candidate Manifest) bool {
	return RollbackAllowed(installed, candidate) && !IsDevVersion(installed) && VersionOlder(installed, candidate.Version) && (failed == "" || VersionOlder(failed, candidate.Version))
}

// RollbackAllowed proves this installed version is inside the release's
// declared backwards-compatible range. A missing bound is not proof.
func RollbackAllowed(installed string, candidate Manifest) bool {
	cmp, err := compareSemver(installed, candidate.CompatibleFrom)
	return candidate.RollbackCompatible && err == nil && cmp >= 0
}

// SwitchHooks contains the effects crossing the supervisor and loopback seam.
type SwitchHooks struct {
	PreventRollback bool
	Restart         func() error
	Ready           func(version, oldBoot string) error
	Commit          func() error
	Cancel          func() error
}

// SwitchUpdate runs only after the old process proved its idle admission barrier.
// A failed candidate is rolled back to the explicit last-good, never the manifest.
func SwitchUpdate(candidate, oldBoot string, h SwitchHooks) error {
	installed, err := InstalledVersion()
	if err != nil {
		return fmt.Errorf("switch installed version: %w", err)
	}
	s, stateErr := ReadOptionalUpdateState()
	if stateErr != nil {
		return stateErr
	}
	s.Phase = updateSwitching
	s.Candidate = candidate
	s.Previous = installed
	s.RunningVersion = installed
	s = WorkerUpdateState(s)
	s.OldBoot = oldBoot
	s.PreventRollback = h.PreventRollback
	currentDir, err := linkedVersionDir(CurrentLink())
	if err != nil {
		return err
	}
	if err := replaceLink(LastGoodLink(), currentDir); err != nil {
		return err
	}
	if err := WriteUpdateState(s); err != nil {
		return err
	}
	if err := activate(VersionDir(candidate)); err != nil {
		s.Phase = updatePostponed
		s.WorkerPID = 0
		s.WorkerStart = ""
		_ = WriteUpdateState(s)
		if cancelErr := h.Cancel(); cancelErr != nil {
			return fmt.Errorf("activate candidate: %w; cancel drain: %w", err, cancelErr)
		}
		return fmt.Errorf("activate candidate: %w", err)
	}
	if err := h.Restart(); err == nil {
		if err := h.Ready(candidate, oldBoot); err == nil {
			if err := replaceLink(LastGoodLink(), VersionDir(candidate)); err != nil {
				return err
			}
			s.Phase = updateUpdated
			s.RunningVersion = candidate
			s.WorkerPID = 0
			s.WorkerStart = ""
			if err := WriteUpdateState(s); err != nil {
				return err
			}
			if err := h.Commit(); err != nil {
				return fmt.Errorf("open admission after verified update: %w", err)
			}
			pruneOldVersions(candidate)
			return nil
		}
	}
	s.FailedVersion = candidate
	if h.PreventRollback {
		s.Phase = updateFailed
		s.RunningVersion = ""
		s.WorkerPID = 0
		s.WorkerStart = ""
		if err := WriteUpdateState(s); err != nil {
			return err
		}
		return fmt.Errorf("candidate %s failed readiness; incompatible release requires attended recovery", candidate)
	}
	if err := activate(currentDir); err != nil {
		return fmt.Errorf("rollback to %s: %w", installed, err)
	}
	// Persist failure before restarting so a power loss cannot reinstall it.
	if err := WriteUpdateState(s); err != nil {
		return err
	}
	if err := h.Restart(); err != nil {
		return fmt.Errorf("restart rollback: %w", err)
	}
	if err := h.Ready(installed, oldBoot); err != nil {
		return fmt.Errorf("rollback readiness: %w", err)
	}
	s.Phase = updatePostponed
	s.RunningVersion = installed
	s.WorkerPID = 0
	s.WorkerStart = ""
	if err := WriteUpdateState(s); err != nil {
		return err
	}
	if err := h.Commit(); err != nil {
		return fmt.Errorf("commit rollback: %w", err)
	}
	return nil
}

func replaceLink(link, dir string) error {
	target, err := filepath.Rel(Prefix(), dir)
	if err != nil {
		return fmt.Errorf("update link path: %w", err)
	}
	tmp := link + ".tmp"
	_ = os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return fmt.Errorf("create update link: %w", err)
	}
	if err := os.Rename(tmp, link); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace update link: %w", err)
	}
	return syncPrefix()
}
func syncPrefix() error {
	f, err := os.Open(Prefix())
	if err != nil {
		return fmt.Errorf("open update directory: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync update directory: %w", err)
	}
	return nil
}
func processStart(pid int) string {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return ""
	}
	end := bytes.LastIndexByte(raw, ')')
	if end < 0 {
		return ""
	}
	fields := strings.Fields(string(raw)[end+1:])
	if len(fields) <= 19 {
		return ""
	}
	return fields[19]
}

// PrepareUpdateBoot repairs an interrupted switch before any installer can
// consult latest. A live worker owns readiness; its restart launches current.
func PrepareUpdateBoot() (bool, error) {
	s, err := ReadUpdateState()
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	if s.Phase != updateSwitching {
		return false, nil
	}
	if s.WorkerPID > 0 && s.WorkerStart != "" && processStart(s.WorkerPID) == s.WorkerStart {
		return true, nil
	}
	if s.PreventRollback {
		s.Phase = updateFailed
		s.RunningVersion = ""
		return true, WriteUpdateState(s)
	}
	target, err := linkedVersionDir(LastGoodLink())
	if err != nil {
		return true, fmt.Errorf("boot last-good: %w", err)
	}
	if err := activate(target); err != nil {
		return true, err
	}
	goodVersion, err := installedVersionIn(target)
	if err != nil {
		return true, err
	}
	if goodVersion == s.Candidate {
		s.Phase = updateUpdated
		s.RunningVersion = goodVersion
		s.WorkerPID = 0
		s.WorkerStart = ""
		return true, WriteUpdateState(s)
	}
	s.FailedVersion = s.Candidate
	s.Phase = updatePostponed
	s.RunningVersion = goodVersion
	s.WorkerPID = 0
	s.WorkerStart = ""
	return true, WriteUpdateState(s)
}

func linkedVersionDir(link string) (string, error) {
	target, err := os.Readlink(link)
	if err != nil {
		return "", fmt.Errorf("read version link: %w", err)
	}
	if filepath.IsAbs(target) {
		return target, nil
	}
	return filepath.Join(Prefix(), target), nil
}

// WorkerUpdateState binds an in-progress phase to its exact process instance.
func WorkerUpdateState(s UpdateState) UpdateState {
	s.WorkerPID = os.Getpid()
	s.WorkerStart = processStart(os.Getpid())
	return s
}
func workerAlive(s UpdateState) bool {
	return s.WorkerPID > 0 && s.WorkerStart != "" && processStart(s.WorkerPID) == s.WorkerStart
}

// ObserveUpdateState can reopen a drain only after its owner is provably gone.
// It leaves switching to boot/independent supervisor recovery.
func ObserveUpdateState() (UpdateState, error) {
	unlock, lockErr := LockInstall(0)
	if lockErr != nil {
		return ReadUpdateState()
	}
	defer unlock()
	s, err := ReadUpdateState()
	if err != nil {
		return s, err
	}
	if (s.Phase == "staging" || s.Phase == "draining") && s.WorkerStart != "" && !workerAlive(s) {
		s.Phase = updatePostponed
		s.WorkerPID = 0
		s.WorkerStart = ""
		return s, WriteUpdateState(s)
	}
	return s, nil
}

// RecoverStoppedUpdate is the transient unit's ExecStopPost. It performs no
// release lookup and no staging; only unfinished work owned by a dead worker.
func RecoverStoppedUpdate(h SwitchHooks) error {
	s, err := ReadUpdateState()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if workerAlive(s) {
		return nil
	}
	if s.Phase == updateUpdated {
		return h.Commit()
	}
	if s.Phase == "staging" || s.Phase == "draining" || s.Phase == updatePostponed {
		s.Phase = updatePostponed
		s.WorkerPID = 0
		s.WorkerStart = ""
		if err := WriteUpdateState(s); err != nil {
			return err
		}
		return h.Cancel()
	}
	if s.Phase != updateSwitching {
		return nil
	}
	if s.PreventRollback {
		s.Phase = updateFailed
		s.RunningVersion = ""
		return WriteUpdateState(s)
	}
	target, err := linkedVersionDir(LastGoodLink())
	if err != nil {
		return err
	}
	if err := activate(target); err != nil {
		return err
	}
	goodVersion, err := installedVersionIn(target)
	if err != nil {
		return err
	}
	if goodVersion == s.Candidate {
		s.Phase = updateUpdated
		s.RunningVersion = goodVersion
		s.WorkerPID = 0
		s.WorkerStart = ""
		if err := WriteUpdateState(s); err != nil {
			return err
		}
		return h.Commit()
	}
	s.FailedVersion = s.Candidate
	s = WorkerUpdateState(s)
	if err := WriteUpdateState(s); err != nil {
		return err
	}
	if err := h.Restart(); err != nil {
		return fmt.Errorf("recovery restart: %w", err)
	}
	if err := h.Ready(goodVersion, s.OldBoot); err != nil {
		return fmt.Errorf("recovery readiness: %w", err)
	}
	s.Phase = updatePostponed
	s.RunningVersion = goodVersion
	s.WorkerPID = 0
	s.WorkerStart = ""
	if err := WriteUpdateState(s); err != nil {
		return err
	}
	return h.Commit()
}

// ReadOptionalUpdateState treats only absence as an empty initial state.
func ReadOptionalUpdateState() (UpdateState, error) {
	s, err := ReadUpdateState()
	if errors.Is(err, os.ErrNotExist) {
		return UpdateState{}, nil
	}
	return s, err
}
