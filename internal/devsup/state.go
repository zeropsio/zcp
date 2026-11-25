package devsup

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
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
	// StatusStarting: a start is under way; the group lives and its port
	// does not accept TCP or its healthPath has not answered yet; or no group
	// lives and no exit is recorded — a start about to begin, a stop under way.
	// StartPhase says which.
	StatusStarting Status = "starting"
	// StatusRunning: a live group, answered and accepting when it has a port.
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

// ListenWait bounds how long the supervisor waits for a started server's
// healthPath to answer, to record starting → running; past it, the status
// follows the port alone.
const ListenWait = 10 * time.Minute

// StartPhase is where a starting server is (CapabilityStartPhase), decided
// by the supervisor alone: it owns the start, the port wait and the clock.
type StartPhase string

const (
	// StartLaunching: a start is under way — a restart's stop grace, the
	// port wait, a boot's stagger — and its process has not started yet.
	StartLaunching StartPhase = "launching"
	// StartAwaitingPort: its process lives and its port has not answered
	// since this start — it accepts nothing, or its healthPath has not
	// answered — within ListenWait.
	StartAwaitingPort StartPhase = "awaiting_port"
	// StartStalled: its process lives, its healthPath did not answer within
	// ListenWait, and its port does not accept now; the supervisor waits for
	// an answer no more.
	StartStalled StartPhase = "stalled"
	// StartListened: its healthPath answered since this start and its port
	// does not accept now.
	StartListened StartPhase = "listened"
)

// startPhaseOf is the StartPhase of a server reported starting: underWay a
// start runs or no group of it lives, answered its healthPath answered since
// that start or its listen wait ran out, waitEnded the wait ran out first.
func startPhaseOf(underWay, answered, waitEnded bool) StartPhase {
	switch {
	case underWay:
		return StartLaunching
	case waitEnded:
		return StartStalled
	case answered:
		return StartListened
	default:
		return StartAwaitingPort
	}
}

// startAwaited says whether the supervisor itself still has a start to finish.
func startAwaited(status Status, phase StartPhase, retryPending bool) bool {
	return (status == StatusStarting && (phase == StartLaunching || phase == StartAwaitingPort)) ||
		(status == StatusCrashed && retryPending)
}

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
	// StartPhase is set only while Status is starting.
	StartPhase StartPhase `json:"startPhase,omitempty"`
	// Restarts counts the starts after the first in this state file's life.
	Restarts int `json:"restarts"`
	// LastRestartReason says why the supervisor last restarted it, or why it
	// stopped restarting it on watched changes.
	LastRestartReason string `json:"lastRestartReason,omitempty"`
	Exit              *Exit  `json:"exit"`
	// RetryPending: the server crashed while starting, or the preview server
	// exited after it answered, and the supervisor starts it once more after
	// its siblings settle; set only while crashed.
	RetryPending bool `json:"retryPending,omitempty"`
	// TaskLimit: the server crashed, or its start was refused, because the
	// service's task limit was reached; set only while crashed. RetryPending
	// says whether a start after backoff is still to come.
	TaskLimit *TaskLimit `json:"taskLimit,omitempty"`
	// OutOfMemory: the server's last start is the one after systemd restarted
	// the service because it ran out of memory.
	OutOfMemory bool   `json:"outOfMemory,omitempty"`
	LogPath     string `json:"logPath"`
	// PreviewPort is the port the supervisor forwards to this server on every
	// interface (previewPortBase + the container's ZEROPS_StableNumber); set
	// only for the server the declaration names the preview server.
	PreviewPort int `json:"previewPort,omitempty"`
	// PreviewLastConnAt is when the preview forward last took a connection in
	// this supervisor's life, absent until one came; PreviewOpenConns is how
	// many it holds open now, 0 included. Both are set only beside a
	// PreviewPort (CapabilityActivity) and live in memory: state.json never
	// holds them, and a new supervisor starts without them.
	PreviewLastConnAt *time.Time `json:"previewLastConnAt,omitempty"`
	PreviewOpenConns  *int       `json:"previewOpenConns,omitempty"`
	// RssKB is the resident memory of the server's whole process group in
	// KiB, measured when the state is read; 0 when it has no live group.
	RssKB int64 `json:"rssKb,omitempty"`
}

// statusFacts are what a server's status is derived from (deriveStatus).
type statusFacts struct {
	desired Desired
	// starting: a start is under way; live: a process group runs.
	starting, live bool
	// port is the declared port, 0 for none; accepts: it accepts TCP now,
	// read only for a live group whose healthPath answered.
	port    int
	accepts bool
	// answered: this start's healthPath answered once, or the listen wait
	// passed without an answer.
	answered bool
	// exited: an exit is recorded.
	exited bool
}

// deriveStatus is a server's status, the one derivation zcp and Mate read: a
// live server with a port runs once its healthPath has answered and while its
// port accepts — a dev server that accepts at once and compiles its first page
// for a minute is starting, and the preview port holds its page meanwhile. A
// server runs only while a group of it lives: one with none and no exit is
// about to start, or is being stopped, and never runs — the preview port
// would send its page's reload to a port nothing listens on.
func deriveStatus(f statusFacts) Status {
	switch {
	case f.desired == DesiredStopped:
		return StatusStopped
	case f.starting:
		return StatusStarting
	case !f.live && f.exited:
		return StatusCrashed
	case !f.live:
		return StatusStarting
	case f.port > 0 && (!f.answered || !f.accepts):
		return StatusStarting
	}
	return StatusRunning
}

