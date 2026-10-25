package tools

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A Mate's change lands its work on its repository's `main` (SPEC §3.2a).
// `main` moves only by HQ's merge, so the change is not a courtesy — it is
// the ONLY way the Mate's code reaches it. The agent pushes a branch of its
// own with plain git and opens that branch's change (open-change): one change
// per branch, an open change being (application, repository, branch). An HQ
// that keeps an open change per branch says so in the Mate's state
// (openPerBranch); an older one keeps one open change per repository and
// answers it to any opener. zcp keeps each change it knows of in the change
// ledger (workflow.HQChange), never on a pair: pairs share repositories, and
// leases have no pair. What became of a change is read from the Mate's own
// state in HQ.

// hqBase is the branch a change lands on.
const hqBase = "main"

// changeTitleRunes bounds a title: HQ keeps at most 120 characters.
const changeTitleRunes = 120

// hqChanges is every change the ledger keeps, the pairs' records an older zcp
// wrote moved into it first (workflow.HQChanges). A ledger that cannot be
// read reads as none: every reader says only what it knows.
func hqChanges(stateDir string) []workflow.HQChange {
	changes, _ := workflow.HQChanges(stateDir)
	return changes
}

// changesIn is changes of appID's repo, by number.
func changesIn(changes []workflow.HQChange, appID, repo string) []workflow.HQChange {
	var out []workflow.HQChange
	for _, c := range changes {
		if c.AppID == appID && c.Repo == repo {
			out = append(out, c)
		}
	}
	return out
}

// recordHQChange keeps change number of appID's repo, on branch, in the
// ledger. Best-effort: a call that could not write it finds the change again
// in the Mate's state next time, which costs a call and nothing else.
func recordHQChange(stateDir, appID, repo string, number int, branch, title, source string) {
	_ = workflow.RecordHQChange(stateDir, workflow.HQChange{
		AppID: appID, Repo: repo, Number: number, Branch: branch, Title: title, Source: source,
	})
}

// hqReleasedBranch is the branch an older zcp kept a wired pair's work on in
// its checkout: the Mate's own, never a change's — HQ bound that zcp's
// changes, opened naming no branch, to ChangeBranch (workflow.ReleasedBranchOf
// moves such a record into the ledger on the change's branch).
func hqReleasedBranch(hqc hq.Client) string { return "mate/" + hqc.ProjectID() }

// takeChangeBranch says how the agent takes branch, the Mate's open change's,
// in its own shell on the mounted checkout. A checkout an older zcp kept the
// change's work on holds it as the Mate's own branch (hqReleasedBranch),
// which git cannot keep beside the change's branch beneath it: that one is
// renamed instead.
func takeChangeBranch(hqc hq.Client, branch string) string {
	take := fmt.Sprintf("in your own shell on the mounted checkout take it (`git fetch origin %s && git checkout %s`)", branch, branch)
	if released := hqReleasedBranch(hqc); strings.HasPrefix(branch, released+"/") {
		take += fmt.Sprintf(" — or, where the checkout still has the branch %q an older zcp kept this work on, which git cannot keep beside it, rename that one instead (`git branch -m %s %s && git checkout %s`)",
			released, released, branch, branch)
	}
	return take
}

// hqOpenChangeWork says, on an HQ that keeps one open change per repository,
// that while change c is open this Mate's work in its repository goes on its
// branch, and how to take it: work cut from main is no fast-forward of that
// branch, and that HQ opens no second change for it. "" for a change whose
// branch HQ did not name.
func hqOpenChangeWork(hqc hq.Client, c workflow.HQChange) string {
	if c.Branch == "" {
		return ""
	}
	return fmt.Sprintf("change #%d in %q is open on the branch %q, so this Mate's work there goes on that branch rather than on a branch cut from %q: %s, carry the work on and push it (`git push origin %s`), never force-pushing; cut the next work's branch from %q once #%d is merged or closed",
		c.Number, c.Repo, c.Branch, hqBase, takeChangeBranch(hqc, c.Branch), c.Branch, hqBase, c.Number)
}

// handedChangesIn is the open changes state says a person handed the Mate in
// appID's repo, by number; none for a state of another application.
func handedChangesIn(state hq.MateState, appID, repo string) []hq.MateChange {
	if state.AppID == nil || *state.AppID != appID {
		return nil
	}
	var handed []hq.MateChange
	for _, c := range state.Changes {
		if c.Repo == repo && c.State == hq.ChangeOpen && c.Handed {
			handed = append(handed, c)
		}
	}
	slices.SortFunc(handed, func(a, b hq.MateChange) int { return a.Number - b.Number })
	return handed
}

