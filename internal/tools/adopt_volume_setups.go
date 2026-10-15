package tools

import (
	"context"
	"path/filepath"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// adoptVolumeSetups are the setups of the repository's zerops.yaml that mount
// a local-storage volume of the project — what the adopt route recognises
// variant B from (docs/spec-mate.md §2.8a). The discover step runs before the
// runtimes are mounted, so the file is read from the first runtime container
// that holds one (/var/www), and from its mount when the container is out of
// reach; none yields nothing.
func adoptVolumeSetups(ctx context.Context, ssh ops.SSHDeployer, existing []platform.ServiceStack, stateDir string) []workflow.AdoptVolumeSetup {
	volumes := map[string]bool{}
	for _, svc := range existing {
		if topology.IsLocalStorageType(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName) {
			volumes[svc.Name] = true
		}
	}
	if len(volumes) == 0 {
		return nil
	}
	root := projectRootFromState(stateDir)
	for _, svc := range existing {
		if svc.IsSystem() || topology.IsManagedService(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName) {
			continue
		}
		doc := readAdoptZeropsYml(ctx, ssh, svc.Name, filepath.Join(root, svc.Name))
		if doc == nil {
			continue
		}
		var out []workflow.AdoptVolumeSetup
		for _, entry := range doc.Zerops {
			if v := entry.Run.Volume; volumes[v.Hostname] {
				out = append(out, workflow.AdoptVolumeSetup{Setup: entry.Setup, RunBase: entry.Run.Base, Volume: v.Hostname, MountPath: v.MountPath})
			}
		}
		return out
	}
	return nil
}

func readAdoptZeropsYml(ctx context.Context, ssh ops.SSHDeployer, host, mount string) *ops.ZeropsYmlDoc {
	if ssh != nil {
		out, err := ssh.ExecSSH(ctx, host, "cat /var/www/zerops.yaml 2>/dev/null || cat /var/www/zerops.yml")
		if err == nil {
			if doc, perr := ops.ParseZeropsYmlContent(out, ""); perr == nil && len(doc.Zerops) > 0 {
				return doc
			}
		}
	}
	doc, err := ops.ParseZeropsYml(mount)
	if err != nil {
		return nil
	}
	return doc
}
