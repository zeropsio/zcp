package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// projectLabSSH keeps the project key on the SSH stdin channel, exactly as
// the production runner does. No service variable supplies it.
type projectLabSSH struct{ *labSSH }

func (s projectLabSSH) ExecSSHStdin(ctx context.Context, hostname, command string, input []byte) ([]byte, error) {
	s.mu.Lock()
	s.commands = append(s.commands, command)
	s.mu.Unlock()
	cmd := exec.CommandContext(ctx, "sh", "-c", strings.ReplaceAll(command, "/var/www", s.dirs[hostname])) //nolint:gosec // The lab executes the production SSH command against its temporary checkout.
	cmd.Dir = s.dirs[hostname]
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + s.home, "GIT_CONFIG_NOSYSTEM=1", "GIT_SSL_CAINFO=" + s.caFile, "GIT_TRACE=1", "GIT_TRACE_CURL=1"}
	cmd.Stdin = bytes.NewReader(input)
	out, err := cmd.CombinedOutput()
	if bytes.Contains(out, []byte(labKey)) {
		s.t.Error("project Git traced its authorization")
	}
	if err != nil {
		return out, &platform.SSHExecError{Hostname: hostname, Output: string(out), Err: err}
	}
	return out, nil
}

func TestReconcileHQRepositories_PlainProject_UsesProjectDoor(t *testing.T) {
	t.Parallel()
	lab := newHQLabWithHQ(t, newFakeHQReady(t))
	lab.rt.MateEnabled = false
	var calls []string
	old := lab.hq.srv.Config.Handler
	lab.hq.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/project/") {
			old.ServeHTTP(w, r)
			return
		}
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Project "+labKey {
			t.Error("project route lacked Project authorization")
		}
		switch r.URL.Path {
		case "/api/project/self":
			if r.URL.Query().Get("projectId") != labMate {
				t.Error("missing project assertion")
			}
			writeHQJSON(w, 200, map[string]string{"orgId": labOrg, "projectId": labMate, "appId": labApp})
		case "/api/project/repos":
			var in map[string]string
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in["name"] != "appdev" || in["projectId"] != labMate {
				t.Errorf("repository request = %v", in)
			}
			lab.hq.mu.Lock()
			lab.hq.ensureRepo(r.Context(), labApp, "appdev")
			lab.hq.mu.Unlock()
			writeHQJSON(w, 200, map[string]string{"appId": labApp, "name": "appdev"})
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	report := reconcileHQRepositories(t.Context(), lab.mock, lab.hq.srv.Client(), projectLabSSH{lab.ssh}, lab.rt, lab.stateDir, hqPassNow)
	if want := []string{"GET /api/project/self", "POST /api/project/repos"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("plain project calls = %v, want %v; report=%v", calls, want, report)
	}
	want := lab.hq.srv.URL + "/git/" + labOrg + "/" + labApp + "/appdev.git"
	if got := lab.git("remote", "get-url", "origin"); got != want {
		t.Errorf("origin = %q, want %q", got, want)
	}
	if meta := lab.meta(); meta.HQ != nil || meta.RemoteURL != want {
		t.Errorf("plain repository must keep its remote without a Mate HQ record: %+v", meta)
	}
	envs, _ := lab.mock.GetServiceEnv(t.Context(), "svc-appdev")
	if len(envs) != 0 {
		t.Error("plain wiring wrote a service credential")
	}
	if config := lab.git("config", "--local", "--list"); strings.Contains(config, labKey) || strings.Contains(config, labCredential) || strings.Contains(config, "extraheader") {
		t.Error("plain wiring persisted authorization")
	}
	if lab.git("merge-base", "HEAD", "origin/main") == "" {
		t.Error("checkout does not share HQ's main history")
	}
	if !strings.Contains(strings.Join(report, " "), "zcp hq git") {
		t.Errorf("report lacks transient Git invocation: %v", report)
	}
}

func TestReconcileHQRepositories_PlainProject_PreservesExternalOrigin(t *testing.T) {
	t.Parallel()
	for _, recorded := range []bool{false, true} {
		t.Run(fmt.Sprint(recorded), func(t *testing.T) {
			t.Parallel()
			lab := newHQLabWithHQ(t, newFakeHQReady(t))
			lab.rt.MateEnabled = false
			const external = "https://github.com/acme/app.git"
			lab.git("remote", "add", "origin", external)
			if recorded {
				if err := workflow.UpdateServiceMeta(lab.stateDir, "appdev", func(m *workflow.ServiceMeta) error { m.RemoteURL = external; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			report := reconcileHQRepositories(t.Context(), lab.mock, lab.hq.srv.Client(), projectLabSSH{lab.ssh}, lab.rt, lab.stateDir, hqPassNow)
			if got := lab.git("remote", "get-url", "origin"); got != external {
				t.Errorf("external origin changed to %q", got)
			}
			if len(lab.hq.calls) != 0 {
				t.Errorf("external origin enrolled in HQ: %v; report=%v", lab.hq.calls, report)
			}
		})
	}
}
