package tools

import (
	"net/url"
	"os"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A Mate made on main delivered to its organization's Gitea: main's zcp
// pointed each pair's `origin` at the repository main's broker gave it,
// `<GITEA_URL>/<the group's org>/<name>.git`, and recorded it on the pair
// (workflow.MainGiteaRepo). HQ's import brought those repositories in under
// the same names, each Mate's open pull request as its open change with the
// same number, so such a pair is wired to HQ the way a pair whose wiring
// stopped half-way is (wireHQPair): the repository of that name, `origin`
// moved there with the old one kept as zerops-original-origin, and its
// GIT_TOKEN the Mate credential once the old host's helper is gone. Any other
// remote stays the user's own. GITEA_URL and the zcp service's GITEA_TOKEN
// stay as they are: the old system is kept whole, and nothing writes to it.
// Migration-only: it goes with the migration.

// mainGitea is what tells the remote main's zcp set from one of the user's
// own: the old Gitea's host, and the group's org on it as a pair's record
// names it.
type mainGitea struct {
	host string
	org  string
}

// mainGiteaFrom reads it for metas: GITEA_URL's host name, "" on a container
// main never wired, and the org the first record names, "" while none does.
func mainGiteaFrom(metas []*workflow.ServiceMeta) mainGitea {
	var g mainGitea
	if u, err := url.Parse(strings.TrimSpace(os.Getenv(ops.GiteaURLEnvKey))); err == nil {
		g.host = u.Hostname()
	}
	for _, m := range metas {
		if m == nil || m.MainGitea == nil {
			continue
		}
		if org, _, ok := strings.Cut(m.MainGitea.FullName, "/"); ok && org != "" {
			g.org = org
			break
		}
	}
	return g
}

// mainGiteaOf is mainGiteaFrom over every pair of the Mate.
func mainGiteaOf(stateDir string) mainGitea {
	metas, _ := workflow.ListServiceMetas(stateDir)
	return mainGiteaFrom(metas)
}

// repository is the name of the repository main's zcp wired the pair to, and
// whether the pair's remote is that one. It is main's own rule
// (giteaRemoteIsThePairs, at zcp 91639bdaf): the remote's host name is the
// old Gitea's, its path ends in `<org>/<name>`, and that is the very
// repository the pair's record names — or, with no record, one named after
// the pair in the group's org, any org while no record names one. Main's
// broker answered the clone URL `https://<host>/<org>/<name>`.
func (g mainGitea) repository(m *workflow.ServiceMeta) (string, bool) {
	if g.host == "" || m == nil || m.RemoteURL == "" {
		return "", false
	}
	u, err := url.Parse(m.RemoteURL)
	if err != nil || !strings.EqualFold(u.Hostname(), g.host) {
		return "", false
	}
	segments := strings.Split(strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git"), "/")
	if len(segments) < 2 {
		return "", false
	}
	org, name := segments[len(segments)-2], segments[len(segments)-1]
	if org == "" || name == "" {
		return "", false
	}
	if m.MainGitea != nil && m.MainGitea.FullName != "" {
		return name, strings.EqualFold(org+"/"+name, m.MainGitea.FullName)
	}
	return name, strings.EqualFold(name, m.Hostname) && (g.org == "" || strings.EqualFold(org, g.org))
}

// owns reports whether the pair's remote is the one main's zcp set.
func (g mainGitea) owns(m *workflow.ServiceMeta) bool {
	_, ok := g.repository(m)
	return ok
}

// carryMainGitea brings into the pair's HQ record what main's record still
// owes: its pull request as the change on record, so the next delivery or
// pass asks HQ what became of it — open, merged since main last looked, or
// closed — the way it asks of any change; a merge the checkout has not
// absorbed — the same squash and head, now on HQ's `main`; and the words it
// kept, for the change that carries its pull request's number now.
func carryMainGitea(record *workflow.HQRepoRef, main *workflow.MainGiteaRepo) {
	if record == nil || main == nil {
		return
	}
	if record.Change == 0 {
		record.Change = main.PullRequest
	}
	if record.Landed == nil {
		record.Landed = main.Landed
	}
	if record.ChangeDescription == nil && main.ChangeDescription != nil {
		record.ChangeDescription = &workflow.ChangeDescription{Text: main.ChangeDescription.Text, Change: main.ChangeDescription.PullRequest}
	}
}
