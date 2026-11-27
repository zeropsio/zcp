# Bootstrap session ownership

- SO-1 — Without a conversation identity, a new process leaves an interrupted bootstrap inactive until the agent explicitly resumes it. Breaks: another conversation inherits unfinished work.
- SO-2 — Starting another bootstrap keeps an interrupted bootstrap available for explicit resume, even when it is old. Breaks: one conversation erases another's unfinished work.
