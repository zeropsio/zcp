---
name: review-before-done
description: Before claiming any task done, re-read the full diff, run everything, hunt orphans, and verify each claim you are about to make.
source: zerops-curated
version: 1
---

# Review before "done"

"Done" is a claim about evidence, not effort. Before you say it:

1. **Read the entire diff** top to bottom as a reviewer would — not just the parts you remember writing. Every hunk must be intended, explainable, and in scope.
2. **Hunt orphans**: variables written but never read, parameters no longer used, imports/config keys left behind, functions nothing calls, comments describing code that changed. Removing something means cleaning up every reference.
3. **Run everything relevant** — build, the affected test suites, the linter — and READ the output. "It should pass" is not a result.
4. **Exercise the change for real** once: run the actual flow a user would hit, not only the unit tests around it.
5. **Verify each claim** in your summary: if you write "tests pass", you ran them; if you write "no behavior change", you diffed the behavior. Report failures and skips plainly — a half-done honest report beats a clean-sounding false one.
6. **Leave the tree clean**: no commented-out code, no debug prints, no "temporary" flags. History lives in version control, not in the file.
