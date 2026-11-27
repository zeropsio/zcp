# The board (`board.go`, docs/spec-mate.md §6.11)

A Mate's agent proposes, for its application's board, work nobody asked for that it found outside every open change, after reading the board's open tasks. The cases live in `board_test.go`.

- BD-1 — The proposal checks its title (one line of 1–120 characters) and its brief (1–8,000 characters), and answers `proposed.board` with both trimmed, writing nothing and asking HQ nothing; outside the bounds nothing is proposed. Breaks: a card whose Put on board HQ refuses, or a proposal that reaches HQ by itself.
- BD-2 — Below `BoardMateVersion`, a dev build judged by the release it was cut from, the proposal is refused and nothing is proposed. Breaks: the agent tells the person of a card an older Mate never draws.
- BD-3 — The list reads `GET /api/mate/tasks` once under the Mate's key and tells each open task's id, title, state, who has it and where it came from, at most 100 with the rest counted. Breaks: the agent proposes a task the board already holds, or takes a task this Mate's person took for another's.
- BD-4 — Each failure of the list is said in words that tell the agent whether asking again helps: HQ not answering, its key refused, an HQ that keeps no board for it, any other refusal. Breaks: the agent retries a refused read, or gives up on what it found.
- BD-5 — A Mate whose state names no application has no board: the proposal proposes nothing and the list reads nothing. Breaks: a card whose Put on board has no application to write to.
