// Tests for: internal/ops/dev_server.go, dev_server_supervisor.go — what a
// dev-server call answers of a server still starting, of the lines its log
// gained, and of a lease's unsaved work.
package ops

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/ops/devservertest"
)

// witness: internal/ops/dev_server.go | len(ds.Servers()) == 0 → len(ds.Servers()) > 0
func TestDevServer_DSNoPreview_EmptyPartIsNotAnError(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name      string
		params    DevServerParams
		servers   DevServers
		wantEmpty bool
	}{
		{
			name:      "empty lease declaration",
			params:    DevServerParams{Hostname: "apppool-3"},
			servers:   DevServers{Hostname: "apppool-3", Service: "apppool", DeclarationExists: true},
			wantEmpty: true,
		},
		{
			name:      "missing declaration",
			params:    DevServerParams{Hostname: "appdev"},
			servers:   DevServers{Hostname: "appdev"},
			wantEmpty: true,
		},
		{
			name:      "another part declared",
			params:    DevServerParams{Hostname: "webpool-2"},
			servers:   DevServers{Hostname: "webpool-2", Service: "webpool", DeclarationExists: true, Declared: devsup.Declaration{Servers: []devsup.Server{{Name: "api", Service: "apipool"}}}},
			wantEmpty: true,
		},
		{
			name:    "explicit unknown name",
			params:  DevServerParams{Hostname: "apppool-3", Name: "web"},
			servers: DevServers{Hostname: "apppool-3", DeclarationExists: true},
		},
		{
			name:    "invalid declaration",
			params:  DevServerParams{Hostname: "apppool-3"},
			servers: DevServers{Hostname: "apppool-3", DeclarationExists: true, DeclarationErr: errors.New("invalid declaration")},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := statusDevServer(context.Background(), nil, tt.params, &tt.servers)
			if !tt.wantEmpty {
				if err == nil {
					t.Errorf("status = %+v, want an error", got)
				}
				return
			}
			if err != nil || got == nil {
				t.Fatalf("status = %+v, error = %v, want an empty status", got, err)
			}
			if got.Running || got.Message != "no preview server declared for "+tt.params.Hostname {
				t.Errorf("status = %+v, want no preview server declared for %s", got, tt.params.Hostname)
			}
		})
	}
	t.Run("status action", func(t *testing.T) {
		t.Parallel()
		c := devservertest.NewSupervised(`{"version":1,"servers":[]}`)
		got := runDevServer(t, c, DevServerParams{Action: "status", Hostname: "appdev"})
		if got.Running || got.Message != "no preview server declared for appdev" {
			t.Errorf("status = %+v, want no preview server declared for appdev", got)
		}
	})
}

// witness: internal/ops/dev_server.go | operation == devServerActionStatus → operation != devServerActionStatus
func TestDevServer_DSActionNoSup_SupervisorLossKeepsTheActionError(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, action, hostname string
		lease                  *LeaseTargetInfo
		wrapped                bool
		wantResult             bool
	}{
		{"service status", "status", "appdev", nil, true, true},
		{"service start", "start", "appdev", nil, false, false},
		{"service start after wait", "start", "appdev", nil, true, false},
		{"service restart", "restart", "appdev", nil, false, false},
		{"service stop", "stop", "appdev", nil, false, false},
		{"service logs", "logs", "appdev", nil, false, false},
		{"lease status", "status", "devpool-3", &LeaseTargetInfo{Alias: "devpool-3", Pool: "devpool"}, true, true},
		{"lease start", "start", "devpool-3", &LeaseTargetInfo{Alias: "devpool-3", Pool: "devpool"}, false, false},
		{"lease restart", "restart", "devpool-3", &LeaseTargetInfo{Alias: "devpool-3", Pool: "devpool"}, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var input error = &NoSupervisorError{Hostname: tt.hostname, Installed: true}
			if tt.wrapped {
				input = fmt.Errorf("read dev server after wait: %w", input)
			}
			result, err := devServerActionError(tt.action, tt.hostname, tt.lease, "", input)
			if !tt.wantResult {
				var noSup *NoSupervisorError
				if result != nil || !errors.As(err, &noSup) || err != input {
					t.Errorf("result/error = %+v/%v, want the original missing-supervisor error", result, err)
				}
				return
			}
			if err != nil || result == nil {
				t.Fatalf("result/error = %+v/%v, want a status result", result, err)
			}
			if result.Reason != "no_supervisor" || result.Action != "status" || result.Hostname != tt.hostname {
				t.Errorf("status result = %+v, want no_supervisor for %s", result, tt.hostname)
			}
			if tt.lease != nil && result.Pool != "devpool" {
				t.Errorf("pool = %q, want devpool", result.Pool)
			}
		})
	}
}

