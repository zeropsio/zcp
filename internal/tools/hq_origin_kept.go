package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/workflow"
)

// recordHQOrigin records on a wired pair how the Mate's own state in HQ says
// its repository's code is kept (workflow.HQRepoRef.Origin), so status names
// it without asking HQ. A state that names no repositories, from an older HQ
// or of another application, leaves the record as it is.
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

// hqOriginIn is how state says the code of the repository ref names is kept;
// false when state cannot say: it names no repositories, or not that one, or
// holds the Mate in another application.
func hqOriginIn(state hq.MateState, ref *workflow.HQRepoRef) (workflow.HQOrigin, bool) {
	if state.Repos == nil || state.AppID == nil || *state.AppID != ref.AppID {
		return workflow.HQOrigin{}, false
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
	if m.HQ.Origin == nil {
		return ""
	}
	url, helper := hqc.RepoURL(m.HQ.AppID, m.HQ.Repo), ""
	switch m.HQ.Origin.Mode {
	case hq.OriginOnGitHub:
		if m.HQ.Origin.URL == "" {
			return ""
		}
		url, helper = m.HQ.Origin.URL, hq.GitHubHelperConfig+" && "
	case hq.OriginFollowed, workflow.HQOriginNone:
	default:
		return ""
	}
	out, err := sshDeployer.ExecSSH(ctx, m.Hostname, fmt.Sprintf(
		`cd %s && %scur=$(git remote get-url origin 2>/dev/null || true); if [ "$cur" != %s ]; then { git remote set-url origin %s 2>/dev/null || git remote add origin %s; } && echo %s; fi`,
		ops.ShellQuote(hqPairWorkingDir(m)), helper, ops.ShellQuote(url), ops.ShellQuote(url), ops.ShellQuote(url), originMovedMarker))
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
	hqc, enrolled := openHQ(httpClient)
	if !enrolled {
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
