package mate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zeropsio/zcp/internal/runtime"
)

// The status file is how zcp tells the mate server what a new Mate's setup is
// doing without a browser (docs/spec-mate.md): the boot import of the tier's
// runtimes writes its section, the stand-up writes its own, and the server
// reads the whole file to serve `/mate/setup.json` and to relay the stand-up's
// progress to the run card. Two processes write it — the supervisor
// (`zcp service start mate`) and the zcp MCP server under the agent — so every
// write is a read-modify-write of ONE section under a lock, landed by rename:
// a reader never sees a torn file, and a writer never loses the other's
// section.
//
// Schema v1. An absent file is an older zcp; a reader ignores fields it does
// not know. A field a reader may ignore is added within the version; one a
// reader must understand, or a field whose meaning changes, bumps it.

// EnvStatusFile names the status file in the mate server's environment. The
// launch sets it (LaunchEnvLines); a process that did not inherit it finds
// the same file at the default (StatusFilePath).
const EnvStatusFile = "ZCP_STATUS_FILE"

// StatusVersion is the schema the file is written in.
const StatusVersion = 1

// Runtimes states: none — nothing to import (no MATE_SETUP_RUNTIMES);
// pending — listed, not started yet; importing — the import is sent or its
// processes run; done — every listed service is there and settled; failed —
// the import was refused or a service's process failed.
const (
	RuntimesNone      = "none"
	RuntimesPending   = "pending"
	RuntimesImporting = "importing"
	RuntimesDone      = "done"
	RuntimesFailed    = "failed"
)

// RuntimesWaitingClosedOff is the runtimes section's error line while it is
// pending only because the project is not closed off yet: the import waits
// for that with no end of its own, so a reader (the stand-up) tells it apart
// from an import about to start.
const RuntimesWaitingClosedOff = "waiting for the project to be closed off"

// RuntimesWaitingOwnDeploy is the runtimes section's error line while it is
// pending until zcp's own first deploy has finished: an import into the
// project while that deploy still ran left zcp's app version without its
// user data. Unlike RuntimesWaitingClosedOff it ends on its own, soon.
const RuntimesWaitingOwnDeploy = "waiting for this Mate's own deploy to finish"

// A runtime service's state: creating while its import process runs,
// building/deploying while a build it was imported with runs, running once
// the import is done with it (a stage half imported with no build waits for
// its first deploy), failed when a process for it failed or the import
// refused it.
const (
	ServiceCreating  = "creating"
	ServiceBuilding  = "building"
	ServiceDeploying = "deploying"
	ServiceRunning   = "running"
	ServiceFailed    = "failed"
)

// Stand-up states and phases.
const (
	StandupIdle    = "idle"
	StandupRunning = "running"
	StandupDone    = "done"
	StandupFailed  = "failed"

	PhaseDevelopment = "development"
	PhaseStage       = "stage"
)

// A stand-up service's step, and the step's state.
const (
	StepBuild  = "build"
	StepDeploy = "deploy"
	StepVerify = "verify"

	StepPending = "pending"
	StepRunning = "running"
	StepDone    = "done"
	StepFailed  = "failed"
)

// Status is the whole file.
type Status struct {
	Version   int            `json:"version"`
	UpdatedAt string         `json:"updatedAt"`
	Runtimes  RuntimesStatus `json:"runtimes"`
	Standup   StandupStatus  `json:"standup"`
}

// RuntimesStatus is the boot import's section.
type RuntimesStatus struct {
	State string `json:"state"`
	// UpdatedAt is when the boot import last wrote this section.
	UpdatedAt string           `json:"updatedAt"`
	StartedAt string           `json:"startedAt"`
	EndedAt   string           `json:"endedAt"`
	Error     string           `json:"error"`
	Services  []RuntimeService `json:"services"`
}

// RuntimeService is one service the boot import lists.
type RuntimeService struct {
	Hostname  string `json:"hostname"`
	State     string `json:"state"`
	ProcessID string `json:"processId"`
	Error     string `json:"error"`
}

// StandupStatus is the stand-up's section.
type StandupStatus struct {
	State     string           `json:"state"`
	Phase     string           `json:"phase"`
	StartedAt string           `json:"startedAt"`
	EndedAt   string           `json:"endedAt"`
	Services  []StandupService `json:"services"`
	Error     string           `json:"error"`
	// Process is the zcp MCP server that runs the stand-up. The zcp MCP
	// server under an agent can go down without a restart of the Mate, and
	// a running section whose process is provably gone — its PID absent, or
	// reused by a process with another start time — is a stand-up that died.
	Process *StandupProcess `json:"process,omitempty"`
}

