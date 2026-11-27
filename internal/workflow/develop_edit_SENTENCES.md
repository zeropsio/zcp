# Develop-edit envelope and copy guidance

Cases live in `develop_edit_test.go`.

- IG-2 — An edit envelope leaves the current deploy work file unread and unchanged. Breaks: starting an edit is blocked by or changes an unrelated deploy session.
- IG-3 — An edit briefing renders each copy and its step without the short turn-line budget. Breaks: the agent misses work held by another lease.
- IG-4 — A dev half's edit eligibility reads live code from its active platform version, including the empty-code placeholder. Breaks: an unbuilt service is treated as an edit or a running copy is treated as a first deploy.
