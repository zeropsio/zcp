---
description: Checking a Mate's work behind a sign-in — making a test user, asking for an account made for testing into the Mate's own vault, and using its values unseen in a page or a command.
---

# Checking behind a sign-in

Where the app owns its users, make a test user yourself (its sign-up, a seed, its own console). Where the identity comes from elsewhere — Google, GitHub, Zerops — you cannot make one: ask for an account made for testing, one with access to nothing that matters, and say exactly that in the why.

Ask for what the sign-in takes, each as a value of its own, into your own vault (`mate=true`: only you read it, no app does): the e-mail with `zerops_env action="request" mate=true key="TEST_EMAIL" sensitive=false why="<one line>"`, the password with `zerops_env action="request" mate=true key="TEST_PASSWORD" why="<one line>"`.

Use a value without reading it. In a page, write `{{env:TEST_PASSWORD}}` in a `zerops_browser` command — `["fill","<sel>","{{env:TEST_PASSWORD}}"]` — which fills it in and answers with the placeholder; in an `eval` it goes inside a quoted string. In a command, `zcp with TEST_EMAIL,TEST_PASSWORD -- <command>` puts the values in that command's environment, so the command is a script or program that reads them there, or `zcp with TEST_PASSWORD -- sh -c '<line with "$TEST_PASSWORD">'` in single quotes: a `$TEST_PASSWORD` on `zcp with`'s own line is empty, since your shell expands it and does not have the value. Never print, log or commit it, and never screenshot it in a visible field.