// DS-STARTING — A start, restart or status of a server the supervisor reports starting with its start under way or its port still awaited answers `starting` with when to read it again, never a failed start, so an agent waits for a dev server that compiles for 90 s instead of restarting it.
// witness: internal/ops/dev_server.go | case devsup.StartLaunching, devsup.StartAwaitingPort: → case devsup.StartLaunching:
func TestDevServer_DSStarting_AnsweredStartingWithARecheck(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"start", "restart", "status"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			c := devservertest.NewSupervised(twoServers)
			c.HTTP = map[int]int{8080: 0}
			c.Logs = map[string][]string{"api": {"--- zcp-devsup: start #1 at 2026-10-14T21:00:00Z: pnpm dev", "compiling..."}}

			result := runDevServer(t, c, DevServerParams{Action: action, Hostname: "appdev", Name: "api", WaitSeconds: 1})

			recheck := `Check api again in about 30 s with zerops_dev_server action=status hostname="appdev" name="api".`
			if result.Running || result.Reason != reasonStarting || !strings.Contains(result.Message, recheck) {
				t.Errorf("running/reason/message = %v/%q/%q, want not running, %q and %q", result.Running, result.Reason, result.Message, reasonStarting, recheck)
			}
		})
	}
}

// DS-STALLED — A server whose listen wait the supervisor reports run out with its healthPath never answering answers `start_stalled` with the run's log, to fix and restart, never "check again": the supervisor waits for its answer no more, so the wait would never end.
// witness: internal/ops/dev_server.go | return startingStalled → return startingWait
func TestDevServer_DSStalled_ToFixNotToWaitFor(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"restart", "status"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			c := devservertest.NewSupervised(twoServers)
			c.HTTP = map[int]int{8080: 0}
			c.StartPhase = map[string]devsup.StartPhase{"api": devsup.StartStalled}
			c.Logs = map[string][]string{"api": {"--- zcp-devsup: start #1 at 2026-10-14T21:00:00Z: pnpm dev", "Error: Cannot find module 'vite'"}}

			result := runDevServer(t, c, DevServerParams{Action: action, Hostname: "appdev", Name: "api", WaitSeconds: 1})

			if result.Running || result.Reason != reasonStartStalled || strings.Contains(result.Message, "again in about 30 s") {
				t.Errorf("running/reason/message = %v/%q/%q, want not running, %q and no recheck", result.Running, result.Reason, result.Message, reasonStartStalled)
			}
			if !strings.Contains(result.LogTail, "Cannot find module 'vite'") {
				t.Errorf("logTail = %q, want the run's log to fix it by", result.LogTail)
			}
		})
	}
}

// DS-LISTENED — A status of a server whose healthPath answered since its start and whose port refuses now probes it as a running one and answers what the probe found, never `starting`: a server whose watcher outlived its crashed child is no start to wait for.
// witness: internal/ops/dev_server.go | case devsup.StartLaunching, devsup.StartAwaitingPort: → case devsup.StartLaunching, devsup.StartAwaitingPort, devsup.StartListened:
func TestDevServer_DSListened_ProbedNotWaitedFor(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised(twoServers)
	c.HTTP = map[int]int{8080: 0}
	c.StartPhase = map[string]devsup.StartPhase{"api": devsup.StartListened}

	result := runDevServer(t, c, DevServerParams{Action: "status", Hostname: "appdev", Name: "api"})

	if result.Running || result.Reason != "connection_refused" {
		t.Errorf("running/reason = %v/%q, want not running and connection_refused", result.Running, result.Reason)
	}
}

