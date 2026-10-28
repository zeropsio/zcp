package init_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	zcpinit "github.com/zeropsio/zcp/internal/init"
	"github.com/zeropsio/zcp/internal/mate"
)

type initUpdateTransport func(*http.Request) (*http.Response, error)

func (f initUpdateTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// non-parallel: the init rig redirects HOME and commands; loopback uses http.DefaultTransport.
func TestRun_Mate_InitDrain(t *testing.T) {
	for _, tc := range []struct {
		name               string
		active, unitExists bool
		drain              string
		status             int
		wantSwitch         bool
		noUpdate           bool
	}{
		{"init switches a running Mate to a newer install only after it reports idle", true, true, `{"protocol":1,"drained":true,"bootId":"old-boot"}`, 200, true, false},
		{"init leaves a busy Mate running when draining is postponed", true, true, `{"protocol":1,"drained":false,"bootId":"old-boot"}`, 200, false, false},
		{"init leaves a Mate running when its drain protocol is unknown", true, true, `{"protocol":2,"drained":true,"bootId":"old-boot"}`, 200, false, false},
		{"init leaves a Mate running when its drain boot identity is absent", true, true, `{"protocol":1,"drained":true}`, 200, false, false},
		{"init leaves a Mate running when it has no drain endpoint", true, true, `{}`, 404, false, false},
		{"init starts a newly installed Mate at once when none is running", false, false, "", 0, true, false},
		{"init starts an inactive Mate at once with the newly installed version", false, true, "", 0, true, false},
		{"init drains before restarting a running Mate for an environment-only change", true, true, `{"protocol":1,"drained":true,"bootId":"old-boot"}`, 200, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := newMateRig(t)
			old, fresh := aboveMinimum(t, 1), aboveMinimum(t, 2)
			if tc.noUpdate {
				fresh = old
			}
			layVersion(t, rig.home, old)
			layVersion(t, rig.home, fresh)
			pointLink(t, mate.CurrentLink(), old)
			pointLink(t, mate.LastGoodLink(), old)
			if tc.unitExists {
				if err := os.WriteFile(rig.unitPath, []byte("[Unit]\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			zcpinit.SetCommandOutput(func(_ string, args ...string) ([]byte, error) {
				if len(args) > 0 && args[0] == "is-active" {
					if tc.active {
						return []byte("active\n"), nil
					}
					return []byte("inactive\n"), fmt.Errorf("exit status 3")
				}
				return []byte("no\n"), nil
			})
			var events []string
			// Simulate the stage result. The owning installer's activation-hook contract is
			// covered in mate tests; assertions here observe init's real supervisor/API consumer.
			zcpinit.SetMateEnsureInstalled(func(opts mate.EnsureOptions) (mate.Result, error) {
				events = append(events, "install")
				if tc.noUpdate {
					return mate.Result{Action: mate.ActionNone, From: old, To: old}, nil
				}
				if tc.active && opts.Activate != nil {
					if err := opts.Activate(mate.Manifest{Version: fresh, RollbackCompatible: true, CompatibleFrom: old}); err != nil {
						return mate.Result{}, err
					}
				}
				if !tc.active {
					pointLink(t, mate.CurrentLink(), fresh)
				}
				return mate.Result{Action: mate.ActionUpdated, From: old, To: fresh}, nil
			})
			restarted := false
			zcpinit.SetCommandRunner(func(name string, args ...string) error {
				cmd := strings.Join(append([]string{name}, args...), " ")
				if strings.Contains(cmd, "systemctl restart") {
					events = append(events, "restart")
					restarted = true
					if tc.active {
						state, err := mate.ReadUpdateState()
						if err != nil || state.Phase != "switching" {
							t.Errorf("restart must carry admission fence, state=%+v err=%v", state, err)
						}
						release, err := mate.LockInstall(0)
						if err != nil {
							t.Errorf("restart must release the install lock: %v", err)
						} else {
							release()
						}
					}
				}
				if strings.Contains(cmd, "unit create") {
					events = append(events, "start")
					restarted = true
				}
				return nil
			})
			original := http.DefaultTransport
			http.DefaultTransport = initUpdateTransport(func(r *http.Request) (*http.Response, error) {
				body := `{}`
				status := http.StatusOK
				switch r.URL.Path {
				case "/api/mate/update/drain":
					events = append(events, "drain")
					if restarted {
						t.Error("Mate restarted before it reported idle")
					}
					if version, err := mate.InstalledVersion(); err != nil || version != old {
						t.Errorf("busy Mate's current changed before drain: %q, %v", version, err)
					}
					var input struct {
						Version   string `json:"version"`
						Deadline  int    `json:"deadlineMs"`
						Automatic bool   `json:"automatic"`
					}
					if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
						t.Fatal(err)
					}
					if input.Version != fresh || input.Deadline != 600000 || input.Automatic {
						t.Errorf("unexpected drain contract: %+v", input)
					}
					body, status = tc.drain, tc.status
				case "/api/mate/update/readiness":
					events = append(events, "ready")
					body = fmt.Sprintf(`{"protocol":1,"version":%q,"bootId":"new-boot","ready":true}`, fresh)
				case "/.well-known/t3/environment":
					body = `{"basePath":"/mate"}`
				case "/api/mate/update/commit":
					events = append(events, "commit")
				case "/api/mate/update/cancel":
					events = append(events, "cancel")
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			t.Cleanup(func() { http.DefaultTransport = original })
			if err := zcpinit.Run(rig.baseDir, containerInfo()); err != nil {
				t.Fatal(err)
			}
			var want []string
			switch {
			case tc.active && tc.wantSwitch:
				want = []string{"install", "drain", "restart", "ready", "commit"}
			case tc.active:
				want = []string{"install", "drain", "cancel"}
			case tc.unitExists:
				want = []string{"install", "restart", "ready"}
			default:
				want = []string{"install", "start", "ready"}
			}
			if !reflect.DeepEqual(events, want) {
				t.Errorf("init lifecycle = %v, want %v", events, want)
			}
			wantVersion := old
			if tc.wantSwitch {
				wantVersion = fresh
			}
			if version, err := mate.InstalledVersion(); err != nil || version != wantVersion {
				t.Errorf("current = %q, want %q (err=%v)", version, wantVersion, err)
			}
			if version, err := mate.LastGoodVersion(); err != nil || version != wantVersion {
				t.Errorf("last-good = %q, want %q (err=%v)", version, wantVersion, err)
			}
		})
	}
}

func TestRun_Mate_ActiveUnchanged_CommitsBootLastGood(t *testing.T) {
	// non-parallel: the rig redirects HOME, installer and command seams.
	rig := newMateRig(t)
	old, fresh := aboveMinimum(t, 1), aboveMinimum(t, 2)
	layVersion(t, rig.home, old)
	layVersion(t, rig.home, fresh)
	pointLink(t, mate.CurrentLink(), old)
	pointLink(t, mate.LastGoodLink(), old)
	zcpinit.SetMateEnsureInstalled(func(mate.EnsureOptions) (mate.Result, error) {
		return mate.Result{Action: mate.ActionNone, From: old, To: old}, nil
	})
	if err := zcpinit.Run(rig.baseDir, containerInfo()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rig.unitPath, []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pointLink(t, mate.CurrentLink(), fresh)
	zcpinit.SetMateEnsureInstalled(func(mate.EnsureOptions) (mate.Result, error) {
		return mate.Result{Action: mate.ActionNone, From: fresh, To: fresh}, nil
	})
	zcpinit.SetCommandOutput(func(_ string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "is-active" {
			return []byte("active\n"), nil
		}
		return []byte("no\n"), nil
	})
	proved := false
	zcpinit.SetMateWaitReady(func(version, boot string, _ time.Time) error {
		if version != fresh || boot != "" {
			t.Errorf("unexpected boot readiness: version=%s boot=%s", version, boot)
		}
		proved = true
		return nil
	})
	t.Cleanup(zcpinit.ResetMateWaitReady)
	rig.commands = nil
	if err := zcpinit.Run(rig.baseDir, containerInfo()); err != nil {
		t.Fatal(err)
	}
	if !proved {
		t.Error("unchanged running Mate skipped its boot readiness proof")
	}
	if calls := rig.restartCalls(); len(calls) != 0 {
		t.Errorf("unchanged active Mate restarted: %v", calls)
	}
	if version, err := mate.LastGoodVersion(); err != nil || version != fresh {
		t.Errorf("last-good=%q want=%q err=%v", version, fresh, err)
	}
}

func TestRun_Mate_ExistingSwitch_PostponesInit(t *testing.T) {
	// non-parallel: the rig redirects HOME, installer and command seams.
	rig := newMateRig(t)
	rig.installBundle(t)
	if err := mate.WriteUpdateState(mate.UpdateState{Phase: "switching", Candidate: pinnedTestVersion, PreventRollback: true}); err != nil {
		t.Fatal(err)
	}
	if err := zcpinit.Run(rig.baseDir, containerInfo()); err != nil {
		t.Fatal(err)
	}
	if rig.installs != 0 || len(rig.restartCalls()) != 0 {
		t.Errorf("init interfered with an unfinished switch: installs=%d restarts=%v", rig.installs, rig.restartCalls())
	}
}