// StandupProcess names a process the way the work sessions do: its PID and
// its start time, which tells the process from a later one under the same
// PID ("" where the platform cannot read it, which trusts the bare PID).
type StandupProcess struct {
	PID   int    `json:"pid"`
	Start string `json:"start"`
}

// StandupService is one half the stand-up builds, deploys and verifies.
type StandupService struct {
	Hostname  string `json:"hostname"`
	Step      string `json:"step"`
	State     string `json:"state"`
	ProcessID string `json:"processId"`
	At        string `json:"at"`
	Error     string `json:"error"`
}

// DefaultStatusFilePath is where the launch puts the file: beside the
// identity contract, outside any bundle directory.
func DefaultStatusFilePath() string {
	return filepath.Join(runtime.HomeDir(), ".zcp", "state", "mate-status.json")
}

// StatusFilePath is the file this process writes: the one ZCP_STATUS_FILE
// names, else the default.
func StatusFilePath() string {
	if p := os.Getenv(EnvStatusFile); p != "" {
		return p
	}
	return DefaultStatusFilePath()
}

// statusLockWait bounds the wait for the other writer's lock. A write holds
// it for one read and one rename, so a wait this long is a stuck writer, and
// the status is not worth hanging a stand-up or a launch on.
const statusLockWait = 5 * time.Second

// ReadStatus reads the file. An absent, unreadable or torn file is an error —
// the caller reads that as "no status".
func ReadStatus(path string) (Status, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Status{}, fmt.Errorf("read %s: %w", path, err)
	}
	var s Status
	if err := json.Unmarshal(raw, &s); err != nil {
		return Status{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return s, nil
}

// UpdateStatus applies change to the file under its lock and lands the result
// atomically: a temporary file in the same directory, then a rename. A file
// that does not parse is replaced, never a reason to fail the write. The
// version, the time and the empty-section defaults are set here, so a writer
// touches only what it owns.
func UpdateStatus(path string, change func(*Status)) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("status file: mkdir %s: %w", dir, err)
	}
	unlock, err := lockStatus(path + ".lock")
	if err != nil {
		return err
	}
	defer unlock()

	current, _ := ReadStatus(path)
	change(&current)
	normalizeStatus(&current)

	body, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return fmt.Errorf("status file: marshal: %w", err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("status file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(append(body, '\n')); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("status file: write %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("status file: close %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("status file: rename onto %s: %w", path, err)
	}
	return nil
}

// UpdateRuntimes changes the boot import's section, moving its clock.
func UpdateRuntimes(path string, change func(*RuntimesStatus)) error {
	return UpdateStatus(path, func(s *Status) {
		change(&s.Runtimes)
		s.Runtimes.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	})
}

// UpdateStandup changes the stand-up's section.
func UpdateStandup(path string, change func(*StandupStatus)) error {
	return UpdateStatus(path, func(s *Status) { change(&s.Standup) })
}

// normalizeStatus stamps the schema and fills what no writer has set yet.
func normalizeStatus(s *Status) {
	s.Version = StatusVersion
	s.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if s.Runtimes.State == "" {
		s.Runtimes.State = RuntimesNone
	}
	if s.Runtimes.Services == nil {
		s.Runtimes.Services = []RuntimeService{}
	}
	if s.Standup.State == "" {
		s.Standup.State = StandupIdle
	}
	if s.Standup.Services == nil {
		s.Standup.Services = []StandupService{}
	}
}

// errStatusLockBusy is a lock the other writer held past statusLockWait.
var errStatusLockBusy = errors.New("status file lock held past the wait")

// lockStatus takes the status file's lock, the same flock the install lock
// uses, waiting up to statusLockWait.
func lockStatus(lockPath string) (func(), error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("status file: open %s: %w", lockPath, err)
	}
	deadline := time.Now().Add(statusLockWait)
	for {
		ok, err := tryLockFile(f)
		if err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("status file: lock %s: %w", lockPath, err)
		}
		if ok {
			return func() {
				_ = unlockFile(f)
				_ = f.Close()
			}, nil
		}
		if !time.Now().Before(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("status file: %s: %w", lockPath, errStatusLockBusy)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
