# CI/CD on Zerops

## Keywords
ci cd, github, gitlab, github actions, gitlab ci, webhook, automatic deploy, trigger, pipeline, continuous deployment

## TL;DR
Zerops supports GitHub/GitLab webhook triggers (new tag or push to branch) and GitHub Actions / GitLab CI via `zcli push` with an access token.

## GitHub Integration (Webhook)

### Setup (GUI)
1. Service detail → Build, Deploy, Run Pipeline Settings
2. Connect with GitHub repository
3. Select repo + authorize (requires **full access** for webhooks)
4. Choose trigger: **New tag** (optional regex filter) or **Push to branch**

### GitHub Actions
```yaml
# .github/workflows/deploy.yaml
name: Deploy
on: push
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: zeropsio/actions@main
        with:
          access-token: ${{ secrets.ZEROPS_TOKEN }}
          service-id: <service-id>
```

- `access-token`: From Settings → Access Token Management
- `service-id`: From service URL or three-dot menu → Copy Service ID

## GitLab Integration (Webhook)

### Setup (GUI)
1. Service detail → Build, Deploy, Run Pipeline Settings
2. Connect with GitLab repository
3. Authorize (requires **full access** for webhooks)
4. Choose trigger: **New tag** (optional regex) or **Push to branch**

## Skip Pipeline
Include `ci skip` or `skip ci` in commit message (case-insensitive).

## Disconnect
Service detail → Build, Deploy, Run → Stop automatic build trigger.

## Gotchas
1. **Full repo access required**: Webhook integration needs full access to create/manage webhooks
2. **`ci skip` in commit message**: Prevents pipeline trigger — useful for docs-only changes
3. **Service ID not obvious**: Find it in service URL or three-dot menu → Copy Service ID

## See Also
- zerops://config/zerops-yml
- zerops://config/zcli
- zerops://platform/infrastructure
