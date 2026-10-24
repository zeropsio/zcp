package tools

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

type catSSH struct{ yaml string }

func (s catSSH) ExecSSH(context.Context, string, string) ([]byte, error) { return []byte(s.yaml), nil }

func (s catSSH) ExecSSHStdin(ctx context.Context, hostname, command string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, hostname, command)
}

func (s catSSH) ExecSSHBackground(context.Context, string, string, time.Duration) ([]byte, error) {
	return nil, nil
}

// TestAdoptVolumeSetups_ReadFromTheRuntimeContainerBeforeAnyMountExists: the
// discover step runs before the runtimes are mounted, so the repository's
// zerops.yaml reaches adopt over SSH, not through a mount that is not there yet.
func TestAdoptVolumeSetups_ReadFromTheRuntimeContainerBeforeAnyMountExists(t *testing.T) {
	t.Parallel()
	existing := []platform.ServiceStack{
		{Name: "vol", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "local-storage:single@1"}},
		{Name: "webdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "bun@1.3"}},
	}
	yml := "zerops:\n  - setup: dev\n    run:\n      base: bun@1.3\n      volume:\n        hostname: vol\n        mountPath: /var/www/ws\n"
	got := adoptVolumeSetups(context.Background(), catSSH{yml}, existing, t.TempDir()+"/a/b")
	if len(got) != 1 || got[0].Setup != "dev" || got[0].Volume != "vol" || got[0].MountPath != "/var/www/ws" {
		t.Fatalf("got %+v, want the dev setup mounting vol at /var/www/ws", got)
	}
}

// volumeSSH answers the zerops.yaml read with yaml and the volume probe with
// what each host holds at the mount.
type volumeSSH struct {
	yaml    string
	content map[string]string
}

func (s volumeSSH) ExecSSH(_ context.Context, host, cmd string) ([]byte, error) {
	if strings.HasPrefix(cmd, "cat ") {
		return []byte(s.yaml), nil
	}
	c, ok := s.content[host]
	if !ok {
		return nil, errors.New("ssh: connect refused")
	}
	return []byte("@@zcp code-volume content " + c + "\n"), nil
}

func (s volumeSSH) ExecSSHStdin(ctx context.Context, hostname, command string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, hostname, command)
}

func (volumeSSH) ExecSSHBackground(context.Context, string, string, time.Duration) ([]byte, error) {
	return nil, nil
}

// TestAdoptVolumeSetups_ReadWhetherTheDevSetupsWorkInTheVolumeAndWhatItHolds:
// adopt gets, per setup mounting a shared volume, whether its run works in the
// mount and what each runtime of its base finds there. A Laravel app and worker
// work in /var/www, so their storage volume is never probed; F2's idle dev
// setups work wherever zcp deploys them, so each runtime's view is read, and
// one that cannot be read proves nothing.
func TestAdoptVolumeSetups_ReadWhetherTheDevSetupsWorkInTheVolumeAndWhatItHolds(t *testing.T) {
	t.Parallel()
	laravelYml := "zerops:\n" +
		"  - setup: app\n    run:\n      base: php-nginx@8.4\n      documentRoot: public\n      volume:\n        hostname: storage\n        mountPath: /var/www/storage\n" +
		"  - setup: worker\n    run:\n      base: php@8.4\n      start: php artisan queue:work\n      volume:\n        hostname: storage\n        mountPath: /var/www/storage\n"
	f2Yml := "zerops:\n" +
		"  - setup: dev\n    run:\n      base: bun@1.3\n      start: zsc noop --silent\n      volume:\n        hostname: vol\n        mountPath: /var/www/ws\n" +
		"  - setup: dev-api\n    run:\n      base: go@1\n      os: ubuntu\n      start: zsc noop --silent\n      volume:\n        hostname: vol\n        mountPath: /var/www/ws\n"
	svc := func(name, typ string) platform.ServiceStack {
		return platform.ServiceStack{Name: name, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: typ}}
	}
	tests := []struct {
		name        string
		existing    []platform.ServiceStack
		ssh         volumeSSH
		wantWorksIn []bool
		wantContent []map[string]topology.VolumeContent
	}{
		{
			name:        "Laravel app and worker sharing storage",
			existing:    []platform.ServiceStack{svc("app", "php-nginx@8.4"), svc("worker", "php@8.4"), svc("storage", "local-storage:single@1")},
			ssh:         volumeSSH{yaml: laravelYml, content: map[string]string{"app": "foreign", "worker": "foreign"}},
			wantWorksIn: []bool{false, false},
			wantContent: []map[string]topology.VolumeContent{nil, nil},
		},
		{
			name:        "F2 before the first deploy mounts the volume",
			existing:    []platform.ServiceStack{svc("webdev", "bun@1.3"), svc("apidev", "go@1"), svc("vol", "local-storage:single@1")},
			ssh:         volumeSSH{yaml: f2Yml, content: map[string]string{"webdev": "absent", "apidev": "absent"}},
			wantWorksIn: []bool{true, true},
			wantContent: []map[string]topology.VolumeContent{{"webdev": topology.VolumeAbsent}, {"apidev": topology.VolumeAbsent}},
		},
		{
			name:        "F2 with a runtime out of reach",
			existing:    []platform.ServiceStack{svc("webdev", "bun@1.3"), svc("apidev", "go@1"), svc("vol", "local-storage:single@1")},
			ssh:         volumeSSH{yaml: f2Yml, content: map[string]string{"webdev": "empty"}},
			wantWorksIn: []bool{true, true},
			wantContent: []map[string]topology.VolumeContent{{"webdev": topology.VolumeEmpty}, {}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := adoptVolumeSetups(context.Background(), tt.ssh, tt.existing, t.TempDir()+"/a/b")
			if len(got) != len(tt.wantWorksIn) {
				t.Fatalf("got %+v", got)
			}
			for i, s := range got {
				if s.WorksIn != tt.wantWorksIn[i] || !maps.Equal(s.Content, tt.wantContent[i]) {
					t.Errorf("setup %s: worksIn=%v content=%v, want %v %v", s.Setup, s.WorksIn, s.Content, tt.wantWorksIn[i], tt.wantContent[i])
				}
			}
		})
	}
}
