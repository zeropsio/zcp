# AGENTS.md budget (`build_agents.go` BuildAgentsMD)

The rendered AGENTS.md rides in every request of every Mate, so its size is held to a budget. The case lives in `build_agents_test.go`.

- AB-1 — Every AGENTS.md BuildAgentsMD renders, less the guided or authoring block where one is appended, fits the budget its case names. Breaks: the file every request of every Mate carries grows unseen, and each release re-attaches the larger copy to every live conversation.
