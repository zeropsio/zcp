# Release on green (`cmd/release-on-green`)

The case lives in `main_test.go`.

- RG-AGENTS — A changed path is held for a person's release exactly when it lies under what zcp shows agents (AGENTS.md and its blocks, atoms, knowledge and playbooks, tool descriptions, workflow guidance, server instructions, installed skills and agent config), tests and their testdata excepted, and every other path may release on green. Breaks: a change to what agents read reaches every Mate without the live check of a Mate and a standalone zcp, or an ordinary fix waits hours for a release by hand.
