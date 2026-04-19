---
id: export-06-zerops-yaml
priority: 2
phases: [export-active]
title: "Export — zerops.yaml verification and generation"
---

## Prepare — zerops.yaml Verification

Check if zerops.yaml exists and has the correct setup:

```bash
ssh {devHostname} "cat /var/www/zerops.yml 2>/dev/null || cat /var/www/zerops.yaml 2>/dev/null || echo 'NOT_FOUND'"
```

If zerops.yaml is missing or incomplete:
1. Detect framework from service type (e.g., `nodejs@22` → Node.js)
2. Load matching recipe: `zerops_knowledge recipe="{runtime}-hello-world"`
3. Generate zerops.yaml from recipe template + discovered ports and env vars
4. Write to container and commit:
   ```bash
   ssh {devHostname} "cat > /var/www/zerops.yaml << 'ZEROPS_EOF'
   {generated zerops.yaml content}
   ZEROPS_EOF"
   ssh {devHostname} "cd /var/www && git add zerops.yaml && git commit -m 'add zerops.yaml' && git push"
   ```
5. Mark generated sections with comments: `# VERIFY: default from <recipe> template`
