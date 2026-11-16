package tools

import "github.com/zeropsio/zcp/internal/hq"

// mergeLimitedRule is what a Mate is told wherever zcp's words would have its
// person merge or release, in an application that limits who merges
// (hq.MateState.MergeLimited): the Mate does not know whether its person is
// one of the people who may, so it never sends them to merge or release, and
// hands the work over by the one action every person has there.
const mergeLimitedRule = `In this application only some people merge and release, and the person you work for may not be one of them: never tell them to merge or release it themselves. When the work is ready, tell them in plain words that the change is ready and that they press "Ask to merge" on it in Mate; the people who merge here take it from there. A change that waits for a merge is normal, not a problem to solve: go on working meanwhile.`

// mergeLimitedNote is the rule after a sentence said in an application that
// limits who merges, "" where nothing is limited.
func mergeLimitedNote(limited bool) string {
	if limited {
		return " " + mergeLimitedRule
	}
	return ""
}

// whoMerges names who merges a change, and releases: the person — or, in an
// application that limits who merges, someone who may there, never promised
// to be the Mate's person.
func whoMerges(limited bool) string {
	if limited {
		return "someone who merges here"
	}
	return "the person"
}

// reviewAskedOnlyThen ends a sentence on a change that is not ready yet: the
// person's review is asked for only once it is described — or, in an
// application that limits who merges, the people who merge there are.
func reviewAskedOnlyThen(limited bool) string {
	if limited {
		return "it is ready for the people who merge here only then"
	}
	return "the person is asked to review it only then"
}

// mergeLimitedHeard reports whether the application HQ holds the Mate in
// limits who merges, as id's link last heard its state; false while no state
// is heard, as from an HQ that limits nothing.
func mergeLimitedHeard(id hq.Identity) bool {
	doc, err := id.ReadMateState()
	if err != nil {
		return false
	}
	state, err := doc.Heard()
	return err == nil && state.MergeLimited
}