// handedWork says a handed change is the Mate's and where its work goes:
// onto its branch, taken from HQ when its gone Mate pushed to it, cut from
// main when nothing is there yet. On an HQ that keeps one open change per
// repository (onePerRepo) no other change opens there while it is open.
func handedWork(change hq.MateChange, repo, branch string, onePerRepo bool) string {
	take := fmt.Sprintf("its work so far is on the branch %q: in your own shell on the mounted checkout take that branch (`git fetch origin %s && git checkout %s`)",
		branch, branch, branch)
	if change.Head == nil {
		take = fmt.Sprintf("nothing is pushed to its branch %q yet: in your own shell on the mounted checkout cut it from %q (`git fetch origin %s && git checkout -b %s origin/%s`)",
			branch, hqBase, hqBase, branch, hqBase)
	}
	words := fmt.Sprintf("change #%d in %q was handed to this Mate from a Mate that is gone and is its change now — %s, carry the change on there and push to it (`git push origin %s`), never force-pushing",
		change.Number, repo, take, branch)
	if onePerRepo {
		words += fmt.Sprintf(", and open no other change in %q while it is open", repo)
	}
	return words
}

// hqTakeHanded keeps every change a person handed this Mate in appID's repo,
// from a Mate that is gone, in the ledger on the branch it was on, and says
// each once: the Mate's own state in HQ is the only word of a hand-over. A
// change the ledger keeps already is said no more.
func hqTakeHanded(stateDir string, hqc hq.Client, state hq.MateState, appID, repo string, kept []workflow.HQChange) []string {
	var lines []string
	for _, handed := range handedChangesIn(state, appID, repo) {
		if slices.ContainsFunc(kept, func(c workflow.HQChange) bool { return c.Number == handed.Number }) {
			continue
		}
		branch := hqc.BranchOf(handed.Number, handed.Branch)
		recordHQChange(stateDir, appID, repo, handed.Number, branch, titleOf(handed), workflow.HQChangeFromHanded)
		lines = append(lines, handedWork(handed, repo, branch, !state.OpenPerBranch))
	}
	return lines
}

// titleOf is a change's title as the Mate's state names it, "" for none.
func titleOf(c hq.MateChange) string {
	if c.Title == nil {
		return ""
	}
	return *c.Title
}

// changeOutcome is what the Mate's state says became of a change zcp keeps.
type changeOutcome int

const (
	// changeStillOpen: the state names it open.
	changeStillOpen changeOutcome = iota
	// changeMerged: the state names it merged.
	changeMerged
	// changeClosed: the state names it closed without merging — or, from an
	// HQ that keeps one open change per repository, lists it no more while
	// it would still list it.
	changeClosed
	// changeUnlisted: an HQ that lists every open change lists it no more —
	// it settled, and more changes settled since than its state lists, so
	// whether it merged is its review's to say.
	changeUnlisted
	// changeUnknown: an older HQ lists it no more, and lists as many of the
	// Mate's changes in the repository as it lists at all, each newer: an
	// open change older than those falls out of that HQ's window (I7), so
	// it is kept, unknown, never read as closed.
	changeUnknown
)

// outcomeOf is what state says became of c, with the change as state names
// it (nil when it names none).
func outcomeOf(state hq.MateState, c workflow.HQChange) (changeOutcome, *hq.MateChange) {
	listed, oldest := 0, 0
	for i := range state.Changes {
		s := &state.Changes[i]
		if s.Repo != c.Repo {
			continue
		}
		if s.Number == c.Number {
			switch s.State {
			case hq.ChangeOpen:
				return changeStillOpen, s
			case hq.ChangeMerged:
				return changeMerged, s
			}
			return changeClosed, s
		}
		listed++
		if oldest == 0 || s.Number < oldest {
			oldest = s.Number
		}
	}
	switch {
	case state.OpenPerBranch:
		return changeUnlisted, nil
	case listed >= hq.MateChangesPerRepo && c.Number < oldest:
		return changeUnknown, nil
	}
	return changeClosed, nil
}

