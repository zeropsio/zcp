package tools

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync"
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
	got := adoptVolumeSetups(context.Background(), catSSH{yml}, existing, []string{"webdev"}, t.TempDir()+"/a/b")
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
			got := adoptVolumeSetups(context.Background(), tt.ssh, tt.existing, hostnames(tt.existing), t.TempDir()+"/a/b")
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

// hostCatSSH answers the zerops.yaml read like catSSH and records the hosts it
// is asked to reach.
type hostCatSSH struct {
	yaml  string
	mu    sync.Mutex
	hosts []string
}

func (s *hostCatSSH) ExecSSH(_ context.Context, host, _ string) ([]byte, error) {
	s.mu.Lock()
	s.hosts = append(s.hosts, host)
	s.mu.Unlock()
	return []byte(s.yaml), nil
}

// ExecSSHStdin answers one command as ExecSSH does; the input is not read.
func (s *hostCatSSH) ExecSSHStdin(ctx context.Context, host, command string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

func (*hostCatSSH) ExecSSHBackground(context.Context, string, string, time.Duration) ([]byte, error) {
	return nil, nil
}

// TestAdoptVolumeSetups_SkipsPool: a pool or a Box is never read for the
// repository's zerops.yaml — by service hostname that reaches a random pool
// container (docs/spec-workflows.md §8 PS-3) — so the setups come from the
// first real runtime, even when the pool is listed before it.
func TestAdoptVolumeSetups_SkipsPool(t *testing.T) {
	t.Parallel()
	existing := []platform.ServiceStack{
		{Name: "vol", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "local-storage:single@1"}},
		{Name: "swarm", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1"}},
		{Name: "sandbox", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "ubuntu/zbox@1"}},
		{Name: "webdev", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "bun@1.3"}},
	}
	yml := "zerops:\n  - setup: dev\n    run:\n      base: bun@1.3\n      volume:\n        hostname: vol\n        mountPath: /var/www/ws\n"
	ssh := &hostCatSSH{yaml: yml}
	got := adoptVolumeSetups(context.Background(), ssh, existing, hostnames(existing), t.TempDir()+"/a/b")
	if len(got) != 1 || got[0].Setup != "dev" {
		t.Fatalf("got %+v, want the dev setup read from webdev", got)
	}
	for _, h := range ssh.hosts {
		if h == "swarm" || h == "sandbox" {
			t.Errorf("SSH reached %q, want a pool and a Box never read; hosts = %v", h, ssh.hosts)
		}
	}
}

// hostnames are the services' hostnames, in order — a scope naming them all.
func hostnames(services []platform.ServiceStack) []string {
	out := make([]string, 0, len(services))
	for _, s := range services {
		out = append(out, s.Name)
	}
	return out
}

// repoSSH answers each host's zerops.yaml read with its own repository's file
// (a host without one answers the read failing) and the volume probe with
// what the host holds at the mount, recording every host it reaches.
type repoSSH struct {
	yaml    map[string]string
	content map[string]string
	mu      sync.Mutex
	hosts   []string
}

func (s *repoSSH) ExecSSH(_ context.Context, host, cmd string) ([]byte, error) {
	s.mu.Lock()
	s.hosts = append(s.hosts, host)
	s.mu.Unlock()
	if strings.HasPrefix(cmd, "cat ") {
		y, ok := s.yaml[host]
		if !ok {
			return nil, errors.New("cat: /var/www/zerops.yml: No such file or directory")
		}
		return []byte(y), nil
	}
	c, ok := s.content[host]
	if !ok {
		return nil, errors.New("ssh: unexpected command")
	}
	return []byte("@@zcp code-volume content " + c + "\n"), nil
}

