// Tests for: ops/deploy_ssh.go — a deploy whose source is a lease alias
// (docs/spec-workflows.md §8 DP-1, §4.9).
package ops

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// leaseDeployState records one lease (webpool-<number> → node-id-<number> of
// pool webpool) in a fresh state directory and returns the directory and the
// alias. The git lock map is shared by the package, so a test that holds a
// lock uses a number no parallel sibling uses.
func leaseDeployState(t *testing.T, number int64) (string, string) {
	t.Helper()
	stateDir := t.TempDir()
	rec := leaseRecord{
		PoolHostname:      "webpool",
		PoolServiceID:     "svc-pool",
		ContainerID:       "c-" + strconv.FormatInt(number, 10),
		ContainerNumber:   number,
		ContainerHostname: "node-id-" + strconv.FormatInt(number, 10) + ".runtime.webpool.zerops",
		Lease:             "secret-lease-value",
		Branch:            "feat-x",
		AcquiredAt:        "2026-10-23T12:00:00Z",
	}
	if err := writeLeaseRecord(stateDir, rec); err != nil {
		t.Fatalf("write lease record: %v", err)
	}
	return stateDir, rec.alias()
}

// leaseDeployServices is a project that holds the part's stage and the dev
// pool — and no service named like the lease alias.
func leaseDeployServices() []platform.ServiceStack {
	return []platform.ServiceStack{
		{ID: "svc-stage", Name: "appstage"},
		{ID: "svc-pool", Name: "webpool", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1"}},
	}
}

func TestDeploySSH_LeaseSource_NoFindServiceAndPushesFromLease(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		number   int64
		sha      string
		results  []sshResult
		wantPush []string
	}{
		{
			name:     "working tree",
			number:   11,
			wantPush: []string{"zcli push --service-id svc-stage"},
		},
		{
			name:   "from a commit",
			number: 12,
			sha:    "abc123",
			results: []sshResult{
				{output: []byte("f0115ba1234567\n")},     // resolve
				{output: []byte(validCommitZeropsYaml)},  // git show <sha>:zerops.yaml
				{output: []byte("/tmp/zcp-extract-1\n")}, // mktemp -d
				{output: []byte("")},                     // extract
				{output: []byte("ok")},                   // push
				{output: []byte("")},                     // cleanup
			},
			wantPush: []string{"zcli push --service-id svc-stage --no-git", "/tmp/zcp-extract-1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir, alias := leaseDeployState(t, tt.number)
			mock := platform.NewMock().WithServices(leaseDeployServices())
			ssh := &mockSSHDeployer{output: []byte("ok"), results: tt.results}

			result, err := DeploySSHWithLeases(context.Background(), mock, "proj-1", ssh, testAuthInfo(),
				stateDir, alias, "appstage", "app", "", tt.sha)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.SourceService != alias || result.TargetService != "appstage" || result.TargetServiceID != "svc-stage" {
				t.Errorf("result = source %q target %q id %q", result.SourceService, result.TargetService, result.TargetServiceID)
			}
			if result.ReachesNewContainersOnly {
				t.Error("a deploy to the part's stage must not carry the pool-image note")
			}
			if len(ssh.calls) == 0 {
				t.Fatal("no ssh calls")
			}
			var pushCmd string
			for _, c := range ssh.calls {
				if c.hostname != alias {
					t.Errorf("ssh hostname = %q, want the lease alias %q for every call (command %q)", c.hostname, alias, c.command)
				}
				if strings.Contains(c.command, "zcli push") {
					pushCmd = c.command
				}
			}
			for _, want := range tt.wantPush {
				if !strings.Contains(pushCmd, want) {
					t.Errorf("push command %q lacks %q", pushCmd, want)
				}
			}
		})
	}
}

func TestDeploySSH_LeaseSource_LockKeyedOnAlias(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		number    int64
		holdAlias bool // else the lock of another source is held
	}{
		{name: "alias held", number: 21, holdAlias: true},
		{name: "another source held", number: 22},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir, alias := leaseDeployState(t, tt.number)
			mock := platform.NewMock().WithServices(leaseDeployServices())

			held := "webpool-other-" + alias
			if tt.holdAlias {
				held = alias
			}
			unlock, err := deploySourceGitLocks.lock(context.Background(), held)
			if err != nil {
				t.Fatalf("hold lock: %v", err)
			}
			defer unlock()

			ssh := &mockSSHDeployer{output: []byte("ok")}
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			_, derr := DeploySSHWithLeases(ctx, mock, "proj-1", ssh, testAuthInfo(),
				stateDir, alias, "appstage", "app", "", "")
			if !tt.holdAlias {
				if derr != nil {
					t.Fatalf("unexpected error with an unrelated lock held: %v", derr)
				}
				return
			}
			if !errors.Is(derr, context.DeadlineExceeded) {
				t.Fatalf("error = %v, want the lock wait to end with the context deadline", derr)
			}
			if len(ssh.calls) != 0 {
				t.Errorf("ssh calls = %d while the alias lock is held, want 0", len(ssh.calls))
			}
		})
	}
}

