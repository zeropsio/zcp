# tool-use-policy

This is the base permit list every role inherits. Role-specific narrowing (for example, an editorial reader that performs no container-side execution) is declared in the brief that pointer-includes this atom.

## Permitted tools

- **Read** — read files from the mount. Use for any file whose content you need to inspect before acting.
- **Write** — create new files on the mount. Use when the target path does not yet exist.
- **Edit** — modify existing files on the mount. Requires a prior Read of the same path in the current session; see principles/file-op-sequencing.md.
- **Grep** — content search across the mount. Prefer Grep over Bash-invoked `rg` or `grep`; Grep is permissioned directly.
- **Glob** — filename pattern search across the mount. Prefer Glob over Bash-invoked `find`.
- **Bash** — shell execution, wrapped for execution-side work as described in principles/where-commands-run.md. Every app-toolchain invocation takes the form `ssh {hostname} "cd /var/www && {command}"`. Plain-mount inspection (`ls`, `cat` against the mount) is orchestrator-side.

## Tool selection heuristic

- Need to look at file contents? Read.
- Need to search for a string across many files? Grep.
- Need to list files matching a pattern? Glob.
- Need to run app-toolchain or app-runtime commands? Bash with ssh-wrapped form.
- Need to start a long-running dev server? Use the dev-server MCP tool, not raw SSH — see principles/dev-server-contract.md.
- Need to modify a file? Read it, then Edit. For brand-new files, Write.

## MCP tools specific to the workflow

`zerops_*` tools are invoked by name through the MCP bridge, not through Bash. Examples: `zerops_workflow`, `zerops_import`, `zerops_deploy`, `zerops_discover`, `zerops_mount`, `zerops_browser`, `zerops_dev_server`, `zerops_record_fact`. These run orchestrator-side.

## Role-specific overrides

The brief that pointer-includes this atom may narrow the permit list for its role — for example, by removing Bash for a reader-only role, or by requiring that Write precede any Edit within the role's first authoring pass. Role-specific rules are authoritative over this base list where they narrow; they cannot expand beyond it.
