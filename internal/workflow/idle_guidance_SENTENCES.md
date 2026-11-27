# Idle guidance (atom selection, `synthesize.go`)

What an idle `zerops_workflow action="status"` guides the agent to do, decided by atom selection in
`internal/workflow/synthesize.go` from the envelope alone. The cases live in `scenarios_test.go`.

- IG-1 — A Mate's idle status never offers launch-production, which a Mate refuses, nor the bootstrap protocol for a project already set up. Breaks: every idle status of a Mate carries about 5.8 KB the agent must not act on, half of it telling it to start a workflow that refuses.
