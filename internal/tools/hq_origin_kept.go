package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// recordHQOrigin records on a wired pair how the Mate's own state in HQ says
// its repository's code is kept (workflow.HQRepoRef.Origin), so status names
// it without asking HQ. A state from an HQ older than repositories records
// the way unknown (Mode ""), so status does not ask that HQ on every call; a
// pass asks again. A state of another application, or not naming the
// repository, leaves the record as it is.
func recordHQOrigin(stateDir string, m *workflow.ServiceMeta, state hq.MateState) {
	origin, ok := hqOriginIn(state, m.HQ)
	if !ok || m.HQ.Origin != nil && *m.HQ.Origin == origin {
		return
	}
	_ = workflow.UpsertServiceMeta(stateDir, m.Hostname, func(meta *workflow.ServiceMeta, existed bool) error {
		if existed && meta.HQ != nil && meta.HQ.AppID == m.HQ.AppID && meta.HQ.Repo == m.HQ.Repo {
			meta.HQ.Origin = &origin
		}
		return nil
	})
	m.HQ.Origin = &origin
}

// hqOriginIn is how state says the code of the repository ref names is kept,
// unknown (Mode "") from an HQ older than repositories; false when state
// cannot say: it does not name that repository, or holds the Mate in another
// application.
func hqOriginIn(state hq.MateState, ref *workflow.HQRepoRef) (workflow.HQOrigin, bool) {
	if state.AppID == nil || *state.AppID != ref.AppID {
		return workflow.HQOrigin{}, false
	}
	if state.Repos == nil {
		return workflow.HQOrigin{}, true
	}
	for _, repo := range state.Repos {
		switch {
		case repo.Name != ref.Repo:
			continue
		case repo.Origin == nil:
			return workflow.HQOrigin{Mode: workflow.HQOriginNone}, true
		}
		return workflow.HQOrigin{Mode: repo.Origin.Mode, URL: repo.Origin.URL}, true
	}
	return workflow.HQOrigin{}, false
}

// hqPairOrigin points a wired pair's checkout's origin where its record says
// the code is kept — the one writer of it once the pair is wired: on GitHub,
// the GitHub repository, git asking only zcp for it (hq.GitHubHelperConfig);
// in HQ or followed, its repository in HQ. A record that does not say leaves
// origin as it is. Never a second remote: what zcp needs of HQ it reaches at
// HQ's own address (ops.BuildMainSyncCommand). The line says a move, "" for
// none.
func hqPairOrigin(ctx context.Context, sshDeployer ops.SSHDeployer, hqc hq.Client, m *workflow.ServiceMeta) string {
	url := hqPairOriginTarget(hqc.Address(), hqOrganisation(), m)
	if url == "" {
		return ""
	}
	helper := ""
	if m.HQ.Origin.Mode == hq.OriginOnGitHub {
		helper = hq.GitHubHelperConfig + " && "
	}
	// Wiring again keeps the origin it replaces as a remote of its own
	// (ops.BuildGitOriginSyncCommand); one naming where origin points is a
	// second remote of the same repository, and goes.
	out, err := sshDeployer.ExecSSH(ctx, m.Hostname, fmt.Sprintf(
		`cd %s && %s{ [ "$(git remote get-url %s 2>/dev/null)" != %s ] || git remote remove %s 2>/dev/null || true; } && cur=$(git remote get-url origin 2>/dev/null || true); if [ "$cur" != %s ]; then { git remote set-url origin %s 2>/dev/null || git remote add origin %s; } && echo %s; fi`,
		ops.ShellQuote(hqPairWorkingDir(m)), helper, ops.KeptOriginRemote, ops.ShellQuote(url), ops.KeptOriginRemote,
		ops.ShellQuote(url), ops.ShellQuote(url), ops.ShellQuote(url), originMovedMarker))
	switch {
	case err != nil:
		return fmt.Sprintf("its checkout's origin could not be pointed at %s, where its code is kept (%v) — retrying on the next pass", url, err)
	case !strings.Contains(string(out), originMovedMarker):
		return ""
	case helper != "":
		return fmt.Sprintf("its code is kept on GitHub, so its checkout's origin is %s now: fetch and push it in your own shell on the mounted checkout, never over ssh", url)
	}
	return fmt.Sprintf("its code is kept in HQ, so its checkout's origin is its repository there again (%s)", url)
}

