# RBAC on Zerops

## Keywords
rbac, roles, permissions, access control, owner, admin, developer, guest, token, integration token, read only

## TL;DR
Zerops has 4 user roles (Owner > Admin > Developer > Guest) and per-project access levels (Full or Read Only), with integration tokens for API/CI access.

## User Roles (Highest to Lowest)

| Role | Org Access | Billing | Manage Roles | Create Projects |
|------|-----------|---------|--------------|----------------|
| **Owner** | Full | Yes | All roles | Yes |
| **Admin** | Full | No | Developer & Guest | Yes |
| **Developer** | No | No | No | Yes |
| **Guest** | No | No | No | No |

At least one Owner is always required.

## Project Access Levels

| Level | Deploy | SSH | File Browser | Delete | Env Vars |
|-------|--------|-----|-------------|--------|----------|
| **Full** | Yes | Yes | Yes | Yes | Visible |
| **Read Only** | No | No | No | No | REDACTED |

## Integration Tokens
- **Full access all projects** — unrestricted
- **Read access all projects** — view only
- **Custom per-project** — granular control
- Token permissions cannot exceed creator's permissions
- Developers/Guests can only create custom per-project tokens

## API-Only Roles
- `BASIC_USER`: Can perform operations, cannot delete projects
- `READ_ONLY`: View-only, secrets shown as REDACTED

## API Permission Flags
- `canViewFinances` — view billing
- `canEditFinances` — modify billing (auto-enables canViewFinances)
- `canCreateProjects` — create projects (gets OWNER role on created projects)

## Gotchas
1. **Token inherits creator's permissions**: A Developer's token can never have Admin privileges
2. **Read Only shows REDACTED env vars**: Cannot read secrets through GUI or API with read-only access

## See Also
- zerops://platform/infrastructure
- zerops://config/zcli
