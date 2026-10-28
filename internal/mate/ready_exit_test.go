package mate_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/zeropsio/zcp/internal/mate"
)

// non-parallel: HOME, PATH and http.DefaultTransport are replaced.
// synctest injects time into the real wait's deadline, probe contexts and ticker.
type exitReadinessTransport struct {
	version func() string
	ready   func() bool
}

func (p exitReadinessTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	body := fmt.Sprintf(`{"protocol":1,"version":%q,"bootId":"new","ready":%t}`, p.version(), p.ready())
	if req.URL.Path == "/.well-known/t3/environment" {
		body = `{"basePath":"/mate"}`
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

// readinessUnitArgv is exactly the production systemctl argv.
const readinessUnitArgv = "show zerops@mate.service --property=MainPID,ActiveState,SubState,ExecMainStatus,InvocationID,ExecMainStartTimestampMonotonic"

// fakeReadinessUnit serves the unit state last written by update twice: as a
// systemctl script on PATH for mate.ObserveRestart, and in process for
// observe, whose waits run a query per tick without starting a process.
func fakeReadinessUnit(t *testing.T, state func() string) (update func(), observe func() mate.RestartReadiness) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "state")
	// Both fakes receive exactly the production systemctl argv.
	script := "#!/bin/sh\n[ \"$1 $2 $3\" = '" + readinessUnitArgv + "' ] || exit 3\ncat '" + path + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "systemctl"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	show := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name != "systemctl" || strings.Join(args, " ") != readinessUnitArgv {
			return nil, fmt.Errorf("unexpected unit query %s %q", name, args)
		}
		// As while a process starts, everything else due in the bubble runs
		// first: a deadline that fell due with the tick has cancelled ctx.
		synctest.Wait()
		// exec.CommandContext starts nothing on a cancelled context.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return os.ReadFile(path)
	}
	update = func() {
		t.Helper()
		if err := os.WriteFile(path, []byte(state()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	update()
	return update, func() mate.RestartReadiness { return mate.ObserveRestartWith(context.Background(), show) }
}

const oldReadinessUnit = "MainPID=10\nActiveState=active\nSubState=running\nExecMainStatus=0\nInvocationID=old\nExecMainStartTimestampMonotonic=100\nExecMainExitTimestampMonotonic=0\n"
const liveReadinessUnit = "MainPID=20\nActiveState=active\nSubState=running\nExecMainStatus=0\nInvocationID=new\nExecMainStartTimestampMonotonic=200\nExecMainExitTimestampMonotonic=0\n"

func TestRestartReadiness_UnitSequences_AttributesCandidateExit(t *testing.T) {
	oldFailed := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(oldReadinessUnit, "ActiveState=active", "ActiveState=failed"), "MainPID=10", "MainPID=0"), "ExecMainStatus=0", "ExecMainStatus=1")
	candidateExited := strings.ReplaceAll(liveReadinessUnit, "MainPID=20", "MainPID=0")
	candidateFailed := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(candidateExited, "ActiveState=active", "ActiveState=failed"), "SubState=running", "SubState=failed"), "ExecMainStatus=0", "ExecMainStatus=1")
	type sequenceCase struct {
		name        string
		states      []string
		wantExit    bool
		wantTimeout bool
		wantElapsed time.Duration
	}
	for _, decision := range []struct {
		title string
		cases []sequenceCase
	}{
		{
			title: "Decision: Stopping the old Mate during an update never counts as the new Mate's failure.",
			cases: []sequenceCase{
				{name: "old stop failed then new start", states: []string{oldFailed, liveReadinessUnit, liveReadinessUnit}, wantElapsed: 100 * time.Millisecond},
				{name: "restart with no new start within the wait", states: []string{oldFailed}, wantTimeout: true, wantElapsed: time.Second},
				{name: "older start is not a candidate", states: []string{strings.ReplaceAll(oldFailed, "Monotonic=100", "Monotonic=50")}, wantTimeout: true, wantElapsed: time.Second},
			},
		},
		{
			title: "Decision: An update stops waiting the moment the Mate's process exits.",
			cases: []sequenceCase{
				{name: "new start then clean candidate exit", states: []string{liveReadinessUnit, candidateExited}, wantExit: true},
				{name: "new start then failed candidate exit", states: []string{liveReadinessUnit, candidateFailed}, wantExit: true},
				{name: "old stop failed then new start then candidate exit", states: []string{oldFailed, liveReadinessUnit, candidateFailed}, wantExit: true, wantElapsed: 100 * time.Millisecond},
				{name: "candidate failed with only newer start", states: []string{strings.ReplaceAll(candidateFailed, "InvocationID=new", "InvocationID=old")}, wantExit: true},
				{name: "candidate failed with only changed invocation", states: []string{strings.ReplaceAll(candidateFailed, "Monotonic=200", "Monotonic=100")}, wantExit: true},
			},
		},
	} {
		t.Run(decision.title, func(t *testing.T) {
			for _, tc := range decision.cases {
				t.Run(tc.name, func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						state := oldReadinessUnit
						updateUnit, observe := fakeReadinessUnit(t, func() string { return state })
						wait := observe()
						index := 0
						state = tc.states[index]
						updateUnit()
						original := http.DefaultTransport
						http.DefaultTransport = exitReadinessTransport{version: func() string { return "0.15.202" }, ready: func() bool {
							index = min(index+1, len(tc.states)-1)
							state = tc.states[index]
							updateUnit()
							return index == len(tc.states)-1 && !tc.wantTimeout && !tc.wantExit
						}}
						t.Cleanup(func() { http.DefaultTransport = original })
						start := time.Now()
						err := wait.WaitReady("0.15.202", "old", start.Add(time.Second))
						if gotExit := err != nil && strings.Contains(err.Error(), "mate unit process exited"); gotExit != tc.wantExit {
							t.Fatalf("ASSERTION: candidate exit=%v, want %v; readiness error=%v", gotExit, tc.wantExit, err)
						}
						if gotTimeout := err != nil && strings.Contains(err.Error(), "context deadline exceeded"); gotTimeout != tc.wantTimeout {
							t.Fatalf("ASSERTION: readiness deadline=%v, want %v; error=%v", gotTimeout, tc.wantTimeout, err)
						}
						if err != nil && !tc.wantExit && !tc.wantTimeout {
							t.Fatalf("ASSERTION: viable candidate readiness failed: %v", err)
						}
						if elapsed := time.Since(start); elapsed != tc.wantElapsed {
							t.Fatalf("ASSERTION: readiness waited %s, want %s", elapsed, tc.wantElapsed)
						}
					})
				})
			}
		})
	}
}

