---
surface: env-comment
verdict: fail
reason: field-narration
title: "Comment narrates what the field does instead of why"
---

> ```yaml
> # deployFiles: ./ ships the working tree to the container on every deploy.
> # minContainers: 2 sets the minimum replica count to 2.
> # zeropsSetup: dev uses the dev configuration from zerops.yaml.
> - hostname: appdev
>   zeropsSetup: dev
>   buildFromGit: https://github.com/zerops-recipe-apps/nestjs-showcase-app
>   minContainers: 2
> ```

**Why this fails the env-comment test.**
Every line narrates what the field literally means. A reader can infer
that directly from the field name + documentation; the comment adds
no understanding. Spec §5 test: *"Does each service-block comment
explain a decision (scale, mode, why this service exists at this tier),
not just narrate what the field does?"*

**Correct shape**: each comment explains a decision or trade-off the
reader can't infer from the field name alone.

```yaml
# appdev — dev-container workspace for this tier. Setup: dev means the
# container idles with a no-op start so SSH-driven iteration loops stay
# alive across commits. buildFromGit points at the per-codebase repo so
# the dev slot starts from a clean tree (no uncommitted state from the
# previous iteration leaks in).
- hostname: appdev
  zeropsSetup: dev
  buildFromGit: https://github.com/zerops-recipe-apps/nestjs-showcase-app
```

Anti-pattern: templated opening repeated across services ("enables
zero-downtime rolling deploys" on every block). Each block's reasoning
is service-specific.
