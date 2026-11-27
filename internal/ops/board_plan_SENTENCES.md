# A plan for the board (`board_plan.go`, docs/spec-mate.md §6.11)

- PL-CHECK — A plan holds 2–20 tasks, each with its own key of 1–40 characters of a-z, 0-9 and -, a title and brief within one task's bounds, and needs naming only keys of the same plan, kept in the agent's order with repeats dropped; every refusal names the task or key it is about. Breaks: a card whose Put on board HQ refuses, or an agent that cannot tell which task to fix.
- PL-CYCLE — Needs that go in a circle are found, a task needing itself included, and named as the keys walked from a task back to itself; needs that only point back to earlier work are no circle. Breaks: a plan whose tasks wait on each other forever, or a sound plan refused.