func TestDeploySSH_LeaseToOwnPool_NoteAndFlag(t *testing.T) {
	t.Parallel()

	stateDir, alias := leaseDeployState(t, 31)
	mock := platform.NewMock().WithServices(leaseDeployServices())
	ssh := &mockSSHDeployer{output: []byte("ok")}

	result, err := DeploySSHWithLeases(context.Background(), mock, "proj-1", ssh, testAuthInfo(),
		stateDir, alias, "webpool", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.ReachesNewContainersOnly {
		t.Error("ReachesNewContainersOnly = false for a deploy from a lease to its own pool")
	}
	if result.TargetServiceID != "svc-pool" {
		t.Errorf("target id = %q, want svc-pool", result.TargetServiceID)
	}
	for _, want := range []string{"containers webpool creates after it", alias + " keeps the version it runs", "not updated"} {
		if !strings.Contains(result.Message, want) {
			t.Errorf("message %q lacks %q", result.Message, want)
		}
	}
	for _, banned := range []string{"your dev", "runs the new code"} {
		if strings.Contains(result.Message, banned) {
			t.Errorf("message %q must not say %q", result.Message, banned)
		}
	}
}

// TestDeploySSH_NotALease_KeepsTodaysErrors: an alias-shaped host with no
// record, and a service name, resolve as services — the lease path is only
// for a recorded alias (DP-2).
func TestDeploySSH_NotALease_KeepsTodaysErrors(t *testing.T) {
	t.Parallel()

	stateDir, _ := leaseDeployState(t, 41)
	tests := []struct {
		name   string
		source string
	}{
		{name: "alias shape without a record", source: "webpool-42"},
		{name: "unknown hostname", source: "ghost"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock().WithServices(leaseDeployServices())
			ssh := &mockSSHDeployer{output: []byte("ok")}
			_, err := DeploySSHWithLeases(context.Background(), mock, "proj-1", ssh, testAuthInfo(),
				stateDir, tt.source, "appstage", "app", "", "")
			var pe *platform.PlatformError
			if !errors.As(err, &pe) || pe.Code != platform.ErrServiceNotFound {
				t.Fatalf("error = %v, want SERVICE_NOT_FOUND", err)
			}
			if len(ssh.calls) != 0 {
				t.Errorf("ssh calls = %d, want 0", len(ssh.calls))
			}
		})
	}
}

// TestSourceMountDir_LeaseAlias: the zerops.yaml DM-2 validation reads the
// lease's checkout through its mount /var/www/<alias> (DP-1).
func TestSourceMountDir_LeaseAlias(t *testing.T) {
	t.Parallel()
	if got := SourceMountDir(defaultWorkingDir, "webpool-3", ""); got != "/var/www/webpool-3" {
		t.Fatalf("SourceMountDir = %q, want /var/www/webpool-3", got)
	}
}

// TestDeploySSH_DevSourceToPool_NoteAndFlag: a deploy whose target is a pool
// reaches only the containers the pool creates later whatever its source
// (docs/spec-workflows.md §8 DP-7) — a plain dev service names no lease.
func TestDeploySSH_DevSourceToPool_NoteAndFlag(t *testing.T) {
	t.Parallel()

	services := append(leaseDeployServices(), platform.ServiceStack{ID: "svc-dev", Name: "appdev",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}})
	mock := platform.NewMock().WithServices(services)
	ssh := &mockSSHDeployer{output: []byte("ok")}

	result, err := DeploySSHWithLeases(context.Background(), mock, "proj-1", ssh, testAuthInfo(),
		t.TempDir(), "appdev", "webpool", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.ReachesNewContainersOnly {
		t.Error("ReachesNewContainersOnly = false for a deploy whose target is a pool")
	}
	for _, want := range []string{"containers webpool creates after it", "leased or free, keep their version", "not updated"} {
		if !strings.Contains(result.Message, want) {
			t.Errorf("message %q lacks %q", result.Message, want)
		}
	}
	if strings.Contains(result.Message, "leased container") {
		t.Errorf("message %q names a leased container for a dev-service source", result.Message)
	}
	for _, c := range ssh.calls {
		if c.hostname != "appdev" {
			t.Errorf("ssh reached %q, want only the source appdev", c.hostname)
		}
	}
}
