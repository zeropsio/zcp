package mate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
)

// The status file is how zcp tells the mate server what a new Mate's stand-up
// is doing without a browser (docs/spec-mate.md): the stand-up writes its
// section, and the server reads the file to serve `/mate/setup.json` and to
// relay the stand-up's progress to the run card. Two processes write it — the
// supervisor (`zcp service start mate`, at launch) and the zcp MCP server
// under the agent — so every write is a read-modify-write under a lock,
// landed by rename: a reader never sees a torn file, and a writer never loses
// the other's write.
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
	Version   int           `json:"version"`
	UpdatedAt string        `json:"updatedAt"`
	Standup   StandupStatus `json:"standup"`
	Copies    *CopyStatus   `json:"copies,omitempty"`
	Stages    []StageStatus `json:"stages,omitempty"`
}

// StageStatus is the last main-follow observation for one stage.
type StageStatus struct {
	Hostname   string                    `json:"hostname"`
	Repo       string                    `json:"repo"`
	MainFollow topology.MainFollowStatus `json:"mainFollow"`
}

// CopyStatus is the Mate-wide projection of its working copies. A nil
// list has not been read; a non-nil empty list was read and has no entries.
type CopyStatus struct {
	Leases      []LeaseCopyStatus `json:"leases"`
	DevServices []DevCopyStatus   `json:"devServices"`
}

// LeaseCopyStatus identifies a lease without its credential.
type LeaseCopyStatus struct {
	Alias             string                     `json:"alias"`
	Pool              string                     `json:"pool"`
	ContainerHostname string                     `json:"containerHostname"`
	Branch            string                     `json:"branch"`
	Repo              string                     `json:"repo,omitempty"`
	AcquiredAt        string                     `json:"acquiredAt"`
	State             CopyState                  `json:"state"`
	StateAt           string                     `json:"stateAt"`
	DevServers        *[]topology.DevServerState `json:"devServers,omitempty"`
	DevServersReadAt  string                     `json:"devServersReadAt,omitempty"`
}

// DevCopyStatus is a classic dev half and its last observed checkout.
type DevCopyStatus struct {
	Hostname         string                     `json:"hostname"`
	State            CopyState                  `json:"state"`
	StateAt          string                     `json:"stateAt"`
	Branch           *string                    `json:"branch,omitempty"`
	BranchReadAt     string                     `json:"branchReadAt,omitempty"`
	DevServers       *[]topology.DevServerState `json:"devServers,omitempty"`
	DevServersReadAt string                     `json:"devServersReadAt,omitempty"`
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
	// CallStartedAt is when the call now running it began: StartedAt for
	// the call that started it, later for a call that goes on with it (the
	// stage call of a section a first call left waiting). A reader matches
	// the section to that call by it.
	CallStartedAt string `json:"callStartedAt,omitempty"`
	// Verdict is the pooled stand-up's verdict: a row per pooled service and
	// the headline derived from them (StandupHeadlineOf). nil where no
	// pooled part was stood up — a reader keeps today's wake then.
	Verdict *StandupVerdict `json:"verdict,omitempty"`
}

// StandupVerdict is the verdict a reader shows and wakes by, never
// re-derives: the rows and their one headline.
type StandupVerdict struct {
	Headline StandupHeadline `json:"headline"`
	Rows     []StandupRow    `json:"rows"`
}

// StandupHeadline is the verdict's one word for the whole stand-up.
type StandupHeadline string

const (
	// HeadlineBuilding: a row is still building and none failed.
	HeadlineBuilding StandupHeadline = "building"
	// HeadlineReady ("ready to work"): every pool row is done — the pools are
	// leasable — and a stage row is still building.
	HeadlineReady StandupHeadline = "ready"
	// HeadlineDone: every row is done, the stages built too.
	HeadlineDone StandupHeadline = "done"
	// HeadlineFailed: a row failed.
	HeadlineFailed StandupHeadline = "failed"
)

// StandupHeadlineOf is the one derivation of the verdict's headline from its
// rows: failed, done, ready (to work) or building (StandupHeadline). A
// failed row makes it failed whatever the others are (pools done and a stage
// failed is failed: only failed starts the agent's turn); ready needs a pool
// row; no rows (nothing adopted yet) is building, never done.
func StandupHeadlineOf(rows []StandupRow) StandupHeadline {
	if len(rows) == 0 {
		return HeadlineBuilding
	}
	pools := 0
	poolsDone, allDone := true, true
	for _, r := range rows {
		if r.State == RowFailed {
			return HeadlineFailed
		}
		if r.Kind == RowPool {
			pools++
		}
		if r.State != RowDone {
			allDone = false
			poolsDone = poolsDone && r.Kind != RowPool
		}
	}
	if allDone {
		return HeadlineDone
	}
	if poolsDone && pools > 0 {
		return HeadlineReady
	}
	return HeadlineBuilding
}

