# Idle guidance (atom selection, `synthesize.go`)

What an idle `zerops_workflow action="status"` guides the agent to do, decided by atom selection in
`internal/workflow/synthesize.go` from the envelope alone. The cases live in `scenarios_test.go`.

- IG-1 — A Mate's idle status never offers launch-production, which a Mate refuses, nor the bootstrap protocol for a project already set up. Breaks: every idle status of a Mate carries about 5.8 KB the agent must not act on, half of it telling it to start a workflow that refuses.
- IG-2 — An edit envelope leaves the current deploy work file unread and unchanged. Breaks: starting an edit is blocked by or changes an unrelated deploy session.
- IG-3 — An edit briefing renders each copy and its step without the short turn-line budget. Breaks: the agent misses work held by another lease.
- IG-4 — A dev half's edit eligibility reads live code from its active platform version, including the empty-code placeholder. Breaks: an unbuilt service is treated as an edit or a running copy is treated as a first deploy.