// DS-PORT-ELSEWHERE — A server whose port is still awaited while its process listens on other ports only answers `port_elsewhere`, naming those ports, so a wrong declared port is fixed at once instead of waited for until the listen wait runs out.
// witness: internal/ops/dev_server.go | len(others) > 0 && !slices.Contains(others, result.Port) { → len(others) > 0 && slices.Contains(others, result.Port) {
func TestDevServer_DSPortElsewhere_NamesThePortsItListensOn(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"start", "status"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			c := devservertest.NewSupervised(twoServers)
			c.HTTP = map[int]int{8080: 0}
			c.GroupPorts = map[string][]int{"api": {8081}}

			result := runDevServer(t, c, DevServerParams{Action: action, Hostname: "appdev", Name: "api", WaitSeconds: 1})

			if result.Reason != reasonPortElsewhere || !strings.Contains(result.Message, ":8081") {
				t.Errorf("reason/message = %q/%q, want %q naming :8081", result.Reason, result.Message, reasonPortElsewhere)
			}
		})
	}
}

// DS-NEWLOG — A status carries at most 40 of the lines a server's log gained, at most 30 of them error lines put first, so an error under a burst of hot-reload lines is never cut off and the newest lines — a watcher's recovery — always have room, and it counts the lines it leaves out.
// witness: internal/ops/dev_server_supervisor.go | errs = errs[max(0, len(errs)-newLogErrorLines):] → errs = errs[max(0, len(errs)-newLogLines):]
func TestDevServer_DSNewLog_ErrorLinesFirstThenTheNewest(t *testing.T) {
	t.Parallel()
	lines := make([]string, 0, 61)
	var errs, rest []string
	for i := range 60 {
		line := fmt.Sprintf("hmr update /src/App.tsx (x%d)", i)
		if i%3 != 0 {
			line = fmt.Sprintf("src/App.tsx(%d,5): error TS2304: Cannot find name 'x'.", i)
			errs = append(errs, line)
		} else {
			rest = append(rest, line)
		}
		lines = append(lines, line)
	}
	lines = append(lines, "Found 0 errors. Watching for file changes.")
	rest = append(rest, lines[len(lines)-1])

	got := newLogOf(lines, 3)

	want := DevServerNewLog{Errors: errs[len(errs)-30:], Tail: rest[len(rest)-10:], Omitted: 3 + len(lines) - 40}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("new log = %+v, want %+v", got, want)
	}
}

// DS-ERROR-SHAPE — A log line is an error line only in an error's shape, never because a file name holds the word or a test summary counts none failed, so a designer editing FormErrors.tsx is never told of errors.
// witness: internal/ops/dev_server_supervisor.go | (?i)[a-z]*(error|exception)s?\s*:(\s|$)| → (?i)\w*(errors?|exceptions?)\b|
func TestDevServer_DSErrorShape_OnlyAnErrorsShape(t *testing.T) {
	t.Parallel()
	for line, want := range map[string]bool{
		"[vite] Internal server error: Failed to resolve import \"./Card\"": true,
		"TypeError: Cannot read properties of undefined":                    true,
		"src/App.tsx(3,5): error TS2304: Cannot find name 'x'.":             true,
		"npm ERR! code ELIFECYCLE":                                          true,
		"panic: runtime error: index out of range":                          true,
		"9:41:02 PM [vite] hmr update /src/zerops/FormErrors.tsx":           false,
		"page reload src/lib/onError.ts":                                    false,
		"Tests: 0 failed, 12 passed":                                        false,
		"Found 0 errors. Watching for file changes.":                        false,
	} {
		if got := len(newLogOf([]string{line}, 0).Errors) == 1; got != want {
			t.Errorf("%q an error line = %v, want %v", line, got, want)
		}
	}
}

// DS-NEWLOG-RUN — A status carries only the lines of the server's current run; the lines an earlier run wrote since the previous status are counted as omitted, so an error a restart already fixed never reads as current.
// witness: internal/ops/dev_server_supervisor.go | lines = lines[i:] → lines = lines[0:]
func TestDevServer_DSNewLogRun_OnlyTheCurrentRun(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised(twoServers)
	c.LogCursor = true
	c.Logs = map[string][]string{"api": {"--- zcp-devsup: start #1 at 2026-10-14T21:00:00Z: pnpm dev", "ready"}}
	cursors := NewDevServerLogCursors()
	readNewLog(context.Background(), c, "appdev", "api", cursors)
	c.Logs["api"] = append(c.Logs["api"], "Error: Cannot find module './Card'", "--- zcp-devsup: start #2 at 2026-10-14T21:05:00Z: pnpm dev", "ready")

	got := readNewLog(context.Background(), c, "appdev", "api", cursors)

	want := &DevServerNewLog{Tail: []string{"--- zcp-devsup: start #2 at 2026-10-14T21:05:00Z: pnpm dev", "ready"}, Omitted: 1}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("new log = %+v, want %+v", got, want)
	}
}

