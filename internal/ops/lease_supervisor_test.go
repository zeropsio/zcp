// Tests for: ops/lease_supervisor.go, the lease cases of dev_server.go — a
// lease always has its supervisor (docs/spec-workflows.md §8 DP-9). A pool
// container released with reset=true comes back as its image and Zerops never
// runs its run.start again, so zcp starts `zcp-devsup run` itself: after an
// acquire's checkout, and once when a dev-server call on a lease finds none
// answering. A dev service is never started this way.
package ops

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops/devservertest"
	"github.com/zeropsio/zcp/internal/platform"
)

// supervisorAnswer is a leaseSSH handler: the start of the supervisor answers
// answer (or fails with err); every other command (the checkout) succeeds.
func supervisorAnswer(answer string, err error) func(host, command string) ([]byte, error) {
	return func(_, command string) ([]byte, error) {
		if strings.Contains(command, "zcp-devsup run") {
			return []byte(answer), err
		}
		return nil, nil
	}
}

func TestLeaseAcquire_EnsuresTheSupervisorAfterTheCheckout(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		answer   string
		err      error
		wantNote []string // empty: no note
	}{
		{name: "a reused container: none runs, zcp starts it", answer: "@@zcp-devsup check 2\n@@zcp-devsup started 0\n"},
		{name: "a fresh container: run.start's supervisor answers", answer: "@@zcp-devsup check 0\n"},
		{
			name: "no zcp-devsup in the pool's image: not a dev pool", answer: "@@zcp-devsup check 127\n",
			wantNote: []string{"zcp-devsup", "not a dev pool", "no dev servers"},
		},
		{
			name: "the started supervisor does not come up", answer: "@@zcp-devsup check 2\n@@zcp-devsup started 2\nzcp-devsup: boom\n@@zcp-devsup log 0\n",
			wantNote: []string{"boom", "zerops_dev_server"},
		},
		{
			name: "a supervisor holds the socket but does not answer", answer: "@@zcp-devsup check 5\n",
			wantNote: []string{"did not answer"},
		},
		{
			name: "ssh fails", err: sshFailure("kex_exchange_identification: read: Connection reset", exitError(t, 255)),
			wantNote: []string{"could not", "Connection reset", "zerops_dev_server"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fx := newLeaseFixture(t)
			fx.swarm.acquired = acquiredAs(3)
			ssh := &leaseSSH{}
			ssh.setHandler(supervisorAnswer(tt.answer, tt.err))
			fx.deps.SSH = ssh

			view, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "feature")
			if err != nil {
				t.Fatalf("acquire: %v (a supervisor that is missing or will not start never fails the acquire)", err)
			}
			if view.State != LeaseStateHeld {
				t.Errorf("state = %q, want held", view.State)
			}
			cmds := ssh.commands()
			starts, checkout := -1, -1
			for i, c := range cmds {
				switch {
				case strings.Contains(c, "zcp-devsup run"):
					if starts >= 0 {
						t.Errorf("the supervisor was ensured twice: %v", cmds)
					}
					starts = i
				case strings.Contains(c, "fetch -q --prune origin"):
					checkout = i
				}
			}
			if starts < 0 || checkout < 0 || starts < checkout {
				t.Fatalf("commands = %v, want the checkout, then the supervisor ensured", cmds)
			}
			if ssh.calls[starts].hostname != "devpool-3" {
				t.Errorf("the supervisor was ensured on %q, want the alias devpool-3", ssh.calls[starts].hostname)
			}
			if len(tt.wantNote) == 0 && view.Note != "" {
				t.Errorf("note = %q, want none", view.Note)
			}
			for _, want := range tt.wantNote {
				if !strings.Contains(view.Note, want) {
					t.Errorf("note %q lacks %q", view.Note, want)
				}
			}
			if strings.Contains(view.Note, testLease) {
				t.Errorf("note leaks the lease: %q", view.Note)
			}
			if _, err := loadLeaseRecord(fx.stateDir, "devpool-3"); err != nil {
				t.Errorf("the lease record is gone: %v", err)
			}
		})
	}
}

