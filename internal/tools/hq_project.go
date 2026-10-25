package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// reconcileProjectRepositories is the plain-project branch of repository
// setup. Its remote is ordinary Git storage: no Mate changes, recipe or
// release record, and no copy of the project key in the dev service.
func reconcileProjectRepositories(ctx context.Context, httpClient ops.HTTPDoer, ssh ops.SSHDeployer, rt runtime.Info, stateDir string) []string {
	if httpClient == nil {
		return nil
	}
	id := hqIdentity(rt)
	client, err := hq.OpenProject(httpClient, id)
	if err != nil {
		return nil
	}
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil {
		return []string{fmt.Sprintf("HQ project repositories: %v", err)}
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].Hostname < metas[j].Hostname })
	var report []string
	var self hq.ProjectIdentity
	readSelf := false
	for _, m := range metas {
		if !m.IsComplete() || m.HQ != nil {
			continue
		}
		if m.RemoteURL != "" && !ops.IsHQRemote(m.RemoteURL, id.Origin) {
			continue
		}
		raw, err := ssh.ExecSSH(ctx, m.Hostname, ops.BuildProjectOriginReadCommand(hqPairWorkingDir(m)))
		if err != nil {
			report = append(report, m.Hostname+": could not read Git origin; nothing enrolled in HQ")
			continue
		}
		origin := strings.TrimSpace(string(raw))
		if origin != "" && !ops.IsHQRemote(origin, id.Origin) {
			continue
		}
		if origin != "" && origin == m.RemoteURL {
			continue
		}
		if !readSelf {
			self, err = client.Self(ctx)
			if err != nil {
				return append(report, fmt.Sprintf("HQ project enrollment: %v", err))
			}
			readSelf = true
		}
		repo, err := client.EnsureRepo(ctx, m.Hostname)
		if err != nil {
			report = append(report, fmt.Sprintf("%s: HQ project repository: %v", m.Hostname, err))
			continue
		}
		remote, err := client.RepoURL(self.OrgID, repo.AppID, repo.Name)
		if err == nil {
			err = client.ProbeRepo(ctx, remote)
		}
		if err != nil {
			report = append(report, fmt.Sprintf("%s: HQ project Git: %v", m.Hostname, err))
			continue
		}
		out, err := ssh.ExecSSHStdin(ctx, m.Hostname, ops.BuildProjectRepositoryCommand(hqPairWorkingDir(m), remote, id.Origin), []byte(id.Key+"\n"))
		if err != nil {
			// A key can be refused between the probe and Git's fetch. Ask the
			// known target again for its typed refusal; never replay the fetch.
			if probeErr := client.ProbeRepo(ctx, remote); probeErr != nil {
				report = append(report, fmt.Sprintf("%s: HQ project Git: %v", m.Hostname, probeErr))
			} else {
				report = append(report, m.Hostname+": HQ project Git setup failed; inspect the checkout before trying again")
			}
			continue
		}
		if strings.Contains(string(out), "ZCP_PROJECT_ORIGIN_KEPT") {
			continue
		}
		if err := workflow.UpdateServiceMeta(stateDir, m.Hostname, func(meta *workflow.ServiceMeta) error { meta.RemoteURL = remote; return nil }); err != nil {
			report = append(report, fmt.Sprintf("%s: record HQ project remote: %v", m.Hostname, err))
			continue
		}
		report = append(report, fmt.Sprintf("%s: repository %q wired in HQ; from its mounted checkout use `zcp hq git fetch` and `zcp hq git push` with the project's key", m.Hostname, repo.Name))
	}
	return report
}
