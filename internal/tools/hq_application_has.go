package tools

import (
	"strings"

	"github.com/zeropsio/zcp/internal/hq"
)

// What a Mate is told by what its application has — a production and how it
// is run, a Stage, other Mates (hq.MateState.Production, HasStage, Mates) —
// at the moments it matters: a change opened, a change described and ready,
// develop's start, a change merged. Each sentence is written once here, as a
// clause a pass joins with "; " and an answer turns into a sentence
// (sentenceOf). Guidance only: no tool refuses or decides anything by these
// facts.
//
// An HQ older than the facts sends none of them, which reads exactly as an
// application with nothing live, no Stage and no count. So "nothing live" is
// said only when Mates is present — an HQ that sends the facts always sends
// it — and Production is not (nothingLive); from an older HQ every function
// here answers "" and the text is what it was. A production of a kind zcp
// does not know says nothing about production: neither that a release
// publishes to it nor that nothing is live. oneRunOfAsks is what one change
// holds, which AGENTS.md's "What one change holds" words in full
// (docs/spec-mate.md §10.10).
const (
	noProductionYet         = "this application has no production yet, so never call merged work live, deployed or released"
	hasProductionLead       = "this application has a production"
	releasePublishes        = hasProductionLead + ", and a merge does not publish to it, a release does: say so when you report the work, and never call it live before a release"
	productionRunsOutside   = "this application's production runs outside Mate, so Mate releases nothing to it and cannot tell what a merge publishes there — a merge may reach production by itself: never tell the person merged work is live, and never tell them it is not; say it is on main, and that whoever runs production knows when it goes out"
	oneRunOfAsks            = "keep each change to one run of asks, anything beyond the interface in a change of its own"
	describeProductionRisks = "in its description, under a plain heading, call out what could hurt production: data changes, setup or environment changes, anything a rollback would not undo"
	stageRunsMain           = "after a merge the application's Stage runs what is on main"
	stageChecksFirst        = ": that is where it is checked before it goes live"
	othersWorkHere          = "other Mates work in this application too: start new work from an up-to-date main, expect main to move, and " + oneRunOfAsks
)

// releasedByMate reports a production HQ releases to: only a release
// publishes there.
func releasedByMate(state hq.MateState) bool {
	return state.Production == hq.ProductionZerops || state.Production == hq.ProductionGit
}

// runsOutside reports a production run outside Mate: live, but what a merge
// publishes there is not Mate's to know.
func runsOutside(state hq.MateState) bool {
	return state.Production == hq.ProductionOutside
}

// nothingLive reports that an HQ that sends the facts names no production.
func nothingLive(state hq.MateState) bool {
	return state.Mates > 0 && state.Production == ""
}

// mergeKeepsOnMain is what the agent tells the person of a merge while
// nothing is live. Where the application limits who merges (limited), putting
// the project live is not promised to be the person's (mergeLimitedRule).
func mergeKeepsOnMain(limited bool) string {
	live := "they put the project live in Mate"
	if limited {
		live = "the project is put live in Mate"
	}
	return "when you tell the person the work is ready, say in plain words that merging keeps it on main, where later work and other Mates build on it, and that nothing is published until " + live
}

// mergeAndProduction is what a merge does to production, for a change ready
// or merged: nothing until a release, unknown where production runs outside
// Mate, nothing at all while there is none; none for an older HQ and for a
// kind zcp does not know.
func mergeAndProduction(state hq.MateState) []string {
	switch {
	case releasedByMate(state):
		return []string{releasePublishes}
	case runsOutside(state):
		return []string{productionRunsOutside}
	case nothingLive(state):
		return []string{noProductionYet}
	}
	return nil
}

// stageWords say where a merge runs first; with a production outside Mate a
// merge may be live before anyone checks, so Stage is not promised to come
// first.
func stageWords(state hq.MateState) []string {
	switch {
	case !state.HasStage:
		return nil
	case runsOutside(state):
		return []string{stageRunsMain}
	}
	return []string{stageRunsMain + stageChecksFirst}
}

// applicationOpenedWords follow a change opened: with a production, however
// it is run, what its description owes the people it reaches.
func applicationOpenedWords(state hq.MateState) string {
	if !releasedByMate(state) && !runsOutside(state) {
		return ""
	}
	return sentencesOf(hasProductionLead + ": " + oneRunOfAsks + ", and " + describeProductionRisks)
}

// applicationReadyWords follow a change described and ready: what a merge
// does and does not do here, for the agent to say as it reports the work.
func applicationReadyWords(state hq.MateState) string {
	clauses := mergeAndProduction(state)
	if nothingLive(state) {
		clauses = append(clauses, mergeKeepsOnMain(state.MergeLimited))
	}
	return sentencesOf(append(clauses, stageWords(state)...)...)
}

// applicationStartWords is develop's start's clause on how work is cut here:
// beside other Mates, or toward a production; "" for neither.
func applicationStartWords(state hq.MateState) string {
	switch {
	case state.Mates > 1:
		return othersWorkHere
	case releasedByMate(state), runsOutside(state):
		return hasProductionLead + ": " + oneRunOfAsks
	}
	return ""
}

// applicationMergedWords end a merged change's note: what the merge did and
// did not publish, and where it runs first.
func applicationMergedWords(state hq.MateState) string {
	clauses := append(mergeAndProduction(state), stageWords(state)...)
	if len(clauses) == 0 {
		return ""
	}
	return "; " + strings.Join(clauses, "; ")
}

// sentencesOf turns clauses into sentences after an answer's own, each led by
// a space; "" for none.
func sentencesOf(clauses ...string) string {
	var b strings.Builder
	for _, c := range clauses {
		b.WriteString(" " + sentenceOf(c))
	}
	return b.String()
}