func TestDevServer_SupervisorStartedByZcpOnlyInALease(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		lease   bool
		running bool // a supervisor answers already
		action  string
		wantErr bool
	}{
		{name: "lease start", lease: true, action: "start"},
		// One that answers never gets a second beside it.
		{name: "lease whose supervisor answers", lease: true, running: true, action: "status"},
		{name: "lease restart", lease: true, action: "restart"},
		{name: "lease status", lease: true, action: "status"},
		{name: "lease stop", lease: true, action: "stop"},
		{name: "lease logs", lease: true, action: "logs"},
		// A dev service keeps today's behaviour: its supervisor is its dev
		// setup's run.start, so a start answers NoSupervisorError (the move
		// onto the supervisor) and nothing is started over ssh.
		{name: "dev service start", lease: false, action: "start", wantErr: true},
		{name: "dev service status", lease: false, action: "status"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &devservertest.Container{Installed: true, SupervisorVersion: "v9.300.0", Formats: []int{1}, Declaration: previewDeclaration}
			if tt.running {
				c = devservertest.NewSupervised(previewDeclaration)
			}
			c.Logs = map[string][]string{"web": {"ready"}}
			p := DevServerParams{Action: tt.action, Hostname: "appdev", Name: "web", Command: "pnpm dev", Port: 3000}
			client := devPoolProjectClient()
			if tt.lease {
				p.Hostname, p.StateDir = "devpool-3", leaseState(t)
			}

			result, err := ExecuteDevServer(context.Background(), c, client, "p1", p)

			if !tt.lease {
				if c.SupervisorStarts != 0 || sentWith(c, "zcp-devsup run") != 0 {
					t.Errorf("zcp started a supervisor on a dev service: %v", c.Sent())
				}
				var noSup *NoSupervisorError
				if tt.wantErr && !errors.As(err, &noSup) {
					t.Errorf("err = %v, want NoSupervisorError (the move onto the supervisor)", err)
				}
				if !tt.wantErr && (err != nil || result.Reason != reasonNoSupervisor) {
					t.Errorf("result = %+v, %v; want the no-supervisor answer", result, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s on a lease whose supervisor is gone: %v", tt.action, err)
			}
			wantStarts := 1
			if tt.running {
				wantStarts = 0
			}
			if c.SupervisorStarts != wantStarts || sentWith(c, "zcp-devsup run") != wantStarts {
				t.Errorf("supervisors started = %d (%d start commands), want %d", c.SupervisorStarts, sentWith(c, "zcp-devsup run"), wantStarts)
			}
			if result.Reason == reasonNoSupervisor {
				t.Errorf("result = %+v, want the supervisor's answer after zcp started it", result)
			}
			if result.Pool != "devpool" {
				t.Errorf("pool = %q, want devpool", result.Pool)
			}
		})
	}
}

// devPoolProjectClient holds the pool devpool and a dev service appdev.
func devPoolProjectClient() *platform.Mock {
	return poolClient(0).WithServices([]platform.ServiceStack{
		{
			ID: "svc-pool", Name: "devpool", ProjectID: "p1", Status: "ACTIVE",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: "USER", ServiceStackTypeVersionName: "swarm@1"},
		},
		{
			ID: "svc-app", Name: "appdev", ProjectID: "p1", Status: "ACTIVE",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: "USER", ServiceStackTypeVersionName: "nodejs@22"},
		},
	})
}

func TestDevServer_LeaseSupervisorMissingOrNotComingUp_SaysWhy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		installed bool
		action    string
		want      []string
	}{
		{name: "no zcp-devsup: start refused", installed: false, action: "start", want: []string{"devpool", "zcp-devsup", "not a dev pool", "run.prepareCommands"}},
		{name: "no zcp-devsup: status", installed: false, action: "status", want: []string{"devpool", "not a dev pool"}},
		{name: "will not come up: start refused", installed: true, action: "start", want: []string{"permission denied", "supervisor.log"}},
		{name: "will not come up: status", installed: true, action: "status", want: []string{"permission denied"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &devservertest.Container{
				Installed: tt.installed, SupervisorVersion: "v9.300.0", Formats: []int{1}, Declaration: previewDeclaration,
				FailStart: true, StartOutput: "zcp-devsup: control socket: permission denied",
			}

			result, err := ExecuteDevServer(context.Background(), c, poolClient(0), "p1",
				DevServerParams{Action: tt.action, Hostname: "devpool-3", StateDir: leaseState(t), Command: "pnpm dev", Port: 3000})

			var text string
			switch {
			case tt.action == "start":
				var pe *platform.PlatformError
				if !errors.As(err, &pe) {
					t.Fatalf("err = %v, want a refusal", err)
				}
				text = pe.Message + " " + pe.Suggestion
			case err != nil:
				t.Fatalf("status: %v", err)
			default:
				if result.Reason != reasonNoSupervisor {
					t.Errorf("reason = %q, want %q", result.Reason, reasonNoSupervisor)
				}
				text = result.Message
			}
			for _, want := range tt.want {
				if !strings.Contains(text, want) {
					t.Errorf("answer %q lacks %q", text, want)
				}
			}
			wantStarts := 0
			if tt.installed {
				wantStarts = 1
			}
			if c.SupervisorStarts != wantStarts {
				t.Errorf("supervisors started = %d, want %d (once per call, never retried in a loop)", c.SupervisorStarts, wantStarts)
			}
		})
	}
}

func TestLeaseDevServerChecks_NoSupervisor_SaysWhatALeaseNeeds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		installed bool
		want      []string
	}{
		{name: "no zcp-devsup in the image", installed: false, want: []string{"not a dev pool"}},
		{name: "installed, nothing runs it", installed: true, want: []string{"devpool-3", `zerops_dev_server action=status hostname="devpool-3"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &devservertest.Container{Installed: tt.installed, SupervisorVersion: "v9.300.0", Formats: []int{1}, Declaration: previewDeclaration}

			checks := leaseDevServerChecks(context.Background(), c, "devpool-3")

			if len(checks) != 1 || checks[0].Name != "dev_servers" {
				t.Fatalf("checks = %+v, want one dev_servers note", checks)
			}
			for _, want := range tt.want {
				if !strings.Contains(checks[0].Detail, want) {
					t.Errorf("detail %q lacks %q", checks[0].Detail, want)
				}
			}
			// A lease's supervisor is never a matter of its run.start.
			if strings.Contains(checks[0].Detail, "run.start is not") {
				t.Errorf("detail %q blames run.start", checks[0].Detail)
			}
			// Verify reads; it starts nothing.
			if c.SupervisorStarts != 0 || sentWith(c, "zcp-devsup run") != 0 {
				t.Errorf("verify started a supervisor: %v", c.Sent())
			}
		})
	}
}
