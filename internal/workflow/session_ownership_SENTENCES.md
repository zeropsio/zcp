# Bootstrap session ownership

- SO-1 — A Mate leaves an interrupted bootstrap for explicit resume; a standalone engine recovers its sole interrupted bootstrap. Breaks: another conversation inherits unfinished work or standalone recovery is lost.
- SO-2 — Starting another bootstrap in a Mate keeps an interrupted bootstrap available for explicit resume, even when it is old. Breaks: one conversation erases another's unfinished work.
- SO-3 — An unfinished Mate bootstrap holds its hostnames while its session exists, including between processes. Breaks: another bootstrap overwrites its unfinished service.
