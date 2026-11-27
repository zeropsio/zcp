# HQ checks handoff

- CGU-CHANGE — A change decodes HQ's checks handoff object and an absent handoff. Breaks: a given-up change cannot be read or appears to have no handoff.
- CGU-STATE — A Mate state decodes all its changes when one carries HQ's checks handoff object and another has none. Breaks: one given-up change hides the Mate's whole state.
