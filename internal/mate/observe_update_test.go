package mate_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/mate"
)

// non-parallel: HOME and the default HTTP client/transport are process-global.
type observedSwitchTransport struct {
	readiness        mate.UpdateReadiness
	environment      string
	contentType      string
	environmentStall bool
	readinessCalls   int
	environmentCalls int
}

func (p *observedSwitchTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	var body string
	contentType := "application/json"
	switch req.URL.Path {
	case "/api/mate/update/readiness":
		p.readinessCalls++
		raw, err := json.Marshal(p.readiness)
		if err != nil {
			return nil, fmt.Errorf("encode observed readiness: %w", err)
		}
		body = string(raw)
	case "/.well-known/t3/environment":
		p.environmentCalls++
		if p.environmentStall {
			<-req.Context().Done()
			return nil, req.Context().Err()
		}
		body = p.environment
		contentType = p.contentType
	default:
		return nil, fmt.Errorf("unexpected switch observation request: %s", req.URL.Path)
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

func restoredSwitch(t *testing.T) (mate.UpdateState, *observedSwitchTransport) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	seedInstalledVersion(t, "0.15.200")
	if err := os.Symlink("versions/0.15.200", mate.LastGoodLink()); err != nil {
		t.Fatal(err)
	}
	transport := &observedSwitchTransport{
		readiness:   mate.UpdateReadiness{Protocol: 1, Version: "0.15.200", BootID: "restored-boot", Ready: true},
		environment: `{"basePath":"/mate"}`,
		contentType: "application/json; charset=utf-8",
	}
	originalTransport, originalClient := http.DefaultTransport, http.DefaultClient
	http.DefaultTransport = transport
	http.DefaultClient = &http.Client{Transport: transport}
	t.Cleanup(func() { http.DefaultTransport, http.DefaultClient = originalTransport, originalClient })
	return mate.UpdateState{
		Protocol: 1, Phase: "switching", Candidate: "0.15.202", Previous: "0.15.200",
		RunningVersion: "0.15.200", FailedVersion: "0.15.202",
		WorkerPID: 999999999, WorkerStart: "dead-worker", OldBoot: "original-boot",
	}, transport
}

func TestObserveUpdateState_LateLastGoodReady_Postponed(t *testing.T) {
	t.Run("A dead update worker's switch settles when last-good becomes ready, so a newer release can update.", func(t *testing.T) {
		state, transport := restoredSwitch(t)
		if err := mate.WriteUpdateState(state); err != nil {
			t.Fatal(err)
		}
		got, err := mate.ObserveUpdateState()
		if err != nil {
			t.Fatal(err)
		}
		want := mate.UpdateState{
			Protocol: 1, Phase: "postponed", Candidate: "0.15.202", Previous: "0.15.200",
			RunningVersion: "0.15.200", FailedVersion: "0.15.202", OldBoot: "original-boot",
		}
		if got != want {
			t.Fatalf("late ready last-good: got %+v, want %+v", got, want)
		}
		persisted, err := mate.ReadUpdateState()
		if err != nil || persisted != want {
			t.Fatalf("settled receipt: got %+v, error=%v, want %+v", persisted, err, want)
		}
		if transport.readinessCalls != 1 || transport.environmentCalls != 1 {
			t.Fatalf("proof calls: readiness=%d environment=%d, want 1/1", transport.readinessCalls, transport.environmentCalls)
		}
		if !mate.AutomaticUpdateAllowed("0.15.200", persisted.FailedVersion, mate.Manifest{Version: "0.15.203", RollbackCompatible: true, CompatibleFrom: "0.15.200"}) {
			t.Fatal("settled last-good must admit the newer release 0.15.203")
		}
	})
}

func TestObserveUpdateState_Settlement_FailedVersionSurvives(t *testing.T) {
	t.Run("failedVersion survives the settlement, so the failed candidate is not retried.", func(t *testing.T) {
		state, _ := restoredSwitch(t)
		if err := mate.WriteUpdateState(state); err != nil {
			t.Fatal(err)
		}
		if _, err := mate.ObserveUpdateState(); err != nil {
			t.Fatal(err)
		}
		persisted, err := mate.ReadUpdateState()
		if err != nil {
			t.Fatal(err)
		}
		if persisted.Phase != "postponed" || persisted.FailedVersion != "0.15.202" {
			t.Fatalf("failed candidate after settlement: phase=%q failedVersion=%q, want postponed/0.15.202", persisted.Phase, persisted.FailedVersion)
		}
		if mate.AutomaticUpdateAllowed("0.15.200", persisted.FailedVersion, mate.Manifest{Version: "0.15.202", RollbackCompatible: true, CompatibleFrom: "0.15.200"}) {
			t.Fatal("settlement must not retry failed candidate 0.15.202")
		}
	})
}

func TestObserveUpdateState_UnprovenSwitch_Unchanged(t *testing.T) {
	t.Run("A switch whose worker still lives, or whose restored process is not proven ready on a new boot, is never settled.", func(t *testing.T) {
		for _, scenario := range []string{
			"live worker", "not ready", "wrong version", "old boot", "empty boot", "wrong protocol",
			"C4 HTML", "C4 base path", "C4 timeout", "incompatible switch", "installed differs",
			"previous differs", "last-good differs", "missing old boot", "missing worker pid", "missing worker start", "install lock held",
		} {
			t.Run(scenario, func(t *testing.T) {
				state, transport := restoredSwitch(t)
				switch scenario {
				case "live worker":
					if runtime.GOOS != "linux" {
						t.Skip("live worker identity uses Linux /proc start time")
					}
					state = mate.WorkerUpdateState(state)
					if state.WorkerStart == "" {
						t.Fatal("Linux worker fixture has no process start identity")
					}
				case "not ready":
					transport.readiness.Ready = false
				case "wrong version":
					transport.readiness.Version = "0.15.202"
				case "old boot":
					transport.readiness.BootID = "original-boot"
				case "empty boot":
					transport.readiness.BootID = ""
				case "wrong protocol":
					transport.readiness.Protocol = 0
				case "C4 HTML":
					transport.environment = "<html></html>"
					transport.contentType = "text/html"
				case "C4 base path":
					transport.environment = `{"basePath":"/"}`
				case "C4 timeout":
					transport.environmentStall = true
				case "incompatible switch":
					state.PreventRollback = true
				case "installed differs":
					seedInstalledVersion(t, "0.15.202")
				case "previous differs":
					state.Previous = "0.15.199"
				case "last-good differs":
					writeFakePackage(t, mate.VersionDir("0.15.199"), "0.15.199")
					if err := os.Remove(mate.LastGoodLink()); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink("versions/0.15.199", mate.LastGoodLink()); err != nil {
						t.Fatal(err)
					}
				case "missing old boot":
					state.OldBoot = ""
				case "missing worker pid":
					state.WorkerPID = 0
				case "missing worker start":
					state.WorkerStart = ""
				case "install lock held":
					unlock, err := mate.LockInstall(0)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(unlock)
				}
				if err := mate.WriteUpdateState(state); err != nil {
					t.Fatal(err)
				}
				got, err := mate.ObserveUpdateState()
				if err != nil || got != state {
					t.Fatalf("unproven switch changed: got %+v, error=%v, want %+v", got, err, state)
				}
				persisted, err := mate.ReadUpdateState()
				if err != nil || persisted != state {
					t.Fatalf("unproven receipt changed: got %+v, error=%v, want %+v", persisted, err, state)
				}
			})
		}
	})
}
