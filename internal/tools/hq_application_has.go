package tools

import (
	"strings"

	"github.com/zeropsio/zcp/internal/hq"
)

// What a Mate is told by what its application has — a production, a Stage,
// other Mates (hq.MateState.HasProduction, HasStage, Mates) — at the moments
// it matters: a change opened, a change described and ready, develop's
// start, a change merged. Each sentence is written once here, as a clause a
// pass joins with "; " and an answer turns into a sentence (sentenceOf).
// Guidance only: no tool refuses or decides anything by these facts.
//
// An HQ older than the facts sends none of them, which reads exactly as an
// application with nothing live, no Stage and no count. So "nothing live" is
// said only when Mates is present — an HQ that sends the facts — and
// HasProduction is not (nothingLive); from an older HQ every function here
// answers "" and the text is what it was.
const (
	noProductionYet         = "this application has no production yet, so never call merged work live, deployed or released"
	hasProductionLead       = "this application has a production"
	releasePublishes        = hasProductionLead + ", and a merge does not publish to it, a release does: say so when you report the work, and never call it live before a release"
	keepChangesSmall        = "keep each change small and able to merge on its own"
	describeProductionRisks = "in its description, under a plain heading, call out what could hurt production: data changes, setup or environment changes, anything a rollback would not undo"
	stageRunsMain           = "after a merge the application's Stage runs what is on main: that is where it is checked before it goes live"
	othersWorkHere          = "other Mates work in this application too: start new work from an up-to-date main, expect main to move, and " + keepChangesSmall
)

// nothingLive reports that an HQ that sends the facts names no production.
func nothingLive(state hq.MateState) bool {
	return state.Mates > 0 && !state.HasProduction
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

// applicationOpenedWords follow a change opened: with a production, what its
// description owes the people who release.
func applicationOpenedWords(state hq.MateState) string {
	if !state.HasProduction {
		return ""
	}
	return sentencesOf(hasProductionLead + ": " + keepChangesSmall + ", and " + describeProductionRisks)
}

// applicationReadyWords follow a change described and ready: what a merge
// does and does not do here, for the agent to say as it reports the work.
func applicationReadyWords(state hq.MateState) string {
	var clauses []string
	switch {
	case state.HasProduction:
		clauses = append(clauses, releasePublishes)
	case nothingLive(state):
		clauses = append(clauses, noProductionYet, mergeKeepsOnMain(state.MergeLimited))
	}
	if state.HasStage {
		clauses = append(clauses, stageRunsMain)
	}
	return sentencesOf(clauses...)
}

// applicationStartWords is develop's start's clause on how work is cut here:
// beside other Mates, or toward a production; "" for neither.
func applicationStartWords(state hq.MateState) string {
	switch {
	case state.Mates > 1:
		return othersWorkHere
	case state.HasProduction:
		return hasProductionLead + ": " + keepChangesSmall
	}
	return ""
}

// applicationMergedWords end a merged change's note: merged is not live, and
// where the merge runs first.
func applicationMergedWords(state hq.MateState) string {
	var clauses []string
	switch {
	case state.HasProduction:
		clauses = append(clauses, releasePublishes)
	case nothingLive(state):
		clauses = append(clauses, noProductionYet)
	}
	if state.HasStage {
		clauses = append(clauses, stageRunsMain)
	}
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
