// Tests for: ops/lease_wiring.go, the acquire path of lease_prepare.go — a
// part's dev servers reach the same branch's sibling workspaces by container
// hostname: zcp hands the supervisor the overlay the declaration's siblingEnv
// names (`zcp-devsup ctl env`) before it starts or reloads a lease's servers,
// only when the supervisor lists the env capability (docs/spec-workflows.md §8
// "Dev pools (DP)", docs/spec-mate.md §2.8a.1, §2.8a.2).
package ops

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/ops/devservertest"
)

// twoLeaseServers declares web (:3000) and api (:8080).
const twoLeaseServers = `{"version":1,"servers":[{"name":"web","port":3000,"command":"pnpm dev"},{"name":"api","port":8080,"command":"go run ."}]}`

func TestAwaitOverlayRestarts_WaitsForEachServerTheChangeRestarts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		reads       int // answers before the supervisor's restarts show
		stopped     string
		budget      time.Duration
		wantPending []string
	}{
		{name: "the restarts show after two more reads", reads: 2, budget: 5 * time.Second},
		{name: "a stopped server is not waited for", reads: 1, stopped: "api", budget: 5 * time.Second},
		{name: "the budget ends first: the result names what is still to restart", reads: 1 << 20, budget: 30 * time.Millisecond, wantPending: []string{"api", "web"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := devservertest.NewSupervised(twoLeaseServers)
			c.EnvRestartReads = tt.reads
			if tt.stopped != "" {
				c.Servers[tt.stopped].Status, c.Servers[tt.stopped].Desired = devsup.StatusStopped, devsup.DesiredStopped
			}
			before, err := supervisorCtl(context.Background(), c, "webpool-3", "status")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := supervisorCtl(context.Background(), c, "webpool-3", "env", "--set", "HQ_HOST=node-id-7.runtime.hqpool.zerops"); err != nil {
				t.Fatal(err)
			}

			st, pending, err := awaitOverlayRestarts(context.Background(), c, "webpool-3", before, tt.budget, time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(pending, tt.wantPending) {
				t.Errorf("pending = %v, want %v", pending, tt.wantPending)
			}
			if len(tt.wantPending) > 0 {
				return
			}
			for name, s := range st.Servers {
				wantRestarts := 1
				if name == tt.stopped {
					wantRestarts = 0
				}
				if s.Restarts != wantRestarts {
					t.Errorf("%s restarts = %d in the state returned, want %d", name, s.Restarts, wantRestarts)
				}
			}
		})
	}
}

// wiringVersion is a supervisor's `ctl version` answer listing env.
const wiringVersion = `{"version":"v9.400.0","formats":[1],"capabilities":["env"]}`

// wiringAcquireHandler answers a group acquire's commands: the checkout (a new
// branch, cut from groupBase), the checked-out declaration with the
// supervisor's version (version "" for a container without zcp-devsup) and
// the supervisor step.
func wiringAcquireHandler(version, decl, ensure string) func(host, command string) ([]byte, error) {
	return func(_, command string) ([]byte, error) {
		switch {
		case strings.Contains(command, "fetch -q --prune origin"):
			return []byte("@@zcp-lease base " + groupBase + "\n"), nil
		case strings.Contains(command, "@@zcp-devsup declaration"):
			out := "@@zcp-devsup version 127\n"
			if version != "" {
				out = version + "\n@@zcp-devsup version 0\n"
			}
			return []byte(out + decl + "\n@@zcp-devsup declaration 0\n"), nil
		case strings.Contains(command, "zcp-devsup run"):
			return []byte(ensure), nil
		case strings.Contains(command, ctlCall):
			return []byte(ctlTook), nil
		}
		return nil, nil
	}
}

// supervisorStep is the supervisor step's command sent to host.
func supervisorStep(t *testing.T, s *leaseSSH, host string) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.calls {
		if c.hostname == host && strings.Contains(c.command, "zcp-devsup run") {
			return c.command
		}
	}
	t.Fatalf("no supervisor step was sent to %s", host)
	return ""
}

// groupSiblingDeclaration has each part reach the other's lease of the branch.
const groupSiblingDeclaration = `{"version":1,"siblingEnv":{"HQ_HOST":"hqpool","WEB_HOST":"webpool"},"servers":[]}`

