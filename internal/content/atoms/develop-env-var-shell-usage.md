---
id: develop-env-var-shell-usage
priority: 3
phases: [develop-active]
environments: [container]
envelopeDeployStates: [deployed]
title: "Use env vars in shell commands — reference, don't paste"
references-atoms: [develop-env-var-model]
---

### Reference by name in container-side commands

When SSHing into a container to run a command that needs a secret
(psql, prisma, redis-cli, curl auth header), refer to the env var by
name in a **single-quoted** command body. Bash inside the runtime container
expands it at exec time from its already-injected OS env — the value
never enters your context.

```bash
# WRONG — value pasted from earlier discover output into the command
ssh apidev 'npx prisma migrate --url postgresql://postgres:U_UjIq5TC...@db:5432/db'

# RIGHT — single-quoted, ${db_*} expanded at exec time inside apidev
ssh apidev 'npx prisma migrate --url postgresql://${db_superUser}:${db_superUserPassword}@${db_hostname}:${db_port}/${db_dbName}'
```

Same for `curl` auth headers (`Authorization: Bearer ${api_token}`),
`redis-cli`, `aws s3`, anything that takes a secret on the command line.

**Read vs use.** Inspecting values for diagnosis is fine — mask in
output so secrets don't enter your context:

```bash
ssh apidev 'env | grep -E "^(DB_|APP_)" | sed "s/=.*/=<set>/"'
```

If you DO pull values into context (export classification, debugging
an unresolved ref), the next command should still reference by
`${name}`, not the value you just saw.