// ExecSSHStdin answers one command as ExecSSH does; the input is not read.
func (s *repoSSH) ExecSSHStdin(ctx context.Context, host, command string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

func (*repoSSH) ExecSSHBackground(context.Context, string, string, time.Duration) ([]byte, error) {
	return nil, nil
}

// s2Project is the live project of the variant-B adopt (S2): the owner's
// app pair, listed first and holding its own repository's zerops.yaml, two
// managed services, and a variant-B repository on the volume lvmvol — two dev
// services mounting it at /var/www/ws, each running the setup named after it,
// and their stages, which deploy only their part and hold no zerops.yaml.
func s2Project() ([]platform.ServiceStack, *repoSSH) {
	svc := func(name, typ string) platform.ServiceStack {
		return platform.ServiceStack{ID: name + "-id", Name: name, Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: typ}}
	}
	ownerYml := "zerops:\n" +
		"  - setup: appdev\n    run:\n      base: nodejs@24\n      start: zsc noop --silent\n" +
		"  - setup: appstage\n    run:\n      base: nodejs@24\n      start: node server.js\n"
	volYml := "zerops:\n" +
		"  - setup: lvmwebdev\n    run:\n      base: python@3.12\n      os: ubuntu\n      volume:\n        hostname: lvmvol\n        mountPath: /var/www/ws\n      start: zsc noop --silent\n" +
		"  - setup: lvmapidev\n    run:\n      base: python@3.12\n      os: ubuntu\n      volume:\n        hostname: lvmvol\n        mountPath: /var/www/ws\n      start: zsc noop --silent\n" +
		"  - setup: lvmwebstage\n    run:\n      base: python@3.12\n      os: ubuntu\n      start: python3 -u web/server.py\n" +
		"  - setup: lvmapistage\n    run:\n      base: python@3.12\n      os: ubuntu\n      start: python3 -u api/server.py\n"
	existing := []platform.ServiceStack{
		svc("appstage", "alpine/nodejs@24"), svc("appdev", "alpine/nodejs@24"),
		svc("cache", "valkey:single@7.2"), svc("db", "postgresql:single@18"),
		svc("lvmapidev", "ubuntu/python@3.12"), svc("lvmapistage", "ubuntu/python@3.12"),
		svc("lvmvol", "local-storage:single@1"),
		svc("lvmwebdev", "ubuntu/python@3.12"), svc("lvmwebstage", "ubuntu/python@3.12"),
	}
	ssh := &repoSSH{
		yaml:    map[string]string{"appstage": ownerYml, "appdev": ownerYml, "lvmwebdev": volYml, "lvmapidev": volYml},
		content: map[string]string{"appstage": "absent", "appdev": "absent", "lvmwebdev": "checkout", "lvmapidev": "checkout", "lvmwebstage": "absent", "lvmapistage": "absent"},
	}
	return existing, ssh
}

// TestAdoptVolumeSetups_ReadsTheScopedRuntimesNotTheProjectsFirstRuntime: the
// setups mounting a volume are read through the runtimes the adopt scope
// names, each from its own repository's zerops.yaml — never from whichever
// runtime of the project sorts first. In S2 that was the owner's appstage,
// whose zerops.yaml mounts nothing, so the volume's repository was never read.
// Every setup carries the setups its file declares, and only the scoped
// runtimes are reached.
func TestAdoptVolumeSetups_ReadsTheScopedRuntimesNotTheProjectsFirstRuntime(t *testing.T) {
	t.Parallel()
	existing, ssh := s2Project()
	got := adoptVolumeSetups(context.Background(), ssh, existing, []string{"lvmwebdev", "lvmapidev", "lvmwebstage", "lvmapistage"}, t.TempDir()+"/a/b")
	if len(got) != 2 || got[0].Setup != "lvmwebdev" || got[1].Setup != "lvmapidev" {
		t.Fatalf("got %+v, want the setups lvmwebdev and lvmapidev mounting lvmvol", got)
	}
	files := []string{"lvmwebdev", "lvmapidev", "lvmwebstage", "lvmapistage"}
	both := map[string]topology.VolumeContent{"lvmwebdev": topology.VolumeCheckout, "lvmapidev": topology.VolumeCheckout}
	for _, s := range got {
		if s.Volume != "lvmvol" || s.MountPath != "/var/www/ws" || !s.WorksIn || !maps.Equal(s.Content, both) || !slices.Equal(s.FileSetups, files) {
			t.Errorf("setup %s = %+v, want lvmvol at /var/www/ws, worked in, content %v, file setups %v", s.Setup, s, both, files)
		}
	}
	for _, h := range ssh.hosts {
		if h == "appstage" || h == "appdev" {
			t.Errorf("SSH reached %q, which the scope does not name; hosts = %v", h, ssh.hosts)
		}
	}
}