// learnChangeLanding reads what became of c, a change the ledger keeps, from
// the Mate's own state in HQ, freshly read, and says so once. Nothing pushes
// this fact: a person merges in the app's review, and a design that waited to
// be told would be right for one caller and silently wrong for the rest.
// Passes, describes and the launch refusal ask, so a merge landed since the
// last read is known before the caller acts on the change.
//
// The change is forgotten as soon as it is no longer open. "" while it is
// open, which is the ordinary state and worth no words — or unknown.
func learnChangeLanding(stateDir string, c workflow.HQChange, state hq.MateState) string {
	outcome, _ := outcomeOf(state, c)
	if outcome == changeStillOpen || outcome == changeUnknown {
		return ""
	}
	_ = workflow.ForgetHQChange(stateDir, c.AppID, c.Repo, c.Number)
	switch outcome {
	case changeMerged:
		return fmt.Sprintf("change #%d is merged — this Mate's work is on %q now; bring it up to date in your own shell on the mounted checkout, never over ssh (`git checkout %s && git fetch origin && git merge --ff-only origin/%s`), and cut the next work's branch from it", c.Number, hqBase, hqBase, hqBase)
	case changeUnlisted:
		return fmt.Sprintf("change #%d is no longer open — HQ lists it among this Mate's changes no more, so whether it merged is its review's to say (%s)", c.Number, changeBranchWords(c))
	case changeStillOpen, changeClosed, changeUnknown:
	}
	return fmt.Sprintf("change #%d was closed without merging — nothing of it is on %q", c.Number, hqBase)
}

// changeBranchWords names c's branch, or that HQ named none.
func changeBranchWords(c workflow.HQChange) string {
	if c.Branch == "" {
		return "a branch HQ did not name"
	}
	return fmt.Sprintf("its branch %q", c.Branch)
}

// hqChangesPass is the changes' turn of a repository pass, after its pairs':
// per repository of the application HQ holds the Mate in, what became of each
// change the ledger keeps there (learnChangeLanding), then each change handed
// to the Mate (hqTakeHanded) — and, at develop's start (atStart), on an HQ
// that keeps one open change per repository, the change still open, where
// the work goes (hqOpenChangeWork). A change another step holds (a describe)
// is left to the next pass. Changes of another application than the one HQ
// holds the Mate in stay there, said once.
func hqChangesPass(ctx context.Context, stateDir string, hqc hq.Client, state hq.MateState, atStart bool) []string {
	if state.AppID == nil {
		return nil
	}
	app := *state.AppID
	changes := hqChanges(stateDir)
	var lines []string
	for _, c := range changes {
		if c.AppID != app {
			_ = workflow.ForgetHQChange(stateDir, c.AppID, c.Repo, c.Number)
			lines = append(lines, fmt.Sprintf("change #%d in %q stays in the application it was opened in: HQ holds this Mate in another now", c.Number, c.Repo))
		}
	}
	for _, repo := range hqChangeRepos(stateDir, app, changes) {
		var said []string
		for _, c := range changesIn(changes, app, repo) {
			release, err := workflow.HoldHQChange(ctx, stateDir, c.AppID, c.Repo, c.Number, 0)
			if err != nil {
				continue
			}
			line := learnChangeLanding(stateDir, c, state)
			release()
			if line != "" {
				said = append(said, line)
			}
		}
		kept := changesIn(hqChanges(stateDir), app, repo)
		handed := hqTakeHanded(stateDir, hqc, state, app, repo, kept)
		said = append(said, handed...)
		if atStart && !state.OpenPerBranch && len(handed) == 0 && len(kept) == 1 {
			if work := hqOpenChangeWork(hqc, kept[0]); work != "" {
				said = append(said, work)
			}
		}
		for _, line := range said {
			lines = append(lines, fmt.Sprintf("repository %q: %s", repo, line))
		}
	}
	return lines
}

// hqChangeRepos is every repository of app a pass turns to for its changes:
// each one the ledger keeps a change in, and each a wired pair has — a change
// may be handed there — by name.
func hqChangeRepos(stateDir, app string, changes []workflow.HQChange) []string {
	var repos []string
	for _, c := range changes {
		if c.AppID == app && !slices.Contains(repos, c.Repo) {
			repos = append(repos, c.Repo)
		}
	}
	metas, _ := workflow.ListServiceMetas(stateDir)
	for _, m := range metas {
		if hqPairWired(m) && m.HQ.AppID == app && !slices.Contains(repos, m.HQ.Repo) {
			repos = append(repos, m.HQ.Repo)
		}
	}
	slices.Sort(repos)
	return repos
}

// trackedSelf is lazySelf with whether a pass read the state at all: the
// changes' turn runs only once a pair's turn asked HQ, so the pairs' backoff
// paces it too.
func trackedSelf(ctx context.Context, hqc hq.Client) (self func() (hq.MateState, error), read func() bool) {
	lazy := lazySelf(ctx, hqc)
	asked := false
	return func() (hq.MateState, error) {
		asked = true
		return lazy()
	}, func() bool { return asked }
}
