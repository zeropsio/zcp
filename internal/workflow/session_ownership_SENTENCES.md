# Bootstrap session ownership

- SO-1 — A Mate leaves an interrupted bootstrap for explicit resume; a standalone engine recovers its sole interrupted bootstrap. Breaks: another conversation inherits unfinished work or standalone recovery is lost.
- SO-2 — Starting another bootstrap in a Mate keeps an interrupted bootstrap with a recorded plan available for explicit resume, even when it is old. Breaks: one conversation erases another's unfinished work.
- SO-3 — An unfinished Mate bootstrap holds its hostnames while its session exists, including between processes. Breaks: another bootstrap overwrites its unfinished service.
- SO-4 — A Mate prunes only old dead plan-less bootstraps without incomplete metas, while standalone retention stays unchanged. Breaks: abandoned empty sessions pile up or unfinished work is erased.
- SO-5 — An incomplete meta keeps its old interrupted bootstrap when another bootstrap starts. Breaks: one conversation erases another's unfinished services.
- SO-6 — A resume cannot claim an old empty bootstrap already pruned by another start. Breaks: stale state recreates an unregistered session.
