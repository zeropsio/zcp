// Tests for: ops/code_volume.go and ops/delete.go — the reads the pool
// migration's retire makes of a variant-B pair's code volume before and while
// it deletes it (docs/spec-workflows.md §13 PM-13).
package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

const codeVolumeRetireYAML = `zerops:
  - setup: dev
    run:
      base: bun@1.3
      volume:
        hostname: vol
        mountPath: /var/www/ws
  - setup: dev-api
    run:
      base: go@1
      volume:
        hostname: vol
        mountPath: /var/www/ws
  - setup: web
    run:
      base: bun@1.3
      volume:
        hostname: vol
        mountPath: /var/www/ws
  - setup: api
    run:
      base: go@1
  - setup: uploads
    run:
      base: go@1
      volume:
        hostname: files
        mountPath: /var/www/files
`

// TestCodeVolumeMountingSetups_NamesEverySetupThatMountsTheVolume: the
// retire's pre-check reads the checkout's config file — zerops.yaml, else
// zerops.yml — over SSH and names every setup whose run.volume is the code
// volume, never one mounting another volume or none.
func TestCodeVolumeMountingSetups_NamesEverySetupThatMountsTheVolume(t *testing.T) {
	t.Parallel()
	for _, file := range []string{"zerops.yaml", "zerops.yml"} {
		t.Run(file, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, file), []byte(codeVolumeRetireYAML), 0o644); err != nil {
				t.Fatal(err)
			}
			ssh := &censusLocalSSH{}
			gotFile, setups, err := CodeVolumeMountingSetups(context.Background(), ssh, "webdev", root, "vol")
			if err != nil {
				t.Fatalf("CodeVolumeMountingSetups: %v", err)
			}
			if gotFile != file || !slices.Equal(setups, []string{"dev", "dev-api", "web"}) {
				t.Fatalf("file %q, setups %q; want %s naming dev, dev-api and web", gotFile, setups, file)
			}
			if len(ssh.calls) != 1 || !strings.HasPrefix(ssh.calls[0], "webdev: ") {
				t.Errorf("calls = %q, want one round trip on webdev", ssh.calls)
			}
		})
	}
}

func TestCodeVolumeMountingSetups_NoConfigOrBrokenConfig_Error(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{"no config file": "", "a config that does not parse": "zerops: [\n"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			if body != "" {
				if err := os.WriteFile(filepath.Join(root, "zerops.yaml"), []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if _, setups, err := CodeVolumeMountingSetups(context.Background(), &censusLocalSSH{}, "webdev", root, "vol"); err == nil {
				t.Fatalf("setups = %q, want an error: nothing read is never a volume nothing mounts", setups)
			}
		})
	}
}

// TestVolumeStillMounted_TheAsyncRefusal pins the platform's refusal of a
// volume delete while a service still mounts it, as live-verified: the delete
// answers a process that FAILS with no failReason and the error
// containerAction "still mounted", which the process read folds into
// FailReason. Any other outcome is not that refusal.
func TestVolumeStillMounted_TheAsyncRefusal(t *testing.T) {
	t.Parallel()
	refused := "containerAction: This Local Storage volume is still mounted by other services. Delete them first, or remove run.volume from their zerops.yaml and deploy."
	other := "containerAction: something else went wrong"
	tests := []struct {
		name string
		proc *platform.Process
		want bool
	}{
		{"the still-mounted refusal", &platform.Process{Status: platform.ProcessStatusFailed, FailReason: &refused}, true},
		{"another container failure", &platform.Process{Status: platform.ProcessStatusFailed, FailReason: &other}, false},
		{"a failure with no reason", &platform.Process{Status: platform.ProcessStatusFailed}, false},
		{"a finished delete", &platform.Process{Status: platform.ProcessStatusFinished}, false},
		{"no process", nil, false},
	}
	for _, tt := range tests {
		if got := VolumeStillMounted(tt.proc); got != tt.want {
			t.Errorf("%s: VolumeStillMounted = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// TestIsServiceGone_EveryNotFoundSpelling: a by-id read of a deleted service
// answers HTTP 400 serviceStackNotFound (live-verified), not only a 404 —
// both spellings, and the platform's plain serviceNotFound, read as gone.
func TestIsServiceGone_EveryNotFoundSpelling(t *testing.T) {
	t.Parallel()
	stackNotFound := platform.NewPlatformError(platform.ErrAPIError, "Service stack not found.", "")
	stackNotFound.APICode = "serviceStackNotFound"
	serviceNotFound := platform.NewPlatformError(platform.ErrAPIError, "Service not found.", "")
	serviceNotFound.APICode = "serviceNotFound"
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"404 service not found", platform.NewPlatformError(platform.ErrServiceNotFound, "not found", ""), true},
		{"400 serviceStackNotFound", stackNotFound, true},
		{"wrapped 400 serviceStackNotFound", errors.Join(errors.New("read service"), stackNotFound), true},
		{"serviceNotFound", serviceNotFound, true},
		{"another API error", platform.NewPlatformError(platform.ErrAPIError, "boom", ""), false},
		{"a plain error", errors.New("connection reset"), false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		if got := IsServiceGone(tt.err); got != tt.want {
			t.Errorf("%s: IsServiceGone = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// TestPoolMigrateEnvReferencesTo_EachRetiredServiceNamed: a variant-B retire
// deletes several services — every dev service and the code volume — and
// names, per one, every env variable that references it, the retired
// services' own env never among them.
func TestPoolMigrateEnvReferencesTo_EachRetiredServiceNamed(t *testing.T) {
	t.Parallel()
	services := []platform.ServiceStack{
		{ID: "svc-web", Name: "webdev"},
		{ID: "svc-api", Name: "apidev"},
		{ID: "svc-vol", Name: "vol"},
		{ID: "svc-webstage", Name: "webstage"},
	}
	mock := platform.NewMock().
		WithProjectEnv([]platform.ProjectEnvVar{{Key: "API", Content: "http://${apidev_hostname}"}}).
		WithServiceEnv("svc-web", []platform.ServiceEnvVar{{Key: "API", Content: "${apidev_hostname}"}}).
		WithServiceEnv("svc-api", []platform.ServiceEnvVar{{Key: "WEB", Content: "${webdev_hostname}"}}).
		WithServiceEnv("svc-vol", nil).
		WithServiceEnv("svc-webstage", []platform.ServiceEnvVar{{Key: "FILES", Content: "${vol_hostname}"}, {Key: "WEB", Content: "${webdev_zeropsSubdomain}"}})

	refs, unread, err := PoolMigrateEnvReferencesTo(context.Background(), mock, "proj-1", []string{"webdev", "apidev", "vol"}, services)
	if err != nil {
		t.Fatalf("PoolMigrateEnvReferencesTo: %v", err)
	}
	want := map[string][]string{"apidev": {"project API"}, "vol": {"webstage FILES"}, "webdev": {"webstage WEB"}}
	for host, w := range want {
		if !slices.Equal(refs[host], w) {
			t.Errorf("references to %s = %q, want %q", host, refs[host], w)
		}
	}
	if len(refs) != len(want) || len(unread) != 0 {
		t.Errorf("refs = %v, unread = %v", refs, unread)
	}
}
