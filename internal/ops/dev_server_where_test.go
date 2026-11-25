package ops

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/ops/devservertest"
)

// DS-READ-NOSTART — Reading a lease's dev servers for orientation never starts its missing supervisor, so an agent's message cannot change the container.
// witness: internal/ops/dev_server.go | ds, err := ReadDevServers(ctx, ssh, hostname, root) → ds, _, err := recoverLeaseSupervisor(ctx, ssh, hostname, root, nil, &NoSupervisorError{Hostname: hostname, Installed: true})
func TestDevServer_DSReadNoStart_LeavesSupervisorStopped(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised(previewDeclaration)
	c.Running = false
	server, checkout := ReadDevServerStatus(context.Background(), c, nil, "p1", leaseState(t), "devpool-3", "", true)
	got := <-server
	<-checkout
	if got.ServersUnread == "" || got.Servers != nil {
		t.Fatalf("supervisor read = %+v, want unread", got)
	}
	for _, command := range c.Sent() {
		if strings.Contains(command, "@@zcp-devsup check") {
			t.Errorf("read started the supervisor: %s", command)
		}
		if strings.Contains(command, leaseStatusMarker) && strings.Contains(command, "ctl status") {
			t.Error("checkout read asked the supervisor again")
		}
	}
}

// DS-READ-NORELOAD — Reading dev servers for orientation leaves an undeclared running server alone, so an agent's message cannot stop a person's process.
// witness: internal/ops/dev_server.go | result := &DevServerResult{Hostname: hostname, Read: ds} → _, _ = dropUndeclared(ctx, ssh, ds); result := &DevServerResult{Hostname: hostname, Read: ds}
func TestDevServer_DSReadNoReload_KeepsUndeclaredServer(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised(oneServer)
	c.Servers["other"] = &devsup.ServerState{Source: devsup.SourceDeclared, Status: devsup.StatusRunning}
	server, _ := ReadDevServerStatus(context.Background(), c, nil, "p1", "", "apidev", "", false)
	got := <-server
	if len(got.Servers) != 2 || got.Servers[1].Name != "other" || got.Servers[1].Status != "running" {
		t.Fatalf("supervisor read = %+v, want both running servers", got)
	}
	for _, command := range c.Sent() {
		if strings.Contains(command, "ctl reload") {
			t.Errorf("read reloaded the supervisor: %s", command)
		}
	}
}

// DS-READ-EMPTY — An answering supervisor with no servers is distinct from one that did not answer, so an agent never reads an unread supervisor as empty.
// witness: internal/ops/dev_server.go | servers <- DevServerRead{ServersUnread: devServerReadReason(err)} → servers <- DevServerRead{Servers: []topology.DevServerState{}}
func TestDevServer_DSReadEmpty_DistinguishesUnread(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		running    bool
		wantUnread bool
	}{
		{"answered empty", true, false},
		{"supervisor absent", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := devservertest.NewSupervised("")
			c.Running = tc.running
			server, _ := ReadDevServerStatus(context.Background(), c, nil, "p1", "", "appdev", "", false)
			got := <-server
			if tc.wantUnread {
				if got.ServersUnread == "" || got.Servers != nil {
					t.Fatalf("got %+v, want unread", got)
				}
			} else if got.ServersUnread != "" || got.Servers == nil || len(got.Servers) != 0 {
				t.Fatalf("got %+v, want an answered empty list", got)
			}
		})
	}
}

type malformedWhereSupervisor struct{ *devservertest.Container }

func (c malformedWhereSupervisor) ExecSSH(context.Context, string, string) ([]byte, error) {
	return []byte(`{"env":{"KEY":"private-value"}}`), nil
}

// DS-READ-SAFE — A malformed supervisor answer cannot expose its environment in the reason the agent reads.
// witness: internal/ops/dev_server.go | ServersUnread: devServerReadReason(err) → ServersUnread: DevServerReadFailure(err.Error())
func TestDevServer_DSReadSafe_HidesSupervisorEnvironment(t *testing.T) {
	t.Parallel()
	ssh := malformedWhereSupervisor{devservertest.NewSupervised("")}
	server, _ := ReadDevServerStatus(context.Background(), ssh, nil, "p1", "", "appdev", "", false)
	if got := <-server; got.ServersUnread != "supervisor read failed" {
		t.Fatalf("unread reason = %q; want safe supervisor failure", got.ServersUnread)
	}
}