// TestLeaseAcquire_SiblingEnv_SentWithTheSupervisorStep: an acquire's
// supervisor step hands each lease its group's overlay as a call of its own
// after the supervisor is ensured — before the reload of one that answered
// (run.start's, booted on an empty /var/www), right after the start of one
// zcp started — so the declared servers run under it (DP-22). The ensure
// itself carries no overlay: the overlay is computed and sent under the
// group's lock, which the ensure, whose start may wait, never holds (DP-26).
// A refusal is a note, never a failed acquire.
func TestLeaseAcquire_SiblingEnv_SentWithTheSupervisorStep(t *testing.T) {
	t.Parallel()
	const overlay = "zcp-devsup ctl 'env' '--set' 'HQ_HOST=node-id-3.runtime.hqpool.zerops' '--set' 'WEB_HOST=node-id-7.runtime.webpool.zerops' --json"
	tests := []struct {
		name      string
		decl      string
		ensure    string
		envAnswer string
		wantEnv   string
		// wantOrder are the supervisor step's calls, in order.
		wantOrder []string
		wantNote  []string
	}{
		{
			name: "every member gets the group's overlay, then the reload", decl: groupSiblingDeclaration,
			ensure: "@@zcp-devsup check 0\n", envAnswer: ctlTook, wantEnv: overlay,
			wantOrder: []string{"zcp-devsup run", overlay, "zcp-devsup ctl 'reload'"},
		},
		{
			name: "no siblingEnv: the empty overlay replaces a previous holder's, after the start", decl: `{"version":1,"servers":[]}`,
			ensure: "@@zcp-devsup check 2\n@@zcp-devsup started 0\n", envAnswer: ctlTook, wantEnv: "zcp-devsup ctl 'env' '--clear' --json",
			wantOrder: []string{"zcp-devsup run", "zcp-devsup ctl 'env' '--clear' --json"},
		},
		{
			name: "a refused overlay is a note, never a failed acquire, and the reload still goes", decl: groupSiblingDeclaration,
			ensure: "@@zcp-devsup check 0\n", envAnswer: "{\"code\":1,\"error\":\"env: the state directory is read-only\"}\n@@zcp-devsup ctl 1\n",
			wantEnv: overlay, wantOrder: []string{"zcp-devsup run", overlay, "zcp-devsup ctl 'reload'"},
			wantNote: []string{"the state directory is read-only", "HQ_HOST", "zerops_dev_server"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gf := newGroupFixture(t)
			acquire := wiringAcquireHandler(wiringVersion, tt.decl, tt.ensure)
			gf.ssh.setHandler(func(host, command string) ([]byte, error) {
				if strings.Contains(command, "zcp-devsup ctl 'env'") {
					return []byte(tt.envAnswer), nil
				}
				return acquire(host, command)
			})

			views, err := LeaseAcquireGroup(context.Background(), gf.deps, []string{groupHQPool, groupWebPool}, testRepo, "feat")
			if err != nil {
				t.Fatalf("acquire: %v", err)
			}
			for _, v := range views {
				if step := supervisorStep(t, gf.ssh, v.Alias); strings.Contains(step, "ctl 'env'") {
					t.Errorf("%s's ensure carries the overlay; it is a call of its own:\n%s", v.Alias, step)
				}
				var order []string
				for _, c := range sentTo(gf.ssh, v.Alias) {
					for _, want := range tt.wantOrder {
						if strings.Contains(c, want) {
							order = append(order, want)
						}
					}
				}
				if !slices.Equal(order, tt.wantOrder) {
					t.Errorf("%s's supervisor step = %q, want %q", v.Alias, order, tt.wantOrder)
				}
				if len(tt.wantNote) == 0 && v.Note != "" {
					t.Errorf("%s note = %q, want none", v.Alias, v.Note)
				}
				for _, want := range tt.wantNote {
					if !strings.Contains(v.Note, want) {
						t.Errorf("%s note %q lacks %q", v.Alias, v.Note, want)
					}
				}
			}
		})
	}
}

// sentTo are the commands sent to host, in order.
func sentTo(s *leaseSSH, host string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, c := range s.calls {
		if c.hostname == host {
			out = append(out, c.command)
		}
	}
	return out
}

func TestLeaseAcquire_SiblingEnv_SupervisorWithoutEnvCapability_NotedNotSent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		decl     string
		wantNote []string
	}{
		{name: "a non-empty overlay: the note says what is missing and how to get it", decl: groupSiblingDeclaration,
			wantNote: []string{"v9.200.0", "HQ_HOST", "env", "run.prepareCommands", "zerops_lease action=rollout"}},
		{name: "an empty overlay: nothing to say", decl: `{"version":1,"servers":[]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gf := newGroupFixture(t)
			gf.ssh.setHandler(wiringAcquireHandler(`{"version":"v9.200.0","formats":[1]}`, tt.decl, "@@zcp-devsup check 0\n"))

			views, err := LeaseAcquireGroup(context.Background(), gf.deps, []string{groupHQPool, groupWebPool}, testRepo, "feat")
			if err != nil {
				t.Fatalf("acquire: %v", err)
			}
			for _, v := range views {
				for _, c := range sentTo(gf.ssh, v.Alias) {
					if strings.Contains(c, "ctl 'env'") {
						t.Errorf("%s: ctl env sent to a supervisor that does not list it:\n%s", v.Alias, c)
					}
				}
				if len(tt.wantNote) == 0 && v.Note != "" {
					t.Errorf("%s note = %q, want none", v.Alias, v.Note)
				}
				for _, want := range tt.wantNote {
					if !strings.Contains(v.Note, want) {
						t.Errorf("%s note %q lacks %q", v.Alias, v.Note, want)
					}
				}
			}
		})
	}
}
