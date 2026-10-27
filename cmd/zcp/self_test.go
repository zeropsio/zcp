// Tests for: `zcp self restart` (cmd/zcp/self.go) — how the Mate server asks
// the platform to restart the container zcp runs in, which installs the
// latest zcp and Mate (docs/spec-mate.md §2.9).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// TestCLIDispatch_SelfIsAVerb: `zcp self …` is a CLI verb, never MCP serve
// mode — which would wait on stdin for a client that never comes.
func TestCLIDispatch_SelfIsAVerb(t *testing.T) {
	t.Parallel()
	if _, ok := cliDispatch()["self"]; !ok {
		t.Fatal(`"self" is not a CLI verb: run() would fall through to serve mode and wait on stdin`)
	}
}

// selfLiveEnv is a zcp container's live env store: its own service, its
// project, and the project's key and API host.
func selfLiveEnv() map[string]string {
	return map[string]string{
		"serviceId":    "svc-zcp-1",
		"projectId":    "proj-1",
		"hostname":     "zcp",
		"ZCP_API_KEY":  "key-1",
		"ZCP_API_HOST": "https://api.app-prg1.zerops.io",
	}
}

func lookupIn(env map[string]string) func(string) string {
	return func(key string) string { return env[key] }
}

// recordingRestarter is the platform client the command builds: it records
// every service it is asked to restart and answers as the platform Mock does.
type recordingRestarter struct {
	mock *platform.Mock

	mu  sync.Mutex
	ids []string
}

func (r *recordingRestarter) RestartService(ctx context.Context, serviceID string) (*platform.Process, error) {
	r.mu.Lock()
	r.ids = append(r.ids, serviceID)
	r.mu.Unlock()
	return r.mock.RestartService(ctx, serviceID)
}

func (r *recordingRestarter) asked() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.ids...)
}

// TestRunSelfCmd_Restart_RestartsTheContainersOwnService: the command builds
// the platform client from the live env store's key and API host, restarts
// the service the container is (serviceId), and answers with the platform's
// process — `{"processId":"…"}` and nothing else — as soon as the platform
// accepted it.
func TestRunSelfCmd_Restart_RestartsTheContainersOwnService(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want func(t *testing.T, out string)
	}{
		{"json answers the process alone", []string{"restart", "--json"}, func(t *testing.T, out string) {
			t.Helper()
			if want := `{"processId":"proc-restart-svc-zcp-1"}` + "\n"; out != want {
				t.Errorf("stdout = %q, want %q", out, want)
			}
		}},
		{"text names the process", []string{"restart"}, func(t *testing.T, out string) {
			t.Helper()
			if !strings.Contains(out, "proc-restart-svc-zcp-1") || strings.Count(out, "\n") != 1 {
				t.Errorf("stdout = %q, want one line naming process proc-restart-svc-zcp-1", out)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := &recordingRestarter{mock: platform.NewMock()}
			var gotToken, gotHost string
			newClient := func(token, apiHost string) (serviceRestarter, error) {
				gotToken, gotHost = token, apiHost
				return client, nil
			}
			var out bytes.Buffer
			if code := runSelfCmd(tt.args, &out, lookupIn(selfLiveEnv()), newClient); code != 0 {
				t.Fatalf("exit %d, want 0 (stdout %q)", code, out.String())
			}
			if got := client.asked(); len(got) != 1 || got[0] != "svc-zcp-1" {
				t.Errorf("restarted %v, want exactly the container's own service svc-zcp-1", got)
			}
			if gotToken != "key-1" || gotHost != "api.app-prg1.zerops.io" {
				t.Errorf("client built with token %q host %q, want key-1 on api.app-prg1.zerops.io", gotToken, gotHost)
			}
			tt.want(t, out.String())
		})
	}
}

// TestRunSelfCmd_Restart_RefusesOutsideAContainer: a restart with no
// container to restart, no key to ask with, or a platform that would not
// take it exits 1 with `{"error":"…"}` — one line the Mate shows a person as
// the reason — and never restarts anything it could not name.
func TestRunSelfCmd_Restart_RefusesOutsideAContainer(t *testing.T) {
	t.Parallel()
	without := func(keys ...string) map[string]string {
		env := selfLiveEnv()
		for _, k := range keys {
			delete(env, k)
		}
		return env
	}
	tests := []struct {
		name       string
		env        map[string]string
		clientErr  error
		restartErr error
		wantErr    string
		wantAsked  bool
	}{
		{"outside a container", without("serviceId", "projectId", "hostname"), nil, nil, "not running in a Zerops container", false},
		{"no project key", without("ZCP_API_KEY"), nil, nil, "ZCP_API_KEY", false},
		{"client cannot be built", selfLiveEnv(), errors.New("bad API host"), nil, "bad API host", false},
		{"platform refuses the restart", selfLiveEnv(), nil, errors.New("service is busy\nretry later"), "the platform refused the restart: service is busy retry later", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock()
			if tt.restartErr != nil {
				mock.WithError("RestartService", tt.restartErr)
			}
			client := &recordingRestarter{mock: mock}
			built := false
			newClient := func(string, string) (serviceRestarter, error) {
				built = true
				if tt.clientErr != nil {
					return nil, tt.clientErr
				}
				return client, nil
			}
			var out bytes.Buffer
			if code := runSelfCmd([]string{"restart", "--json"}, &out, lookupIn(tt.env), newClient); code != 1 {
				t.Errorf("exit %d, want 1", code)
			}
			if strings.Count(out.String(), "\n") != 1 {
				t.Errorf("stdout = %q, want one JSON line", out.String())
			}
			var answer map[string]string
			if err := json.Unmarshal(out.Bytes(), &answer); err != nil {
				t.Fatalf("stdout %q is not one JSON answer: %v", out.String(), err)
			}
			if len(answer) != 1 || !strings.Contains(answer["error"], tt.wantErr) || strings.Contains(answer["error"], "\n") {
				t.Errorf("answer = %v, want only an error, one line naming %q", answer, tt.wantErr)
			}
			if asked := len(client.asked()) > 0; asked != tt.wantAsked {
				t.Errorf("platform asked to restart = %v, want %v", asked, tt.wantAsked)
			}
			if !tt.wantAsked && tt.clientErr == nil && built {
				t.Error("a refused restart built the platform client")
			}
		})
	}
}

// TestRunSelfCmd_BadArguments_NothingRestarted: a call the verb cannot run
// answers exit 1 with the usage before any client is built.
func TestRunSelfCmd_BadArguments_NothingRestarted(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
	}{
		{"no subcommand", nil},
		{"no subcommand, json", []string{"--json"}},
		{"unknown subcommand", []string{"stop", "--json"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			built := false
			newClient := func(string, string) (serviceRestarter, error) {
				built = true
				return nil, errors.New("not reached")
			}
			var out bytes.Buffer
			if code := runSelfCmd(tt.args, &out, lookupIn(selfLiveEnv()), newClient); code != 1 {
				t.Errorf("exit %d, want 1", code)
			}
			if built {
				t.Error("a bad call built the platform client")
			}
			if !strings.Contains(out.String(), "usage: zcp self restart") {
				t.Errorf("stdout = %q, want the usage", out.String())
			}
		})
	}
}
