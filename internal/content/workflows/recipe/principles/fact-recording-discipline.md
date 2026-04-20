# fact-recording-discipline

Recorded facts are the substrate the downstream writer reads to produce every reader-facing content surface. Record them at the moment of freshest knowledge — when the fix is applied, when the platform behavior is observed, when the cross-codebase contract is established. The record IS the classification moment.

## When to call `zerops_record_fact`

Every time you:

- Apply a fix for a non-trivial build, deploy, or runtime failure.
- Verify a non-obvious platform behavior (for example, idempotency semantics of a managed-execution primitive, readiness-gate timing, L7 routing, subdomain assignment).
- Establish a cross-codebase contract binding (DB schema owner, NATS queue-group name, HTTP response shape, shared entity ownership).
- Notice that the scaffold emitted a known-trap pattern that required a runtime rewrite (env-var shadow, S3 `forcePathStyle: true` missing, URL-embedded NATS credentials, and so on).
- Observe a platform behavior that a fresh reader would be surprised by — whether or not it broke anything.

Record early, record often. A fact that turns out to be unneeded costs nothing; a fact that should have been recorded but wasn't costs the next agent a round of re-archaeology.

## Fields

`zerops_record_fact` accepts the following fields. Required: `type`, `title`.

- **type** — one of `gotcha_candidate`, `ig_item_candidate`, `verified_behavior`, `platform_observation`, `fix_applied`, `cross_codebase_contract`.
- **title** — a short declarative phrase the downstream writer will read first.
- **substep** — the substep name under which the fact was observed.
- **codebase** — the hostname of the codebase the fact concerns, if codebase-specific.
- **mechanism** — one sentence on what the platform actually does, or what the fix actually changed.
- **failureMode** — what broke, if anything; the observable symptom.
- **fixApplied** — the exact change that unblocked the run, if a fix was applied.
- **evidence** — a short quote or reference to the log line, command output, or file path that grounds the fact.
- **scope** — routes between lanes: `content` (default — writer consumes), `downstream` (prepended to the next dispatch brief; writer does not consume), `both` (sparingly used, visible in both lanes).
- **routeTo** — the published surface this fact belongs on. One of: `content_gotcha`, `content_intro`, `content_ig`, `content_env_comment`, `claude_md`, `zerops_yaml_comment`, `scaffold_preamble`, `feature_preamble`, `discarded`.

## The recording step IS the classification moment

The `routeTo` field decides which reader-facing surface the fact belongs on. You declare it when you record — not later, not the writer's inference.

You know more at recording time than any downstream consumer will: you have the failure mode in front of you, the fix still paged in, the error class you just traced. Classify now:

- A platform invariant or platform-and-framework surprise a porter would hit on any recipe — `content_gotcha`.
- A platform-forced code change worth an item in the integration guide — `content_ig`.
- A scaffold decision that belongs in the per-codebase assistant context — `claude_md`.
- A self-inflicted bug that a future scaffold of the same framework would not recur — `discarded`.
- A cross-codebase contract binding the next scaffold agent needs — `scaffold_preamble` (scope: `downstream`).
- An assumption the next feature agent would otherwise re-investigate — `feature_preamble` (scope: `downstream`).
- A configuration decision whose reasoning belongs beside the config — `zerops_yaml_comment` or `content_env_comment`.

The downstream writer consumes your `routeTo` value as the routing decision. If you route a fact to `claude_md`, the writer does not re-consider publishing it as a public gotcha.

## What NOT to do

- Do not record micro-steps. Record root-cause mechanisms. Over-recording every small observation buries the signals in volume.
- Do not defer classification to the writer. The writer operates on fresh context and cannot reconstruct the decision you made with the failure mode in front of you.
- Do not write content into the fact fields. `failureMode` is "the api container exited with code 1 on the second SIGTERM", not "users will notice a rolling-deploy hiccup."

## Short version

Record at freshest knowledge. Classify at record time. Trust your `routeTo` — the writer consumes it as your decision, not as a suggestion.