func TestWaitReady_UpdateDecisions(t *testing.T) {
	t.Run("Decision: A Mate whose process exits during an update is rolled back without waiting for the deadline.", func(t *testing.T) {
		for _, tc := range []struct{ name, state string }{
			{"nonzero exit before first probe", "MainPID=0\nActiveState=activating\nSubState=auto-restart\nExecMainStatus=1\nInvocationID=new\nExecMainStartTimestampMonotonic=200\nExecMainExitTimestampMonotonic=210\n"},
			{"clean exit before first probe", "MainPID=0\nActiveState=activating\nSubState=auto-restart\nExecMainStatus=0\nInvocationID=new\nExecMainStartTimestampMonotonic=200\nExecMainExitTimestampMonotonic=210\n"},
			{"unit failed before exec", "MainPID=0\nActiveState=failed\nSubState=failed\nExecMainStatus=203\nInvocationID=new\nExecMainStartTimestampMonotonic=100\nExecMainExitTimestampMonotonic=110\n"},
			{"observed process exits", liveReadinessUnit},
			{"unit failed without new invocation", strings.ReplaceAll(strings.ReplaceAll(oldReadinessUnit, "ActiveState=active", "ActiveState=failed"), "MainPID=10", "MainPID=0")},
			{"unit failed before main spawn", "MainPID=0\nActiveState=failed\nSubState=failed\nExecMainStatus=0\nInvocationID=new\nExecMainStartTimestampMonotonic=0\nExecMainExitTimestampMonotonic=0\n"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					t.Setenv("HOME", t.TempDir())
					seedInstalledVersion(t, "0.15.200")
					writeFakePackage(t, mate.VersionDir("0.15.202"), "0.15.202")
					state := oldReadinessUnit
					updateUnit, observe := fakeReadinessUnit(t, func() string { return state })
					start := time.Now()
					installed := "0.15.200"
					restarts := 0
					var rollbackAt time.Duration
					var wait mate.RestartReadiness
					original := http.DefaultTransport
					http.DefaultTransport = exitReadinessTransport{version: func() string { return installed }, ready: func() bool {
						if installed == "0.15.202" && tc.name == "observed process exits" {
							state = strings.Replace(liveReadinessUnit, "MainPID=20", "MainPID=0", 1)
							updateUnit()
						}
						return installed == "0.15.200"
					}}
					t.Cleanup(func() { http.DefaultTransport = original })
					err := mate.SwitchUpdate("0.15.202", "old", mate.SwitchHooks{
						Restart: func() error {
							wait = observe()
							restarts++
							installed, _ = mate.InstalledVersion()
							if restarts == 1 {
								state = tc.state
							} else {
								rollbackAt = time.Since(start)
								state = liveReadinessUnit
							}
							updateUnit()
							return nil
						},
						Ready:  func(version, boot string) error { return wait.WaitReady(version, boot, time.Now().Add(60*time.Second)) },
						Commit: func() error { return nil }, Cancel: func() error { return nil },
					})
					if err != nil {
						t.Fatal(err)
					}
					if restarts != 2 || installed != "0.15.200" {
						t.Fatalf("ASSERTION: exited candidate must roll back: restarts=%d current=%s", restarts, installed)
					}
					if tc.name == "unit failed without new invocation" {
						if rollbackAt != 60*time.Second {
							t.Fatalf("ASSERTION: old invocation's failure must wait for the deadline; rollback took %s, want 60s", rollbackAt)
						}
					} else if rollbackAt > 100*time.Millisecond {
						t.Fatalf("ASSERTION: exited candidate rollback took %s; want at most 100ms, without waiting for the 60s deadline", rollbackAt)
					}
					recorded, err := mate.ReadUpdateState()
					if err != nil || recorded.FailedVersion != "0.15.202" || recorded.RunningVersion != "0.15.200" {
						t.Fatalf("rollback record=%+v, err=%v", recorded, err)
					}
				})
			})
		}
	})
	t.Run("Decision: A Mate whose process is still running is waited for until it is ready or the deadline passes.", func(t *testing.T) {
		for _, readyAt := range []time.Duration{44 * time.Second, 60 * time.Second, 61 * time.Second} {
			t.Run(readyAt.String(), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					state := oldReadinessUnit
					updateUnit, observe := fakeReadinessUnit(t, func() string { return state })
					wait := observe()
					state = liveReadinessUnit
					updateUnit()
					start := time.Now()
					original := http.DefaultTransport
					http.DefaultTransport = exitReadinessTransport{version: func() string { return "0.15.202" }, ready: func() bool { return time.Since(start) >= readyAt }}
					t.Cleanup(func() { http.DefaultTransport = original })
					err := wait.WaitReady("0.15.202", "old", start.Add(60*time.Second))
					wantReady := readyAt < 60*time.Second
					if (err == nil) != wantReady {
						t.Fatalf("ready=%v error=%v; want ready=%v", err == nil, err, wantReady)
					}
					wantElapsed := min(readyAt, 60*time.Second)
					if elapsed := time.Since(start); elapsed != wantElapsed {
						t.Fatalf("ASSERTION: live process waited %s, want %s", elapsed, wantElapsed)
					}
				})
			})
		}
	})
}