// leaseWithUnsavedWork answers the read of a lease's checkout (readLeaseFacts) with
// one modified file and two unpushed commits; every other command goes to the
// container.
type leaseWithUnsavedWork struct{ *devservertest.Container }

func (c leaseWithUnsavedWork) ExecSSH(ctx context.Context, hostname, command string) ([]byte, error) {
	if strings.Contains(command, leaseActivityMarker) {
		return []byte(leaseActivityMarker + "1792000000 \n" + leaseStatusMarker + "\n M src/App.tsx\n" + leaseUnpushedMarker + "\n2\n"), nil
	}
	return c.Container.ExecSSH(ctx, hostname, command)
}

// DS-UNSAVED — A status of a lease carries what releasing the lease would
// lose, so the person sees that the Preview holds work no change has yet.
// witness: internal/ops/dev_server.go | result.Unsaved, result.LeaseActivity = facts.unsaved, facts.activity → result.LeaseActivity = facts.activity
func TestDevServer_DSUnsaved_AStatusOfALeaseCarriesItsUnsavedWork(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised(previewDeclaration)

	result, err := ExecuteDevServer(context.Background(), leaseWithUnsavedWork{c}, poolClient(0), "p1",
		DevServerParams{Action: "status", Hostname: "devpool-3", StateDir: leaseState(t)})
	if err != nil {
		t.Fatal(err)
	}

	if u := result.Unsaved; u == nil || u.FileCount != 1 || u.UnpushedCommits != 2 {
		t.Errorf("unsaved = %+v, want one file and two unpushed commits", u)
	}
}

// witness: internal/ops/dev_server.go | bySupervisor = v.Supports(devsup.CapabilityWait) → bySupervisor = true
func TestDevServer_DSWaitBound_DecidedOnce(t *testing.T) {
	t.Parallel()
	newVersion := SupervisorVersion{Capabilities: []string{"wait"}}
	oldVersion := SupervisorVersion{Capabilities: []string{"env", "activity", "hold", "logs-since", "start-phase"}}
	for _, tt := range []struct {
		action       string
		asked        int
		version      SupervisorVersion
		seconds      int
		bySupervisor bool
	}{
		{"start", 0, newVersion, 15, true}, {"start", 30, newVersion, 30, true}, {"start", 120, newVersion, 120, true}, {"start", 900, newVersion, 300, true},
		{"restart", 0, newVersion, 15, true}, {"restart", 30, newVersion, 30, true}, {"restart", 120, newVersion, 120, true}, {"restart", 900, newVersion, 300, true},
		{"status", 0, newVersion, 0, false}, {"status", 30, newVersion, 30, true}, {"status", 120, newVersion, 120, true}, {"status", 900, newVersion, 300, true},
		{"start", 0, oldVersion, 15, false}, {"start", 30, oldVersion, 30, false}, {"start", 120, oldVersion, 45, false}, {"start", 900, oldVersion, 45, false},
		{"restart", 0, oldVersion, 15, false}, {"restart", 30, oldVersion, 30, false}, {"restart", 120, oldVersion, 45, false}, {"restart", 900, oldVersion, 45, false},
		{"status", 0, oldVersion, 0, false}, {"status", 30, oldVersion, 0, false}, {"status", 120, oldVersion, 0, false}, {"status", 900, oldVersion, 0, false},
	} {
		seconds, bySupervisor := devServerWait(tt.action, tt.asked, tt.version)
		if seconds != tt.seconds || bySupervisor != tt.bySupervisor {
			t.Errorf("devServerWait(%q, %d, %v) = %d, %v; want %d, %v", tt.action, tt.asked, tt.version.Capabilities, seconds, bySupervisor, tt.seconds, tt.bySupervisor)
		}
	}
}

