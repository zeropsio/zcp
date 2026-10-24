package tools

import (
	"context"
	"path/filepath"
	"slices"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// adoptVolumeSetups are the setups that mount a local-storage volume of the
// project, read through the runtimes the adopt scope names — each from its
// own repository's zerops.yaml, never from whichever runtime of the project
// sorts first — with whether each works in the mount, the setups its file
// declares, and what the runtimes holding it find there: what the adopt route
// recognises variant B from (docs/spec-mate.md §2.8a.4a). The discover step
// runs before the runtimes are mounted, so each file is read from the runtime
// container (/var/www), and from its mount when the container is out of
// reach; a runtime holding none adds nothing. A setup the files of several
// runtimes declare alike is one setup, held by each of them.
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
	scoped := make(map[string]bool, len(scope))
	for _, h := range scope {
		scoped[h] = true
	}
	root := projectRootFromState(stateDir)
	var out []workflow.AdoptVolumeSetup
	var holders [][]string
	index := map[adoptSetupKey]int{}
	for _, svc := range existing {
		if !scoped[svc.Name] || !isAdoptRuntime(svc) {
			continue
		}
		doc := readAdoptZeropsYml(ctx, ssh, svc.Name, filepath.Join(root, svc.Name))
		if doc == nil {
			continue
		}
		files := doc.SetupNames()
		for _, entry := range doc.Zerops {
			v := entry.Run.Volume
			if !volumes[v.Hostname] {
				continue
			}
			key := adoptSetupKey{setup: entry.Setup, runBase: entry.Run.Base, volume: v.Hostname, mountPath: v.MountPath, worksIn: entry.WorksIn(v.MountPath)}
			i, seen := index[key]
			if !seen {
				i = len(out)
				index[key] = i
				out = append(out, workflow.AdoptVolumeSetup{Setup: key.setup, RunBase: key.runBase, Volume: key.volume, MountPath: key.mountPath, WorksIn: key.worksIn})
				holders = append(holders, nil)
			}
			holders[i] = append(holders[i], svc.Name)
			for _, f := range files {
				if !slices.Contains(out[i].FileSetups, f) {
					out[i].FileSetups = append(out[i].FileSetups, f)
				}
			}
		}
	}
	typeByHost := make(map[string]string, len(existing))
	for _, svc := range existing {
		typeByHost[svc.Name] = svc.ServiceStackTypeInfo.ServiceStackTypeVersionName
	}
	probeAdoptVolumes(ctx, ssh, typeByHost, out, holders)
	return out
}

// adoptSetupKey is what makes two runtimes' setups one: the same name, base
// and mount, worked in alike.
type adoptSetupKey struct {
	setup, runBase, volume, mountPath string
	worksIn                           bool
}

// probeAdoptVolumes reads what each runtime holding a setup — its own file
// declares it — of the setup's run.base finds at the mount of a volume that
// ≥2 setups work in: the content proof adopt needs before it takes the
// volume for a code volume. A runtime that cannot be read proves nothing and
// stays out of Content.
func probeAdoptVolumes(ctx context.Context, ssh ops.SSHDeployer, typeByHost map[string]string, setups []workflow.AdoptVolumeSetup, holders [][]string) {
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
		for _, host := range holders[i] {
			if topology.CanonicalBaseName(typeByHost[host]) != topology.CanonicalBaseName(s.RunBase) {
				continue
			}
			key := [2]string{host, s.MountPath}
			content, done := seen[key]
			if !done {
				content, _ = ops.CodeVolumeContent(ctx, ssh, host, s.MountPath)
				seen[key] = content
			}
			if content != "" {
				setups[i].Content[host] = content
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
