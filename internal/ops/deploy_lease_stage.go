package ops

import (
	"cmp"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/ops/git"
	"github.com/zeropsio/zcp/internal/topology"
)

// A stage tracks main (docs/spec-workflows.md §8 DP-8): a branch is reviewed
// on its lease's preview URL, and a stage runs main. A deploy from a lease to
// a stage is never refused for the commit it ships — a corrective deploy is
// never gated, and merging a branch is not the agent's call — so zcp steers
// instead: it fetches main before it resolves what ships, so sha="origin/main"
// ships main as the remote has it now, and a deploy that ships anything else
// says so in its warnings.

// leaseMainFetchBudget bounds the fetch of origin/main a deploy from a lease
// to a stage runs first; one that runs out compares against the copy the
// lease fetched last.
const leaseMainFetchBudget = 30 * time.Second

// leaseMainMarker heads the main check's answer line.
const leaseMainMarker = "ZCP:MAIN:"

// leaseMainRef is the lease's remote-tracking ref of the branch a stage
// tracks: the one a lease cuts new branches from (DP-6).
const leaseMainRef = "refs/remotes/origin/" + leaseDefaultBranch

// leaseMainFetchCommand fetches the remote's main into leaseMainRef. It needs
// no credential of its own: a lease's repository keeps the helper that answers
// from the session's GIT_TOKEN (DP-6), and a prompt fails instead of hanging.
var leaseMainFetchCommand = "GIT_TERMINAL_PROMPT=0 git fetch -q --no-tags origin " +
	shellQuote("+refs/heads/"+leaseDefaultBranch+":"+leaseMainRef)

// isLeaseStageTarget reports whether a deploy from a lease to a target of
// serviceType is a deploy to the part's stage: the target is an app runtime —
// anything but a pool, a Box or a managed service. The target's mode cannot
// tell: a pool never pairs with a stage (PS-2) and adopt records a lone
// runtime beside it as a dev.
func isLeaseStageTarget(serviceType string) bool {
	return topology.RuntimeClassFor(serviceType).IsAppRuntime()
}

// leaseMainState is how the commit a deploy ships relates to origin/main.
type leaseMainState int

const (
	leaseMainUnknown leaseMainState = iota // not checked, or the check could not tell
	leaseMainOn                            // the commit is origin/main or one of its ancestors
	leaseMainOff                           // the commit is not on origin/main
)

// leaseStageMain is what a deploy from a lease to a stage learns of
// origin/main in the lease's repository. A nil one belongs to every other
// deploy: it fetches nothing, checks nothing and adds no note.
type leaseStageMain struct {
	alias, stage string
	// fetchErr is why origin/main could not be fetched, "" when it was.
	fetchErr string
	// main is origin/main's commit, "" when the repository has none.
	main  string
	state leaseMainState
	// unknown is why the check could not tell, when it could not.
	unknown string
}

// newLeaseStageMain is the main check of a deploy from lease to stage, a
// target of serviceType; nil when lease is nil or the target is no stage.
func newLeaseStageMain(lease *LeaseTargetInfo, stage, serviceType string) *leaseStageMain {
	if lease == nil || !isLeaseStageTarget(serviceType) {
		return nil
	}
	return &leaseStageMain{alias: lease.Alias, stage: stage}
}

// fetch refreshes origin/main in the lease's repository at dir. A failure is
// recorded, never returned: the deploy goes on and the note says it compared
// against the copy fetched last.
func (m *leaseStageMain) fetch(ctx context.Context, r git.Runner, dir string) {
	if m == nil {
		return
	}
	fetchCtx, cancel := context.WithTimeout(ctx, leaseMainFetchBudget)
	defer cancel()
	_, stderr, err := r.Run(fetchCtx, dir, leaseMainFetchCommand)
	if err == nil {
		return
	}
	m.fetchErr = firstLine(stderr)
	if m.fetchErr == "" {
		m.fetchErr = err.Error()
	}
}

