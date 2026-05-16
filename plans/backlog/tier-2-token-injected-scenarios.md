**Surfaced**: 2026-05-16 — Karel's matrix design `plans/real-life-test-matrix-2026-05-16.md` lists Tier 2 + Tier 3 scenarios that need real-token injection:

  - #7 git-push-setup-then-actions (needs writable GitHub PAT)
  - #9 launch-production-with-existing-cicd (needs LaunchKey + GitHub PAT)
  - #10 launch-to-existing-prod-project (needs project-scoped token)
  - #11 launch-failure-build-stuck (needs LaunchKey)

Current behavioral framework (`eval/behavioral/flow-eval.sh` + `eval/behavioral/scenarios/*.md`) doesn't pass ENV vars from the operator's shell through to the headless agent on the container. Persona is a plain string; embedding a real token there leaks it into transcript + retrospective files.

**Why deferred**: Tier 1 scenarios (#1 kanban-laravel, #3 api-node-postgres, #4 landing-page-static) pass without token injection — they're greenfield bootstrap on the source eval-zcp project, no second-account access needed. Tier 2 chains are higher-fidelity but require framework plumbing first.

**Trigger to promote**: enough Tier 1 coverage is locked (3-5 scenarios green); push v9.92 unblocked; ready to harden the CI/CD + production-launch path with full real-token tests.

---

## What needs to change

### flow-eval.sh — propagate operator's env to container

```bash
# Current ssh invocation (line ~75):
ssh "${SSH_OPTS[@]}" "$REMOTE_HOST" \
  "zcp eval behavioral run --scenarios-dir '$SCENARIOS_DIR_REMOTE' --id '$cmd'"

# Change to:
ssh "${SSH_OPTS[@]}" -o SendEnv="ZCP_E2E_*" "$REMOTE_HOST" \
  "zcp eval behavioral run --scenarios-dir '$SCENARIOS_DIR_REMOTE' --id '$cmd'"

# Plus AcceptEnv ZCP_E2E_* in ~/.ssh/sshd_config on the container, OR
# wrap in an env-file approach:
#   echo "ZCP_E2E_LAUNCH_KEY=$ZCP_E2E_LAUNCH_KEY" > /tmp/e2e-env
#   scp /tmp/e2e-env zcp:/tmp/e2e-env
#   ssh zcp ". /tmp/e2e-env && zcp eval behavioral run ..."
#   ssh zcp "rm -f /tmp/e2e-env"
```

Operator runs:

```
ZCP_E2E_LAUNCH_KEY=... ZCP_E2E_GITHUB_PAT=... \
  ./eval/behavioral/flow-eval.sh launch-with-existing-cicd
```

### Persona convention — reference, don't embed

Persona MUST NEVER carry a literal token. Instead point at the env var:

```yaml
userPersona: |
  Jsi developer co má funkční dev/stage pair v Zerops. Chceš teď
  vytvořit produkční projekt s push-mode CI/CD přes GitHub Actions.

  Máš tyto credentials připravené:
   - Zerops LaunchKey (account-wide, one-shot) v env var
     $ZCP_E2E_LAUNCH_KEY — pass it na agent přes `Bash echo $ZCP_E2E_LAUNCH_KEY`
     když agent požádá o launchKey.
   - GitHub fine-grained PAT (Contents+Secrets+Workflows na <repo>)
     v env var $ZCP_E2E_GITHUB_PAT — same fetch pattern.

  Tvoje preference: ...
```

Agent reads via Bash, plugs into ZCP. Tokens never appear in persona string, transcript stays clean (the Bash tool result has the value briefly but not the persona itself).

### Scenario YAML schema extension

Add optional frontmatter field telling the framework which env vars are required:

```yaml
requiredEnvVars:
  - ZCP_E2E_LAUNCH_KEY
  - ZCP_E2E_GITHUB_PAT
```

If unset, scenario fails fast at framework level (not deep in agent loop) with a clear "missing token" error.

### Sentinel scanning post-retro

Even with the indirection, defensive: scan retrospective + transcript for token-shaped strings (long base64-ish runs) before saving. If found, warn operator + redact.

```bash
# In flow-eval.sh after pulling artifacts:
for f in "$RUNS_DIR_LOCAL/$SUITE_ID"/*/{transcript.jsonl,self-review.md}; do
  if grep -E 'ghp_[A-Za-z0-9]{20,}|YJQTh\.[a-zA-Z0-9._-]{20,}' "$f" > /dev/null; then
    echo "WARN: token-shaped string found in $f — review before committing"
  fi
done
```

## Tier 2 scenario sketches

### #6 — develop-loop-after-bootstrap (no tokens needed)

Chained scenario, two phases:
1. Bootstrap a standard pair (e.g. Node+postgres)
2. User edits code, asks agent to redeploy

Just bash-chain — first agent run does bootstrap to completion, then SECOND prompt issued for develop. No token needed.

### #7 — git-push-setup-then-actions

Pre-state: bootstrapped standard pair (chain from #6).
User prompt: "nastav mi git push na <repo> a Actions na ten dev service, ať se to buildí samo".
Agent must:
- Walk user through GitHub PAT requirements (fine-grained Contents+Secrets+Workflows)
- Persona reveals PAT via Bash from `$ZCP_E2E_GITHUB_PAT`
- Run `git-push-setup` with remoteUrl
- Run `build-integration=actions` confirm
- Verify ServiceMeta updates + workflow YAML returned matches Phase 1c composer

### #9 — launch-with-existing-cicd (Karel's "už předtím dělal CICD setup")

Pre-state: bootstrapped pair + CICD-actions wired (chain from #7).
User prompt: "rovnou ho udělej do produkce, používám stejný GitHub repo + chci stejný Actions setup pro prod".
Agent must:
- Recognize launch-production path
- Pull LaunchKey from `$ZCP_E2E_LAUNCH_KEY` via Bash
- Compose launch yaml (Phase 2b shape)
- Mutate via real platform (CreateAndImportProject)
- Emit cicd handoff atoms for prod-side Actions setup (Phase 6b atoms needed; may surface gap)
- Verify post-launch project + cleanup

### #10 — launch-to-existing-prod-project (Phase 2c path)

Pre-state: target prod project pre-provisioned (manual step before run, OR setup hook).
User prompt: "mám existující prod projekt, deploynu tam current dev/stage. Token mám připravený."
Agent reveals `$ZCP_E2E_EXISTING_PROD_TOKEN` + `$ZCP_E2E_EXISTING_PROJECT_ID` via Bash.
Tests Phase 2c handler path end-to-end.

### #11 — launch-failure-build-stuck (failure-recovery)

Source repo: deliberately broken (e.g. composer.json with non-existent dep).
Pre-state: bootstrapped pair, ready to launch.
Agent calls launch-production with `$ZCP_E2E_LAUNCH_KEY`, mutation succeeds, build fails ~30s later.
Agent must:
- Detect failure via status / process polling
- Pull build logs (`zerops_logs source=build`)
- Diagnose root cause
- Surface to user with structured recovery hint
- Suggest fix (composer.json edit) + retry

Tests Karel's (c) recovery flow.

## Implementation sequence

1. **Add env propagation** to flow-eval.sh (~20 LOC + sshd_config note in container README)
2. **Add requiredEnvVars** to scenario YAML schema + framework fail-fast (~30 LOC)
3. **Add sentinel scan** post-pull (~10 LOC, optional warn-only)
4. **Write #6** (no tokens, simplest chain)
5. **Write #7** (single token, chains from #6 state OR self-bootstrap)
6. **Write #9** (Karel's primary ask)
7. **Write #10** (Phase 2c live)
8. **Write #11** (failure-recovery, exercises agent diagnostic skills)

Tier 2 completion = scenarios 1-11 all green; behavioral matrix has clear pass/fail and reproducible artifacts.
