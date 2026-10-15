package tools

import (
	"context"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

type catSSH struct{ yaml string }

func (s catSSH) ExecSSH(context.Context, string, string) ([]byte, error) { return []byte(s.yaml), nil }

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