func TestWaitReady_StaleUnitState_DoesNotAbort(t *testing.T) {
	for _, stale := range []string{
		strings.ReplaceAll(oldReadinessUnit, "MainPID=10", "MainPID=0"),
		strings.ReplaceAll(strings.ReplaceAll(oldReadinessUnit, "ActiveState=active", "ActiveState=failed"), "MainPID=10", "MainPID=0"),
		strings.ReplaceAll(liveReadinessUnit, "ExecMainStatus=0", "ExecMainStatus=1"),
	} {
		t.Run(stale, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				state := oldReadinessUnit
				if strings.Contains(stale, "ActiveState=failed") {
					state = stale
				}
				updateUnit, observe := fakeReadinessUnit(t, func() string { return state })
				wait := observe()
				state = stale
				updateUnit()
				start := time.Now()
				original := http.DefaultTransport
				http.DefaultTransport = exitReadinessTransport{version: func() string { return "0.15.202" }, ready: func() bool {
					if time.Since(start) >= time.Second {
						state = liveReadinessUnit
						updateUnit()
						return true
					}
					return false
				}}
				t.Cleanup(func() { http.DefaultTransport = original })
				if err := wait.WaitReady("0.15.202", "old", start.Add(60*time.Second)); err != nil {
					t.Fatalf("stale state aborted startup: %v", err)
				}
				if elapsed := time.Since(start); elapsed != time.Second {
					t.Fatalf("elapsed=%s, want 1s", elapsed)
				}
			})
		})
	}
}

