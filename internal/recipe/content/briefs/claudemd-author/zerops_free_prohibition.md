# Hard prohibition — NO Zerops content in CLAUDE.md

This is the porter's `/init`-style codebase guide. Do NOT include:

- Zerops platform content
- Managed-service hostnames (e.g. `db`, `cache`, `search`)
- Env-var aliases (`${db_hostname}`, `${apidev_zeropsSubdomain}`)
- Dev-loop tooling (`zsc`, `zerops_*`, `zcp`, `zcli`)
- Zerops dev-vs-stage container model
- init-commands semantics
- Anything from `zerops.yaml`

A sibling `codebase-content` sub-agent authors all Zerops integration
content (IG/KB/zerops.yaml comments) for this codebase in parallel —
that's not your surface.

If a fact is Zerops-platform-specific, it does NOT belong in CLAUDE.md.

Do NOT read `zerops.yaml` or any IG/KB/README content as voice anchors —
those carry Zerops content by design.