// StandupRowKind is what a row's service is to the tier.
type StandupRowKind string

const (
	RowPool  StandupRowKind = "pool"
	RowStage StandupRowKind = "stage"
)

// StandupRowState is one service's state in the verdict.
type StandupRowState string

const (
	// RowBuilding: its version named Commit is submitted and not yet active.
	RowBuilding StandupRowState = "building"
	// RowDone: its version named Commit is active, and for a pool its bearer
	// resolves.
	RowDone StandupRowState = "done"
	// RowFailed: Class and Cause say why.
	RowFailed StandupRowState = "failed"
)

// StandupRow is one pooled service's line of the verdict, read from its app
// version and process, never from a container.
type StandupRow struct {
	Hostname     string          `json:"hostname"`
	Kind         StandupRowKind  `json:"kind"`
	Commit       string          `json:"commit"`
	State        StandupRowState `json:"state"`
	AppVersionID string          `json:"appVersionId,omitempty"`
	// ProcessID is the build-and-deploy process; its build log is the
	// failure's evidence.
	ProcessID string `json:"processId,omitempty"`
	// Class is set on RowFailed only.
	Class platform.ErrorClass `json:"class,omitempty"`
	// Cause is the source's own words, verbatim (platform message, build
	// log line, the bearer variable that does not resolve).
	Cause string `json:"cause,omitempty"`
	// Next is the one next step zcp gives for the row, shown word for word
	// to the person and handed to the agent ("" while it builds).
	Next string `json:"next,omitempty"`
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
	return UpdateStatusChecked(path, func(s *Status) error {
		change(s)
		return nil
	})
}

// UpdateStatusChecked leaves the file unchanged when reading an owned source fails.
func UpdateStatusChecked(path string, change func(*Status) error) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("status file: mkdir %s: %w", dir, err)
	}
	unlock, err := lockStatus(path + ".lock")
	if err != nil {
		return fmt.Errorf("status file: lock: %w", err)
	}
	defer unlock()

	current, _ := ReadStatus(path)
	if err := change(&current); err != nil {
		return fmt.Errorf("status file: update: %w", err)
	}
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

// UpdateStandup changes the stand-up's section.
func UpdateStandup(path string, change func(*StandupStatus)) error {
	return UpdateStatus(path, func(s *Status) { change(&s.Standup) })
}

// RecordStageMainFollow replaces the observation for this stage without
// losing other stages or the other sections of the status file.
func RecordStageMainFollow(path string, stage StageStatus) error {
	if err := UpdateStatus(path, func(s *Status) {
		for i := range s.Stages {
			if s.Stages[i].Hostname == stage.Hostname {
				s.Stages[i] = stage
				return
			}
		}
		s.Stages = append(s.Stages, stage)
	}); err != nil {
		return fmt.Errorf("record stage main-follow result: %w", err)
	}
	return nil
}

// standupRestartCause is a verdict row a restart stopped while it built.
const standupRestartCause = "zcp restarted during the stand-up"

// MarkLaunch is the status a mate launch starts from. A stand-up the file
// says is running was stopped by this restart (the server and every agent
// under it go down with the unit), so it is marked failed: nothing else ever
// would. Its verdict ends with it: every row still building is failed as
// unreachable — nothing is lost, the next run waits on what is in flight —
// and the headline is derived again from the rows (StandupHeadlineOf).
// A boot run that starts then writes the verdict afresh.
func MarkLaunch(path string, now time.Time) error {
	if err := UpdateStandup(path, func(s *StandupStatus) {
		if s.State != StandupRunning {
			return
		}
		s.State = StandupFailed
		s.EndedAt = now.UTC().Format(time.RFC3339)
		s.Error = "the stand-up was stopped by a restart of the Mate's server"
		v := s.Verdict
		if v == nil {
			return
		}
		for i := range v.Rows {
			if r := &v.Rows[i]; r.State == RowBuilding {
				r.State, r.Class, r.Cause = RowFailed, platform.ClassUnreachable, standupRestartCause
				r.Next = "Run zerops_standup again: it waits on what is still in flight and never builds twice."
			}
		}
		v.Headline = StandupHeadlineOf(v.Rows)
	}); err != nil {
		return fmt.Errorf("mark the launch in the status file: %w", err)
	}
	return nil
}

// normalizeStatus stamps the schema and fills what no writer has set yet.
func normalizeStatus(s *Status) {
	s.Version = StatusVersion
	s.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
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
