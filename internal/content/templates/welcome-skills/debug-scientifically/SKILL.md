---
name: debug-scientifically
description: Debug with hypotheses and cheap experiments instead of shotgun edits — find the root cause, prove it, then fix it with a regression test.
source: zerops-curated
version: 1
---

# Debug scientifically

Never fix what you haven't reproduced; never explain what you haven't observed.

1. **Reproduce first.** Get a minimal, deterministic reproduction. If you can't reproduce it, you're collecting evidence, not fixing yet.
2. **State a hypothesis** — one sentence: "X fails because Y." If you can't state one, gather more evidence (logs, bisect, smaller input), don't edit code.
3. **Design the cheapest experiment that can DISPROVE it** — a log line, a narrower test, a bisect step, toggling one input. Predict the outcome before running.
4. **Run it and believe the result.** Hypothesis survived? Tighten it. Disproved? Discard it without mercy — half-dead hypotheses cause shotgun edits.
5. **Fix the root cause**, not the symptom. If the failure was possible, something let it be possible — fix that layer.
6. **Lock it in**: add the regression test that would have caught this, then re-run the full affected suite.

Anti-patterns to refuse: changing several suspects at once; "it works now" without knowing why; sleeping/retrying over a race instead of removing it; deleting a failing test.
