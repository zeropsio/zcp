---
surface: zerops-yaml-comment
verdict: fail
reason: field-narration
title: "zerops.yaml comment narrates syntax instead of decision"
---

> ```yaml
> zerops:
>   - setup: prod
>     build:
>       # base is the build-phase runtime image
>       base: nodejs@22
>       # buildCommands is a list of shell commands run at build time
>       buildCommands:
>         - npm ci
>         - npm run build
>       # deployFiles tells the platform which files ship into the runtime
>       deployFiles: ./dist/~
>     run:
>       # httpSupport exposes the service to the L7 balancer
>       httpSupport: true
>       # start runs at container boot
>       start: node dist/main.js
> ```

**Why this fails the zerops-yaml-comment test.**
Every comment restates what the field literally does, which a reader can
infer from the field name and the Zerops docs. The comment adds zero
understanding; in some cases it misleads (e.g., `deployFiles: ./dist/~`
has a non-obvious tilde semantic that the gloss "which files ship into
the runtime" actively hides).

**Correct shape**: teach the non-obvious choice or trade-off.

```yaml
zerops:
  - setup: prod
    build:
      base: nodejs@22
      buildCommands:
        - npm ci                    # exact lockfile install; build fails on drift
        - npm run build             # tsc → ./dist
      # The tilde strips the `dist/` wrapper so `main.js` lands at the
      # container root. Without it, the runtime would look for
      # `./dist/main.js`, which breaks the `start: node main.js` form we
      # use in smaller recipes. Keep the tilde as long as the start path
      # assumes root-level artifacts.
      deployFiles: ./dist/~
    run:
      # httpSupport registers this service with the L7 balancer and
      # terminates TLS at the edge — necessary for the subdomain to
      # route traffic; omitting it means the service runs but is
      # unreachable from outside the project VXLAN.
      httpSupport: true
      start: node dist/main.js
```

Spec §8 anti-pattern: narrates what the field does instead of why the
choice was made.
