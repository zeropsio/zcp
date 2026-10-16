package devsup

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Desired is the agent's last word on a server: start and stop set it, and it
// survives a container restart because it lives in $HOME.
type Desired string

const (
	DesiredRunning Desired = "running"
	DesiredStopped Desired = "stopped"
)

// Status is what a server is, derived once by the supervisor; zcp and Mate
// read it and never recompute it.
type Status string

const (
	// StatusStopped: desired stopped.
	StatusStopped Status = "stopped"
	// StatusCrashed: desired running, no live group, an exit recorded. It
	// stays down for the rest of this container's life unless started.
	StatusCrashed Status = "crashed"
	// StatusStarting: a start is under way, or the group lives and its port
	// does not accept TCP yet.
	StatusStarting Status = "starting"
	// StatusRunning: everything else.
	StatusRunning Status = "running"
)

// Source says where a server comes from.
type Source string

const (
	// SourceDeclared: an entry of .zcp/dev-servers.json.
	SourceDeclared Source = "declared"
	// SourceRuntime: added through `ctl run` (a crew app); it carries its own
	// command, cwd and port and is not restored after a redeploy.
	SourceRuntime Source = "runtime"
)

// Exit is a server's last process end. Signal is the signal's name, or
// "port_busy" / "start_failed" with code -1 when the start itself failed.
type Exit struct {
	Code   int       `json:"code"`
	Signal string    `json:"signal"`
	At     time.Time `json:"at"`
}

// Exit signals for a start that did not get as far as a process.
const (
	ExitPortBusy    = "port_busy"
	ExitStartFailed = "start_failed"
)

// ServerState is one server in state.json.
type ServerState struct {
	Source  Source  `json:"source"`
	Desired Desired `json:"desired"`
	Status  Status  `json:"status"`
	// Command, Cwd and Port are set for runtime entries only; a declared
	// server's are in the declaration.
	Command string `json:"command,omitempty"`
	Cwd     string `json:"cwd,omitempty"`
	Port    int    `json:"port,omitempty"`
	Pid     int    `json:"pid"`
	Pgid    int    `json:"pgid"`
	// LeaderStart is the leader's start time (/proc/<pid>/stat field 22),
	// the identity a stop checks before it signals the group.
	LeaderStart int64      `json:"leaderStart"`
	StartedAt   *time.Time `json:"startedAt"`
	// Restarts counts the starts after the first in this state file's life.
	Restarts int `json:"restarts"`
	// LastRestartReason says why the supervisor last restarted it, or why it
	// stopped restarting it on watched changes.
	LastRestartReason string `json:"lastRestartReason,omitempty"`
	Exit              *Exit  `json:"exit"`
	// RetryPending: the server crashed while starting and the supervisor
	// starts it once more after its siblings settle; set only while crashed.
	RetryPending bool `json:"retryPending,omitempty"`
	// TaskLimit: the server crashed, or its start was refused, because the
	// service's task limit was reached; set only while crashed. RetryPending
	// says whether a start after backoff is still to come.
	TaskLimit *TaskLimit `json:"taskLimit,omitempty"`
	LogPath   string     `json:"logPath"`
	// RssKB is the resident memory of the server's whole process group in
	// KiB, measured when the state is read; 0 when it has no live group.
	RssKB int64 `json:"rssKb,omitempty"`
}

// SupervisorInfo identifies the running supervisor.
type SupervisorInfo struct {
	Version   string    `json:"version"`
	Pid       int       `json:"pid"`
	StartedAt time.Time `json:"startedAt"`
}

// DeclarationInfo is the declaration as the supervisor last read it; Error is
// set while it cannot be run.
type DeclarationInfo struct {
	Version int    `json:"version"`
	Error   string `json:"error"`
}

// State is $HOME/.zcp/devsup/state.json, written only by the supervisor, and
// the answer of `ctl status`.
type State struct {
	Format      int                    `json:"format"`
	Supervisor  SupervisorInfo         `json:"supervisor"`
	Declaration DeclarationInfo        `json:"declaration"`
	Servers     map[string]ServerState `json:"servers"`
}

// StateFormat is the shape of state.json.
const StateFormat = 1

// State directory layout under $HOME/.zcp/devsup.
const (
	stateFile = "state.json"
	sockFile  = "sock"
	logsDir   = "logs"
)

// DefaultStateDir is $HOME/.zcp/devsup.
func DefaultStateDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("state dir: %w", err)
	}
	return filepath.Join(home, ".zcp", "devsup"), nil
}

// LogPath is where server name's output goes.
func LogPath(dir, name string) string { return filepath.Join(dir, logsDir, name+".log") }

func statePath(dir string) string { return filepath.Join(dir, stateFile) }

// ReadState reads the state a supervisor last wrote.
func ReadState(dir string) (State, error) {
	b, err := os.ReadFile(statePath(dir))
	if err != nil {
		return State{}, fmt.Errorf("read supervisor state: %w", err)
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return State{}, fmt.Errorf("parse supervisor state: %w", err)
	}
	return st, nil
}

// writeState replaces state.json atomically.
func writeState(dir string, st State) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode supervisor state: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".state-*.json")
	if err != nil {
		return fmt.Errorf("write supervisor state: %w", err)
	}
	_, werr := tmp.Write(append(b, '\n'))
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("write supervisor state: %w %w", werr, cerr)
	}
	if err := os.Rename(tmp.Name(), statePath(dir)); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("write supervisor state: %w", err)
	}
	return nil
}

// LogTail returns the last n lines of server name's log, reading at most its
// last 256 KiB. A server without a log is unknown.
func LogTail(dir, name string, n int) ([]string, error) {
	if !serverName.MatchString(name) {
		return nil, fmt.Errorf("no server named %q", name)
	}
	f, err := os.Open(LogPath(dir, name))
	if err != nil {
		return nil, fmt.Errorf("no log for a server named %q", name)
	}
	defer f.Close()
	const window = 256 << 10
	if fi, err := f.Stat(); err == nil && fi.Size() > window {
		if _, err := f.Seek(-window, io.SeekEnd); err != nil {
			return nil, fmt.Errorf("read log: %w", err)
		}
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read log: %w", err)
	}
	s := strings.TrimRight(string(b), "\n")
	if s == "" || n == 0 {
		return []string{}, nil
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}
