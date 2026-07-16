---
name: tdd-red-green
description: Drive every behavior change through a failing test first — red, green, then refactor with the tests as a safety net.
source: zerops-curated
version: 1
---

# TDD: red → green → refactor

Work in this loop for every behavior change:

1. **Red** — write the smallest test that states the behavior you want. Run it. It must FAIL, and fail for the right reason (read the failure message; a compile error or a wrong-reason failure doesn't count). If you can't make it fail, the test asserts nothing — rewrite it.
2. **Green** — write the least code that makes that test pass. Resist fixing other things you noticed; note them instead.
3. **Refactor** — with tests green, clean up: names, duplication, dead code. Run the tests after every step. Never refactor and change behavior in the same move.

Rules of thumb:
- One behavior per test; table-driven when cases multiply.
- Test through the public interface, not private internals — a test that must reach past the interface is telling you the shape is wrong.
- A bug fix starts with a test that reproduces the bug. No reproduction, no fix.
- Pure refactors skip red — but every affected test suite must stay green before you continue.
- Never mark work done with a failing or skipped test.
