# Develop-edit envelope and copy guidance

Cases live in `develop_edit_test.go`.

- IG-2 — An edit envelope leaves the current deploy work file unread and unchanged. Breaks: starting an edit is blocked by or changes an unrelated deploy session.
- IG-3 — An edit briefing renders each copy and its step without the short turn-line budget. Breaks: the agent misses work held by another lease.
- IG-4 — A dev half's edit eligibility reads live code from its active platform version, including the empty-code placeholder. Breaks: an unbuilt service is treated as an edit or a running copy is treated as a first deploy.
- IG-5 — Both classic dev copies and pools receive edit guidance, while a deploy session does not. Breaks: the agent misses how its draft is handed over.
- IG-6 — A pool's edit briefing includes its lease loop, but a classic dev copy does not get pool instructions. Breaks: the agent cannot use the selected copy or is told to lease the wrong kind of service.
- IG-7 — A pool's develop briefing selects its standalone or Mate guidance for that environment only; adding a Mate's draft starts its build. Breaks: a standalone branch misses its stage check or a Mate builds a draft when described.
- IG-8 — A pool's idle briefing selects its standalone or Mate guidance for that environment only. Breaks: a standalone pool misses its stage-check guidance or a Mate is told to perform it.
