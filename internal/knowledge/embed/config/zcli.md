# zcli (Zerops CLI)

## Keywords
zcli, cli, command line, deploy, push, login, vpn, ssh, service log, zerops cli, installation

## TL;DR
`zcli` is the production Zerops CLI for deploy (`push`), logs, SSH, and VPN access; install via `npm i -g @zerops/zcli` or curl.

## Installation
```bash
# npm (recommended)
npm i -g @zerops/zcli

# curl (Linux/macOS)
curl -L https://zerops.io/zcli/install.sh | sh
```

## Authentication
```bash
zcli login <token>                          # Login with access token
zcli login <token> --region <region-name>   # Login to specific region
```
Token: Settings → Access Token Management in Zerops GUI.

## Key Commands

### Deploy
```bash
zcli push <service-name>                            # Deploy current directory
zcli push <service-name> --workingDir ./path        # Deploy specific directory
zcli push <service-name> --archiveFilePath ./app.zip  # Deploy archive
```

Flags:
- `--versionName <name>` — Name the deploy version
- `--zeropsYamlPath <path>` — Custom zerops.yaml path
- `--workspaceState clean|staged|all` — What to include (default: `all`)

### Logs
```bash
zcli service log <service-name>                    # Runtime logs
zcli service log <service-name> --showBuildLogs    # Build logs
```

### SSH
```bash
zcli ssh <service-name>                            # SSH into container
```

### VPN
```bash
zcli vpn up <project-id>                           # Connect VPN
zcli vpn down                                      # Disconnect VPN
```

### Service Management
```bash
zcli service start <service-name>
zcli service stop <service-name>
zcli service delete <service-name>
```

### Project Management
```bash
zcli project list
zcli project import <file.yaml>                    # Import project from YAML
```

## Async Process Management
Some commands return immediately with a process ID. Check status via GUI or API.

## Debug Logs
Location: `~/.config/zerops/zerops.log`

## Gotchas
1. **`--workspaceState all` is default**: Deploys everything including uncommitted files — use `staged` for git-only
2. **`push` requires `zerops.yaml`**: Must exist in working directory (or specify with `--zeropsYamlPath`)
3. **VPN connects to one project**: Cannot connect to multiple projects simultaneously
4. **Token scope matters**: Token must have access to the project/service you're operating on

## See Also
- zerops://operations/ci-cd
- zerops://networking/vpn
- zerops://config/zerops-yml
- zerops://platform/rbac
