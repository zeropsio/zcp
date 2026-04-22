# Finalize — env comment authoring rules

Each env exists to describe a distinct deployment context; comments tailored to THAT context are what gives the deliverable its editorial value. Copying one comment block into all six envs produces a file no reader learns from.

## Env lifecycle roles

| Env | Distinct framing |
|---|---|
| 0 — AI Agent | dev workspace for an AI agent — SSH in, build, verify via subdomain |
| 1 — Remote (CDE) | remote dev workspace for humans — SSH/IDE, full toolchain, live edit |
| 2 — Local | local development + `zcli vpn` connecting to a Zerops-hosted validator |
| 3 — Stage | single-container staging that mirrors production configuration |
| 4 — Small Production | production with `minContainers: 2` for rolling-deploy availability |
| 5 — HA Production | production with `cpuMode: DEDICATED`, `mode: HA`, `corePackage: SERIOUS` |

## What each env's commentary covers

Every service that appears in an env's import.yaml carries a comment explaining its role in THAT env. Cover:

- **Role in the dev lifecycle** — what this env exists for (AI agent workspace / remote dev / local validator / staging / small prod / HA prod).
- **What `zeropsSetup: dev` / `zeropsSetup: prod` does for this framework** — dev dependency install / production build + cache warming / equivalent — wherever relevant.
- **Replica-count & scaling rationale** for fields only present in this env. `minContainers` is a runtime-service field only (never on managed services — they use `mode: HA` / `NON_HA`), and `minContainers ≥ 2` only appears on envs 4-5. Envs 0-3 runtime services stay at `minContainers: 1` (rolling-deploy blips are fine in non-prod, SSHFS dev tiers require a single container, extra replicas waste the non-prod budget). On envs 4-5, `minContainers: 2` on a runtime service serves two independent axes: **(a) throughput** — one container can't serve the load — and **(b) HA / rolling-deploy availability** — a single-container pool drops traffic on every rolling deploy or container crash. Name whichever axis applies for this specific service. For a service whose throughput fits in one container at this tier (static SPA, light-traffic admin panel), axis (b) is the sole justification and the comment MUST name it explicitly. Other env-4/5-only fields: `cpuMode: DEDICATED` (env 5), `mode: HA` (env 5 managed services), `corePackage: SERIOUS` (env 5).
- **Managed service role** — what THIS app uses it for (sessions / cache / queue / etc., in minimal tier collapsing to one DB).
- **Project secret** — what the framework uses it for and why it must be shared across containers.

## Self-contained-per-env rule

Each env's import.yaml is published as a standalone deploy target on zerops.io/recipes — users land on one env's page, click deploy, and never see the others. Each env's prose therefore stands alone: phrase comments so a reader arriving only at this env file understands what it does and why, without comparing to siblings.

## Factuality rule

Numbers in your comment must come from the YAML block the comment is attached to, verbatim. If the YAML has `objectStorageSize: 1`, your comment may say "1 GB quota" but not "2 GB" or "20 GB". If the YAML has no number you want to reference, use qualitative phrasing — "single-replica", "HA mode", "modest quota", "sized for this tier" — rather than inventing a number from memory.

The `{env}_import_factual_claims` check fires when a declarative numeric claim in a comment contradicts the adjacent YAML field within the same service block. A failure names both strings in the form `comment claims "2 GB" but adjacent YAML has objectStorageSize: 1`. Two escape hatches:

- **Aspirational phrasing** — "bump to 50 GB via the GUI when usage grows" references a future value, not a current one. The subjunctive marker ("bump to", "upgrade to", "if you", "when usage", etc.) tells the check to skip the line. Use this when the comment's purpose is to teach the operator how to scale, not to assert current configuration.
- **Drop the number** — if the quantity isn't load-bearing for the comment's purpose, rewrite without it. "Single-replica production" teaches the same decision without contradicting a mismatched YAML value.

Default to qualitative phrasing. The number only earns its place when it matches the YAML verbatim AND adds information the YAML field name does not already convey.

## Depth rubric — WHY not WHAT

The comment depth check scores each substantive comment block (≥ 20 chars body, grouped across contiguous `#` lines) on whether it carries at least one reasoning marker. At least **35%** of substantive blocks hit a marker, with a hard floor of 2 reasoning blocks per env. Recognised reasoning markers:

- **Consequence** — `because`, `otherwise`, `without`, `so that`, `means that`, `prevents`, `causes`, `leads to`, `results in`.
- **Trade-off** — `instead of`, `rather than`, `in favor of`, `trade-off`.
- **Constraint** — `must`, `required`, `cannot`, `forced`, `mandatory`, `never`, `always`, `guaranteed`.
- **Operational consequence** — `rotation`, `rotate`, `redeploy`, `restart`, `scale`, `scaling`, `downtime`, `zero-downtime`, `rolling`, `fan-out`, `concurrent`, `race`, `lock`, `drain`.
- **Framework × platform intersection** — `build time`, `build-time`, `runtime`, `cross-service`, `at startup`, `at runtime`, `at import time`, `at deploy time`.
- **Decision framing** — `we chose`, `picked`, `default here`, `this tier`, `this env`, `matches prod`, `mirrors prod`.

