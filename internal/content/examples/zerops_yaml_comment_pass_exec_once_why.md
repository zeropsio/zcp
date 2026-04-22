---
surface: zerops-yaml-comment
verdict: pass
reason: decision-why-ok
title: "execOnce wrap on migrate — why, not what"
---

> ```yaml
> zerops:
>   - setup: prod
>     run:
>       initCommands:
>         # zsc execOnce gates the migration on the current appVersionId.
>         # On a replicated deploy (minContainers >= 2) the same deploy
>         # runs initCommands on every replica in parallel — without
>         # execOnce, concurrent `migrate` calls race for the migration
>         # table lock and one of them will hit a deadlock-retry loop.
>         # execOnce lets the first replica win and the others no-op.
>         #
>         # --retryUntilSuccessful covers the second failure mode: the
>         # DB may not be reachable on the very first boot when the
>         # balancer provisions the container before the network path
>         # to the db service stabilizes. Bounded retry means the deploy
>         # still lands in that window.
>         - zsc execOnce "migrate-${appVersionId}" -- php artisan migrate --force
>           retryUntilSuccessful: true
> ```

**Why this passes the zerops-yaml-comment test.**
- Explains WHY both `execOnce` and `--retryUntilSuccessful` are here,
  not what they do syntactically.
- Names the two failure modes (concurrent replica lock race, DB
  reachability race) the flags defend against.
- A reader deciding whether to copy this pattern into their own
  zerops.yaml can trace each flag to a specific bad-outcome it prevents.

Spec §8 test: *"Does each comment explain a trade-off or consequence
the reader couldn't infer from the field name?"* — yes; the flags'
purpose is not inferable from their names, and the comment teaches
both.
