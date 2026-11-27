---
description: Procedural playbook for a Mate's application board — what is done now and what is proposed, checking the board first, and what to tell the person.
---

# The board — proposing work you found

Your application has one board: the tasks people put on it, which its Mates take. You never put a task there yourself. You propose one, Mate shows the person a card in this conversation, and they put it on the board with Put on board, or not.

## 1. Done now, or proposed

Work the person asked for, and anything it needs to work, is done now: in its own change when **What one change holds** in AGENTS.md says so, never proposed instead, never left for later. A task on the board is not a way to make a change smaller.

When the person asks you to put work on the board — in their words, or through Mate's Put on board or `/board` — propose exactly that work, with a brief whoever takes it can act on, and never start it.

Work nobody asked for that you find outside every open change — a bug in another part, something another repository needs, an unrelated fix you noticed — is proposed, never started: not on this branch, not on a new one.

Work that splits into steps that must land in order — a step builds on another step's change — is proposed as **one plan** whose tasks name what each needs, never as unrelated tasks: the board holds a step until the ones it needs are done and merged.

## 2. Propose it

1. Read the board first: `zerops_board_list`. When an open task already covers the work, propose nothing and tell the person in one sentence that it is on the board.
2. Otherwise `zerops_board_propose title="<one line>" brief="<the whole task>"`. Write the brief for whoever takes it, who has not seen this conversation: what is wrong or missing, where, how you found it, and what done looks like. Every member of the organization reads the board: no secret, value or excerpt of this conversation.
3. For steps that must land in order, propose them together instead: `zerops_board_propose tasks=[{key:"schema", title:"…", brief:"…"}, {key:"api", title:"…", brief:"…", needs:["schema"]}]`, 2–20 tasks in the order they land, each key short (`a-z0-9-`), each `needs` the keys of this plan's tasks it builds on. Each brief stays self-contained: whoever takes a step reads only its brief, never the others or this conversation.
4. Say one sentence — that you proposed it (or the plan) for the board, in its card — and go on with what the person asked for, starting none of it. Never say where the card stands (above or below): Mate places it.

When this Mate cannot show a plan's card yet, propose its tasks one by one, in the order they land, and say in each brief which of them it builds on.

Never ask the person for a token, a link or a board address: the card is the whole ask. When the board cannot be read, propose anyway; the person sees the board before they put the task there.
