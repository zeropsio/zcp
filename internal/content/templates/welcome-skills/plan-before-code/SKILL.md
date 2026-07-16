---
name: plan-before-code
description: Restate the problem, surface invariants and edge cases, and cut the work into thin verifiable slices before writing any code.
source: zerops-curated
version: 1
---

# Plan before code

Before the first edit, spend the few minutes that save the rework:

1. **Restate the problem** in your own words — one paragraph: what changes for the user, what must not change. If you can't restate it, ask.
2. **List the invariants** — the things that must still be true afterwards (data shapes, API contracts, performance floors, security boundaries). These become your tests.
3. **Hunt the edge cases now**: empty, missing, duplicate, concurrent, huge, malformed, unauthorized. Write them down; each is a test case, not a surprise.
4. **Cut thin vertical slices** — each slice is independently buildable, testable, and reviewable, and leaves the tree releasable. Prefer a walking skeleton first: the thinnest end-to-end path that proves the wiring.
5. **Name the seam you'll test through** for each slice before building it.

While building: when reality contradicts the plan, stop and update the plan — don't push through on a wrong map. When a step's scope grows past what you planned, split it instead of stretching it.
