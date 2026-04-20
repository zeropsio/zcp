---
id: cicd-10-verification
priority: 2
phases: [cicd-active, export-active]
title: "CI/CD — Verification and error recovery"
---

## Verification

After any CI/CD setup:

```
zerops_events serviceHostname="{targetHostname}" limit=5
```

**Check for build triggered** — look for `stack.build` process in RUNNING or FINISHED state.

**If no build triggers:**
- Check commit message doesn't contain `[ci skip]` or `[skip ci]`
- GitHub Actions: verify workflow file is on the correct branch, check GitHub Actions tab
- Webhook: verify connection in Zerops dashboard → Deploy tab → check integration status
- Verify access token is valid (not expired)
- Verify the push was to the monitored branch
- Verify GitHub Actions permissions: repo Settings → Actions → General → Workflow permissions must be "Read and write"

**If build fails:**
```
zerops_logs serviceHostname="{targetHostname}" severity=error
```
Deploy creates a NEW container — local files from dev are NOT carried over. Only `deployFiles` content survives.

**If build succeeds:**
```
zerops_verify serviceHostname="{targetHostname}"
```

Present to user: stage URL, repo URL, explain: "Push to {branch} → automatic deploy to {targetHostname}."

---

## Error Recovery

| Problem | Solution |
|---------|---------|
| Push rejected (auth) | Recreate .netrc, verify GIT_TOKEN env var |
| Push rejected (non-fast-forward) | `git pull --rebase` then push again |
| zcli install fails in CI | Check network access, verify `curl` is available on runner |
| `zcli: command not found` | Ensure install step has `echo "$HOME/.local/bin" >> $GITHUB_PATH` |
| `Cannot find corresponding setup` | Add `--setup {name}` to the zcli push command (e.g. `--setup prod`) |
| `ZEROPS_TOKEN` not available | Verify secret name matches exactly, check workflow permissions are Read and write |
| Webhook not triggering | Check Zerops dashboard → Deploy → integration status, re-authorize if needed |
| Actions not triggering | Verify `.github/workflows/` is on the correct branch, check Actions tab for errors |
| Build timeout | Builds have 60-minute hard limit. Check build logs for slow steps. |
| Wrong branch deploying | Verify trigger config: branch name must match exactly |
| `[ci skip]` in commit message | Remove `[ci skip]` / `[skip ci]` from commit message, push again |
