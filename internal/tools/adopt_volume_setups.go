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
// a local-storage volume of the project, with whether each works in the mount
// and what the runtimes find there — what the adopt route recognises variant
// B from (docs/spec-mate.md §2.8a). The discover step runs before the
// runtimes are mounted, so the file is read from the first runtime container
// that holds one (/var/www), and from its mount when the container is out of
// reach; none yields nothing.
func adoptVolumeSetups(ctx context.Context, ssh ops.SSHDeployer, existing []platform.ServiceStack, scope []string, stateDir string) []workflow.AdoptVolumeSetup {
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
		if !isAdoptRuntime(svc) {
			continue
		}
		doc := readAdoptZeropsYml(ctx, ssh, svc.Name, filepath.Join(root, svc.Name))
		if doc == nil {
			continue
		}
		var out []workflow.AdoptVolumeSetup
		for _, entry := range doc.Zerops {
			if v := entry.Run.Volume; volumes[v.Hostname] {
				out = append(out, workflow.AdoptVolumeSetup{Setup: entry.Setup, RunBase: entry.Run.Base, Volume: v.Hostname, MountPath: v.MountPath,
					WorksIn: entry.WorksIn(v.MountPath)})
			}
		}
		probeAdoptVolumes(ctx, ssh, existing, out)
		return out
	}
	return nil
}

// probeAdoptVolumes reads what each runtime of a setup's run.base finds at
// the mount of a volume that ≥2 setups work in — the content proof adopt
// needs before it takes the volume for a code volume. A runtime that cannot
// be read proves nothing and stays out of Content.
func probeAdoptVolumes(ctx context.Context, ssh ops.SSHDeployer, existing []platform.ServiceStack, setups []workflow.AdoptVolumeSetup) {
	if ssh == nil {
		return
	}
	workers := map[string]int{}
	for _, s := range setups {
		if s.WorksIn {
			workers[s.Volume]++
		}
	}
	seen := map[[2]string]topology.VolumeContent{}
	for i, s := range setups {
		if workers[s.Volume] < 2 {
			continue
		}
		setups[i].Content = map[string]topology.VolumeContent{}
		for _, svc := range existing {
			if !isAdoptRuntime(svc) || topology.CanonicalBaseName(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName) != topology.CanonicalBaseName(s.RunBase) {
				continue
			}
			key := [2]string{svc.Name, s.MountPath}
			content, done := seen[key]
			if !done {
				content, _ = ops.CodeVolumeContent(ctx, ssh, svc.Name, s.MountPath)
				seen[key] = content
			}
			if content != "" {
				setups[i].Content[svc.Name] = content
			}
		}
	}
}

// isAdoptRuntime reports a service that holds the repository's code in one
// container reachable by its hostname: not a system service, a managed
// service, a pool (a hostname reaches a random container of it) or a Box
// (docs/spec-workflows.md §8 PS-3).
func isAdoptRuntime(svc platform.ServiceStack) bool {
	return !svc.IsSystem() && topology.RuntimeClassFor(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName).IsAppRuntime()
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
