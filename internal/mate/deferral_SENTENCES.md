# Postponed update (`deferral.go`)

- KD-SINCE — A second not now for the same running Mate keeps the first one's time, so "postponed since" is how long the Mate has really waited. Breaks: every run resets it and a two-day wait reads as minutes.
- KD-STALE — A deferral recorded for another running version does not speak for the installed one. Breaks: a Mate updated by a restart still reads as postponed.
- KD-ORDER — After a not now the deferral is written before the cancel, so the Mate's status re-read on cancel finds it. Breaks: the Mate re-reads the old record and shows no postponement until its next boot.
- KD-NOANSWER — A drain the Mate never answered records nothing and still reopens. Breaks: a Mate that is down or booting reads as having refused, with a reason it never gave.
- KD-UNKNOWN — A run that could not read the release list keeps the deferral and its since and tells the Mate nothing. Breaks: one failed read resets "postponed since" and a two-day wait reads as minutes.
- KD-UNWRITABLE — A yes whose record cannot be written still restarts. Breaks: a full or read-only disk keeps every Mate on its old version for good.
- KD-RESET — Forgetting a zcp restart attempt keeps the deferral. Breaks: a new zcp build silently erases a Mate's postponement and its since.
- KD-POSTPONED — While the deferral speaks for the installed Mate, the status's updater is "postponed" with since, for and why. Breaks: the person and HQ see "idle" while a newer version waits.
- KD-FORCE-WORK — An operator's forced update whose readiness names any blocker other than `native resume …`, an unknown one included, neither closes intake nor restarts, and names that blocker. Breaks: a forced update restarts a Mate in the middle of a person's turn because zcp did not know the word for its work.
- KD-FORCE-RESUME — A forced update whose readiness names only `native resume …` blockers, with or without a leading `<id>: `, closes intake, reads readiness again and says yes, clearing the deferral. Breaks: the Mate whose old drain refuses on a conversation it merely cannot resume is never replaced, not even by an operator.
- KD-FORCE-REREAD — A forced update that finds live work on the readiness read after its close cancels and says not now. Breaks: a turn accepted while intake was closing is interrupted by the restart.