// hqPairOriginTarget is where the wiring points a wired pair's checkout's
// origin (hqPairOrigin) by its record of how the code is kept: on GitHub, the
// GitHub repository; in HQ or followed, its repository at the HQ at address.
// "" when the record does not say, or HQ's address is unknown.
func hqPairOriginTarget(address, orgID string, m *workflow.ServiceMeta) string {
	if m.HQ.Origin == nil {
		return ""
	}
	switch m.HQ.Origin.Mode {
	case hq.OriginOnGitHub:
		return m.HQ.Origin.URL
	case hq.OriginFollowed, workflow.HQOriginNone:
		if address == "" {
			return ""
		}
		target, err := hq.RepoURLAt(address, orgID, m.HQ.AppID, m.HQ.Repo)
		if err != nil {
			return ""
		}
		return target
	}
	return ""
}

// hqCheckoutOriginBound bounds status's read of the checkouts' origins: a
// dev service that is stopped or restarting never holds status for long.
const hqCheckoutOriginBound = 5 * time.Second

// attachHQCheckoutOrigin reads, on each wired pair's dev half, its checkout's
// origin as it is now, and where the wiring points it when that differs
// (hqPairOriginTarget): status names what git pushes to, never only what the
// record says it will be. Container only; a read that fails, or does not
// answer within hqCheckoutOriginBound, leaves the half without it. It writes
// only these two fields of services, so it runs beside attachRepoStatus.
func attachHQCheckoutOrigin(ctx context.Context, services []workflow.ServiceSnapshot, ssh ops.SSHDeployer, rt runtime.Info, stateDir string) {
	if !rt.InContainer || ssh == nil {
		return
	}
	address, orgID := hqAddress(), hqOrganisation()
	ctx, cancel := context.WithTimeout(ctx, hqCheckoutOriginBound)
	defer cancel()
	var wg sync.WaitGroup
	sem := make(chan struct{}, attachRepoStatusConcurrency)
	for i := range services {
		if services[i].HQRepo == "" {
			continue
		}
		m, err := workflow.FindServiceMeta(stateDir, services[i].Hostname)
		if err != nil || !hqPairWired(m) || m.Hostname != services[i].Hostname {
			continue
		}
		wg.Add(1)
		go func(i int, m *workflow.ServiceMeta) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out, err := ssh.ExecSSH(ctx, m.Hostname, "cd "+ops.ShellQuote(hqPairWorkingDir(m))+" && { git remote get-url origin 2>/dev/null || true; }")
			if err != nil {
				return
			}
			now := strings.TrimSpace(string(out))
			if now == "" {
				now = workflow.HQOriginNone
			}
			// Each goroutine writes only its own index.
			services[i].HQCheckoutOrigin = now
			if target := hqPairOriginTarget(address, orgID, m); target != "" && target != now {
				services[i].HQOriginPending = target
			}
		}(i, m)
	}
	wg.Wait()
}

// originMovedMarker is what hqPairOrigin's command says when it moved origin.
const originMovedMarker = "ZCP_ORIGIN_MOVED"

// hqKeptOnGitHub is, per wired pair whose code its record says is kept on
// GitHub, the line that tells develop's start how it is worked on: its
// checkout's origin is GitHub (hqPairOrigin points it there), so the agent
// pushes its branch to origin and opens its change. A pair kept in HQ, or
// followed, is worked on as every other.
func hqKeptOnGitHub(stateDir string) []string {
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil {
		return nil
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].Hostname < metas[j].Hostname })
	var lines []string
	for _, m := range metas {
		if !hqPairWired(m) || m.HQ.Origin == nil || m.HQ.Origin.Mode != hq.OriginOnGitHub {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: its code is kept on GitHub at %s, and its checkout's origin is GitHub: %s",
			m.Hostname, m.HQ.Origin.URL, originOnGitHub(originCheckout(stateDir, m.Hostname))))
	}
	return lines
}

// hqStatusOriginBound bounds status's one read of how a wired pair's code is
// kept: status must answer promptly whether HQ does or not.
const hqStatusOriginBound = 5 * time.Second

// recordMissingHQOrigins asks HQ, once and bounded, how the code of each
// wired pair with no record of it is kept — a pair wired before zcp recorded
// it — and records it, so status names it. HQ not answering leaves the
// record absent, which status names unknown.
func recordMissingHQOrigins(ctx context.Context, httpClient ops.HTTPDoer, stateDir string) {
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil {
		return
	}
	var missing []*workflow.ServiceMeta
	for _, m := range metas {
		if hqPairWired(m) && m.HQ.Origin == nil {
			missing = append(missing, m)
		}
	}
	if len(missing) == 0 {
		return
	}
	hqc, linked := openHQ(httpClient)
	if !linked {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, hqStatusOriginBound)
	defer cancel()
	state, err := hqc.Once().Self(ctx)
	if err != nil {
		return
	}
	for _, m := range missing {
		recordHQOrigin(stateDir, m, state)
	}
}
