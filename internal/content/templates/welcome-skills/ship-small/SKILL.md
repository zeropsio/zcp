---
name: ship-small
description: Ship the smallest change that delivers value, keep the tree releasable at every step, and let working software drive the next decision.
source: zerops-curated
version: 1
---

# Ship small

Big-bang changes hide risk; small shipped steps expose it early.

1. **Find the smallest change that delivers observable value** — one behavior a user (or the next developer) can see working. If a task needs a week, find the two-day core that proves it.
2. **Keep every commit releasable**: it compiles, tests pass, the feature set is coherent. Half-finished states hide behind interfaces, not behind broken builds.
3. **Integrate continuously** — merge/rebase small and often; a long-lived branch is a debt with compound interest.
4. **Prefer working software over speculative structure**: build the abstraction when the second caller arrives, not before. Delete code you "might need later" — version control remembers it.
5. **Let feedback steer**: after each shipped slice, look at what it taught you (a failing assumption, a slow query, a confused user) and re-plan the next slice from evidence, not from the original guess.
6. **When a step balloons, split it** — a growing diff is a signal to cut scope, never to push harder.