// witness: internal/ops/dev_server.go | servers[name] = st.Servers[name] // the wait's answer → servers[name] = ds.State.Servers[name] // the wait's answer
// witness: internal/ops/dev_server.go | result.Reason = reasonStarting // the retry is still awaited → result.Reason = reasonCrashed // the retry is still awaited
func TestDevServer_DSWaitAnswer_WhatTheSupervisorReportsWhenItsWaitEnds(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, action     string
		found            devsup.ServerState
		wait             int
		running          bool
		reason, wantText string
	}{
		{"running", "status", devsup.ServerState{Status: devsup.StatusRunning, Desired: devsup.DesiredRunning, Pid: 800, Pgid: 800}, 120, true, "", ""},
		{"crashed", "status", devsup.ServerState{Status: devsup.StatusCrashed, Desired: devsup.DesiredRunning, Exit: &devsup.Exit{Code: 1}}, 120, false, "crashed", "Error: the compile failed"},
		{"starting", "status", devsup.ServerState{Status: devsup.StatusStarting, Desired: devsup.DesiredRunning, StartPhase: devsup.StartAwaitingPort, Pid: 800, Pgid: 800}, 120, false, "starting", "waitSeconds"},
		{"retry pending on status", "status", devsup.ServerState{Status: devsup.StatusCrashed, Desired: devsup.DesiredRunning, Exit: &devsup.Exit{Code: 1}, RetryPending: true}, 120, false, "starting", "waitSeconds"},
		{"retry pending on start", "start", devsup.ServerState{Status: devsup.StatusCrashed, Desired: devsup.DesiredRunning, Exit: &devsup.Exit{Code: 1}, RetryPending: true}, 120, false, "starting", "waitSeconds"},
		{"retry pending on restart", "restart", devsup.ServerState{Status: devsup.StatusCrashed, Desired: devsup.DesiredRunning, Exit: &devsup.Exit{Code: 1}, RetryPending: true}, 120, false, "starting", "waitSeconds"},
		{"retry pending without a wait", "status", devsup.ServerState{Status: devsup.StatusCrashed, Desired: devsup.DesiredRunning, Exit: &devsup.Exit{Code: 1}, RetryPending: true}, 0, false, "crashed", "waitSeconds"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := devservertest.NewSupervised(twoServers)
			c.Servers["api"].Status = devsup.StatusStarting
			c.Servers["api"].StartPhase = devsup.StartAwaitingPort
			if tt.wait == 0 {
				*c.Servers["api"] = tt.found
			}
			c.WaitFinds = map[string]devsup.ServerState{"api": tt.found}
			c.Logs = map[string][]string{"api": {"Error: the compile failed"}}
			result := runDevServer(t, c, DevServerParams{Action: tt.action, Hostname: "appdev", Name: "api", WaitSeconds: tt.wait})
			if result.Running != tt.running || result.Reason != tt.reason {
				t.Errorf("running/reason = %v/%q, want %v/%q", result.Running, result.Reason, tt.running, tt.reason)
			}
			if tt.running && result.URL != "http://appdev:8080/" {
				t.Errorf("url = %q, want the server's address", result.URL)
			}
			if tt.wantText != "" && !strings.Contains(result.Message+result.LogTail, tt.wantText) {
				t.Errorf("message/log = %q/%q, want %q", result.Message, result.LogTail, tt.wantText)
			}
			if tt.found.RetryPending && (!strings.Contains(result.Message, RetryPendingNote) || !strings.Contains(result.LogTail, "Error: the compile failed")) {
				t.Errorf("message/log = %q/%q, want retry and crash log", result.Message, result.LogTail)
			}
			if tt.wait == 0 {
				for _, command := range c.Sent() {
					if strings.Contains(command, "ctl 'wait'") {
						t.Errorf("status without wait was sent wait: %s", command)
					}
				}
			}
		})
	}
	t.Run("older supervisor", func(t *testing.T) {
		t.Parallel()
		c := devservertest.NewSupervised(twoServers)
		c.Capabilities = []string{"env", "activity", "hold", "logs-since", "start-phase"}
		c.Servers["api"].Status, c.Servers["api"].StartPhase = devsup.StatusStarting, devsup.StartAwaitingPort
		result := runDevServer(t, c, DevServerParams{Action: "status", Hostname: "appdev", Name: "api", WaitSeconds: 120})
		if result.Reason != "starting" {
			t.Errorf("reason = %q, want starting", result.Reason)
		}
		for _, command := range c.Sent() {
			if strings.Contains(command, "ctl 'wait'") {
				t.Errorf("older supervisor was sent wait: %s", command)
			}
		}
	})
}