func TestRestartReadiness_ExistingProofs_Required(t *testing.T) {
	for _, invalid := range []string{"protocol", "version", "old boot", "empty boot", "engine not ready", "C4 html", "C4 base path"} {
		t.Run(invalid, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				state := oldReadinessUnit
				updateUnit, observe := fakeReadinessUnit(t, func() string { return state })
				wait := observe()
				state = liveReadinessUnit
				updateUnit()
				transport := &updateProofTransport{invalid: invalid}
				original := http.DefaultTransport
				http.DefaultTransport = transport
				t.Cleanup(func() { http.DefaultTransport = original })
				if err := wait.WaitReady("0.15.111", "old", time.Now().Add(60*time.Second)); err != nil {
					t.Fatal(err)
				}
				if transport.readinessCalls != 2 {
					t.Fatalf("accepted incomplete readiness proof, requests=%d", transport.readinessCalls)
				}
			})
		})
	}
}

func TestRestartReadiness_UnknownUnit_WaitsForProof(t *testing.T) {
	for _, unknownBefore := range []bool{true, false} {
		t.Run(fmt.Sprint(unknownBefore), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				state := oldReadinessUnit
				if unknownBefore {
					state = "unreadable unit properties"
				}
				updateUnit, observe := fakeReadinessUnit(t, func() string { return state })
				wait := observe()
				state = "unreadable unit properties"
				updateUnit()
				start := time.Now()
				original := http.DefaultTransport
				http.DefaultTransport = exitReadinessTransport{version: func() string { return "0.15.202" }, ready: func() bool { return time.Since(start) >= time.Second }}
				t.Cleanup(func() { http.DefaultTransport = original })
				if err := wait.WaitReady("0.15.202", "old", start.Add(60*time.Second)); err != nil {
					t.Fatalf("unknown unit aborted startup: %v", err)
				}
				if elapsed := time.Since(start); elapsed != time.Second {
					t.Fatalf("waited %s, want 1s", elapsed)
				}
			})
		})
	}
}

func TestRestartReadiness_RecoveryProcessExits_FailsImmediately(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		seedInstalledVersion(t, "0.15.202")
		writeFakePackage(t, mate.VersionDir("0.15.200"), "0.15.200")
		if err := os.Symlink("versions/0.15.200", mate.LastGoodLink()); err != nil {
			t.Fatal(err)
		}
		if err := mate.WriteUpdateState(mate.UpdateState{Phase: "switching", Previous: "0.15.200", Candidate: "0.15.202", OldBoot: "old"}); err != nil {
			t.Fatal(err)
		}
		state := oldReadinessUnit
		// The production ObserveRestart runs the fake systemctl on PATH.
		updateUnit, _ := fakeReadinessUnit(t, func() string { return state })
		var wait mate.RestartReadiness
		var observed error
		committed := false
		start := time.Now()
		original := http.DefaultTransport
		http.DefaultTransport = exitReadinessTransport{version: func() string { return "0.15.200" }, ready: func() bool { return false }}
		t.Cleanup(func() { http.DefaultTransport = original })
		err := mate.RecoverStoppedUpdate(mate.SwitchHooks{
			Restart: func() error {
				wait = mate.ObserveRestart(context.Background())
				state = strings.Replace(liveReadinessUnit, "MainPID=20", "MainPID=0", 1)
				updateUnit()
				return nil
			},
			Ready: func(version, boot string) error {
				observed = wait.WaitReady(version, boot, start.Add(60*time.Second))
				return observed
			},
			Commit: func() error { committed = true; return nil }, Cancel: func() error { return nil },
		})
		if err == nil || observed == nil || !strings.Contains(observed.Error(), "mate unit process exited") || committed {
			t.Fatalf("recovery error=%v observation=%v committed=%v", err, observed, committed)
		}
		if elapsed := time.Since(start); elapsed != 0 {
			t.Fatalf("exited recovery waited %s, want immediate failure", elapsed)
		}
	})
}
