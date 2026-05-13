---
id: launch-production/active
atomIds: [launch-delete-key, launch-intro, launch-pipeline-configure-dashboard, launch-post-checklist, launch-scope-prompt, launch-classify-platform-envs, launch-mutation-key-required, launch-pipeline-configuring, launch-pipeline-configured, launch-pipeline-skipped, launch-status-recovery, launch-write-prod-setup]
description: "Launch-production workflow mid-flow on a source project — bundle composed, awaiting one-shot launch key for the mutation pipeline."
---
### Delete the launch-window API key

The production project is live. **Delete the launch-window key now** so ZCP has no further path to mutate prod:

1. Open [Settings → Access Tokens Management](https://app.zerops.io/settings/token-management).
2. Find the token named `zcp-launch-<production-project-name>`.
3. Click **Revoke** (or **Delete**).

ZCP has already discarded the in-memory copy. Revoking the key in Zerops dashboard closes the trust boundary completely.

---

### Launch production — overview

You are launching the source project to a separate Zerops production project. ZCP prepares the bundle, source-control changes, and verification steps; you (the user) generate a one-shot Zerops API key for the mutation window and **delete that key** after launch completes.

Six top-level statuses gate progress:

| Status | Means |
|---|---|
| `scope-prompt` | ZCP needs: production project name, region, optional custom domain, scaling overrides. |
| `classify-prompt` | Project envs need bucketing (infrastructure / auto-secret / external-secret / plain-config). |
| `ready-to-launch` | Bundle composed, source-control changes pushed, schema clean, blockers cleared. Awaiting one-shot launch key. |
| `launching` | One-shot key in use; ZCP is creating + importing + polling first deploy. |
| `failed` | A mutation step failed; `blockers[]` describes recovery. |
| `launched` | Done. Delete the launch key. Set external secrets in Zerops UI. Attach custom domain in Zerops UI per emitted DNS records. |

ZCP has **zero standing access** to the production project. The one-shot key flows in via the `launchKey` parameter only during `publish` action; ZCP never writes it to state, logs, or audit trail. The MCP tool-call transcript itself records the parameter (that surface is your client's, not ZCP's) — generate the key right before `publish`, then revoke it in the Zerops dashboard the moment `launched` status returns.

---

### Configure CD pipeline in Zerops dashboard

The production runtime has no CD pipeline yet — ongoing pushes will NOT auto-build. Configure it once via dashboard. (ZCP cannot do this through the launch-window key; see `plans/backlog/launch-pipeline-close-loop-oauth.md` for the Path A future.)

For each runtime listed in the `pipeline-not-configured-*` blockers:

1. Open the **deep-link** from the blocker (`https://app.zerops.io/dashboard/project/<projectID>/service-stack/<svcID>/service-stack-source-code`).
2. Click **Connect to GitHub** (or GitLab). Authorize Zerops if asked — uses your existing org-level grant, no extra setup.
3. Select the source repository listed in the blocker's `recommendation.repositoryFullName`.
4. Set the trigger:
   - **Event type:** `Tag`
   - **Tag regex:** the value from `recommendation.tagRegex` (default `^v\d+\.\d+\.\d+$` per Zerops production-checklist).
   - **Zerops YAML setup:** `prod` (matches the setup block written during launch).
5. Save.

Repeat for each runtime in the blockers list. When done, re-call `workflow="launch-production"` with the same `launchKey` — ZCP reads the live integration status and clears the blockers from the response.

To deploy after setup: `git tag v1.0.0 && git push --tags` (matching your tag regex).

---

### Launch complete — user-owned steps remaining

ZCP has imported services and validated first deploy. The following steps require the user to act in the Zerops dashboard. ZCP cannot perform them (no standing prod access).

1. **Delete the launch-window key** — open Settings → Access Tokens Management and revoke the token named `zcp-launch-<production-project-name>`.
2. **Set external secrets** — open the production project, navigate to each service that needs Stripe/OpenAI/SMTP/etc. values, and set them under Env Variables → Secret. ZCP listed the keys needed in the prior response.
3. **Attach custom domain** (if requested at scope time) — Project → Public Access → HTTP Routing → Add Domain. Use the DNS records ZCP emitted; add them at the registrar; click Verify in dashboard.
4. **Verify production smoke test** — hit the live URL with a known request shape; check response and logs in dashboard.

After step 4 passes, the launch is complete. For ongoing prod iteration: generate a separate project-scoped `ZCP_API_KEY` (Custom access per project, this one project, Full access) and configure a fresh ZCP MCP session against the production project.

5. **Pipeline trigger (if launched response had no `pipeline-not-configured-*` blockers)** — push a release tag to deploy: `git tag v1.0.0 && git push --tags` (matching the integration's tag regex, default `^v\d+\.\d+\.\d+$`). If the launched response carried such blockers, configure each runtime via Zerops dashboard first using the deep-link the blocker provides.

---

### Launch scope — collect production target details

**This workflow is stateless multi-call narrowing.** Every response's `inputs` block is the running accumulator: pass all previously-accepted parameters forward on every next `action="start"` call. `action="complete"` is reserved for bootstrap and returns `BOOTSTRAP_NOT_ACTIVE` here.

Apply suggestions from `sourceContext`:

- **`productionProjectName`** — `sourceContext.suggestedTargetName` (`<source>-dev` / `<source>-stage` → `<source>-prod`, else `<source>-prod` appended). Confirm name with user; don't silently rename.
- **`targetService`** — `sourceContext.suggestedRuntime` when single. The new prod project rebuilds fresh from git, so promotion is the dev/stage pair *as a unit*; for standard-mode pairs the headline is the stage hostname (validated last-known-good). Either half is accepted — the handler normalizes to the canonical key. Managed deps are bundled implicitly.
- **`region`** — optional, default `eu-central`.
- **`customDomain`** — optional; ZCP emits DNS records + verification probes, user attaches in Zerops UI.
- **`keepNonHA`** — optional `[]hostname` to keep at `NON_HA` (default: all managed deps go `HA`).
- **`envOverrides`** — optional plain-config overrides. No secret values; ZCP never receives them.

When `sourceContext.availableRuntimes` has multiple entries, the user must pick. Use `AskUserQuestion` if your harness exposes it (structured choice UI); else surface the choice inline and wait for the user's next turn.

After scope is complete, ZCP advances to `classify-prompt` for env classification.

---

### Launch classify — platform envs auto-handled

The `classifications` rows in the `classify-prompt` response carry only envs that need your judgment. Several known platform-injected envs are handled by ZCP without asking — you will not see them in the row table.

Auto-handled (by exact key):

| Key | Auto-action |
|---|---|
| `zeropsSubdomainHost` | classified as `infrastructure` — the new prod project re-emits its own subdomain pair. |
| `zeropsSubdomainString` | classified as `infrastructure` — same. |
| `envIsolation` | dropped — project-level setting; new project picks its own. |
| `sshIsolation` | dropped — project-level setting; carrying forward would reference the source project's containers. |
| `ZCP_API_KEY`, `ZCP_AGENT_TYPE`, `ZCP_BASE_HOST`, `ZCP_BUILTINS_DIR`, `ZCP_PROJECT_DIR` | dropped — ZCP control-plane envs only meaningful in the dev-side ZCP container. |

If a key is in the list above, you do not need to classify it; the bundle composer routes it (or excludes it) deterministically. Membership is closed and matches by **exact key only** — keys merely starting with `ZCP_` (e.g. `ZCP_CUSTOM_USER_THING`) fall through to your classification as normal.

---

### One-shot API key required for publish

ZCP cannot create the production project with its standing token (project-scoped). Walk the user through generating a temporary **account-wide** Zerops API key for the launch window — and wait for them to paste the value back before calling the workflow again:

1. Open [Settings → Access Tokens Management](https://app.zerops.io/settings/token-management).
2. Click **Create token**. Name it `zcp-launch-<production-project-name>`.
3. Leave **Custom access per project** UNCHECKED — needs account-wide scope to create projects.
4. Copy the token value (shown once).
5. Paste the value back into the conversation.

When the value lands, re-call the launch workflow with the publish action and the token value passed as `launchKey`. Do NOT invent or guess a value, and do NOT proceed without it — the key is the gate.

The key flows through the workflow handler only — never persisted to state, logs, or transcripts. Once the launch reaches `launched` status, ZCP returns a mandatory checklist that includes **deleting the key** at the same dashboard URL.

---

### Checking pipeline integration for ongoing CD

ZCP is verifying whether each runtime service in the new production project has a CD pipeline integration configured. This reads-only — ZCP never mutates pipeline config (Path B trust model, see `docs/spec-launch-production-platform-spike.md §B.3`).

Possible outcomes:

- **Configured** → ongoing builds will fire on the integration's trigger (tag-push for prod-recommended setup).
- **Not configured** → response will carry a `pipeline-not-configured-<hostname>` blocker with a Zerops dashboard deep-link and the recommended config payload (`repositoryFullName`, `eventType=TAG`, `tagRegex`, `zeropsYamlSetup=prod`). User configures via dashboard, then re-calls `workflow="launch-production"` with the same `launchKey` to recheck.

---

### Pipeline integration confirmed

ZCP read each runtime's `external-repository-integration-status` and saw all configured. Ongoing CD is wired:

- Tag pushes matching the configured regex trigger Zerops builds automatically.
- Push to deploy: `git tag v1.0.0 && git push --tags` (substituting your version).

State file (`.zcp/state/launch-production/<launchID>.json`) records the live config under `pipelineConfigurations` for audit.

---

### Pipeline configuration skipped

`skipPipelineSetup=true` told ZCP not to check or recommend pipeline integration. The production project is live; the first deploy ran from source HEAD via `buildFromGit`.

Without an integration, subsequent code changes do NOT auto-build. Options:

- **Manual `zcli push`** from local or CI per release.
- **Add integration later** in Zerops dashboard (`Project → Service → Source code → Connect to GitHub/GitLab`). Set the event type to `Tag`, the tag regex to `^v\d+\.\d+\.\d+$` (or your release-version convention), and the Zerops YAML setup to `prod`.

Re-run `workflow="launch-production"` with the same `launchKey` if you want ZCP to verify integration setup; that lifts the skip and runs the configuring-pipeline check.

---

### Launch status — mid-flight recovery

When `action="status"` returns `kind: "launch-active"`, a launch-production workflow is mid-flight for this source project. Conversation context was likely lost (compaction, restart). The envelope carries enough state to resume:

| Field | Use |
|---|---|
| `targetProjectName` | Pass back as `productionProjectName` on the resume call. |
| `status` | Tells you which phase to expect on the next response (e.g. `ready-to-launch` means you still need `launchKey`; `launching` / `configuring-pipeline` means polling). |
| `lastUpdate` | Sanity-check that this is the launch you remember — if minutes old, it's the active one; if days old, the user may have abandoned it (ask before resuming). |
| `ambiguousChoices` | When present, multiple non-terminal launches exist for this source. Pick a `productionProjectName` from the list before the resume call. |

Resume call shape:

```
zerops_workflow workflow="launch-production" productionProjectName="<from envelope>"
```

The `launchKey` is NOT required at the status step — only generate and pass it when the workflow re-enters `ready-to-launch` and you intend to advance to `launching`. Status is read-only; ZCP never constructs a project-admin client on this path.

---

### Write prod setup block to source zerops.yaml

Launch needs `setup: prod` in the source repo's `zerops.yaml` **before** publishing. Production builds from the same git URL as dev/stage; the prod-specific build/run commands live under a separate `setup:` entry that the launch bundle references.

Append the block to `zerops.yaml` at repo root, commit, and push to the configured remote. The launch workflow verifies the block exists before mutating the destination project.

```yaml
zerops:
  - setup: prod
    build:
      base: <runtime>
      buildCommands:
        - <production build commands — typically same as stage with NODE_ENV=production or APP_ENV=production semantics>
      deployFiles: <production deploy artifact paths>
    run:
      base: <runtime>
      start: <production start command>
      healthCheck:
        httpGet:
          port: <port>
          path: /health
```

`healthCheck` is required — production deploys gate readiness via the `prod-healthcheck-required` blocker if missing.

After commit + push, re-call `zerops_workflow workflow="launch-production" action="status"` to re-probe; the workflow advances to `ready-to-launch` once the block resolves.
