package ops

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// A variant-B pair keeps its code on a local-storage volume mounted under
// /var/www (docs/spec-mate.md §2.8a): a workingDir there is a container path,
// while a folder named like a service stays zcp's local mount of it.
func TestDeploy_WorkingDirOnCodeVolume_RunsThere(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		workingDir string
		wantErr    bool
	}{
		{name: "the code volume's folder runs there", workingDir: "/var/www/ws"},
		{name: "a deeper code volume folder runs there", workingDir: "/var/www/code/repo"},
		{name: "a service's mount stays refused beside a volume", workingDir: "/var/www/apidev", wantErr: true},
		{name: "a folder in a service's mount stays refused beside a volume", workingDir: "/var/www/webdev/ws", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock().WithServices([]platform.ServiceStack{
				{ID: "svc-web", Name: "webdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "bun@1.3"}},
				{ID: "svc-api", Name: "apidev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "go@1"}},
				{ID: "svc-vol", Name: "vol", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "local-storage:single@1"}},
			})
			ssh := &mockSSHDeployer{output: []byte("ok")}

			_, err := DeploySSH(context.Background(), mock, "proj-1", ssh, testAuthInfo(), "apidev", "apidev", "dev-api", tt.workingDir, "")

			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "SSHFS mount") {
					t.Fatalf("err = %v, want the mount-path refusal", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v, want the deploy to run", err)
			}
			pushed := false
			for _, c := range ssh.calls {
				if strings.Contains(c.command, "zcli push") {
					pushed = true
					if !strings.HasPrefix(c.command, "cd '"+tt.workingDir+"' &&") {
						t.Errorf("push = %q, want it to run in %s", c.command, tt.workingDir)
					}
				}
			}
			if !pushed {
				t.Fatalf("no push ran: %+v", ssh.calls)
			}
		})
	}
}

func TestSourceMountDir_ShowsTheWorkingDirBelowTheSourceMount(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ workingDir, want string }{
		{"", "/root/apidev"},
		{"/var/www", "/root/apidev"},
		{"/var/www/ws", "/root/apidev/ws"},
		{"/opt/app", "/root/apidev"},
	} {
		if got := SourceMountDir("/root", "apidev", tt.workingDir); got != tt.want {
			t.Errorf("SourceMountDir(%q) = %q, want %q", tt.workingDir, got, tt.want)
		}
	}
}