// Restart reasons that name no path.
const (
	reasonRetry   = "it crashed within its start window"
	reasonAsked   = "a restart was asked for"
	reasonStart   = "a start was asked for"
	reasonDecl    = "its declaration changed"
	reasonBoot    = "the supervisor started again"
	reasonRunSpec = "its run entry changed"
	// reasonEnv: `ctl env` changed the overlay the server's group started
	// under (docs/spec-mate.md §2.8a.2).
	reasonEnv = "env changed"
	// reasonLimit marks a start after the task limit was met; the recorded
	// reason is limitReason.
	reasonLimit = "task limit"
	// reasonExited: the preview server exited on its own after it answered,
	// and is started once more (onceMore).
	reasonExited = "it exited on its own after it started"
)

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
// the answer of `ctl status`, which alone carries the preview activity.
type State struct {
	Format      int             `json:"format"`
	Supervisor  SupervisorInfo  `json:"supervisor"`
	Declaration DeclarationInfo `json:"declaration"`
	// Env is the lease-wide overlay `ctl env` last set, empty when none is:
	// every server starts with the service's environment plus these, an
	// overlay entry winning over the service's value of the same name. It
	// holds container hostnames, never a secret.
	Env     map[string]string      `json:"env"`
	Servers map[string]ServerState `json:"servers"`
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

// LogRead is `ctl logs <name> --since <cursor>`: the complete lines the log
// gained after Cursor's read, the newest n of them; Dropped counts the others
// (lines older than the last 256 KiB of the log are not counted). Cursor is
// where the next read starts.
type LogRead struct {
	Lines   []string `json:"lines"`
	Cursor  string   `json:"cursor"`
	Dropped int      `json:"dropped"`
}

// logReadWindow is how much of a log one read takes, from its end.
const logReadWindow = 256 << 10

// logHeadSize bounds the part of a log's first line that names the file.
const logHeadSize = 4096

// LogSince reads server name's log after cursor, an earlier LogSince's
// Cursor. A cursor of another log file — empty, one rotated away, a container
// whose $HOME is new — reads the log from its start.
func LogSince(dir, name, cursor string, n int) (LogRead, error) {
	if !serverName.MatchString(name) {
		return LogRead{}, fmt.Errorf("no server named %q", name)
	}
	f, err := os.Open(LogPath(dir, name))
	if err != nil {
		return LogRead{}, fmt.Errorf("no log for a server named %q", name)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return LogRead{}, fmt.Errorf("read log: %w", err)
	}
	return readLogSince(f, fi.Size(), cursor, n)
}

// readLogSince is LogSince over a log of size bytes. A cursor is
// "<file id>:<offset>": the file id is a hash of the log's first line (its
// first logHeadSize bytes) — each log begins with a start marker naming its
// run's start time — and the offset
// is just past the last complete line the read answered, so a line still being
// written comes whole in the next read.
func readLogSince(r io.ReaderAt, size int64, cursor string, n int) (LogRead, error) {
	head := make([]byte, min(size, logHeadSize))
	if _, err := r.ReadAt(head, 0); err != nil && err != io.EOF {
		return LogRead{}, fmt.Errorf("read log: %w", err)
	}
	// A first line longer than the head is named by the head: it no longer
	// grows there. A shorter one still being written names no file yet.
	id := ""
	if line, _, ok := bytes.Cut(head, []byte{'\n'}); ok || len(head) == logHeadSize {
		sum := sha256.Sum256(line)
		id = hex.EncodeToString(sum[:8])
	}
	start := int64(0)
	if cid, offText, ok := strings.Cut(cursor, ":"); ok && id != "" && cid == id {
		if off, err := strconv.ParseInt(offText, 10, 64); err == nil && off >= 0 && off <= size {
			start = off
		}
	}
	partial := false
	if size-start > logReadWindow {
		start, partial = size-logReadWindow, true
	}
	buf := make([]byte, size-start)
	if _, err := r.ReadAt(buf, start); err != nil && err != io.EOF {
		return LogRead{}, fmt.Errorf("read log: %w", err)
	}
	if partial {
		// The window began inside a line: it is not a line of this read.
		i := bytes.IndexByte(buf, '\n')
		start += int64(i + 1)
		buf = buf[i+1:]
	}
	complete := buf[:bytes.LastIndexByte(buf, '\n')+1]
	read := LogRead{Lines: []string{}, Cursor: id + ":" + strconv.FormatInt(start+int64(len(complete)), 10)}
	if len(complete) > 0 {
		read.Lines = strings.Split(strings.TrimSuffix(string(complete), "\n"), "\n")
	}
	if len(read.Lines) > n {
		read.Dropped = len(read.Lines) - n
		read.Lines = read.Lines[read.Dropped:]
	}
	return read, nil
}