// check asks the lease's repository at dir whether sha — the commit the
// deploy ships — is on origin/main.
func (m *leaseStageMain) check(ctx context.Context, r git.Runner, dir, sha string) {
	if m == nil {
		return
	}
	if sha == "" {
		m.unknown = m.alias + " has no commit to compare"
		return
	}
	out, _, err := r.Run(ctx, dir, leaseMainCheckCommand(sha))
	if err != nil {
		m.unknown = "the check did not run: " + err.Error()
		return
	}
	for line := range strings.Lines(out) {
		answer, ok := strings.CutPrefix(strings.TrimSpace(line), leaseMainMarker)
		if !ok {
			continue
		}
		verdict, main, _ := strings.Cut(answer, " ")
		switch verdict {
		case "on":
			m.state, m.main = leaseMainOn, main
		case "off":
			m.state, m.main = leaseMainOff, main
		default:
			m.unknown = m.alias + " has no origin/" + leaseDefaultBranch
			if m.fetchErr != "" {
				m.unknown += ", and fetching it failed: " + m.fetchErr
			}
		}
		return
	}
	m.unknown = "the check gave no answer"
}

// leaseMainCheckCommand answers, in one line headed leaseMainMarker, whether
// sha is on origin/main ("on <main>" / "off <main>") or the repository has no
// origin/main ("none"). It always exits 0, so its answer is never an error.
func leaseMainCheckCommand(sha string) string {
	return "if m=$(git rev-parse --verify -q " + shellQuote(leaseMainRef+"^{commit}") + "); then " +
		"if git merge-base --is-ancestor " + shellQuote(sha) + ` "$m"; then echo "` + leaseMainMarker + `on $m"; ` +
		`else echo "` + leaseMainMarker + `off $m"; fi; ` +
		"else echo " + leaseMainMarker + "none; fi"
}

// withNote is warnings with the main note appended when there is one.
func (m *leaseStageMain) withNote(warnings []string, shipped string, dirty bool) []string {
	if m == nil {
		return warnings
	}
	if note := m.note(shipped, dirty); note != "" {
		return append(warnings, note)
	}
	return warnings
}

// note is the warning a deploy from a lease to a stage carries when what it
// ships is not a commit on origin/main, or when zcp could not tell; "" when it
// ships a commit on origin/main. shipped is the deploy's version name — a
// label and the commit's short sha — and dirty marks uncommitted changes on
// top: of a commit on origin/main only those changes are off main, and the
// note says so. It speaks of what the deploy ships, never of what the stage
// runs: the build may still fail (GF-5).
func (m *leaseStageMain) note(shipped string, dirty bool) string {
	if m.state == leaseMainOn && !dirty {
		return ""
	}
	commit := strings.TrimSuffix(shipped, "-dirty")
	what := commit
	if what == "" {
		what = "the working tree"
	}
	if dirty {
		what += " plus uncommitted changes"
	}
	var b strings.Builder
	b.WriteString("Stage tracks main: ")
	switch {
	case m.state == leaseMainUnknown:
		fmt.Fprintf(&b, "this deploy ships %s from %s to %s, and zcp could not tell whether it is on origin/%s (%s).",
			what, m.alias, m.stage, leaseDefaultBranch, m.unknown)
	case m.state == leaseMainOn:
		fmt.Fprintf(&b, "this deploy ships %s from %s to %s: %s is on origin/%s (at %s), the uncommitted changes are not.",
			what, m.alias, m.stage, cmp.Or(commit, "its commit"), leaseDefaultBranch, shortSHA(m.main))
	default:
		fmt.Fprintf(&b, "this deploy ships %s from %s to %s, which is not on origin/%s (at %s).",
			what, m.alias, m.stage, leaseDefaultBranch, shortSHA(m.main))
	}
	if m.fetchErr != "" && m.main != "" {
		fmt.Fprintf(&b, " origin/%s could not be fetched (%s), so this compares against the copy %s fetched last.",
			leaseDefaultBranch, m.fetchErr, m.alias)
	}
	fmt.Fprintf(&b, " Review a branch on its lease's preview URL instead (zerops_dev_server action=status hostname=%q names it as previewUrl).", m.alias)
	fmt.Fprintf(&b, " Once the branch is merged, put main back on %s: zerops_deploy sourceService=%q targetService=%q sha=\"origin/%s\".",
		m.stage, m.alias, m.stage, leaseDefaultBranch)
	return b.String()
}

// firstLine is s's first non-empty line, trimmed.
func firstLine(s string) string {
	for line := range strings.Lines(s) {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}
