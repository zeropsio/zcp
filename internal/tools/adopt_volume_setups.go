package tools

import (
	"path/filepath"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// adoptVolumeSetups are the setups of the repository's zerops.yaml that mount
// a local-storage volume of the project — what the adopt route recognises
// variant B from (docs/spec-mate.md §2.8a). The repository is read from the
// first runtime's mount that holds one; none yields nothing.
func adoptVolumeSetups(existing []platform.ServiceStack, stateDir string) []workflow.AdoptVolumeSetup {
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
		doc, err := ops.ParseZeropsYml(filepath.Join(root, svc.Name))
		if err != nil {
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
