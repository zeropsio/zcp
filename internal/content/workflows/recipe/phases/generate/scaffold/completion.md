# Scaffold substep — completion predicate

The scaffold substep counts as complete when every one of these predicates holds on every mount in `plan.Research.Targets`:

- The framework's project tree is present at `/var/www/{hostname}/` (zcp side) and at `/var/www/` (container side for that `{hostname}`).
- `.gitignore` exists and covers build artifacts, dependencies, and env files appropriate to the framework (for Node: `node_modules`, `dist`, `.env`, `.DS_Store`; for PHP: `vendor`, `.env`; for Python: `__pycache__`, `.venv`, `.env`; framework-specific cache dirs when applicable).
- `.env.example` is preserved from the scaffolder (if the framework ships one) with empty values; any generated `.env` has been removed; `.env.example` covers every env var the recipe will reference.
- `git init` has run inside the container for this codebase and an initial commit has been recorded.
- The pre-ship assertion chain for this codebase has exited zero — the `SymbolContract` fix-recurrence rules applicable to this hostname role (NATS separate credentials, S3 `forcePathStyle`, routable bind, trust-proxy, graceful-shutdown, queue-group, env self-shadow absence, scaffold artifact absence) have each been verified by the scaffold sub-agent, or inline for single-codebase recipes that skipped the dispatch.

Attest only when all predicates above hold for every codebase. `zerops_workflow action=status` mirrors this state.
