package tools

import (
	"fmt"
	"sort"

	"github.com/zeropsio/zcp/internal/hq"
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

// hqKeptOnGitHub is, per wired pair whose code its record says is kept on
// GitHub, the line that tells develop's start how it is worked on: through
// its checkout's origin, which is GitHub (originOnGitHub). A pair kept in HQ,
// or followed, is worked on as every other.
func hqKeptOnGitHub(stateDir string) []string {
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil {
		return nil
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].Hostname < metas[j].Hostname })
	var lines []string
	for _, m := range metas {
		if !hqPairWired(m) || m.HQ.Origin == nil || m.HQ.Origin.Mode != workflow.HQOriginGitHub {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: its code is kept on GitHub at %s, so %s",
			m.Hostname, m.HQ.Origin.URL, originOnGitHub(m.HQ.Origin.URL, originCheckout(stateDir, m.Hostname))))
	}
	return lines
}
