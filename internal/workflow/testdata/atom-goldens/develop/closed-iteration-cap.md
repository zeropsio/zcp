---
id: develop/closed-iteration-cap
atomIds: [develop-closed-auto, develop-auto-close-semantics]
description: "develop-closed-auto phase, close reason iteration-cap — workflow exhausted retry budget without success."
---
<!-- UNREVIEWED -->

The envelope's `phase: develop-closed-auto` and `closeReason:
auto-complete` are set; full close criteria are in
`develop-auto-close-semantics`. Work is durable — code is in git,
infrastructure on Zerops.

Next actions:

```
zerops_workflow action="start" workflow="develop" intent="{next-task}"
zerops_workflow action="close" workflow="develop"
```

Full auto-close and explicit-close semantics:
`develop-auto-close-semantics`.

---

### Work session auto-close

Work sessions close automatically when either of two conditions hold:

- **`auto-complete`** — every service in scope has both a successful
  deploy and a passing verify. The envelope's `workSession.closedAt`
  becomes set, `closeReason: auto-complete`, and `phase` flips to
  `develop-closed-auto`.
- **`iteration-cap`** — the workflow's retry ceiling was hit. Same
  close-state shape; `closeReason: iteration-cap`.

Explicit `zerops_workflow action="close" workflow="develop"` emits
the same closed state manually and is rarely needed — starting a new
task with a different `intent` replaces the session.

Close scope follows the session topology: standard-mode pairs include
BOTH halves, so skipping the stage cross-deploy leaves the session
active. Dev-only or simple services close after their one successful
deploy + verify.

Close is cleanup, not commitment. Work itself is durable — code is
in git, infrastructure is on Zerops.
