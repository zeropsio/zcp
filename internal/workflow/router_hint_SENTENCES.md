# Bootstrap route hints

- RH-OWNED — A Mate's incomplete bootstrap hint names only sessions owning the offered incomplete metas, with their intent and creation time. Breaks: one conversation resumes another's bootstrap.
- RH-EMPTY — A Mate with no metas offers a new bootstrap first and lists each interrupted session with its intent and creation time for its originating conversation. Breaks: one conversation is directed into another's unfinished work.
- RH-LIVE — A Mate does not offer a bootstrap running under another PID as resumable. Breaks: a conversation is directed into work still running elsewhere.
- RH-STANDALONE — Standalone hints resume the first bootstrap session or offer a start when none exists. Breaks: standalone recovery changes.
- RH-ROUTE — A Mate routing incomplete metas offers only their owners' bootstrap sessions. Breaks: an unrelated interrupted bootstrap is offered alongside the incomplete work.
