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
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/topology"

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
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + s.home, "GIT_CONFIG_NOSYSTEM=1", "GIT_SSL_CAINFO=" + s.caFile, "GIT_TRACE=1", "GIT_TRACE_CURL=1", "GIT_TRACE_REDACT=0"}
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

// The fake HQ still serves real Git; only its project API is added here.
func plainProjectLab(t *testing.T, refuse string, status int, code string) (*hqLab, func(string) int) {
	t.Helper()
	lab := newHQLabWithHQ(t, newFakeHQReady(t))
	lab.rt.MateEnabled = false
	old := lab.hq.srv.Config.Handler
	var mu sync.Mutex
	counts := map[string]int{}
	lab.hq.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		counts[r.URL.Path]++
		mu.Unlock()
		if r.URL.Path == refuse {
			writeHQJSON(w, status, map[string]string{"code": code})
			return
		}
		switch r.URL.Path {
		case "/api/project/self":
			writeHQJSON(w, 200, map[string]string{"orgId": labOrg, "projectId": labMate, "appId": labApp})
		case "/api/project/repos":
			var in struct {
				Name string `json:"name"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			lab.hq.mu.Lock()
			lab.hq.ensureRepo(r.Context(), labApp, in.Name)
			lab.hq.mu.Unlock()
			writeHQJSON(w, 200, map[string]string{"appId": labApp, "name": in.Name})
		case "/api/project/key-recheck":
			w.WriteHeader(http.StatusNoContent)
		default:
			old.ServeHTTP(w, r)
		}
	})
	return lab, func(path string) int { mu.Lock(); defer mu.Unlock(); return counts[path] }
}

func TestReconcileHQRepositories_PlainProject_BranchRefusalWaitsForCheckoutChange(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, refusal string
		prepare       func(*hqLab)
		resolve       func(*hqLab)
	}{
		{"untracked collision", "ZCP_UNTRACKED_COLLISION", func(l *hqLab) {
			l.hq.landOnMain("appdev", map[string]string{"collision.txt": "HQ"})
			l.write(map[string]string{"collision.txt": "local"})
		}, func(l *hqLab) {
			if err := os.Remove(l.pair + "/collision.txt"); err != nil {
				l.t.Fatal(err)
			}
		}},
		{"unrelated history", "ZCP_BASE_NOT_SEED", func(l *hqLab) {
			l.hq.landOnMain("appdev", map[string]string{"remote.txt": "HQ"})
			l.write(map[string]string{"local.txt": "local"})
			l.commit("Own history")
		}, func(l *hqLab) { l.git("branch", "saved-history"); l.git("reset", "--hard", "origin/main") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			lab, count := plainProjectLab(t, "", 0, "")
			tc.prepare(lab)
			pass := func() []string {
				return reconcileHQRepositories(t.Context(), lab.mock, lab.hq.srv.Client(), projectLabSSH{lab.ssh}, lab.rt, lab.stateDir, hqPassNow)
			}
			report := strings.Join(pass(), " ")
			if !strings.Contains(report, tc.refusal) {
				t.Fatalf("branch refusal = %q, want readable %s", report, tc.refusal)
			}
			if tc.refusal == "ZCP_BASE_NOT_SEED" && (!strings.Contains(report, "zcp hq git fetch") || strings.Contains(report, "&& git fetch")) {
				t.Errorf("plain remedy lacks authenticated fetch: %s", report)
			}
			before := count("/api/project/repos")
			state := readHQPairState(lab.stateDir, "appdev")
			state.LastAttemptAt = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
			writeHQPairState(lab.stateDir, "appdev", state)
			pass()
			if got := count("/api/project/repos"); got != before {
				t.Fatalf("unchanged refused checkout retried HQ: %d -> %d", before, got)
			}
			tc.resolve(lab)
			pass()
			if lab.meta().RemoteURL == "" {
				t.Fatalf("changed checkout did not resume wiring")
			}
		})
	}
}

func TestReconcileHQRepositories_PlainProject_RefusalIsBoundedPerPass(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, route, code string
		status            int
		checks            int
	}{
		{"self key refused", "/api/project/self", "key_refused", 401, 1},
		{"repo key refused", "/api/project/repos", "key_refused", 401, 1},
		{"probe key refused", "/git/" + labOrg + "/" + labApp + "/appdev.git/info/refs", "key_refused", 401, 1},
		{"not plain", "/api/project/repos", "not_plain", 403, 0},
		{"unavailable", "/api/project/repos", "down", 503, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			lab, count := plainProjectLab(t, tc.route, tc.status, tc.code)
			if err := workflow.WriteServiceMeta(lab.stateDir, &workflow.ServiceMeta{Hostname: "webdev", Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: "webstage"}}, BootstrapSession: "test", BootstrappedAt: "2026-10-02"}); err != nil {
				t.Fatal(err)
			}
			lab.ssh.dirs["webdev"] = lab.pair
			pass := func() []string {
				return reconcileHQRepositories(t.Context(), lab.mock, lab.hq.srv.Client(), projectLabSSH{lab.ssh}, lab.rt, lab.stateDir, hqPassBackedOff)
			}
			report := pass()
			if got := count("/api/project/key-recheck"); got != tc.checks {
				t.Fatalf("key rechecks = %d, want %d for the whole pass; report=%v", got, tc.checks, report)
			}
			before := count(tc.route)
			pass()
			if got := count(tc.route); got != before {
				t.Fatalf("backed-off refusal retried HTTP: %d -> %d", before, got)
			}
		})
	}
}

func TestReconcileHQRepositories_PlainProject_HeldPairDoesNoIO(t *testing.T) {
	t.Parallel()
	lab, count := plainProjectLab(t, "", 0, "")
	release, err := workflow.LockPair(t.Context(), lab.stateDir, "appdev", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	report := reconcileHQRepositories(t.Context(), lab.mock, lab.hq.srv.Client(), projectLabSSH{lab.ssh}, lab.rt, lab.stateDir, hqPassNow)
	if len(lab.ssh.commands) != 0 || count("/api/project/self") != 0 || len(report) != 0 {
		t.Fatalf("held pair performed IO: commands=%d, report=%v", len(lab.ssh.commands), report)
	}
}

func TestReconcileHQRepositories_PlainProject_WiredPairBacksOffOriginReads(t *testing.T) {
	t.Parallel()
	lab, _ := plainProjectLab(t, "", 0, "")
	pass := func() {
		reconcileHQRepositories(t.Context(), lab.mock, lab.hq.srv.Client(), projectLabSSH{lab.ssh}, lab.rt, lab.stateDir, hqPassBackedOff)
	}
	pass()
	before := len(lab.ssh.commands)
	pass()
	if got := len(lab.ssh.commands); got != before {
		t.Fatalf("wired pair repeated SSH during backoff: %d -> %d", before, got)
	}
}

func TestReconcileHQRepositories_PlainProject_KeepsOtherHQRepositoryBeforeCreation(t *testing.T) {
	t.Parallel()
	for _, remotePath := range []string{"/git/" + labOrg + "/" + labApp + "/other.git", "/git/other-org/other-app/appdev.git"} {
		t.Run(remotePath, func(t *testing.T) {
			t.Parallel()
			lab, count := plainProjectLab(t, "", 0, "")
			remote := lab.hq.srv.URL + remotePath
			lab.git("remote", "add", "origin", remote)
			reconcileHQRepositories(t.Context(), lab.mock, lab.hq.srv.Client(), projectLabSSH{lab.ssh}, lab.rt, lab.stateDir, hqPassNow)
			if count("/api/project/repos") != 0 {
				t.Fatalf("created an unused repository before keeping origin %s", remotePath)
			}
			if got := lab.git("remote", "get-url", "origin"); got != remote {
				t.Fatalf("origin changed: %s", got)
			}
		})
	}
}
