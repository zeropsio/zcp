package tools

import (
	"fmt"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A Mate's change lands its work on its repository's `main` (SPEC §3.2a).
// `main` moves only by HQ's merge, so the change is not a courtesy — it is
// the ONLY way the pair's code reaches it. The agent pushes a branch of its
// own with plain git and opens its change from it (open-change); a Mate has
// at most one open change per repository: opening answers the open one, or
// the next number. Its number is recorded on the pair; what became of it is
// read from the Mate's own state in HQ.

// hqBase is the branch a change lands on.
const hqBase = "main"

// changeTitleRunes bounds a title: HQ keeps at most 120 characters.
const changeTitleRunes = 120

// recordChange stamps the change's number and its branch's short name on the
// pair, in memory and on disk. Best-effort on disk: a pass that could not
// write it opens the same change again next time and finds it, which costs a
// call and nothing else.
func recordChange(stateDir string, m *workflow.ServiceMeta, number int, branch string) {
	m.HQ.Change, m.HQ.Branch = number, branch
	_ = workflow.UpsertServiceMeta(stateDir, m.Hostname, func(meta *workflow.ServiceMeta, existed bool) error {
		if !existed || meta.HQ == nil {
			return nil
		}
		meta.HQ.Change, meta.HQ.Branch = number, branch
		return nil
	})
}

// hqTakeHanded records the change a person handed this Mate in the pair's
// repository, from a Mate that is gone, as the pair's change on the branch
// it was on, and says so once: the Mate's own state in HQ is the only word
// of a hand-over. "" when none is handed, or the pair records it already.
// HQ hands a change only to a Mate holding no open change in the
// repository, so a change the pair records is replaced only once it is no
// longer open (hqLearnLanding runs first).
func hqTakeHanded(stateDir string, hqc hq.Client, m *workflow.ServiceMeta, state hq.MateState) string {
	handed := handedChangeIn(state, m.HQ)
	if handed == nil || m.HQ.Change == handed.Number {
		return ""
	}
	branch := hqc.BranchOf(handed.Number, handed.Branch)
	recordChange(stateDir, m, handed.Number, branch)
	return handedWork(handed, m.HQ.Repo, branch)
}

// handedChangeIn is the open change state says a person handed the Mate in
// the repository ref names, nil for none or a state of another application.
func handedChangeIn(state hq.MateState, ref *workflow.HQRepoRef) *hq.MateChange {
	if state.AppID == nil || *state.AppID != ref.AppID {
		return nil
	}
	for i := range state.Changes {
		if c := &state.Changes[i]; c.Repo == ref.Repo && c.State == hq.ChangeOpen && c.Handed {
			return c
		}
	}
	return nil
}

// handedWork says a handed change is the Mate's and where its work goes:
// onto its branch, taken from HQ when its gone Mate pushed to it, cut from
// main when nothing is there yet.
func handedWork(change *hq.MateChange, repo, branch string) string {
	take := fmt.Sprintf("its work so far is on the branch %q: in your own shell on the mounted checkout take that branch (`git fetch origin %s && git checkout %s`)",
		branch, branch, branch)
	if change.Head == nil {
		take = fmt.Sprintf("nothing is pushed to its branch %q yet: in your own shell on the mounted checkout cut it from %q (`git fetch origin %s && git checkout -b %s origin/%s`)",
			branch, hqBase, hqBase, branch, hqBase)
	}
	return fmt.Sprintf("change #%d in %q was handed to this Mate from a Mate that is gone and is its change now — %s, carry the change on there and push to it (`git push origin %s`), never force-pushing, and open no other change in %q while it is open",
		change.Number, repo, take, branch, repo)
}

// hqLearnLanding reads what became of the pair's change on record from the
// Mate's own state in HQ, freshly read, and says so once. Nothing pushes this
// fact: a person merges in the app's review, and a design that waited to be
// told would be right for one caller and silently wrong for the rest. Passes,
// describes and the launch refusal ask, so a merge landed since the last read
// is known before the caller acts on the pair's change.
//
// The recorded number is cleared as soon as the change is no longer open. A
// change HQ no longer lists for the Mate reads as closed. Returns "" while
// the change is open, which is the ordinary state and worth no words.
func hqLearnLanding(stateDir string, m *workflow.ServiceMeta, state hq.MateState) string {
	if m == nil || m.HQ == nil || m.HQ.Change == 0 {
		return ""
	}
	number := m.HQ.Change
	var found *hq.MateChange
	for i := range state.Changes {
		if c := state.Changes[i]; c.Repo == m.HQ.Repo && c.Number == number {
			found = &state.Changes[i]
			break
		}
	}
	if found != nil && found.State == hq.ChangeOpen {
		return ""
	}
	clearChange(stateDir, m, number)
	if found != nil && found.State == hq.ChangeMerged {
		return fmt.Sprintf("change #%d is merged — this Mate's work is on %q now; bring it up to date in your own shell on the mounted checkout, never over ssh (`git checkout %s && git fetch origin && git merge --ff-only origin/%s`), and cut the next work's branch from it", number, hqBase, hqBase, hqBase)
	}
	return fmt.Sprintf("change #%d was closed without merging — nothing of it is on %q", number, hqBase)
}

// clearChange forgets the number a pair recorded, in memory and on disk, once
// HQ says the change is no longer open without merging. number is the change
// the read was FOR: the disk write applies only while the fresh meta still
// records it — a concurrent describe may have moved the pair on to a newer
// change between the read and the write.
func clearChange(stateDir string, m *workflow.ServiceMeta, number int) {
	m.HQ.Change, m.HQ.Branch = 0, ""
	_ = workflow.UpsertServiceMeta(stateDir, m.Hostname, func(meta *workflow.ServiceMeta, existed bool) error {
		if !existed || meta.HQ == nil || meta.HQ.Change != number {
			return nil
		}
		meta.HQ.Change, meta.HQ.Branch = 0, ""
		return nil
	})
}
