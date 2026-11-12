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
// repository, leaves the record as it is, and a pair gone from disk meanwhile
// (a pool migration re-keyed it) keeps no stub.
func recordHQOrigin(stateDir string, m *workflow.ServiceMeta, state hq.MateState) {
	origin, ok := hqOriginIn(state, m.HQ)
	if !ok || m.HQ.Origin != nil && *m.HQ.Origin == origin {
		return
	}
	_ = workflow.UpsertServiceMeta(stateDir, m.Hostname, func(meta *workflow.ServiceMeta, existed bool) error {
		if !existed || meta.HQ == nil || meta.HQ.AppID != m.HQ.AppID || meta.HQ.Repo != m.HQ.Repo {
			return workflow.ErrSkipWrite
		}
		meta.HQ.Origin = &origin
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

// attachHQStateOrigins projects the current status from the last heard state.
func attachHQStateOrigins(services []workflow.ServiceSnapshot, stateDir string, state *hq.MateState) {
	for i := range services {
		if services[i].HQRepo == "" {
			continue
		}
		services[i].HQOrigin = nil
		if state == nil {
			continue
		}
		m, err := workflow.FindServiceMeta(stateDir, services[i].Hostname)
		if err == nil && hqPairWired(m) {
			if origin, ok := hqOriginIn(*state, m.HQ); ok {
				services[i].HQOrigin = &origin
			}
		}
	}
}

// hqPairOrigin points a wired pair's checkout's origin at its repository in
// HQ — the one writer of it once the pair is wired — however the record says
// its code is kept: in HQ, followed, or on GitHub, which HQ mirrors both
// ways and alone pushes to. A checkout an older zcp pointed at GitHub moves
// here; the helper that zcp wrote into it for github.com stays
// (hq.GitHubHelperConfig), where it brings a read token and nothing more. A
// record that does not say leaves origin as it is. Never a second remote:
// what zcp needs of GitHub it reaches by its address (divergedWords). A
// move off GitHub is recorded on the pair for develop's start to tell the
// agent once (recordHQOriginMoved, tellHQOriginMoved) — the Mate's start
// usually makes it, and its lines reach only its log — and said here only
// when it could not be recorded; any other move is said here, "" for none;
// a repository's address that cannot be built is said, not skipped. A pair
// keyed by its dev pool has no checkout to point — the pool's hostname
// answers from a random container, possibly someone's lease — so nothing
// moves: a lease's origin is the repository it checks out.
func hqPairOrigin(ctx context.Context, sshDeployer ops.SSHDeployer, stateDir string, hqc hq.Client, m *workflow.ServiceMeta) string {
	if m.DevIsPool() {
		return ""
	}
	url, err := hqPairOriginTarget(hqc.Address(), hqc.OrgID(), m)
	if err != nil {
		return fmt.Sprintf("its checkout's origin could not be pointed at its repository in HQ: %v", err)
	}
	if url == "" {
		return ""
	}
	// Wiring again keeps the origin it replaces as a remote of its own
	// (ops.BuildGitOriginSyncCommand); one naming where origin points, or the
	// GitHub repository HQ mirrors, is a second remote of the same
	// repository, and goes. The second is the wiring having moved origin off
	// GitHub itself, and is said as that move.
	var drop strings.Builder
	fmt.Fprintf(&drop, `{ [ "$(git remote get-url %s 2>/dev/null)" != %s ] || git remote remove %s 2>/dev/null || true; } && `,
		ops.KeptOriginRemote, ops.ShellQuote(url), ops.KeptOriginRemote)
	mirrored := m.HQ.Origin.Mode == hq.OriginOnGitHub && m.HQ.Origin.URL != ""
	if mirrored {
		fmt.Fprintf(&drop, `{ [ "$(git remote get-url %s 2>/dev/null)" != %s ] || { git remote remove %s 2>/dev/null; echo %s %s; }; } && `,
			ops.KeptOriginRemote, ops.ShellQuote(m.HQ.Origin.URL), ops.KeptOriginRemote, originMovedMarker, ops.ShellQuote(m.HQ.Origin.URL))
	}
	out, err := sshDeployer.ExecSSH(ctx, m.Hostname, fmt.Sprintf(
		`cd %s && %scur=$(git remote get-url origin 2>/dev/null || true); if [ "$cur" != %s ]; then { git remote set-url origin %s 2>/dev/null || git remote add origin %s; } && echo %s "$cur"; fi`,
		ops.ShellQuote(hqPairWorkingDir(m)), drop.String(),
		ops.ShellQuote(url), ops.ShellQuote(url), ops.ShellQuote(url), originMovedMarker))
	was, moved := originMovedFrom(string(out))
	switch {
	case err != nil:
		return fmt.Sprintf("its checkout's origin could not be pointed at its repository in HQ, %s (%v) — retrying on the next pass", url, err)
	case !moved:
		return ""
	case mirrored && sameGitRepository(was, m.HQ.Origin.URL):
		moved := workflow.HQOriginMoved{From: m.HQ.Origin.URL, To: url}
		if recordHQOriginMoved(stateDir, m, moved) {
			return ""
		}
		// Said now rather than never.
		return originMovedWords(m.Hostname, moved)
	}
	return fmt.Sprintf("its checkout's origin is its repository in HQ again (%s)", url)
}

// originMovedWords is what the agent is told of a checkout of host whose
// origin a pass moved off GitHub: where it points now, that HQ alone pushes
// to GitHub, and what becomes of a branch this container pushed there
// itself — HQ pushes on only to branches it put on GitHub, so such work is
// finished under a new branch name, as a new change.
func originMovedWords(host string, moved workflow.HQOriginMoved) string {
	route := repoRoute{host: host}
	return fmt.Sprintf("its checkout's origin is its repository in HQ now (%s), no longer GitHub: its code is still on GitHub at %s, which HQ mirrors both ways and alone pushes to. "+
		"Take HQ's refs in first (%s), then push your branch to origin (%s) — HQ puts a change's branch on GitHub and opens its pull request. "+
		"A branch you pushed to GitHub yourself is not carried on there: HQ pushes on only to branches it put on GitHub, so that branch's pull request stops following once you push it to origin. "+
		"Finish such work on a new branch (%s) pushed to origin, open a new change for it with open-change, and tell the person the earlier pull request is superseded; "+
		"a branch not pushed anywhere yet, and a change whose branch HQ put on GitHub, go on as they are. "+
		"`zcp git` refuses a push to GitHub, and GitHub's default branch moves only by a merge",
		moved.To, moved.From, route.git("fetch origin"), route.git("push origin <branch>"), route.git("checkout -b <new branch>"))
}

// recordHQOriginMoved records on the pair that a pass moved its checkout's
// origin off GitHub, for develop's start to tell the agent
// (tellHQOriginMoved): the record is the pair's own file, so it outlives the
// restart a zcp update is. false when it could not be written, or the pair
// is gone from disk or wired to another repository meanwhile.
func recordHQOriginMoved(stateDir string, m *workflow.ServiceMeta, moved workflow.HQOriginMoved) bool {
	recorded := false
	err := workflow.UpsertServiceMeta(stateDir, m.Hostname, func(meta *workflow.ServiceMeta, existed bool) error {
		if !existed || meta.HQ == nil || meta.HQ.AppID != m.HQ.AppID || meta.HQ.Repo != m.HQ.Repo {
			return workflow.ErrSkipWrite
		}
		meta.HQ.OriginMoved = &moved
		recorded = true
		return nil
	})
	if err != nil || !recorded {
		return false
	}
	m.HQ.OriginMoved = &moved
	return true
}

// tellHQOriginMoved is, per wired pair whose checkout's origin a pass moved
// off GitHub since the agent was last told, the line develop's start says of
// it (originMovedWords) — once: the record is cleared in the hold it is read
// in, so of two starts at once only one says it, and one that cannot clear
// it says nothing and leaves it for the next. A pair no longer kept on
// GitHub, or keyed by its dev pool since, has nothing to be told: its record
// is cleared unsaid.
func tellHQOriginMoved(stateDir string) []string {
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil {
		return nil
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].Hostname < metas[j].Hostname })
	var lines []string
	for _, m := range metas {
		if !hqPairWired(m) || m.HQ.OriginMoved == nil {
			continue
		}
		var moved *workflow.HQOriginMoved
		err := workflow.UpsertServiceMeta(stateDir, m.Hostname, func(meta *workflow.ServiceMeta, existed bool) error {
			if !existed || meta.HQ == nil || meta.HQ.OriginMoved == nil {
				return workflow.ErrSkipWrite
			}
			moved, meta.HQ.OriginMoved = meta.HQ.OriginMoved, nil
			return nil
		})
		if err != nil || moved == nil || m.DevIsPool() || m.HQ.Origin == nil || m.HQ.Origin.Mode != hq.OriginOnGitHub {
			continue
		}
		lines = append(lines, m.Hostname+": "+originMovedWords(m.Hostname, *moved))
	}
	return lines
}

// originMovedFrom reads what hqPairOrigin's command said: whether origin
// moved — by it, or by the wiring before it, whose kept remote it dropped —
// and the address origin had before, "" for none.
func originMovedFrom(out string) (was string, moved bool) {
	_, rest, moved := strings.Cut(out, originMovedMarker)
	was, _, _ = strings.Cut(rest, "\n")
	return strings.TrimSpace(was), moved
}

// hqPairOriginTarget is where the wiring points a wired pair's checkout's
// origin (hqPairOrigin): its repository at the HQ at address, however its
// record says the code is kept — in HQ, followed, or on GitHub, which HQ
// mirrors. "" when the record does not say, or HQ's address is unknown;
// hq.ErrNoOrganisation when orgID is empty.
func hqPairOriginTarget(address, orgID string, m *workflow.ServiceMeta) (string, error) {
	if m.HQ.Origin == nil {
		return "", nil
	}
	switch m.HQ.Origin.Mode {
	case hq.OriginOnGitHub, hq.OriginFollowed, workflow.HQOriginNone:
		if address == "" {
			return "", nil
		}
		return hq.RepoURLAt(address, orgID, m.HQ.AppID, m.HQ.Repo)
	}
	return "", nil
}

// hqCheckoutOriginBound bounds status's read of the checkouts' origins: a
// dev service that is stopped or restarting never holds status for long.
const hqCheckoutOriginBound = 5 * time.Second

// attachHQCheckoutOrigin reads, on each wired pair's dev half, its checkout's
// origin as it is now, and where the wiring points it when that differs
// (hqPairOriginTarget) — a checkout an older zcp pointed at GitHub, until a
// pass moves it to HQ: status names what git pushes to, never only what the
// record says it will be. Container only; a read that fails, or does not
// answer within hqCheckoutOriginBound, leaves the half without it. It writes
// only these two fields of services, so it runs beside attachRepoStatus. A
// pool keying its pair holds no checkout of its own — its hostname answers
// from a random container — and is read for none.
func attachHQCheckoutOrigin(ctx context.Context, services []workflow.ServiceSnapshot, ssh ops.SSHDeployer, rt runtime.Info, stateDir string) {
	if !rt.InContainer || ssh == nil {
		return
	}
	address := hqAddress(rt)
	ctx, cancel := context.WithTimeout(ctx, hqCheckoutOriginBound)
	defer cancel()
	var wg sync.WaitGroup
	sem := make(chan struct{}, attachRepoStatusConcurrency)
	for i := range services {
		if services[i].HQRepo == "" || services[i].DevIsPool {
			continue
		}
		m, err := workflow.FindServiceMeta(stateDir, services[i].Hostname)
		if err != nil || !hqPairWired(m) || m.Hostname != services[i].Hostname || m.DevIsPool() {
			continue
		}
		m.HQ.Origin = services[i].HQOrigin
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
			target, err := hqPairOriginTarget(address, rt.OrgID, m)
			switch {
			case err != nil:
				services[i].HQOriginBlocked = err.Error()
			case target != "" && target != now:
				services[i].HQOriginPending = target
			}
		}(i, m)
	}
	wg.Wait()
}

// originMovedMarker is what hqPairOrigin's command says when it moved origin,
// before the address origin had (originMovedFrom).
const originMovedMarker = "ZCP_ORIGIN_MOVED"

// hqKeptOnGitHub is, per wired pair whose code its record says is on GitHub
// — connected through the organisation's GitHub app, HQ mirroring it — the
// line that tells develop's start how it is worked on: its checkout's origin
// is its repository in HQ, as every pair's, so the agent pushes its branch to
// origin and opens its change, and HQ alone pushes to GitHub
// (originOnGitHub). A pair kept in HQ, or followed, gets none. A pair keyed
// by its dev pool has no checkout for the line to name — its work is checked
// out in a lease of the pool — so it gets none.
func hqKeptOnGitHub(stateDir string) []string {
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil {
		return nil
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].Hostname < metas[j].Hostname })
	var lines []string
	for _, m := range metas {
		if !hqPairWired(m) || m.DevIsPool() || m.HQ.Origin == nil || m.HQ.Origin.Mode != hq.OriginOnGitHub {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: its code is on GitHub at %s, which HQ mirrors, and its checkout's origin is its repository in HQ: %s",
			m.Hostname, m.HQ.Origin.URL, originOnGitHub(m.Hostname)))
	}
	return lines
}