Anchor each comment on the reasoning marker — the marker is a hook to explain what would go wrong if the decision flipped. Write on that axis, not on the field name.

## Voice — three dimensions of a good comment

1. **WHY this choice** + consequence: "CGO_ENABLED=0 produces a fully static binary — no C libraries linked at runtime" (not "Set CGO_ENABLED to 0").
2. **HOW the platform works here** — contextual behavior that makes the file self-contained so the reader never has to leave: "project-level — propagates to all containers automatically", "priority 10 — starts before app containers so migrations don't hit an absent database", "buildFromGit clones this repo and runs the matching zeropsSetup's build pipeline". Include whenever a field's effect isn't obvious from its name alone.
3. **Name the outcome, not the field** — the reader sees `base: php@8.4`; what they can't see is that project envVariables propagate to child services. Put that on the page.

Dev-to-dev tone — a senior dev explaining their config to a colleague, not documentation, not a tutorial. Direct, concise, no filler. Use dashes for asides (not parentheses, not semicolons). Reference framework commands where they add precision — the framework's dev start command, production dependency install flag, cache-warming CLI.

## Shape

- 2-3 sentences per service (aim for the upper end; single-sentence comments routinely miss the depth ratio).
- 1-2 lines per comment block, ~50-60 chars wide (natural prose, not compressed). Peak line at ~70 chars.
- Above the key, not inline (exception: short value annotations like `DB_NAME: db  # matches PostgreSQL hostname`).
- Multi-line comments for decisions: explain the choice and its consequence in flowing sentences. Group a 2-3 line comment block before a logical section, then let the config breathe.

## Visual style — ASCII `#` only

Comments are ASCII `#` prefixed, one line, natural prose. Section transitions use a single blank-comment line (`#`) followed by the first comment of the next section. That is the full vocabulary — no dividers, no banners, no decoration. Decoration renders inconsistently across zerops.io markdown + code-block rendering, and no downstream consumer (ingestor, publish pipeline, documentation tool) benefits from it. Plain text is the whole convention.

## Canonical example — full envComments input

```
zerops_workflow action="generate-finalize" \
  envComments={
    "0": {
      "service": {
        "appdev": "Development workspace for AI agents. zeropsSetup:dev deploys the full source tree so the agent can SSH in and edit over SSHFS. Subdomain gives the agent a URL to verify output.",
        "appstage": "Staging slot — agent deploys here with zerops_deploy setup=prod to validate the production build before finishing the task.",
        "db": "{dbDisplayName} — carries schema and app data. Shared by appdev and appstage. NON_HA fine for dev/staging; priority 10 so db starts before the app containers."
      },
      "project": "{appSecretKey} is the framework's encryption/signing key. Project-level so sessions remain valid when the L7 balancer routes a request to any app container."
    },
    "1": { "service": {...}, "project": "..." },
    "2": { "service": {...}, "project": "..." },
    "3": { "service": {...}, "project": "..." },
    "4": {
      "service": {
        "app": "Small production — minContainers: 2 guarantees at least two app containers at all times, spreading load and keeping traffic flowing during rolling deploys and container replacement. Zerops autoscales RAM within verticalAutoscaling bounds.",
        "db": "{dbDisplayName} single-node."
      },
      "project": "{appSecretKey} shared across containers — critical in production because sessions break if containers disagree on the key."
    },
    "5": {
      "service": {
        "app": "HA production. cpuMode: DEDICATED pins cores to this service so shared-tenant noise doesn't pollute request latency under sustained load. minContainers: 2 + autoscaling handles capacity; minFreeRamGB leaves 50% headroom for traffic spikes.",
        "db": "{dbDisplayName} HA — replicates data across multiple nodes so a single node failure causes no data loss or downtime. Dedicated CPU ensures DB ops don't compete with co-located workloads."
      },
      "project": "{appSecretKey} shared across every app container — required for session validity behind the L7 balancer at HA scale. corePackage: SERIOUS moves the project balancer, logging, and metrics onto dedicated infrastructure (no shared-tenant overhead) — essential for consistent latency at production throughput."
    }
  }
```

**Placeholders**: `{appSecretKey}` = the framework's secret key env var name from the plan's research data (`APP_KEY`, `SECRET_KEY_BASE`, `SECRET_KEY`, ...). `{dbDisplayName}` = the database display name (PostgreSQL, MariaDB, ...). Replace with the recipe's actual values.

## Preprocessor directive

When the finalize template emits `<@generateRandomString(<32>)>` or any other `<@` function, the env import.yaml's first non-empty line is `#zeropsPreprocessor=on`. Without the directive, the Zerops import API stores the literal angle-bracket string as the env var value. The directive is required whenever `<@` appears in the file, independently of the plan's `needsAppSecret` flag.

## Template-owned secret rationale

When `needsAppSecret == true`, the template auto-emits a multi-line rationale above the secret declaration (why it lives at project level: multi-container L7 balancer + signed-token verification must hold across deploy rolls). The `envComments[i].project` entry covers the ENV-SPECIFIC context (AI-agent workspace, local-dev hybrid, small-prod scale, HA-prod scale) and any additional project-level vars declared for this env; the template owns the secret rationale.
