---
id: develop-first-deploy-scaffold-yaml
priority: 2
phases: [develop-active]
deployStates: [never-deployed]
title: "Scaffold zerops.yaml for the first deploy"
---

### Scaffold `zerops.yaml`

Write `zerops.yaml` at the repository root before the first deploy —
without it, `zerops_deploy` fails at the validation stage and the agent
wastes a deploy slot.

**Shape (one `zerops:` block per runtime hostname the plan targets):**

```yaml
zerops:
  - setup: {hostname}
    build:
      base: <runtime-only key, e.g. nodejs@22 — NOT the composite run key>
      buildCommands: [...]       # optional for pre-built artefacts
      deployFiles: [.]           # dev mode; stage uses the build output dir
    run:
      base: <run key, may be composite: php-nginx@8.4, nodejs@22, ...>
      ports:
        - port: <app-listens-on>
          httpSupport: true
      envVariables:
        <KEY>: <value or ${service_KEY} cross-ref>
      start: <run command, not a build command>
```

**Env var references** — use the discovered catalog (injected at close
of bootstrap and stored in the session). Cross-service references use
`${hostname_KEY}` exactly; inventing alternative spellings resolves to
literal strings at runtime and fails silently.

**Mode-aware tips:**

- `dev` mode: `deployFiles: [.]`, build runs on SSHFS, `run.start` wakes
  the container — no stage pair to worry about.
- `simple` mode: identical layout to dev but single-slot, no stage.
- `standard` mode: emit separate entries for dev AND stage hostnames;
  stage's `deployFiles` points at the build output directory.

Schema: fetch `zerops.yaml` JSON Schema via `zerops_knowledge` if unsure.
