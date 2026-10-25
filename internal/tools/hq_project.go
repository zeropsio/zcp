package tools

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// reconcileProjectRepositories uses the same pair lock and pacing as Mate
// wiring. A branch refusal remains held until the checkout or identity changes.
func reconcileProjectRepositories(ctx context.Context, httpClient ops.HTTPDoer, ssh ops.SSHDeployer, rt runtime.Info, stateDir string, pace hqPassPace) []string {
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
	var self hq.ProjectIdentity
	var selfErr error
	readSelf := false
	identity := func() (hq.ProjectIdentity, error) {
		if !readSelf {
			self, selfErr = client.Self(ctx)
			readSelf = true
		}
		return self, selfErr
	}
	var report []string
	var keyRefusal error
	now := time.Now().UTC()
	for _, m := range metas {
		if !m.IsComplete() {
			continue
		}
		release, lockErr := workflow.LockPair(ctx, stateDir, m.Hostname, 0)
		if lockErr != nil {
			continue
		}
		// Read pacing and metadata while holding the same lock as the git sequence.
		state := readHQPairState(stateDir, m.Hostname)
		m, err = workflow.FindServiceMeta(stateDir, m.Hostname)
		if err != nil || m == nil || !m.IsComplete() || m.HQ != nil || (m.RemoteURL != "" && !hqRemoteIsThePairs(m.RemoteURL, id.Origin, m.Hostname)) {
			release()
			continue
		}
		if state.ProjectInput == id.Fingerprint() && pace == hqPassBackedOff && !hqAttemptDue(state, now) {
			release()
			continue
		}
		var line, refusal, checkout string
		var wired bool
		var passErr error
		if keyRefusal != nil {
			passErr = keyRefusal
		} else {
			line, wired, refusal, checkout, passErr = passProjectPair(ctx, client, ssh, id, identity, stateDir, m, state)
		}
		if passErr != nil {
			line = fmt.Sprintf("HQ project repository: %v", passErr)
		}
		recorded := nextHQAttempt(state, now, line, wired)
		recorded.ProjectInput, recorded.BranchRefusal, recorded.CheckoutState = id.Fingerprint(), refusal, checkout
		writeHQPairState(stateDir, m.Hostname, recorded)
		release()
		if line != "" {
			report = append(report, m.Hostname+": "+line)
		}
		var refused *hq.RefusedError
		if errors.As(passErr, &refused) && refused.Status == 401 && refused.Code == "key_refused" {
			keyRefusal = passErr
		}
	}
	return report
}

func projectCheckoutState(ctx context.Context, ssh ops.SSHDeployer, m *workflow.ServiceMeta) (string, error) {
	raw, err := ssh.ExecSSH(ctx, m.Hostname, ops.BuildProjectCheckoutStateCommand(hqPairWorkingDir(m)))
	if err != nil {
		return "", fmt.Errorf("read checkout state: %w", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}

func passProjectPair(ctx context.Context, client hq.ProjectClient, ssh ops.SSHDeployer, id hq.Identity, identity func() (hq.ProjectIdentity, error), stateDir string, m *workflow.ServiceMeta, state hqPairState) (line string, wired bool, refusal, checkout string, err error) {
	if state.BranchRefusal != "" && state.ProjectInput == id.Fingerprint() {
		checkout, err = projectCheckoutState(ctx, ssh, m)
		if err != nil {
			return "", false, state.BranchRefusal, state.CheckoutState, errors.New("could not inspect the refused checkout; its HQ setup remains pending")
		}
		if checkout == state.CheckoutState {
			return state.LastOutcome, false, state.BranchRefusal, checkout, nil
		}
	}
	raw, err := ssh.ExecSSH(ctx, m.Hostname, ops.BuildProjectOriginReadCommand(hqPairWorkingDir(m)))
	if err != nil {
		return "", false, "", "", errors.New("could not read Git origin; its HQ setup could not be checked")
	}
	origin := strings.TrimSpace(string(raw))
	if origin != "" && !hqRemoteIsThePairs(origin, id.Origin, m.Hostname) {
		return "", true, "", "", nil
	}
	if origin != "" && origin == m.RemoteURL {
		return "", true, "", "", nil
	}
	self, err := identity()
	if err != nil {
		return "", false, "", "", err
	}
	expected, err := client.RepoURL(self.OrgID, self.AppID, m.Hostname)
	if err != nil {
		return "", false, "", "", err
	}
	// HQ creates what it is asked for, so keep another placement before asking.
	if origin != "" && origin != expected {
		return "", true, "", "", nil
	}
	repo, err := client.EnsureRepo(ctx, m.Hostname)
	if err != nil {
		return "", false, "", "", err
	}
	remote, err := client.RepoURL(self.OrgID, repo.AppID, repo.Name)
	if err == nil {
		err = client.ProbeRepo(ctx, remote)
	}
	if err != nil {
		return "", false, "", "", err
	}
	out, err := ssh.ExecSSHStdin(ctx, m.Hostname, ops.BuildProjectRepositoryCommand(hqPairWorkingDir(m), remote, id.Origin), []byte(id.Key+"\n"))
	if err != nil {
		var sshErr *platform.SSHExecError
		if errors.As(err, &sshErr) {
			out = append(out, []byte("\n"+sshErr.Output)...)
		}
		refusal = ops.BaseBranchRefusal(string(out))
		if remedy := ops.BaseBranchRefusalRemedy(refusal); remedy != "" {
			checkout, err = projectCheckoutState(ctx, ssh, m)
			if err != nil {
				checkout = ""
			}
			remedy = strings.ReplaceAll(remedy, "git fetch ", "zcp hq git fetch ")
			return fmt.Sprintf("HQ project checkout refused (%s): in %s, %s", refusal, hqPairWorkingDir(m), remedy), false, refusal, checkout, nil
		}
		if err := client.ProbeRepo(ctx, remote); err != nil {
			return "", false, "", "", err
		}
		return "HQ project Git setup failed; inspect the checkout before trying again", false, "", "", nil
	}
	if strings.Contains(string(out), "ZCP_PROJECT_ORIGIN_KEPT") {
		return "", true, "", "", nil
	}
	if err := workflow.UpdateServiceMeta(stateDir, m.Hostname, func(meta *workflow.ServiceMeta) error { meta.RemoteURL = remote; return nil }); err != nil {
		return "", false, "", "", fmt.Errorf("record HQ project remote: %w", err)
	}
	return fmt.Sprintf("repository %q wired in HQ; from its mounted checkout use `zcp hq git fetch` and `zcp hq git push` with the project's key", repo.Name), true, "", "", nil
}
