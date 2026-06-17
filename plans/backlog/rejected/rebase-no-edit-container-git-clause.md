# REJECTED: add a container-git "rebase --continue --no-edit unsupported" clause

**Why rejected** (2026-06-17): agent-error, not a ZCP defect. Grep-verified ZERO hits
for `--no-edit` / `GIT_EDITOR` / `rebase --continue` across `internal/` — ZCP never
emits the flag; the agent invented `rebase --continue --no-edit` at an interactive
rebase step and self-corrected to `GIT_EDITOR=true`. Adding a clause would (a) create
a hand-authored, env-shaped TELL with no owning CHECK, (b) encode the eval's
MISdiagnosed premise (`--no-edit` has been valid since git ~1.7; the container runs
2.34.1), and (c) fail the "would removing it change LLM behavior?" test. The
container-git-compat principle is already owned at `agents_container.md`.

**Refs**: plans/minor-findings-rootcause-2026-06-17.md (F4).
